package wrapper

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestPredictionInterruptFollowsOfficialGateAndNativeVersion(t *testing.T) {
	for _, tc := range []struct {
		name, userAgent string
		gate, want      bool
	}{
		{"headless-supported", "Codex Remote Headless/0.169.0-alpha.11.1 (Mac OS 26.7.3; arm64) dumb (Codex Remote Headless; diagnostic)", true, true},
		{"desktop-supported", "Codex Desktop/0.167.0-alpha.1 (Mac OS 26.7.3; arm64)", true, true},
		{"official-gate-off", "Codex Remote Headless/0.169.0-alpha.11.1", false, false},
		{"headless-too-old", "Codex Remote Headless/0.167.0-alpha.0", true, false},
		{"unknown-client", "Other Client/0.169.0-alpha.11.1", true, false},
		{"missing-initialization", "", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runtime := newBackendRuntime(Config{Backend: agentproto.BackendCodex, InstanceID: "instance"}).(*codexBackendRuntime)
			runtime.predictions = newTestPredictionRuntime()
			runtime.predictions.cache["user\x00account"] = predictionConfigCache{
				config:  predictionConfig{Enabled: true, Prompt: "cached official prompt", LiveSampleRate: 1},
				expires: time.Now().Add(time.Minute),
			}
			runtime.predictions.controls = map[string]predictionControls{"user\x00account": {InterruptOnCancel: tc.gate}}
			initialized, err := json.Marshal(map[string]any{"result": map[string]any{"userAgent": tc.userAgent}})
			if err != nil {
				t.Fatal(err)
			}
			runtime.observePredictionInitialization(initialized)
			frames := make(chan []byte, 16)
			events := predictionEventRecorder{make(chan []agentproto.Event, 4)}
			defer runtime.CancelPredictions()
			runtime.HandlePrediction(context.Background(), predictionRequest(), frames, events)
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
			value, present := sample["params"].(map[string]any)["interruptOnCancel"]
			if present != tc.want || (present && value != true) {
				t.Fatalf("interruptOnCancel=%v present=%t, want enabled=%t", value, present, tc.want)
			}
		})
	}
}
