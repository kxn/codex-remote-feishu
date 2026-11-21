package teamcli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/workcmd"
)

type testRoundTripper func(*http.Request) (*http.Response, error)

func (f testRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func contextRequest() workcmd.DispatchRequest {
	return workcmd.DispatchRequest{Name: "context", RunRef: "run-one", Payload: json.RawMessage(`{}`)}
}

func TestClientRejectsNonLoopbackAndEndpointOverrides(t *testing.T) {
	calls := 0
	client := &Client{HTTPClient: &http.Client{Transport: testRoundTripper(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("unexpected transport") })}}
	for _, endpoint := range []string{"https://example.com", "http://192.0.2.1", "http://127.0.0.1.evil.test", "file:///tmp/work", "http://user:secret@127.0.0.1", "http://127.0.0.1/open-apis/im/v1/messages", "http://127.0.0.1?path=other"} {
		if _, err := client.Execute(context.Background(), endpoint, "session-secret", contextRequest()); err == nil {
			t.Errorf("accepted %s", endpoint)
		}
	}
	if calls != 0 {
		t.Fatalf("unsafe endpoint reached transport %d times", calls)
	}
}

func TestClientDoesNotFollowRedirectsOrExposeCredentialErrors(t *testing.T) {
	redirected := 0
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected++ }))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	if _, err := NewClient().Execute(context.Background(), origin.URL, "session-secret", contextRequest()); err == nil {
		t.Error("redirect reported success")
	}
	if redirected != 0 {
		t.Error("credential request followed redirect")
	}
	client := &Client{HTTPClient: &http.Client{Transport: testRoundTripper(func(*http.Request) (*http.Response, error) { return nil, errors.New("transport echoed session-secret") })}}
	if _, err := client.Execute(context.Background(), "http://127.0.0.1:321", "session-secret", contextRequest()); err == nil || strings.Contains(err.Error(), "session-secret") {
		t.Fatalf("credential error leaked: %v", err)
	}
	client.HTTPClient.Transport = testRoundTripper(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 401, Body: io.NopCloser(strings.NewReader(`{"error":{"code":"unauthorized","message":"bad session-secret"}}`))}, nil
	})
	if _, err := client.Execute(context.Background(), "http://127.0.0.1:321", "session-secret", contextRequest()); err == nil || strings.Contains(err.Error(), "session-secret") {
		t.Fatalf("server credential error leaked: %v", err)
	}
}
