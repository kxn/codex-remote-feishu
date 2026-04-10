package externalaccess

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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
