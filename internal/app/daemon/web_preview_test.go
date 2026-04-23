package daemon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"sync/atomic"
	"testing"
	"time"

	previewpkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/preview"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/render"
	"github.com/kxn/codex-remote-feishu/internal/externalaccess"
)

type fakePreviewRouteService struct {
	scope     string
	preview   string
	download  bool
	publisher previewpkg.WebPreviewPublisher
}

func (f *fakePreviewRouteService) RewriteFinalBlock(_ context.Context, req previewpkg.FinalBlockPreviewRequest) (previewpkg.FinalBlockPreviewResult, error) {
	return previewpkg.FinalBlockPreviewResult{Block: req.Block}, nil
}

func (f *fakePreviewRouteService) SetWebPreviewPublisher(publisher previewpkg.WebPreviewPublisher) {
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

func TestIssuePreviewScopePrefixReusesGrantWithinSameMessageTTL(t *testing.T) {
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

	req := previewpkg.WebPreviewGrantRequest{ScopePublicID: "scope-1", GrantKey: "message-1"}
	first, err := app.issuePreviewScopePrefix(context.Background(), req)
	if err != nil {
		t.Fatalf("first issuePreviewScopePrefix: %v", err)
	}
	second, err := app.issuePreviewScopePrefix(context.Background(), req)
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

func TestIssuePreviewScopePrefixUsesPreviewGrantDefaultTTL(t *testing.T) {
	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	app.ConfigureAdmin(AdminRuntimeOptions{
		AdminListenHost: "127.0.0.1",
		AdminListenPort: "9501",
		AdminURL:        "http://127.0.0.1:9501/admin/",
		SetupURL:        "http://127.0.0.1:9501/setup",
	})
	now := time.Date(2026, 4, 23, 12, 0, 0, 0, time.UTC)
	app.externalAccessRuntime = ExternalAccessRuntimeConfig{
		Settings: externalAccessSettingsView{
			ListenHost:        "127.0.0.1",
			ListenPort:        0,
			DefaultLinkTTL:    10 * time.Minute,
			DefaultSessionTTL: 10 * time.Minute,
			ProviderKind:      "disabled",
		},
	}
	app.externalAccess = externalaccess.NewService(externalaccess.Options{
		Now:               func() time.Time { return now },
		DefaultLinkTTL:    10 * time.Minute,
		DefaultSessionTTL: 10 * time.Minute,
		IdleTTL:           5 * time.Minute,
	})
	defer app.Shutdown(nil)

	_, err := app.issuePreviewScopePrefix(context.Background(), previewpkg.WebPreviewGrantRequest{
		ScopePublicID: "scope-1",
		GrantKey:      "message-1",
	})
	if err != nil {
		t.Fatalf("issuePreviewScopePrefix: %v", err)
	}

	snapshot := app.externalAccess.Snapshot()
	if snapshot.GrantCount != 1 || len(snapshot.ActiveGrants) != 1 {
		t.Fatalf("expected one active preview grant, got %#v", snapshot)
	}
	grant := snapshot.ActiveGrants[0]
	if got := grant.ExpiresAt.Sub(grant.IssuedAt); got != defaultPreviewGrantTTL {
		t.Fatalf("grant ttl = %s, want %s", got, defaultPreviewGrantTTL)
	}
	if grant.SessionTTL != defaultPreviewGrantTTL {
		t.Fatalf("session ttl = %s, want %s", grant.SessionTTL, defaultPreviewGrantTTL)
	}
}

func TestPreviewGrantKeyFallsBackWhenOnlyThreadIDIsPresent(t *testing.T) {
	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})

	keyA := app.previewGrantKey("gw", "surface-1", render.Block{
		ThreadID: "thread-1",
		Text:     "same output",
	})
	keyB := app.previewGrantKey("gw", "surface-1", render.Block{
		ThreadID: "thread-1",
		Text:     "different output",
	})
	if keyA == keyB {
		t.Fatalf("expected text-based fallback key when only thread id is present, got same key %q", keyA)
	}
}

