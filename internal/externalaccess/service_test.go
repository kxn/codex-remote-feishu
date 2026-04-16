package externalaccess

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestServiceIssueExchangeAndProxy(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin/" {
			t.Fatalf("upstream path = %q, want /admin/", r.URL.Path)
		}
		_, _ = io.WriteString(w, "admin ok")
	}))
	defer upstream.Close()

	now := time.Date(2026, 4, 10, 18, 0, 0, 0, time.UTC)
	service := NewService(Options{Now: func() time.Time { return now }})
	issued, err := service.IssueURL(t.Context(), IssueRequest{
		Purpose:        PurposeDebug,
		TargetURL:      upstream.URL + "/admin/",
		TargetBasePath: "/admin/",
	}, "http://127.0.0.1:9512")
	if err != nil {
		t.Fatalf("IssueURL: %v", err)
	}
	parsed, err := url.Parse(issued.ExternalURL)
	if err != nil {
		t.Fatalf("parse issued url: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, parsed.Path+"?"+parsed.RawQuery, nil)
	rec := httptest.NewRecorder()
	service.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("exchange status = %d, want 302 body=%s", rec.Code, rec.Body.String())
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %#v, want 1 cookie", cookies)
	}
	if cookies[0].Path == "" || !strings.HasPrefix(cookies[0].Path, "/g/") {
		t.Fatalf("cookie path = %q, want /g/.../", cookies[0].Path)
	}

	req = httptest.NewRequest(http.MethodGet, parsed.Path, nil)
	req.AddCookie(cookies[0])
	rec = httptest.NewRecorder()
	service.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("proxy status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "admin ok" {
		t.Fatalf("body = %q, want admin ok", got)
	}
}

func TestServiceRewritesLocationAndCookiePath(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "upstream", Value: "1", Path: "/", Domain: "localhost"})
		http.Redirect(w, r, "/admin/assets/app.css", http.StatusFound)
	}))
	defer upstream.Close()

	service := NewService(Options{})
	issued, err := service.IssueURL(t.Context(), IssueRequest{
		Purpose:        PurposeDebug,
		TargetURL:      upstream.URL + "/admin/",
		TargetBasePath: "/admin/",
	}, "http://127.0.0.1:9512")
	if err != nil {
		t.Fatalf("IssueURL: %v", err)
	}
	parsed, _ := url.Parse(issued.ExternalURL)

	req := httptest.NewRequest(http.MethodGet, parsed.Path+"?"+parsed.RawQuery, nil)
	rec := httptest.NewRecorder()
	service.ServeHTTP(rec, req)
	cookie := rec.Result().Cookies()[0]

	req = httptest.NewRequest(http.MethodGet, parsed.Path, nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	service.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("proxy status = %d, want 302 body=%s", rec.Code, rec.Body.String())
	}
	if location := rec.Header().Get("Location"); location == "" || !strings.Contains(location, "/g/") {
		t.Fatalf("location = %q, want external /g/... path", location)
	}
	gotCookies := rec.Result().Cookies()
	if len(gotCookies) != 1 {
		t.Fatalf("expected rewritten upstream cookie, got %#v", gotCookies)
	}
	if !strings.HasPrefix(gotCookies[0].Path, "/g/") || gotCookies[0].Domain != "" {
		t.Fatalf("rewritten cookie = %#v", gotCookies[0])
	}
}

