package codex

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func predictionCommand(id string) agentproto.Command {
	return agentproto.Command{
		CommandID: id,
		Kind:      agentproto.CommandThreadPredictionRequest,
		Target:    agentproto.Target{ThreadID: "source-thread"},
		Prediction: agentproto.PredictionCommand{
			SourceTurnID: "source-turn", InstanceID: "instance-original",
		},
	}
}

func predictionTestOptions() PredictionOptions {
	effort := "high"
	return PredictionOptions{Prompt: "Official prediction prompt fixture", Effort: &effort, GenerationTrigger: "live"}
}

func predictionRequest(t *testing.T, tr *Translator, command agentproto.Command) map[string]any {
	t.Helper()
	var frames [][]byte
	var err error
	if command.Kind == agentproto.CommandThreadPredictionRequest {
		frames, err = tr.TranslatePredictionRequest(command, predictionTestOptions())
	} else {
		frames, err = tr.TranslateCommand(command)
	}
	if err != nil || len(frames) != 1 {
		t.Fatalf("prediction request = %d frames, %v; want one native request", len(frames), err)
	}
	var request map[string]any
	if err := json.Unmarshal(frames[0], &request); err != nil {
		t.Fatal(err)
	}
	return request
}

func predictionResponse(t *testing.T, tr *Translator, id any, result, rpcError any) Result {
	t.Helper()
	message := map[string]any{"id": id}
	if rpcError != nil {
		message["error"] = rpcError
	} else {
		message["result"] = result
	}
	raw, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	observed, err := tr.ObserveServer(raw)
	if err != nil {
		t.Fatal(err)
	}
	return observed
}

func predictionConversationState(t *testing.T, tr *Translator) string {
	t.Helper()
	raw, err := json.Marshal([]any{tr.currentThreadID, tr.activeTurnByThread,
		tr.pendingRemoteTurnByThread, tr.latestThreadStartParams, tr.latestTurnStartTemplate,
		tr.turnStartByThread, tr.newThreadTurnTemplate})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestPredictionSampleUsesOwnedRPCWithoutChangingConversation(t *testing.T) {
	tr := NewTranslator("instance-original")
	tr.currentThreadID = "active-thread"
	tr.activeTurnByThread["active-thread"] = "active-turn"
	tr.pendingRemoteTurnByThread["active-thread"] = "active-command"
	tr.latestThreadStartParams = map[string]any{"cwd": "/workspace"}
	tr.latestTurnStartTemplate = map[string]any{"model": "unchanged"}
	tr.turnStartByThread["active-thread"] = map[string]any{"effort": "high"}
	tr.newThreadTurnTemplate = map[string]any{"approvalPolicy": "never"}
	before := predictionConversationState(t, tr)
	request := predictionRequest(t, tr, predictionCommand("prediction:1"))
	if request["method"] != "thread/sample" {
		t.Fatalf("prediction must sample without fork or turn: %#v", request)
	}
	params := request["params"].(map[string]any)
	want := map[string]any{
		"threadId": "source-thread", "expectedTurnId": "source-turn",
		"prompt":                     "Official prediction prompt fixture\n\nReturn only JSON matching this schema: {\"type\":\"object\",\"properties\":{\"suggestion\":{\"type\":[\"string\",\"null\"]}},\"required\":[\"suggestion\"],\"additionalProperties\":false}.",
		"effort":                     "high",
		"responsesapiClientMetadata": map[string]any{"prediction_source_thread_id": "source-thread", "prediction_source_turn_id": "source-turn", "prediction_trigger": "live"},
		"threadSource":               "composer_predictions", "turnTrigger": "composer_predictions", "disableMcp": true,
	}
	if !reflect.DeepEqual(params, want) {
		t.Fatalf("unexpected sample params: %#v", params)
	}
	observed := predictionResponse(t, tr, request["id"], map[string]any{
		"sourceTurnId": "source-turn", "text": `{"suggestion":"继续检查测试"}`,
	}, nil)
	if !observed.Suppress || len(observed.Events) != 1 {
		t.Fatalf("owned response must suppress and emit one result: %#v", observed)
	}
	event := observed.Events[0]
	if event.Kind != agentproto.EventThreadPredictionResult || event.CommandID != "prediction:1" || event.ThreadID != "source-thread" || event.TurnID != "" || event.ErrorMessage != "" || event.Prediction == nil || *event.Prediction != (agentproto.PredictionResult{SourceTurnID: "source-turn", Suggestion: "继续检查测试"}) {
		t.Fatalf("prediction must retain original correlation only: %#v", event)
	}
	if len(observed.OutboundToCodex) != 0 || len(observed.OutboundToParent) != 0 || predictionConversationState(t, tr) != before {
		t.Fatal("sampling must not change normal conversation state or produce extra frames")
	}
}

func TestPredictionSampleStrictJSONAndEmptyText(t *testing.T) {
	for _, tc := range []struct {
		name      string
		text      any
		want      string
		wantError bool
	}{
		{"empty text", "", "", false},
		{"null tools or no text", nil, "", false},
		{"empty suggestion", `{"suggestion":""}`, "", false},
		{"invalid JSON", "not JSON", "", true},
		{"extra key", `{"suggestion":"继续","tool":"shell"}`, "\u7ee7\u7eed", false},
		{"trailing JSON", `{"suggestion":"继续"} {}`, "", true},
		{"wrong type", `{"suggestion":1}`, "", true},
		{"wrong key casing", `{"Suggestion":"继续"}`, "", true},
		{"duplicate key", `{"suggestion":"一","suggestion":"二"}`, "\u4e8c", false},
		{"null suggestion", `{"suggestion":null}`, "", false},
		{"null JSON", `null`, "", true},
		{"missing suggestion", `{}`, "", true},
		{"no desktop length cap", `{"suggestion":"` + strings.Repeat("中", 201) + `"}`, strings.Repeat("\u4e2d", 201), false},
		{"rune limit", `{"suggestion":"` + strings.Repeat("中", 200) + `"}`, strings.Repeat("中", 200), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr := NewTranslator("instance-original")
			request := predictionRequest(t, tr, predictionCommand("prediction:1"))
			observed := predictionResponse(t, tr, request["id"], map[string]any{"sourceTurnId": "source-turn", "text": tc.text}, nil)
			if !observed.Suppress || len(observed.Events) != 1 {
				t.Fatalf("owned sample response = %#v", observed)
			}
			event := observed.Events[0]
			if event.Kind != agentproto.EventThreadPredictionResult || event.Prediction == nil || event.Prediction.Suggestion != tc.want || (event.ErrorMessage != "") != tc.wantError {
				t.Fatalf("unexpected parsed prediction: %#v", event)
			}
		})
	}
}

