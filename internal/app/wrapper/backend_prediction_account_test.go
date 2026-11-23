package wrapper

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

const chatGPTAccountNotice = `{"method":"account/updated","params":{"authMode":"chatgpt"}}`

func TestPredictionInitialAuthReadDetectsDifferentPrincipalWithoutNotice(t *testing.T) {
	runtime, older, frames, _ := accountPredictionSample(t)
	command := predictionRequest()
	command.CommandID = "new-request"
	events := predictionEventRecorder{make(chan []agentproto.Event, 4)}
	runtime.HandlePrediction(context.Background(), command, frames, events)
	auth := predictionFrame(t, frames)
	reply, _ := json.Marshal(map[string]any{"id": auth["id"], "result": map[string]any{
		"authMethod": "chatgpt", "authToken": predictionToken(t, "user", "different-account"),
	}})
	observed, err := runtime.ObserveServer(reply)
	if err != nil || !observed.Suppress || len(observed.Events) != 1 || observed.Events[0].Kind != agentproto.EventThreadPredictionInvalidated || older.Err() == nil {
		t.Fatalf("native auth read must revoke work of the earlier principal: %#v %v", observed, err)
	}
}

func TestPredictionAccountProbeDeadlineRevokesDraftAndSkipsQueuedProbe(t *testing.T) {
	runtime, ctx, frames, _ := accountPredictionSample(t)
	events := predictionEventRecorder{make(chan []agentproto.Event, 1)}
	runtime.predictionAccount.sender = events
	if _, err := runtime.ObserveServer([]byte(chatGPTAccountNotice)); err != nil {
		t.Fatal(err)
	}
	probe := <-frames
	runtime.mu.Lock()
	runtime.predictionAccount.probeTimer.Reset(time.Millisecond)
	runtime.mu.Unlock()
	select {
	case batch := <-events.events:
		if len(batch) != 1 || batch[0].Kind != agentproto.EventThreadPredictionInvalidated || ctx.Err() == nil {
			t.Fatalf("account timeout did not revoke the older principal: %#v", batch)
		}
	case <-time.After(time.Second):
		t.Fatal("account deadline sent no invalidation")
	}
	var native bytes.Buffer
	if err := writeChildFrameForRuntime(&native, probe, runtime, nil, nil, nil); err != nil || native.Len() != 0 {
		t.Fatalf("expired account probe crossed the child write boundary: %s %v", native.Bytes(), err)
	}
}

func TestPredictionQueuedSampleWaitsAtNativeWriterForAccountProbe(t *testing.T) {
	for _, account := range []string{"account", "different-account"} {
		t.Run(account, func(t *testing.T) {
			runtime, _, frames, sampleResponse := accountPredictionSample(t)
			sampleID := predictionWriteFrame(sampleResponse).ID
			runtime.predictions.jobs[predictionRequest().CommandID].sampleID = sampleID
			queued, _ := json.Marshal(map[string]any{"id": sampleID, "method": "thread/sample", "params": map[string]any{}})

			if _, err := runtime.ObserveServer([]byte(chatGPTAccountNotice)); err != nil {
				t.Fatal(err)
			}
			probe := predictionFrame(t, frames)
			var native bytes.Buffer
			if err := writeChildFrameForRuntime(&native, queued, runtime, nil, nil, nil); err != nil || native.Len() != 0 {
				t.Fatalf("unverified queued sample crossed the child write boundary: %s %v", native.Bytes(), err)
			}
			verified, _ := json.Marshal(map[string]any{"id": probe["id"], "result": map[string]any{
				"authMethod": "chatgpt", "authToken": predictionToken(t, "user", account),
			}})
			observed, err := runtime.ObserveServer(verified)
			if err != nil || !observed.Suppress {
				t.Fatalf("native verification response escaped: %#v %v", observed, err)
			}
			if account == "account" {
				delayed := <-frames
				if err := writeChildFrameForRuntime(&native, delayed, runtime, nil, nil, nil); err != nil || !bytes.Equal(native.Bytes(), queued) {
					t.Fatalf("same-principal sample was not resumed at the writer: %s %v", native.Bytes(), err)
				}
			} else if err := writeChildFrameForRuntime(&native, queued, runtime, nil, nil, nil); err != nil || native.Len() != 0 || len(frames) != 0 {
				t.Fatalf("different principal reused a previously queued sample: %s %v", native.Bytes(), err)
			}
		})
	}
}

func TestPredictionReservedIDStemDoesNotInterceptOrdinaryTraffic(t *testing.T) {
	runtime := New(Config{Backend: agentproto.BackendCodex, InstanceID: "prediction-prefix-test"}).runtime.(*codexBackendRuntime)
	for _, input := range []string{
		`{"id":"remote-prediction-prep:parent","method":"thread/read"}`,
		`{"id":"relay-prediction:parent","method":"thread/sample"}`,
		`{"id":"remote-prediction-account:parent","method":"getAuthStatus"}`,
	} {
		if !runtime.allowPredictionWrite([]byte(input)) {
			t.Error("ordinary parent request rejected:", input)
		}
	}
	runtime.predictions = newPredictionRuntime()
	observed, err := runtime.ObserveServer([]byte(`{"id":"remote-prediction-account:parent","result":{}}`))
	if err != nil || observed.Suppress {
		t.Fatalf("ordinary parent response suppressed: %#v, err=%v", observed, err)
	}
}