func TestServiceAllowsRootBasePathAssets(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, `<!doctype html><script type="module" src="./assets/app.js"></script>`)
		case "/assets/app.js":
			w.Header().Set("Content-Type", "application/javascript")
			_, _ = io.WriteString(w, `console.log("ok")`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	service := NewService(Options{})
	issued, err := service.IssueURL(t.Context(), IssueRequest{
		Purpose:        PurposeDebug,
		TargetURL:      upstream.URL + "/",
		TargetBasePath: "/",
	}, "http://127.0.0.1:9512")
	if err != nil {
		t.Fatalf("IssueURL: %v", err)
	}
	parsed, _ := url.Parse(issued.ExternalURL)

	req := httptest.NewRequest(http.MethodGet, parsed.Path+"?"+parsed.RawQuery, nil)
	rec := httptest.NewRecorder()
	service.ServeHTTP(rec, req)
	cookie := rec.Result().Cookies()[0]

	req = httptest.NewRequest(http.MethodGet, parsed.Path+"assets/app.js", nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	service.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("asset status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/javascript") {
		t.Fatalf("content-type = %q, want application/javascript", got)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `console.log("ok")` {
		t.Fatalf("asset body = %q", got)
	}
}

func TestServiceRejectsPathOutsideAllowlist(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "should not reach")
	}))
	defer upstream.Close()

	service := NewService(Options{})
	issued, err := service.IssueURL(t.Context(), IssueRequest{
		Purpose:        PurposeDebug,
		TargetURL:      upstream.URL + "/admin/",
		TargetBasePath: "/admin/",
	}, "http://127.0.0.1:9512")
	if err != nil {
		t.Fatalf("IssueURL: %v", err)
	}
	parsed, _ := url.Parse(issued.ExternalURL)

	req := httptest.NewRequest(http.MethodGet, parsed.Path+"?"+parsed.RawQuery, nil)
	rec := httptest.NewRecorder()
	service.ServeHTTP(rec, req)
	cookie := rec.Result().Cookies()[0]

	grantID := strings.Split(strings.TrimPrefix(parsed.Path, "/g/"), "/")[0]
	req = httptest.NewRequest(http.MethodGet, "/g/"+grantID+"/../oops", nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	service.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 body=%s", rec.Code, rec.Body.String())
	}
}

type rotatingProvider struct {
	calls int
}

func (p *rotatingProvider) Kind() string { return "fake" }

func (p *rotatingProvider) EnsurePublicBase(context.Context, string) (PublicBase, error) {
	p.calls++
	return PublicBase{
		BaseURL:   "https://example-" + string(rune('0'+p.calls)) + ".trycloudflare.com",
		StartedAt: time.Unix(int64(p.calls), 0).UTC(),
	}, nil
}

func (p *rotatingProvider) Snapshot() ProviderStatus {
	return ProviderStatus{Kind: p.Kind(), Ready: true}
}

func (p *rotatingProvider) Close() error { return nil }

type blockingCloseProvider struct {
	startOnce    sync.Once
	closeStarted chan struct{}
	unblockClose chan struct{}
}

func (p *blockingCloseProvider) Kind() string { return "fake" }

func (p *blockingCloseProvider) EnsurePublicBase(context.Context, string) (PublicBase, error) {
	return PublicBase{
		BaseURL:   "https://example.trycloudflare.com",
		StartedAt: time.Unix(1, 0).UTC(),
	}, nil
}

func (p *blockingCloseProvider) Snapshot() ProviderStatus {
	return ProviderStatus{Kind: p.Kind(), Ready: true}
}

func (p *blockingCloseProvider) Close() error {
	p.startOnce.Do(func() {
		close(p.closeStarted)
	})
	<-p.unblockClose
	return nil
}

func TestServiceIssueURLDoesNotCacheProviderBaseAcrossCalls(t *testing.T) {
	provider := &rotatingProvider{}
	service := NewService(Options{Provider: provider})

	first, err := service.IssueURL(t.Context(), IssueRequest{
		Purpose:   PurposeDebug,
		TargetURL: "http://127.0.0.1:9501/",
	}, "http://127.0.0.1:9512")
	if err != nil {
		t.Fatalf("IssueURL first: %v", err)
	}
	second, err := service.IssueURL(t.Context(), IssueRequest{
		Purpose:   PurposeDebug,
		TargetURL: "http://127.0.0.1:9501/",
	}, "http://127.0.0.1:9512")
	if err != nil {
		t.Fatalf("IssueURL second: %v", err)
	}
	if provider.calls != 2 {
		t.Fatalf("provider.calls = %d, want 2", provider.calls)
	}
	if strings.HasPrefix(first.ExternalURL, "https://example-2.trycloudflare.com/") {
		t.Fatalf("first external url unexpectedly reused second base: %q", first.ExternalURL)
	}
	if !strings.HasPrefix(second.ExternalURL, "https://example-2.trycloudflare.com/") {
		t.Fatalf("second external url = %q, want refreshed provider base", second.ExternalURL)
	}
}

