package wrapper

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func predictionTimeoutFixture(t *testing.T) (*codexBackendRuntime, chan []byte, predictionEventRecorder) {
	t.Helper()
	runtime := newBackendRuntime(Config{Backend: agentproto.BackendCodex, InstanceID: "instance"}).(*codexBackendRuntime)
	runtime.predictions = newTestPredictionRuntime()
	runtime.predictions.cache["user\x00account"] = predictionConfigCache{
		config: predictionConfig{Enabled: true, Prompt: "official prompt", LiveSampleRate: 1}, expires: time.Now().Add(time.Minute),
	}
	runtime.predictions.controls = map[string]predictionControls{"user\x00account": {}}
	frames := make(chan []byte, 16)
	events := predictionEventRecorder{make(chan []agentproto.Event, 4)}
	t.Cleanup(runtime.CancelPredictions)
	runtime.HandlePrediction(context.Background(), predictionRequest(), frames, events)
	return runtime, frames, events
}

func predictionTimeoutSample(t *testing.T, runtime *codexBackendRuntime, frames chan []byte) map[string]any {
	t.Helper()
	predictionAuthenticate(t, runtime, frames, "user", "account", false)
	resume := predictionFrame(t, frames)
	if resume["method"] != "thread/resume" {
		t.Fatalf("unexpected preparation: %#v", resume)
	}
	predictionReply(t, runtime, resume, map[string]any{"modelProvider": "openai", "model": "supported"})
	sample := predictionFrame(t, frames)
	if sample["method"] != "thread/sample" {
		t.Fatalf("unexpected sample: %#v", sample)
	}
	return sample
}

func TestPredictionPreparationDoesNotStartSampleDeadline(t *testing.T) {
	runtime, frames, _ := predictionTimeoutFixture(t)
	_ = predictionFrame(t, frames)
	runtime.mu.Lock()
	_, hasDeadline := runtime.predictions.jobs[predictionRequest().CommandID].ctx.Deadline()
	runtime.mu.Unlock()
	if hasDeadline {
		t.Fatal("preparation must not start a total deadline that reduces the native sample budget")
	}
}

func TestPredictionNativeSampleTimeoutCancelsAndCompletesOnce(t *testing.T) {
	t.Parallel()
	runtime, frames, events := predictionTimeoutFixture(t)
	sample := predictionTimeoutSample(t, runtime, frames)
	started := time.Now()
	select {
	case raw := <-frames:
		var canceled map[string]any
		if err := json.Unmarshal(raw, &canceled); err != nil {
			t.Fatal(err)
		}
		if elapsed := time.Since(started); elapsed < 29*time.Second || elapsed > 32*time.Second {
			t.Fatalf("native sample timeout after %s, want the desktop's 30-second response budget", elapsed)
		}
		if canceled["method"] != "thread/sample/cancel" || canceled["params"].(map[string]any)["requestId"] != sample["id"] {
			t.Fatalf("timeout canceled a different request: %#v", canceled)
		}
	case <-time.After(33 * time.Second):
		t.Fatal("native sample timeout did not cancel the pending request")
	}
	select {
	case batch := <-events.events:
		if len(batch) != 1 || batch[0].CommandID != predictionRequest().CommandID || batch[0].Prediction == nil || batch[0].Prediction.Suggestion != "" || batch[0].Prediction.SourceTurnID != "turn" {
			t.Fatalf("timeout must end only its own prediction quietly: %#v", batch)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout left the orchestrator waiting")
	}
	response, _ := json.Marshal(map[string]any{"id": sample["id"], "result": map[string]any{"sourceTurnId": "turn", "text": `{"suggestion":"late"}`}})
	result, err := runtime.ObserveServer(response)
	if err != nil || !result.Suppress || len(result.Events) != 0 {
		t.Fatalf("late result escaped after native timeout: %#v %v", result, err)
	}
	select {
	case batch := <-events.events:
		t.Fatalf("timeout completed twice: %#v", batch)
	default:
	}
}