func TestPreviewGrantKeyUsesTurnIdentityWhenAvailable(t *testing.T) {
	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})

	keyA := app.previewGrantKey("gw", "surface-1", render.Block{
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "item-1",
		ID:       "block-1",
		Text:     "first",
	})
	keyB := app.previewGrantKey("gw", "surface-1", render.Block{
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "item-1",
		ID:       "block-1",
		Text:     "second",
	})
	if keyA != keyB {
		t.Fatalf("expected stable key from turn identity, got %q vs %q", keyA, keyB)
	}
}

func TestIssuePreviewScopePrefixUsesDifferentGrantForDifferentMessages(t *testing.T) {
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

	first, err := app.issuePreviewScopePrefix(context.Background(), previewpkg.WebPreviewGrantRequest{
		ScopePublicID: "scope-1",
		GrantKey:      "message-1",
	})
	if err != nil {
		t.Fatalf("first issuePreviewScopePrefix: %v", err)
	}
	second, err := app.issuePreviewScopePrefix(context.Background(), previewpkg.WebPreviewGrantRequest{
		ScopePublicID: "scope-1",
		GrantKey:      "message-2",
	})
	if err != nil {
		t.Fatalf("second issuePreviewScopePrefix: %v", err)
	}
	if first == second {
		t.Fatalf("expected different grants for different messages, got same url %q", first)
	}
	if snapshot := app.externalAccess.Snapshot(); snapshot.GrantCount != 2 {
		t.Fatalf("expected two active grants, got %#v", snapshot)
	}
}