func TestServiceExchangeFallsBackToExistingCookieWhenQueryTokenIsInvalid(t *testing.T) {
	now := time.Date(2026, 4, 10, 18, 0, 0, 0, time.UTC)
	service := NewService(Options{Now: func() time.Time { return now }})
	issued, err := service.IssueURL(t.Context(), IssueRequest{
		Purpose:        PurposePreview,
		TargetURL:      "http://127.0.0.1:9501/preview/s/scope/",
		TargetBasePath: "/preview/s/scope/",
		LinkTTL:        2 * time.Second,
		SessionTTL:     10 * time.Minute,
	}, "http://127.0.0.1:9512")
	if err != nil {
		t.Fatalf("IssueURL: %v", err)
	}
	parsed, _ := url.Parse(issued.ExternalURL)

	req := httptest.NewRequest(http.MethodGet, parsed.Path+"?"+parsed.RawQuery, nil)
	rec := httptest.NewRecorder()
	service.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("initial exchange status = %d, want 302 body=%s", rec.Code, rec.Body.String())
	}
	cookie := rec.Result().Cookies()[0]

	req = httptest.NewRequest(http.MethodGet, parsed.Path+"file-a?t=invalid-token", nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	service.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("invalid query with valid cookie status = %d, want 302 body=%s", rec.Code, rec.Body.String())
	}
	if location := rec.Header().Get("Location"); location != parsed.Path+"file-a" {
		t.Fatalf("location = %q, want %q", location, parsed.Path+"file-a")
	}
}

func TestServiceShutdownRuntimeDoesNotHoldMutexWhileClosingProvider(t *testing.T) {
	provider := &blockingCloseProvider{
		closeStarted: make(chan struct{}),
		unblockClose: make(chan struct{}),
	}
	service := NewService(Options{Provider: provider})

	shutdownDone := make(chan struct{})
	go func() {
		_ = service.ShutdownRuntime()
		close(shutdownDone)
	}()

	select {
	case <-provider.closeStarted:
	case <-time.After(time.Second):
		t.Fatal("provider.Close was not called")
	}

	snapshotDone := make(chan struct{})
	go func() {
		_ = service.Snapshot()
		close(snapshotDone)
	}()

	select {
	case <-snapshotDone:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Snapshot blocked while provider.Close was in flight")
	}

	close(provider.unblockClose)

	select {
	case <-shutdownDone:
	case <-time.After(time.Second):
		t.Fatal("ShutdownRuntime did not finish after provider.Close unblocked")
	}
}

func TestServiceDeactivateListenerKeepsProviderOpen(t *testing.T) {
	provider := &blockingCloseProvider{
		closeStarted: make(chan struct{}),
		unblockClose: make(chan struct{}),
	}
	service := NewService(Options{Provider: provider})
	service.SetListenerState("http://127.0.0.1:9512", true)

	if _, err := service.IssueURL(t.Context(), IssueRequest{
		Purpose:   PurposeDebug,
		TargetURL: "http://127.0.0.1:9501/",
	}, "http://127.0.0.1:9512"); err != nil {
		t.Fatalf("IssueURL: %v", err)
	}

	service.DeactivateListener()

	select {
	case <-provider.closeStarted:
		t.Fatal("provider.Close should not be called during listener-only deactivate")
	case <-time.After(100 * time.Millisecond):
	}

	snapshot := service.Snapshot()
	if snapshot.ListenerActive {
		t.Fatalf("expected listener inactive after deactivate, got %#v", snapshot)
	}
	if snapshot.GrantCount != 0 || snapshot.SessionCount != 0 {
		t.Fatalf("expected grants and sessions to be cleared, got %#v", snapshot)
	}
	if !snapshot.Provider.Ready {
		t.Fatalf("expected provider to stay ready after listener-only deactivate, got %#v", snapshot)
	}
}
