package codex

import (
	"encoding/json"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestNativeDefaultCollaborationSettingsUseResolvedThreadModel(t *testing.T) {
	for _, plan := range []string{"off", "on"} {
		for _, resume := range []bool{false, true} {
			name := plan + "/start"
			if resume {
				name = plan + "/resume"
			}
			t.Run(name, func(t *testing.T) {
				tr := NewTranslator("test")
				cmd := agentproto.Command{Kind: agentproto.CommandPromptSend, Target: agentproto.Target{CWD: t.TempDir()}, Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}}}, Overrides: agentproto.PromptOverrides{PlanMode: plan}, CodexResume: &agentproto.CodexResumePolicy{Mode: agentproto.CodexResumeApplyTargetProfile, ModelProviderID: "openai", ModelMode: agentproto.CodexThreadValueDefault, ReasoningMode: agentproto.CodexThreadValueDefault}}
				if resume {
					cmd.Target.ThreadID = "thread-1"
				}
				frames, err := tr.TranslateCommand(cmd)
				if err != nil || len(frames) != 1 {
					t.Fatalf("prepare: %v", err)
				}
				var request map[string]any
				if err = json.Unmarshal(frames[0], &request); err != nil {
					t.Fatal(err)
				}
				response, _ := json.Marshal(map[string]any{"id": request["id"], "result": map[string]any{"thread": map[string]any{"id": "thread-1"}, "model": "actual-native-model", "modelProvider": "openai", "reasoningEffort": "high"}})
				result, err := tr.ObserveServer(response)
				if err != nil || len(result.OutboundToCodex) != 1 {
					t.Fatalf("follow-up: %v %#v", err, result)
				}
				var turn map[string]any
				if err = json.Unmarshal(result.OutboundToCodex[0], &turn); err != nil {
					t.Fatal(err)
				}
				params := turn["params"].(map[string]any)
				collaboration, _ := params["collaborationMode"].(map[string]any)
				settings, _ := collaboration["settings"].(map[string]any)
				mode := "default"
				if plan == "on" {
					mode = "plan"
				}
				if collaboration["mode"] != mode || settings["model"] != "actual-native-model" || settings["reasoning_effort"] != "high" {
					t.Fatalf("invalid collaboration settings: %#v", collaboration)
				}
			})
		}
	}
}

func TestCollaborationModeRejectsUnknownModelWithoutClaimingTurn(t *testing.T) {
	tr := NewTranslator("test")
	if _, err := tr.ObserveClient([]byte(`{"method":"turn/start","params":{"threadId":"thread-1"}}`)); err != nil {
		t.Fatal(err)
	}
	frames, err := tr.TranslateCommand(agentproto.Command{Kind: agentproto.CommandPromptSend, Origin: agentproto.Origin{Surface: "surface-1"}, Target: agentproto.Target{ThreadID: "thread-1"}, Overrides: agentproto.PromptOverrides{PlanMode: "on"}})
	if err == nil || len(frames) != 0 {
		t.Fatalf("unknown model produced an upstream request: frames=%d err=%v", len(frames), err)
	}
	if _, exists := tr.pendingRemoteTurnByThread["thread-1"]; exists {
		t.Fatal("rejected request retained remote turn ownership")
	}
}
