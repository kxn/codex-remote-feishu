package wrapper

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type predictionRoundTrip func(*http.Request) (*http.Response, error)

func (f predictionRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func predictionToken(t *testing.T, user, account string) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"exp": time.Now().Add(time.Hour).Unix(), "https://api.openai.com/auth": map[string]any{"user_id": user, "chatgpt_account_id": account}})
	if err != nil {
		t.Fatal(err)
	}
	return "header." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}

func TestPredictionOfficialConfigHeadersCacheAndExpiry(t *testing.T) {
	t.Setenv("CODEX_API_BASE_URL", "")
	t.Setenv("CODEX_API_ENDPOINT", "")
	state := newTestPredictionRuntime()
	now := time.Now()
	state.now = func() time.Time { return now }
	token := predictionToken(t, "user", "account")
	identity, err := parsePredictionIdentity(token)
	if err != nil {
		t.Fatal(err)
	}
	routing := predictionRouting{ChatGPTAccountID: "account", BackendOrigin: "https://workspace.example", AccountRoutingOverride: "eu_cr"}
	calls := 0
	state.httpClient = &http.Client{Transport: predictionRoundTrip(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.URL.String() != "https://workspace.example/backend-api/wham/predictions/config" || req.Header.Get("Authorization") != "Bearer "+token || req.Header.Get("ChatGPT-Account-Id") != "account" || req.Header.Get("X-OpenAI-Account-Routing-Override") != "eu_cr" {
			t.Fatal("official config request lost native identity/routing")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"is_enabled":true,"prompt":"official prompt","prompt_version":"5","prediction_reasoning_effort":"high","live_sample_rate":1}`))}, nil
	})}
	for i := 0; i < 2; i++ {
		config, err := state.fetchConfig(context.Background(), token, identity, routing)
		if err != nil || config.Prompt != "official prompt" || config.Effort == nil || *config.Effort != "high" {
			t.Fatalf("config = %#v, %v", config, err)
		}
	}
	if calls != 1 {
		t.Fatalf("calls = %d; want cached request", calls)
	}
	now = now.Add(5 * time.Minute)
	if _, err := state.fetchConfig(context.Background(), token, identity, routing); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("calls after TTL = %d", calls)
	}
	other := identity
	other.UserID = "other-user"
	if _, err := state.fetchConfig(context.Background(), token, other, routing); err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatal("config reused across native principals")
	}
}

func TestPredictionIdentityAndRoutingFailClosed(t *testing.T) {
	token := predictionToken(t, "user", "account")
	identity, err := parsePredictionIdentity(token)
	if err != nil || identity.UserID != "user" || identity.AccountID != "account" {
		t.Fatalf("identity = %#v, %v", identity, err)
	}
	for _, raw := range []string{
		`{"account":{"type":"chatgpt"},"workspaceRouting":null}`,
		`{"account":{"type":"chatgpt"}}`,
		`{"account":{"type":"apiKey"},"workspaceRouting":{"chatgptAccountId":"account","backendOrigin":"https://chatgpt.com","accountRoutingOverride":"NO_CONSTRAINT"}}`,
		`{"account":{"type":"chatgpt"},"workspaceRouting":{"chatgptAccountId":"other","backendOrigin":"https://chatgpt.com","accountRoutingOverride":"NO_CONSTRAINT"}}`,
		`{"account":{"type":"chatgpt"},"workspaceRouting":{"chatgptAccountId":"account","backendOrigin":"https://chatgpt.com/path","accountRoutingOverride":"NO_CONSTRAINT"}}`,
	} {
		if _, err := parsePredictionRouting(json.RawMessage(raw), json.RawMessage(`{"requirements":null}`), identity); err == nil {
			t.Fatalf("accepted unavailable/mismatched routing: %s", raw)
		}
	}
	if _, err := parsePredictionIdentity("not-a-native-jwt"); err == nil {
		t.Fatal("accepted malformed identity")
	}
}

func TestPredictionConfigRequiresOfficialFields(t *testing.T) {
	for _, raw := range []string{`{}`, `{"is_enabled":true,"prompt":"invented"}`, `{"is_enabled":true,"prompt":"","prompt_version":"5","prediction_reasoning_effort":"high"}`, `{"is_enabled":true,"prompt":"official","prompt_version":"5","prediction_reasoning_effort":"high","live_sample_rate":2}`} {
		if _, err := parsePredictionConfig([]byte(raw)); err == nil {
			t.Fatalf("accepted invalid official config: %s", raw)
		}
	}
	config, err := parsePredictionConfig([]byte(`{"is_enabled":false,"prompt":"official","prompt_version":"5","prediction_reasoning_effort":""}`))
	if err != nil || config.Enabled || config.Effort != nil {
		t.Fatalf("disabled config = %#v, %v", config, err)
	}
}

func TestPredictionConfigRejectsOptionalNulls(t *testing.T) {
	for _, extra := range []string{`"live_sample_rate":null`, `"unsupported_models":null`, `"unsupported_models":[null]`} {
		if _, err := parsePredictionConfig([]byte(`{"is_enabled":true,"prompt":"official","prompt_version":"5","prediction_reasoning_effort":"high",` + extra + `}`)); err == nil {
			t.Fatalf("accepted config rejected by desktop: %s", extra)
		}
	}
}

func TestPredictionConfigNeverFollowsCredentialRedirect(t *testing.T) {
	requests := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Redirect(w, r, "https://unknown.example/secret", http.StatusFound)
	}))
	defer server.Close()
	state := newTestPredictionRuntime()
	state.httpClient = server.Client()
	token := predictionToken(t, "user", "account")
	identity, err := parsePredictionIdentity(token)
	if err != nil {
		t.Fatal(err)
	}
	_, err = state.fetchConfig(context.Background(), token, identity, predictionRouting{ChatGPTAccountID: "account", BackendOrigin: server.URL, AccountRoutingOverride: "NO_CONSTRAINT"})
	if err == nil || requests != 1 {
		t.Fatalf("redirect was accepted: requests=%d error=%v", requests, err)
	}
}

func TestPredictionConfigUsesOfficialBaseOverridePath(t *testing.T) {
	t.Setenv("CODEX_API_BASE_URL", " https://configured.example/custom-api/// ")
	state := newTestPredictionRuntime()
	state.httpClient = &http.Client{Transport: predictionRoundTrip(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != "https://native.example/custom-api/wham/predictions/config" || req.Header.Get("X-OpenAI-Account-Routing-Override") != "" {
			t.Fatal("base override/native origin or unconstrained header differs from desktop")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"is_enabled":false,"prompt":"official","prompt_version":"5","prediction_reasoning_effort":""}`))}, nil
	})}
	token := predictionToken(t, "user", "account")
	identity, err := parsePredictionIdentity(token)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = state.fetchConfig(context.Background(), token, identity, predictionRouting{ChatGPTAccountID: "account", BackendOrigin: "https://native.example", AccountRoutingOverride: "NO_CONSTRAINT"}); err != nil {
		t.Fatal(err)
	}
}