func TestIssuePreviewScopePrefixKeepsLaterMessageAliveAfterEarlierGrantExpires(t *testing.T) {
	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	app.ConfigureAdmin(AdminRuntimeOptions{
		AdminListenHost: "127.0.0.1",
		AdminListenPort: "9501",
		AdminURL:        "http://127.0.0.1:9501/admin/",
		SetupURL:        "http://127.0.0.1:9501/setup",
	})
	now := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)
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
		Now:               func() time.Time { return now },
		DefaultLinkTTL:    10 * time.Second,
		DefaultSessionTTL: 30 * time.Second,
		IdleTTL:           5 * time.Minute,
	})
	defer app.Shutdown(nil)

	first, err := app.issuePreviewScopePrefix(context.Background(), previewpkg.WebPreviewGrantRequest{
		ScopePublicID: "scope-1",
		GrantKey:      "message-1",
	})
	if err != nil {
		t.Fatalf("first issuePreviewScopePrefix: %v", err)
	}
	now = now.Add(5 * time.Minute)
	second, err := app.issuePreviewScopePrefix(context.Background(), previewpkg.WebPreviewGrantRequest{
		ScopePublicID: "scope-1",
		GrantKey:      "message-2",
	})
	if err != nil {
		t.Fatalf("second issuePreviewScopePrefix: %v", err)
	}
	now = now.Add(defaultPreviewGrantTTL - 4*time.Minute)

	client := &http.Client{
		Transport: &http.Transport{Proxy: nil},
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	firstResp, err := client.Get(first)
	if err != nil {
		t.Fatalf("first exchange request: %v", err)
	}
	_ = firstResp.Body.Close()
	if firstResp.StatusCode != http.StatusGone {
		t.Fatalf("first status = %d, want 410", firstResp.StatusCode)
	}

	secondResp, err := client.Get(second)
	if err != nil {
		t.Fatalf("second exchange request: %v", err)
	}
	_ = secondResp.Body.Close()
	if secondResp.StatusCode != http.StatusFound {
		t.Fatalf("second status = %d, want 302", secondResp.StatusCode)
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

	reqGrant := previewpkg.WebPreviewGrantRequest{ScopePublicID: "scope-1", GrantKey: "message-1"}
	first, err := app.issuePreviewScopePrefix(context.Background(), reqGrant)
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

	second, err := app.issuePreviewScopePrefix(context.Background(), reqGrant)
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

func TestIssuePreviewScopePrefixReissuesGrantAfterIdleDeactivateAndReusesWarmProvider(t *testing.T) {
	base := time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC)
	listenPort := reserveExternalAccessPort(t)

	var factoryCalls atomic.Int32
	provider := externalaccess.NewTryCloudflareProvider(externalaccess.TryCloudflareOptions{
		BinaryPath:  "cloudflared",
		MetricsPort: reserveExternalAccessPort(t),
		WaitReady: func(context.Context, int) error {
			return nil
		},
		CommandFactory: func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
			factoryCalls.Add(1)
			return exec.CommandContext(ctx, "bash", "-lc", "printf 'https://example.trycloudflare.com\\n'; sleep 60")
		},
	})

	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	app.ConfigureAdmin(AdminRuntimeOptions{
		AdminListenHost: "127.0.0.1",
		AdminListenPort: "9501",
		AdminURL:        "http://127.0.0.1:9501/admin/",
		SetupURL:        "http://127.0.0.1:9501/setup",
	})
	app.externalAccessRuntime = ExternalAccessRuntimeConfig{
		Settings: externalAccessSettingsView{
			ListenHost:        "127.0.0.1",
			ListenPort:        listenPort,
			DefaultLinkTTL:    10 * time.Second,
			DefaultSessionTTL: 30 * time.Second,
			ProviderKind:      "trycloudflare",
		},
	}
	app.externalAccess = externalaccess.NewService(externalaccess.Options{
		Now:               func() time.Time { return base },
		Provider:          provider,
		DefaultLinkTTL:    10 * time.Second,
		DefaultSessionTTL: 30 * time.Second,
		IdleTTL:           5 * time.Minute,
	})
	defer app.Shutdown(nil)

	reqGrant := previewpkg.WebPreviewGrantRequest{ScopePublicID: "scope-1", GrantKey: "message-1"}
	first, err := app.issuePreviewScopePrefix(context.Background(), reqGrant)
	if err != nil {
		t.Fatalf("first issuePreviewScopePrefix: %v", err)
	}
	if got := factoryCalls.Load(); got != 1 {
		t.Fatalf("factoryCalls after first preview issue = %d, want 1", got)
	}

	parsedFirst := mustParseURL(t, first)
	req := httptest.NewRequest(http.MethodGet, parsedFirst.Path+"?"+parsedFirst.RawQuery, nil)
	rec := httptest.NewRecorder()
	app.externalAccess.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("exchange status = %d, want 302 body=%s", rec.Code, rec.Body.String())
	}

	app.onTick(context.Background(), base.Add(6*time.Minute))
	if app.externalAccessListener != nil {
		t.Fatal("expected idle timeout to stop external access listener")
	}
	if len(app.webPreviewGrants) != 0 {
		t.Fatalf("expected preview grants to be cleared after idle deactivate, got %#v", app.webPreviewGrants)
	}
	snapshot := app.externalAccess.Snapshot()
	if snapshot.ListenerActive || !snapshot.Provider.Ready {
		t.Fatalf("expected listener inactive and provider ready after idle deactivate, got %#v", snapshot)
	}

	second, err := app.issuePreviewScopePrefix(context.Background(), reqGrant)
	if err != nil {
		t.Fatalf("second issuePreviewScopePrefix: %v", err)
	}
	if second == first {
		t.Fatalf("expected reissued preview prefix after idle deactivate, got same url %q", second)
	}
	if got := factoryCalls.Load(); got != 1 {
		t.Fatalf("factoryCalls after second preview issue = %d, want 1", got)
	}
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse url %q: %v", raw, err)
	}
	return parsed
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

var _ previewpkg.FinalBlockPreviewService = (*fakePreviewRouteService)(nil)
var _ previewpkg.WebPreviewConfigurable = (*fakePreviewRouteService)(nil)
var _ previewpkg.WebPreviewRouteService = (*fakePreviewRouteService)(nil)
