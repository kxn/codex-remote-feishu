package codex

import (
	"encoding/json"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestPrivatePromptWithoutModelKeepsNativeChoice(t *testing.T) {
	for _, plan := range []string{"", "off", "on"} {
		t.Run(plan, func(t *testing.T) {
			tr := NewTranslator("instance")
			frames, err := tr.TranslateCommand(agentproto.Command{Kind: agentproto.CommandPromptSend, Origin: agentproto.Origin{ChatID: "surface"}, Target: agentproto.Target{CWD: "/work"}, Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hi"}}}, Overrides: agentproto.PromptOverrides{PlanMode: plan}})
			if err != nil || len(frames) != 1 {
				t.Fatalf("thread start: %v %d", err, len(frames))
			}
			var start map[string]any
			if err = json.Unmarshal(frames[0], &start); err != nil {
				t.Fatal(err)
			}
			params, _ := start["params"].(map[string]any)
			if params["model"] != nil {
				t.Fatalf("thread model invented: %v", params["model"])
			}
			result, err := tr.ObserveServer([]byte(`{"id":"relay-thread-start-0","result":{"thread":{"id":"thread"}}}`))
			if err != nil || len(result.OutboundToCodex) != 1 {
				t.Fatalf("turn start: %v %d", err, len(result.OutboundToCodex))
			}
			var turn map[string]any
			if err = json.Unmarshal(result.OutboundToCodex[0], &turn); err != nil {
				t.Fatal(err)
			}
			params, _ = turn["params"].(map[string]any)
			if params["model"] != nil {
				t.Fatalf("turn model invented: %v", params["model"])
			}
			if mode, ok := params["collaborationMode"].(map[string]any); ok {
				if settings, ok := mode["settings"].(map[string]any); ok && settings["model"] != nil {
					t.Fatalf("collaboration model invented: %v", settings["model"])
				}
			}
		})
	}
}

func TestPrivatePlanUsesExactNativeThreadResponseModel(t *testing.T) {
	for _, plan := range []string{"off", "on"} {
		t.Run(plan, func(t *testing.T) {
			tr := NewTranslator("instance")
			cmd := agentproto.Command{CommandID: "command", Kind: agentproto.CommandPromptSend, NativeBinding: &agentproto.NativeTurnBinding{Owner: agentproto.NativeOwner{Application: "private"}}, Target: agentproto.Target{CWD: "/work", ExecutionMode: agentproto.PromptExecutionModeStartNew}, Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hi"}}}, Overrides: agentproto.PromptOverrides{PlanMode: plan}}
			frames, err := tr.TranslateCommand(cmd)
			if err != nil || len(frames) != 1 {
				t.Fatalf("start %v", err)
			}
			result, err := tr.ObserveServer([]byte(`{"id":"relay-thread-start-0","result":{"thread":{"id":"thread"},"model":"actual-native-model"}}`))
			if err != nil || len(result.OutboundToCodex) != 1 {
				t.Fatalf("native response %v %+v", err, result)
			}
			var frame struct {
				Params map[string]any `json:"params"`
			}
			if err = json.Unmarshal(result.OutboundToCodex[0], &frame); err != nil {
				t.Fatal(err)
			}
			mode, _ := frame.Params["collaborationMode"].(map[string]any)
			settings, _ := mode["settings"].(map[string]any)
			if settings["model"] != "actual-native-model" || frame.Params["model"] != nil {
				t.Fatalf("model was not sourced from exact native response: %+v", frame.Params)
			}
			if cmd.Overrides.Model != "" {
				t.Fatal("native model mutated user override")
			}
			// An explicit selection always wins over the previously observed value.
			cmd.Target = agentproto.Target{ThreadID: "thread", CWD: "/work", ExecutionMode: agentproto.PromptExecutionModeResumeExisting}
			cmd.Overrides.Model = "user-choice"
			frames, err = tr.TranslateCommand(cmd)
			if err != nil || len(frames) != 1 {
				t.Fatal(err)
			}
			if err = json.Unmarshal(frames[0], &frame); err != nil {
				t.Fatal(err)
			}
			mode, _ = frame.Params["collaborationMode"].(map[string]any)
			settings, _ = mode["settings"].(map[string]any)
			if settings["model"] != "user-choice" {
				t.Fatalf("old native cache overrode choice: %+v", frame.Params)
			}
		})
	}
}

func TestPrivatePlanUnknownModelResumesAndMissingModelFailsExplicitly(t *testing.T) {
	tr := NewTranslator("instance")
	tr.currentThreadID = "thread"
	cmd := agentproto.Command{CommandID: "command", Kind: agentproto.CommandPromptSend, NativeBinding: &agentproto.NativeTurnBinding{Owner: agentproto.NativeOwner{Application: "private"}}, Target: agentproto.Target{ThreadID: "thread", CWD: "/work", ExecutionMode: agentproto.PromptExecutionModeResumeExisting}, Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hi"}}}, Overrides: agentproto.PromptOverrides{PlanMode: "on"}}
	frames, err := tr.TranslateCommand(cmd)
	if err != nil || len(frames) != 1 {
		t.Fatal(err)
	}
	var frame struct {
		ID     string `json:"id"`
		Method string `json:"method"`
	}
	if err = json.Unmarshal(frames[0], &frame); err != nil {
		t.Fatal(err)
	}
	if frame.Method != "thread/resume" {
		t.Fatalf("unobserved model used directly: %s", frame.Method)
	}
	raw, _ := json.Marshal(map[string]any{"id": frame.ID, "result": map[string]any{"thread": map[string]any{"id": "thread"}}})
	result, err := tr.ObserveServer(raw)
	if err != nil || len(result.OutboundToCodex) != 0 || len(result.Events) != 1 {
		t.Fatalf("missing model not explicitly failed: %+v %v", result, err)
	}
	ev := result.Events[0]
	if ev.TurnCompletionOrigin != agentproto.TurnCompletionOriginThreadResumeMissingModel || ev.RequestID != frame.ID || ev.CommandID != "command" || ev.ErrorMessage == "" {
		t.Fatalf("bad protocol failure: %+v", ev)
	}
	// A later explicit user selection must be usable without guessing a model.
	cmd.Overrides.Model = "user-choice"
	frames, err = tr.TranslateCommand(cmd)
	if err != nil || len(frames) != 1 {
		t.Fatalf("explicit retry failed: %v", err)
	}
	if err = json.Unmarshal(frames[0], &frame); err != nil || frame.Method != "turn/start" {
		t.Fatalf("explicit retry frame=%+v err=%v", frame, err)
	}
}

func TestPrivateNoPlanOverrideNeverSendsInheritedCollaborationMode(t *testing.T) {
	tr := NewTranslator("instance")
	tr.currentThreadID = "thread"
	tr.turnStartByThread["thread"] = map[string]any{"collaborationMode": map[string]any{"mode": "plan", "settings": map[string]any{"model": "old"}}}
	cmd := agentproto.Command{Kind: agentproto.CommandPromptSend, NativeBinding: &agentproto.NativeTurnBinding{Owner: agentproto.NativeOwner{Application: "private"}}, Target: agentproto.Target{ThreadID: "thread"}, Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hi"}}}}
	frames, err := tr.TranslateCommand(cmd)
	if err != nil || len(frames) != 1 {
		t.Fatal(err)
	}
	var frame struct {
		Params map[string]any `json:"params"`
	}
	if err = json.Unmarshal(frames[0], &frame); err != nil {
		t.Fatal(err)
	}
	if frame.Params["collaborationMode"] != nil {
		t.Fatalf("stale collaboration mode leaked: %+v", frame.Params)
	}
}

func TestPrivateTurnStartActualInvalidRequestResponse(t *testing.T) {
	tr := NewTranslator("instance")
	cmd := agentproto.Command{CommandID: "command", Kind: agentproto.CommandPromptSend, NativeBinding: &agentproto.NativeTurnBinding{Owner: agentproto.NativeOwner{Application: "private"}}, Target: agentproto.Target{CWD: "/work", ExecutionMode: agentproto.PromptExecutionModeStartNew}, Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hi"}}}}
	if _, err := tr.TranslateCommand(cmd); err != nil {
		t.Fatal(err)
	}
	result, err := tr.ObserveServer([]byte(`{"id":"relay-thread-start-0","result":{"thread":{"id":"thread"},"model":"actual"}}`))
	if err != nil || len(result.OutboundToCodex) != 1 {
		t.Fatalf("thread response: %+v %v", result, err)
	}
	var frame struct {
		ID string `json:"id"`
	}
	if err = json.Unmarshal(result.OutboundToCodex[0], &frame); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"id": frame.ID, "error": map[string]any{"code": -32600, "message": "Invalid request: missing field settings"}})
	result, err = tr.ObserveServer(raw)
	if err != nil || len(result.Events) != 1 {
		t.Fatalf("error response: %+v %v", result, err)
	}
	event := result.Events[0]
	if event.CommandID != "command" || event.RequestID != frame.ID || event.TurnCompletionOrigin != agentproto.TurnCompletionOriginTurnStartRejected || event.ErrorMessage != "Invalid request: missing field settings" {
		t.Fatalf("unbound error: %+v", event)
	}
}
