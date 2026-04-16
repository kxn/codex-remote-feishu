package daemon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/externalaccess"
)

type fakePreviewRouteService struct {
	scope     string
	preview   string
	download  bool
	publisher feishu.WebPreviewPublisher
}

func (f *fakePreviewRouteService) RewriteFinalBlock(_ context.Context, req feishu.FinalBlockPreviewRequest) (feishu.FinalBlockPreviewResult, error) {
	return feishu.FinalBlockPreviewResult{Block: req.Block}, nil
}

func (f *fakePreviewRouteService) SetWebPreviewPublisher(publisher feishu.WebPreviewPublisher) {
	f.publisher = publisher
}

func (f *fakePreviewRouteService) ServeWebPreview(w http.ResponseWriter, _ *http.Request, scopePublicID, previewID string, download bool) bool {
	f.scope = scopePublicID
	f.preview = previewID
	f.download = download
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("preview ok"))
	return true
}

func TestIssuePreviewScopePrefixReusesGrantWithinTTL(t *testing.T) {
	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	app.ConfigureAdmin(AdminRuntimeOptions{
		AdminListenHost: "127.0.0.1",
		AdminListenPort: "9501",
		AdminURL:        "http://127.0.0.1:9501/admin/",
		SetupURL:        "http://127.0.0.1:9501/setup",
	})
	app.SetExternalAccess(ExternalAccessRuntimeConfig{
		Settings: externalAccessSettingsView{
			ListenHost:        "127.0.0.1",
			ListenPort:        0,
			DefaultLinkTTL:    10 * time.Minute,
			DefaultSessionTTL: 10 * time.Minute,
			ProviderKind:      "disabled",
		},
	})
	defer app.Shutdown(nil)

	first, err := app.issuePreviewScopePrefix(context.Background(), "scope-1")
	if err != nil {
		t.Fatalf("first issuePreviewScopePrefix: %v", err)
	}
	second, err := app.issuePreviewScopePrefix(context.Background(), "scope-1")
	if err != nil {
		t.Fatalf("second issuePreviewScopePrefix: %v", err)
	}
	if first != second {
		t.Fatalf("expected cached prefix grant, got %q vs %q", first, second)
	}
	if snapshot := app.externalAccess.Snapshot(); snapshot.GrantCount != 1 {
		t.Fatalf("expected one active grant, got %#v", snapshot)
	}
}

func TestIssuePreviewScopePrefixReissuesGrantAfterExternalAccessIdleShutdown(t *testing.T) {
	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	app.ConfigureAdmin(AdminRuntimeOptions{
		AdminListenHost: "127.0.0.1",
		AdminListenPort: "9501",
		AdminURL:        "http://127.0.0.1:9501/admin/",
		SetupURL:        "http://127.0.0.1:9501/setup",
	})
	base := time.Now().UTC()
	app.externalAccessRuntime = ExternalAccessRuntimeConfig{
		Settings: externalAccessSettingsView{
			ListenHost:        "127.0.0.1",
			ListenPort:        0,
			DefaultLinkTTL:    10 * time.Second,
			DefaultSessionTTL: 30 * time.Second,
			ProviderKind:      "disabled",
		},
	}
	app.externalAccess = externalaccess.NewService(externalaccess.Options{
		Now:               func() time.Time { return base },
		DefaultLinkTTL:    10 * time.Second,
		DefaultSessionTTL: 30 * time.Second,
		IdleTTL:           5 * time.Minute,
	})
	defer app.Shutdown(nil)

	first, err := app.issuePreviewScopePrefix(context.Background(), "scope-1")
	if err != nil {
		t.Fatalf("first issuePreviewScopePrefix: %v", err)
	}

	client := &http.Client{
		Transport: &http.Transport{Proxy: nil},
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Get(first)
	if err != nil {
		t.Fatalf("exchange request: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}

	app.onTick(context.Background(), base.Add(6*time.Minute))
	if app.externalAccessListener != nil {
		t.Fatal("expected idle timeout to stop external access listener")
	}

	second, err := app.issuePreviewScopePrefix(context.Background(), "scope-1")
	if err != nil {
		t.Fatalf("second issuePreviewScopePrefix: %v", err)
	}
	if second == first {
		t.Fatalf("expected reissued preview prefix after idle shutdown, got same url %q", second)
	}
	if snapshot := app.externalAccess.Snapshot(); snapshot.GrantCount != 1 {
		t.Fatalf("expected one active grant after reissue, got %#v", snapshot)
	}
}

func TestPreviewRouteDelegatesToFinalPreviewer(t *testing.T) {
	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	app.ConfigureAdmin(AdminRuntimeOptions{
		AdminListenHost: "127.0.0.1",
		AdminListenPort: "9501",
		AdminURL:        "http://127.0.0.1:9501/admin/",
		SetupURL:        "http://127.0.0.1:9501/setup",
	})
	previewer := &fakePreviewRouteService{}
	app.SetFinalBlockPreviewer(previewer)

	req := httptest.NewRequest(http.MethodGet, "/preview/s/scope-a/file-b", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()
	app.apiServer.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	if previewer.scope != "scope-a" || previewer.preview != "file-b" || previewer.download {
		t.Fatalf("unexpected preview delegation: %#v", previewer)
	}
	if previewer.publisher == nil {
		t.Fatal("expected preview publisher to be injected")
	}
}

func TestPreviewScopeRootDelegatesToFinalPreviewer(t *testing.T) {
	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	app.ConfigureAdmin(AdminRuntimeOptions{
		AdminListenHost: "127.0.0.1",
		AdminListenPort: "9501",
		AdminURL:        "http://127.0.0.1:9501/admin/",
		SetupURL:        "http://127.0.0.1:9501/setup",
	})
	previewer := &fakePreviewRouteService{}
	app.SetFinalBlockPreviewer(previewer)

	req := httptest.NewRequest(http.MethodGet, "/preview/s/scope-a/", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()
	app.apiServer.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	if previewer.scope != "scope-a" || previewer.preview != "" || previewer.download {
		t.Fatalf("unexpected preview root delegation: %#v", previewer)
	}
}

var _ feishu.FinalBlockPreviewService = (*fakePreviewRouteService)(nil)
var _ feishu.WebPreviewConfigurable = (*fakePreviewRouteService)(nil)
var _ feishu.WebPreviewRouteService = (*fakePreviewRouteService)(nil)
