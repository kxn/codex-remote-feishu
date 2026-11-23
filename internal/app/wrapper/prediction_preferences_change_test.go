package wrapper

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestPredictionDesktopSettingOffDiscardsPendingSample(t *testing.T) {
	runtime, _, _, setEnabled, response := startDesktopSettingSample(t)
	setEnabled(false)
	observed, err := runtime.ObserveServer(response)
	if err != nil || slices.ContainsFunc(observed.Events, hasPredictionCandidate) {
		t.Fatalf("sample completed after Desktop disabled suggestions: %#v, %v", observed.Events, err)
	}
}

func TestPredictionDesktopSettingOffCancelsNativeSample(t *testing.T) {
	runtime, frames, events, setEnabled, response := startDesktopSettingSample(t)
	setEnabled(false)
	if cancel := predictionFrame(t, frames); cancel["method"] != "thread/sample/cancel" {
		t.Fatalf("expected dedicated native cancellation, got %#v", cancel)
	}
	select {
	case batch := <-events.events:
		if len(batch) != 1 || batch[0].Kind != agentproto.EventThreadPredictionInvalidated {
			t.Fatalf("expected invalidation while sampling: %#v", batch)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("closing the preference did not invalidate the in-flight sample")
	}
	observed, err := runtime.ObserveServer(response)
	if err != nil || slices.ContainsFunc(observed.Events, hasPredictionCandidate) {
		t.Fatalf("canceled sample published a candidate: %#v, %v", observed.Events, err)
	}
}

func TestPredictionDesktopSettingOffInvalidatesAlreadyPublishedSample(t *testing.T) {
	runtime, _, events, setEnabled, response := startDesktopSettingSample(t)
	observed, err := runtime.ObserveServer(response)
	if err != nil || !slices.ContainsFunc(observed.Events, hasPredictionCandidate) {
		t.Fatalf("expected published suggestion: %#v, %v", observed.Events, err)
	}
	setEnabled(false)
	select {
	case batch := <-events.events:
		if len(batch) != 1 || batch[0].Kind != agentproto.EventThreadPredictionInvalidated {
			t.Fatalf("expected published suggestion and draft invalidation: %#v", batch)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("turning off Desktop suggestions did not invalidate the published suggestion")
	}
}

func TestPredictionDesktopSettingOffDiscardsAccountHeldSample(t *testing.T) {
	runtime, frames, _, setEnabled, response := startDesktopSettingSample(t)
	if _, err := runtime.ObserveServer([]byte(`{"method":"account/updated","params":{"authMode":"chatgpt"}}`)); err != nil {
		t.Fatal(err)
	}
	probe := predictionFrame(t, frames)
	observed, err := runtime.ObserveServer(response)
	if err != nil || slices.ContainsFunc(observed.Events, hasPredictionCandidate) {
		t.Fatalf("expected account probe to hold the sample: %#v, %v", observed.Events, err)
	}
	setEnabled(false)
	confirmed, _ := json.Marshal(map[string]any{"id": probe["id"], "result": map[string]any{"authMethod": "chatgpt", "authToken": predictionToken(t, "user", "account")}})
	observed, err = runtime.ObserveServer(confirmed)
	if err != nil || slices.ContainsFunc(observed.Events, hasPredictionCandidate) {
		t.Fatalf("account probe released a sample after Desktop disabled suggestions: %#v, %v", observed.Events, err)
	}
}

func startDesktopSettingSample(t *testing.T) (*codexBackendRuntime, chan []byte, predictionEventRecorder, func(bool), []byte) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	owner := sha256.Sum256([]byte(`["user","account"]`))
	setEnabled := func(enabled bool) {
		t.Helper()
		tmp := filepath.Join(home, "new-state")
		data := fmt.Sprintf(`{"electron-renderer-content-owner":"%x","electron-owner-atoms:%x":{"composer-predictions-enabled":%t}}`, owner, owner, enabled)
		if err := os.WriteFile(tmp, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(tmp, filepath.Join(home, ".codex-global-state.json")); err != nil {
			t.Fatal(err)
		}
	}
	setEnabled(true)
	runtime := newBackendRuntime(Config{Backend: agentproto.BackendCodex, InstanceID: "instance"}).(*codexBackendRuntime)
	runtime.predictions = newTestPredictionRuntime()
	runtime.predictions.desktopPreferences = readPredictionDesktopPreferences
	runtime.predictions.cache["user\x00account"] = predictionConfigCache{
		config:  predictionConfig{Enabled: true, Prompt: "official", LiveSampleRate: 1},
		expires: time.Now().Add(time.Minute),
	}
	runtime.predictions.controls = map[string]predictionControls{"user\x00account": {Evaluated: true}}
	frames := make(chan []byte, 16)
	events := predictionEventRecorder{make(chan []agentproto.Event, 4)}
	t.Cleanup(runtime.CancelPredictions)
	runtime.HandlePrediction(context.Background(), predictionRequest(), frames, events)
	predictionAuthenticate(t, runtime, frames, "user", "account", false)
	resume := predictionFrame(t, frames)
	if resume["method"] != "thread/resume" {
		t.Fatalf("expected resume, got %#v", resume)
	}
	predictionReply(t, runtime, resume, map[string]any{"modelProvider": "openai", "model": "supported"})
	sample := predictionFrame(t, frames)
	if sample["method"] != "thread/sample" {
		t.Fatalf("expected native sample, got %#v", sample)
	}
	response, err := json.Marshal(map[string]any{"id": sample["id"], "result": map[string]any{"sourceTurnId": "turn", "text": `{"suggestion":"native suggestion"}`}})
	if err != nil {
		t.Fatal(err)
	}
	return runtime, frames, events, setEnabled, response
}

func hasPredictionCandidate(event agentproto.Event) bool {
	return event.Kind == agentproto.EventThreadPredictionResult && event.Prediction != nil && event.Prediction.Suggestion != ""
}
