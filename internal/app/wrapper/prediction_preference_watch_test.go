package wrapper

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestPredictionDesktopWatchStopsAfterNoCandidate(t *testing.T) {
	for _, scenario := range []struct {
		name, field, payload string
	}{
		{"empty", "result", `{"sourceTurnId":"turn","text":"{}"}`},
		{"native failure", "error", `{"code":-32603,"message":"native failure"}`},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			runtime, _, _, _, response := startDesktopSettingSample(t)
			var sample map[string]json.RawMessage
			if err := json.Unmarshal(response, &sample); err != nil {
				t.Fatal(err)
			}
			reply, err := json.Marshal(map[string]json.RawMessage{
				"id": sample["id"], scenario.field: json.RawMessage(scenario.payload),
			})
			if err != nil {
				t.Fatal(err)
			}
			observed, err := runtime.ObserveServer(reply)
			if err != nil || len(observed.Events) != 1 || hasPredictionCandidate(observed.Events[0]) {
				t.Fatalf("expected quiet prediction completion: %#v, %v", observed.Events, err)
			}
			assertPredictionPreferenceWatchStopped(t, runtime)
		})
	}
}

func TestPredictionDesktopWatchStopsAfterCancellation(t *testing.T) {
	runtime, frames, events, _, _ := startDesktopSettingSample(t)
	runtime.HandlePrediction(context.Background(), agentproto.Command{Kind: agentproto.CommandThreadPredictionCancel,
		Prediction: agentproto.PredictionCommand{RequestCommandID: predictionRequest().CommandID}}, frames, events)
	assertPredictionPreferenceWatchStopped(t, runtime)
}

func TestPredictionDesktopWatchStopsAfterTimeout(t *testing.T) {
	runtime, _, events, _, _ := startDesktopSettingSample(t)
	runtime.mu.Lock()
	job := runtime.predictions.jobs[predictionRequest().CommandID]
	runtime.mu.Unlock()
	runtime.waitPredictionSample(job, time.NewTimer(0), events)
	assertPredictionPreferenceWatchStopped(t, runtime)
}

func TestPredictionDesktopWatchKeepsPublishedCandidateAfterNewSampleFails(t *testing.T) {
	runtime, frames, events, setEnabled, reply := startDesktopSettingSample(t)
	result, err := runtime.ObserveServer(reply)
	if err != nil || len(result.Events) != 1 || !hasPredictionCandidate(result.Events[0]) {
		t.Fatalf("expected earlier published candidate: %#v, %v", result.Events, err)
	}
	request := predictionRequest()
	request.CommandID += ":next"
	request.Target.ThreadID = "next-thread"
	request.Prediction.SourceTurnID = "next-turn"
	runtime.HandlePrediction(context.Background(), request, frames, events)
	sample := predictionTimeoutSample(t, runtime, frames)
	failure, _ := json.Marshal(map[string]any{"id": sample["id"], "error": map[string]any{"code": -32603, "message": "native failure"}})
	if _, err := runtime.ObserveServer(failure); err != nil {
		t.Fatal(err)
	}
	setEnabled(false)
	select {
	case batch := <-events.events:
		if len(batch) != 1 || batch[0].Kind != agentproto.EventThreadPredictionInvalidated {
			t.Fatalf("expected invalidation of earlier published candidate: %#v", batch)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("watch stopped while an earlier published candidate could still be usable")
	}
}

func TestPredictionDesktopWatchOwnerOffOverridesNativeFallback(t *testing.T) {
	runtime, frames, _ := predictionTimeoutFixture(t)
	var ownerDisabled atomic.Bool
	runtime.predictions.desktopPreferences = func(context.Context, predictionIdentity) (predictionDesktopPreferences, error) {
		if !ownerDisabled.Load() {
			return predictionDesktopPreferences{}, nil
		}
		enabled := false
		return predictionDesktopPreferences{owner: &enabled}, nil
	}
	predictionAuthenticate(t, runtime, frames, "user", "account", false)
	configRead := predictionFrame(t, frames)
	if configRead["method"] != "config/read" {
		t.Fatalf("expected native read when owner preference was absent: %#v", configRead)
	}
	predictionReply(t, runtime, configRead, map[string]any{"config": map[string]any{"desktop": map[string]any{"composer-predictions-enabled": true}}})
	resume := predictionFrame(t, frames)
	if resume["method"] != "thread/resume" {
		t.Fatalf("expected resume after obtaining native fallback: %#v", resume)
	}
	predictionReply(t, runtime, resume, map[string]any{"modelProvider": "openai", "model": "supported"})
	sample := predictionFrame(t, frames)
	if sample["method"] != "thread/sample" {
		t.Fatalf("expected native sample: %#v", sample)
	}
	ownerDisabled.Store(true)
	reply, _ := json.Marshal(map[string]any{"id": sample["id"], "result": map[string]any{"sourceTurnId": "turn", "text": `{"suggestion":"native suggestion"}`}})
	result, err := runtime.ObserveServer(reply)
	if err != nil || len(result.Events) != 1 || result.Events[0].Kind != agentproto.EventThreadPredictionInvalidated {
		t.Fatalf("explicit owner opt-out did not override native fallback: %#v, %v", result.Events, err)
	}
}

func assertPredictionPreferenceWatchStopped(t *testing.T, runtime *codexBackendRuntime) {
	t.Helper()
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.predictions.preferenceWatch != nil {
		t.Fatal("Desktop preference is still being watched without a running sample or published candidate")
	}
}