func TestPredictionSampleRPCErrorStaysOffMainTurn(t *testing.T) {
	for _, code := range []int{-32601, -32603} {
		tr := NewTranslator("instance-original")
		tr.currentThreadID = "active-thread"
		tr.activeTurnByThread["active-thread"] = "active-turn"
		before := predictionConversationState(t, tr)
		request := predictionRequest(t, tr, predictionCommand("prediction:1"))
		observed := predictionResponse(t, tr, request["id"], nil, map[string]any{"code": code, "message": "sample failed"})
		if !observed.Suppress || len(observed.Events) != 1 {
			t.Fatalf("sample error response = %#v", observed)
		}
		event := observed.Events[0]
		if event.Kind != agentproto.EventThreadPredictionResult || event.Prediction == nil || event.Prediction.Unsupported != (code == -32601) || event.ErrorMessage == "" || event.TurnID != "" || predictionConversationState(t, tr) != before {
			t.Fatalf("sample error must remain a correlated prediction result: %#v", event)
		}
	}
}

func TestPredictionSampleInvalidRequestOnlyMarksUnknownSampleUnsupported(t *testing.T) {
	for _, tc := range []struct {
		name        string
		message     string
		unsupported bool
	}{
		{"legacy unknown sample", "Invalid request: unknown variant `thread/sample`, expected one of `initialize`, `thread/start`", true},
		{"stale source turn", "Invalid request: sample source turn must be the latest finished turn", false},
		{"source not loaded", "Invalid request: thread/sample requires a loaded source", false},
		{"different unknown method", "Invalid request: unknown variant `thread/sample/cancel`, expected one of `initialize`", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr := NewTranslator("instance-original")
			request := predictionRequest(t, tr, predictionCommand("prediction:1"))
			observed := predictionResponse(t, tr, request["id"], nil, map[string]any{"code": -32600, "message": tc.message})
			if !observed.Suppress || len(observed.Events) != 1 {
				t.Fatalf("owned invalid-request response = %#v", observed)
			}
			event := observed.Events[0]
			if event.Kind != agentproto.EventThreadPredictionResult || event.Prediction == nil || event.Prediction.Unsupported != tc.unsupported || event.ErrorMessage != tc.message || event.TurnID != "" {
				t.Fatalf("only unsupported sample method may disable prediction: prediction=%#v error=%q", event.Prediction, event.ErrorMessage)
			}
		})
	}
}

