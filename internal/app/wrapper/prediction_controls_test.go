package wrapper

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/adapter/codex"
)

func newTestPredictionRuntime() *predictionRuntime {
	p := newPredictionRuntime()
	p.desktopContext = func(context.Context, string) (map[string]any, error) {
		return nil, errors.New("no desktop in unit test")
	}
	return p
}

func controlsPayload(user, account string) string {
	raw, _ := json.Marshal(map[string]any{
		"hash_used": "djb2", "user": map[string]any{"userID": user, "customIDs": map[string]string{"account_id": account}},
		"feature_gates":   map[string]any{"1044080105": map[string]any{"value": true}},
		"dynamic_configs": map[string]any{"3941626706": map[string]any{"value": map[string]any{"st": map[string]float64{"official-category": .57}, "ubl": []string{"official-user-label"}, "abl": []string{"official-assistant-label"}}}},
	})
	outer, _ := json.Marshal(map[string]any{"statsigPayload": string(raw)})
	return string(outer)
}

func TestPredictionControlsUseOfficialBootstrapAndIdentity(t *testing.T) {
	p := newTestPredictionRuntime()
	body := map[string]any{"app_version": "desktop-version", "stable_id": "desktop-installation", "locale": "zh-CN"}
	p.httpClient = &http.Client{Transport: predictionRoundTrip(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.URL.String() != "https://native.example/backend-api/wham/statsig/bootstrap" {
			t.Fatalf("unexpected request %s %s", req.Method, req.URL)
		}
		if req.Header.Get("Authorization") != "Bearer native-token" || req.Header.Get("ChatGPT-Account-Id") != "account" || req.Header.Get("X-OpenAI-Account-Routing-Override") != "US_ONLY" || req.Header.Get("Content-Type") != "application/json" {
			t.Fatal("native routing/auth headers missing")
		}
		var got map[string]any
		if json.NewDecoder(req.Body).Decode(&got) != nil || !reflect.DeepEqual(got, body) {
			t.Fatalf("desktop context changed: %v", got)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(controlsPayload("user", "account")))}, nil
	})}
	controls, err := p.fetchControls(context.Background(), "native-token", predictionIdentity{UserID: "user", AccountID: "account"}, predictionRouting{BackendOrigin: "https://native.example", ChatGPTAccountID: "account", AccountRoutingOverride: "US_ONLY"}, body)
	if err != nil || !controls.InterruptOnCancel || !strings.Contains(string(controls.ModerationPolicy), "official-category") {
		t.Fatalf("controls=%+v err=%v", controls, err)
	}
	for _, principal := range [][2]string{{"other", "account"}, {"user", "other"}} {
		if _, err := parsePredictionControls([]byte(controlsPayload(principal[0], principal[1])), predictionIdentity{UserID: "user", AccountID: "account"}); err == nil {
			t.Fatalf("accepted other principal %v", principal)
		}
	}
}

func TestPredictionControlsMissingAndUnsupportedHash(t *testing.T) {
	identity := predictionIdentity{UserID: "user", AccountID: "account"}
	raw := strings.Replace(controlsPayload("user", "account"), "djb2", "unsupported", 1)
	if _, err := parsePredictionControls([]byte(raw), identity); err == nil {
		t.Fatal("unknown hash must not be guessed")
	}
	var outer map[string]string
	_ = json.Unmarshal([]byte(controlsPayload("user", "account")), &outer)
	var payload map[string]any
	_ = json.Unmarshal([]byte(outer["statsigPayload"]), &payload)
	delete(payload, "feature_gates")
	delete(payload, "dynamic_configs")
	inner, _ := json.Marshal(payload)
	outer["statsigPayload"] = string(inner)
	rawOuter, _ := json.Marshal(outer)
	controls, err := parsePredictionControls(rawOuter, identity)
	if err != nil || controls.InterruptOnCancel || len(controls.ModerationPolicy) != 0 {
		t.Fatalf("unavailable controls must stay unavailable: %+v %v", controls, err)
	}
}

func TestPredictionInterruptRequiresKnownClientVersionBoundary(t *testing.T) {
	for _, client := range []string{"Codex Desktop", "Codex Remote Headless"} {
		for _, test := range []struct {
			version string
			want    bool
		}{
			{"", false}, {"invalid", false}, {"0.166.9", false}, {"0.167.0-alpha.0", false}, {"0.167.0-alpha.1", true}, {"0.167.0-alpha.4.1", true}, {"0.167.0", true}, {"0.168.0-alpha.0", true}, {"0.169.0-alpha.11.1", true},
		} {
			t.Run(client+"/"+test.version, func(t *testing.T) {
				if got := predictionInterruptSupported(client + "/" + test.version + " (Mac OS 26.7.2; arm64)"); got != test.want {
					t.Errorf("version %q got %t, want %t", test.version, got, test.want)
				}
			})
		}
	}
}

func TestPredictionInterruptRejectsUnknownClient(t *testing.T) {
	for _, userAgent := range []string{
		"", "0.169.0-alpha.11.1", "Codex CLI/0.169.0-alpha.11.1", "Codex Remote/0.169.0-alpha.11.1",
		"Unknown/1.0.0", "Codex Remote Headless Extra/0.169.0-alpha.11.1", "Codex Desktop Extra/0.169.0-alpha.11.1",
	} {
		if predictionInterruptSupported(userAgent) {
			t.Errorf("accepted unknown client %q", userAgent)
		}
	}
}

func TestPredictionInitializationRecordsNativeVersion(t *testing.T) {
	r := &codexBackendRuntime{translator: codex.NewTranslator("instance")}
	r.observePredictionInitialization([]byte(`{"id":"relay-bootstrap-initialize","result":{"userAgent":"Codex Remote Headless/0.169.0-alpha.11.1"}}`))
	if !predictionInterruptSupported(r.nativeUserAgent) {
		t.Fatal("headless initialization lost native version")
	}
}

func TestPredictionControlsRefreshAfterRestartAndAccountChange(t *testing.T) {
	calls := 0
	run := func(p *predictionRuntime, account string) predictionControls {
		runtime := &codexBackendRuntime{translator: codex.NewTranslator("instance"), predictions: p}
		command := predictionRequest()
		job := &predictionJob{ctx: context.Background(), command: command}
		p.jobs[command.CommandID] = job
		p.desktopContext = func(context.Context, string) (map[string]any, error) { return map[string]any{"locale": "zh-CN"}, nil }
		p.httpClient = &http.Client{Transport: predictionRoundTrip(func(req *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(controlsPayload("user", req.Header.Get("ChatGPT-Account-Id"))))}, nil
		})}
		return runtime.predictionControlsForJob(job, "native-token", predictionIdentity{UserID: "user", AccountID: account}, predictionRouting{BackendOrigin: "https://native.example", ChatGPTAccountID: account, AccountRoutingOverride: "NO_CONSTRAINT"})
	}
	p := newTestPredictionRuntime()
	if !run(p, "first").InterruptOnCancel || !run(p, "first").InterruptOnCancel || calls != 1 {
		t.Fatalf("current process calls=%d", calls)
	}
	if !run(p, "second").InterruptOnCancel || calls != 2 {
		t.Fatalf("account switch calls=%d", calls)
	}
	if !run(newTestPredictionRuntime(), "first").InterruptOnCancel || calls != 3 {
		t.Fatalf("restart reused stale evaluation; calls=%d", calls)
	}
}