func TestPredictionConfigFailureIsCachedForDesktopTTL(t *testing.T) {
	for _, body := range []string{"http-error", "invalid-json"} {
		t.Run(body, func(t *testing.T) {
			state := newTestPredictionRuntime()
			now := time.Now()
			state.now = func() time.Time { return now }
			requests := 0
			state.httpClient = &http.Client{Transport: predictionRoundTrip(func(req *http.Request) (*http.Response, error) {
				requests++
				status := 200
				if body == "http-error" {
					status = 503
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("not official JSON"))}, nil
			})}
			token := predictionToken(t, "user", "account")
			identity, err := parsePredictionIdentity(token)
			if err != nil {
				t.Fatal(err)
			}
			routing := predictionRouting{ChatGPTAccountID: "account", BackendOrigin: "https://chatgpt.com", AccountRoutingOverride: "NO_CONSTRAINT"}
			if _, err := state.fetchConfig(context.Background(), token, identity, routing); err == nil {
				t.Fatal("first invalid response accepted")
			}
			config, _ := state.fetchConfig(context.Background(), token, identity, routing)
			if requests != 1 || config.Enabled {
				t.Fatalf("failed config not cached as unavailable: calls=%d config=%#v", requests, config)
			}
			now = now.Add(5 * time.Minute)
			_, _ = state.fetchConfig(context.Background(), token, identity, routing)
			if requests != 2 {
				t.Fatal("failed config did not expire at desktop TTL")
			}
		})
	}
}

func TestPredictionConfigCancellationDoesNotCacheUnavailable(t *testing.T) {
	state := newTestPredictionRuntime()
	ctx, cancel := context.WithCancel(context.Background())
	requests := 0
	state.httpClient = &http.Client{Transport: predictionRoundTrip(func(req *http.Request) (*http.Response, error) {
		requests++
		cancel()
		return nil, context.Canceled
	})}
	token := predictionToken(t, "user", "account")
	identity, err := parsePredictionIdentity(token)
	if err != nil {
		t.Fatal(err)
	}
	routing := predictionRouting{ChatGPTAccountID: "account", BackendOrigin: "https://chatgpt.com", AccountRoutingOverride: "NO_CONSTRAINT"}
	_, _ = state.fetchConfig(ctx, token, identity, routing)
	_, _ = state.fetchConfig(context.Background(), token, identity, routing)
	if requests != 2 {
		t.Fatal("aborted config query was cached")
	}
}

type predictionReadFunc func([]byte) (int, error)

func (f predictionReadFunc) Read(p []byte) (int, error) { return f(p) }

func TestPredictionCanceledQueryDoesNotCommitSuccessfulConfig(t *testing.T) {
	state := newTestPredictionRuntime()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state.httpClient = &http.Client{Transport: predictionRoundTrip(func(req *http.Request) (*http.Response, error) {
		body := strings.NewReader(`{"is_enabled":true,"prompt":"official","prompt_version":"5","prediction_reasoning_effort":"high"}`)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(predictionReadFunc(func(p []byte) (int, error) { cancel(); return body.Read(p) }))}, nil
	})}
	token := predictionToken(t, "user", "account")
	identity, err := parsePredictionIdentity(token)
	if err != nil {
		t.Fatal(err)
	}
	_, err = state.fetchConfig(ctx, token, identity, predictionRouting{ChatGPTAccountID: "account", BackendOrigin: "https://chatgpt.com", AccountRoutingOverride: "NO_CONSTRAINT"})
	if err == nil {
		t.Fatal("query cancellation after HTTP was ignored")
	}
	if len(state.cache) != 0 {
		t.Fatal("canceled query committed successful configuration")
	}
}