func TestPredictionCancelOnlyOwnedOriginalRPCAndSuppressesLateResults(t *testing.T) {
	tr := NewTranslator("instance-original")
	first := predictionRequest(t, tr, predictionCommand("prediction:first"))
	second := predictionRequest(t, tr, predictionCommand("prediction:second"))
	cancel := agentproto.Command{Kind: agentproto.CommandThreadPredictionCancel,
		Prediction: agentproto.PredictionCommand{RequestCommandID: "prediction:first"}}
	request := predictionRequest(t, tr, cancel)
	if request["method"] != "thread/sample/cancel" || !reflect.DeepEqual(request["params"], map[string]any{"requestId": first["id"]}) {
		t.Fatalf("cancel must target owned original RPC, not latest sample: %#v", request)
	}
	for _, framesCommand := range []agentproto.Command{cancel, {Kind: agentproto.CommandThreadPredictionCancel, Prediction: agentproto.PredictionCommand{RequestCommandID: "native-request"}}} {
		frames, err := tr.TranslateCommand(framesCommand)
		if err != nil || len(frames) != 0 {
			t.Fatalf("repeat or unknown cancellation must not send another RPC: %q %v", frames, err)
		}
	}
	ack := predictionResponse(t, tr, request["id"], nil, map[string]any{"code": -32603, "message": "already canceled"})
	late := predictionResponse(t, tr, first["id"], map[string]any{"text": `{"suggestion":"迟到"}`}, nil)
	for _, result := range []Result{ack, late} {
		if !result.Suppress || len(result.Events) != 0 || len(result.OutboundToCodex) != 0 || len(result.OutboundToParent) != 0 {
			t.Fatalf("cancel ack and canceled result must stay private: %#v", result)
		}
	}
	other := predictionResponse(t, tr, second["id"], map[string]any{"sourceTurnId": "source-turn", "text": `{"suggestion":"保留"}`}, nil)
	if !other.Suppress || len(other.Events) != 1 || other.Events[0].CommandID != "prediction:second" || other.Events[0].Prediction.Suggestion != "保留" {
		t.Fatalf("cancel must preserve other owned prediction: %#v", other)
	}
}

func TestPredictionSampleTextRequiresActualSourceTurn(t *testing.T) {
	for _, sourceTurnID := range []string{"", "different-turn"} {
		t.Run("source-"+sourceTurnID, func(t *testing.T) {
			tr := NewTranslator("instance-original")
			request := predictionRequest(t, tr, predictionCommand("prediction:1"))
			native := map[string]any{"text": `{"suggestion":"不能显示"}`}
			if sourceTurnID != "" {
				native["sourceTurnId"] = sourceTurnID
			}
			observed := predictionResponse(t, tr, request["id"], native, nil)
			if !observed.Suppress || len(observed.Events) != 1 {
				t.Fatalf("invalid sample source must remain an owned response: %#v", observed)
			}
			event := observed.Events[0]
			if event.Kind != agentproto.EventThreadPredictionResult || event.Prediction == nil || event.Prediction.Suggestion != "" || event.ErrorMessage == "" || event.Prediction.SourceTurnID != sourceTurnID {
				t.Fatalf("missing or mismatched native source must not be replaced by expected source: prediction=%#v error=%q", event.Prediction, event.ErrorMessage)
			}
		})
	}
}

func TestPredictionSampleNoTextDoesNotRequireNativeSourceTurn(t *testing.T) {
	tr := NewTranslator("instance-original")
	request := predictionRequest(t, tr, predictionCommand("prediction:1"))
	observed := predictionResponse(t, tr, request["id"], map[string]any{"text": nil}, nil)
	if !observed.Suppress || len(observed.Events) != 1 || observed.Events[0].ErrorMessage != "" || observed.Events[0].Prediction == nil || observed.Events[0].Prediction.Suggestion != "" {
		t.Fatalf("no-text/tool sample must complete quietly: %#v", observed)
	}
}

func TestPredictionNativeIDsPassThrough(t *testing.T) {
	tr := NewTranslator("instance-original")
	for _, raw := range []string{
		`{"id":"native-prediction","method":"thread/sample","params":{"threadId":"native-thread"}}`,
		`{"id":"native-cancel","method":"thread/sample/cancel","params":{"requestId":"native-prediction"}}`,
	} {
		result, err := tr.ObserveClient([]byte(raw))
		if err != nil || result.Suppress || len(result.Events) != 0 || len(result.OutboundToCodex) != 0 {
			t.Fatalf("native sampling must pass through: %#v %v", result, err)
		}
	}
	result := predictionResponse(t, tr, "prediction-sample-unowned", map[string]any{"text": `{"suggestion":"native"}`}, nil)
	if result.Suppress || len(result.Events) != 0 {
		t.Fatalf("unowned response must pass through: %#v", result)
	}
}

func TestPredictionMissingParametersAreRejected(t *testing.T) {
	for _, missing := range []string{"command", "thread", "turn", "cancel reference"} {
		t.Run(missing, func(t *testing.T) {
			command := predictionCommand("prediction:1")
			switch missing {
			case "command":
				command.CommandID = " "
			case "thread":
				command.Target.ThreadID = " "
			case "turn":
				command.Prediction.SourceTurnID = " "
			case "cancel reference":
				command.Kind = agentproto.CommandThreadPredictionCancel
			}
			tr := NewTranslator("instance-original")
			var frames [][]byte
			var err error
			if command.Kind == agentproto.CommandThreadPredictionRequest {
				frames, err = tr.TranslatePredictionRequest(command, predictionTestOptions())
			} else {
				frames, err = tr.TranslateCommand(command)
			}
			if err == nil || len(frames) != 0 {
				t.Fatalf("missing %s must fail before upstream request: %q %v", missing, frames, err)
			}
		})
	}
}
