package wrapper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestPredictionDesktopPreferencesStayWithinNativePrincipal(t *testing.T) {
	hash := sha256.Sum256([]byte(`["user","account"]`))
	owner := hex.EncodeToString(hash[:])
	for _, tc := range []struct {
		name, state string
		wantOwner   string
		wantLegacy  string
		wantError   bool
	}{
		{"matched explicit false", fmt.Sprintf(`{"electron-renderer-content-owner":"%s","electron-owner-atoms:%s":{"composer-predictions-enabled":false},"electron-persisted-atom-state":{"composer-predictions-enabled":true}}`, owner, owner), "false", "<nil>", false},
		{"matched explicit true", fmt.Sprintf(`{"electron-renderer-content-owner":"%s","electron-owner-atoms:%s":{"composer-predictions-enabled":true},"electron-persisted-atom-state":{"composer-predictions-enabled":false}}`, owner, owner), "true", "<nil>", false},
		{"matched invalid atom skips legacy migration", fmt.Sprintf(`{"electron-renderer-content-owner":"%s","electron-owner-atoms:%s":{"composer-predictions-enabled":"false"},"electron-persisted-atom-state":{"composer-predictions-enabled":false}}`, owner, owner), "<nil>", "<nil>", false},
		{"unscoped migration", `{"electron-persisted-atom-state":{"composer-predictions-enabled":false}}`, "<nil>", "false", false},
		{"other principal", `{"electron-renderer-content-owner":"other"}`, "<nil>", "<nil>", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			codexHome := t.TempDir()
			if err := os.WriteFile(filepath.Join(codexHome, ".codex-global-state.json"), []byte(tc.state), 0600); err != nil {
				t.Fatal(err)
			}
			pref, err := readPredictionDesktopPreferencesAt(codexHome, predictionIdentity{UserID: "user", AccountID: "account"})
			format := func(value *bool) string {
				if value == nil {
					return "<nil>"
				}
				return fmt.Sprint(*value)
			}
			if (err != nil) != tc.wantError || format(pref.owner) != tc.wantOwner || format(pref.legacy) != tc.wantLegacy {
				t.Fatalf("got owner=%s legacy=%s err=%v", format(pref.owner), format(pref.legacy), err)
			}
		})
	}
}

func TestPredictionDesktopUnverifiedNativeStorageDoesNotUseJSON(t *testing.T) {
	codexHome := t.TempDir()
	for file, value := range map[string]string{
		".codex-global-state.json":   `{"electron-persisted-atom-state":{"composer-predictions-enabled":true}}`,
		".codex-global-state.sqlite": "unavailable",
	} {
		if err := os.WriteFile(filepath.Join(codexHome, file), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := readPredictionDesktopPreferencesAt(codexHome, predictionIdentity{UserID: "user", AccountID: "account"}); err == nil {
		t.Fatal("used potentially stale JSON with unknown native desktop storage")
	}
}

func TestPredictionExplicitDesktopConfigOptOutStopsBeforeOfficialConfig(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	runtime := newBackendRuntime(Config{Backend: agentproto.BackendCodex, InstanceID: "instance"}).(*codexBackendRuntime)
	runtime.predictions = newPredictionRuntime()
	httpCalls := make(chan struct{}, 2)
	runtime.predictions.httpClient = &http.Client{Transport: predictionRoundTrip(func(*http.Request) (*http.Response, error) {
		httpCalls <- struct{}{}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"is_enabled":true,"prompt":"official","prompt_version":"5","prediction_reasoning_effort":""}`))}, nil
	})}
	frames := make(chan []byte, 16)
	events := predictionEventRecorder{make(chan []agentproto.Event, 4)}
	t.Cleanup(runtime.CancelPredictions)
	runtime.HandlePrediction(context.Background(), predictionRequest(), frames, events)
	predictionAuthenticate(t, runtime, frames, "user", "account", false)
	configRead := predictionFrame(t, frames)
	if configRead["method"] != "config/read" {
		t.Fatalf("first request after authentication = %v, want native config/read", configRead["method"])
	}
	predictionReply(t, runtime, configRead, map[string]any{
		"config": map[string]any{"desktop": map[string]any{"composer-predictions-enabled": false}},
	})
	expectPredictionStopped(t, frames, events)
	select {
	case <-httpCalls:
		t.Fatal("desktop opt-out still requested official prediction config")
	default:
	}
}

func TestPredictionUnavailableDesktopControlsStopsNativeSampling(t *testing.T) {
	runtime := newBackendRuntime(Config{Backend: agentproto.BackendCodex, InstanceID: "instance"}).(*codexBackendRuntime)
	runtime.predictions = newTestPredictionRuntime()
	runtime.predictions.cache["user\x00account"] = predictionConfigCache{
		config:  predictionConfig{Enabled: true, Prompt: "official prompt", LiveSampleRate: 1},
		expires: time.Now().Add(time.Minute),
	}
	frames := make(chan []byte, 16)
	events := predictionEventRecorder{make(chan []agentproto.Event, 4)}
	t.Cleanup(runtime.CancelPredictions)
	runtime.HandlePrediction(context.Background(), predictionRequest(), frames, events)
	predictionAuthenticate(t, runtime, frames, "user", "account", false)
	resume := predictionFrame(t, frames)
	if resume["method"] != "thread/resume" {
		t.Fatalf("unexpected preparation: %#v", resume)
	}
	predictionReply(t, runtime, resume, map[string]any{"modelProvider": "openai", "model": "supported"})
	expectPredictionStopped(t, frames, events)
}

func expectPredictionStopped(t *testing.T, frames <-chan []byte, events predictionEventRecorder) {
	t.Helper()
	select {
	case frame := <-frames:
		t.Fatalf("ineligible prediction sent another frame: %s", frame)
	case batch := <-events.events:
		if len(batch) != 1 || batch[0].Prediction == nil || batch[0].Prediction.Suggestion != "" {
			t.Fatalf("ineligible prediction did not complete quietly: %#v", batch)
		}
	case <-time.After(time.Second):
		t.Fatal("ineligible prediction did not complete")
	}
}
