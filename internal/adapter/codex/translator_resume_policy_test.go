package codex

import (
	"encoding/json"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestTranslatePromptSendApplyTargetProfilePolicyUsesTypedStartPayload(t *testing.T) {
	tr := NewTranslator("inst-1")
	if _, err := tr.ObserveClient([]byte(`{"method":"thread/start","params":{"cwd":"/tmp/old","modelProvider":"old-provider","model":"old-model","config":{"model_reasoning_effort":"low","review_model":"old-review"}}}`)); err != nil {
		t.Fatalf("seed local thread/start template: %v", err)
	}

	commands, err := tr.TranslateCommand(agentproto.Command{
		Kind:   agentproto.CommandPromptSend,
		Origin: agentproto.Origin{ChatID: "surface-1"},
		Target: agentproto.Target{CWD: "/tmp/project"},
		Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}}},
		CodexResume: &agentproto.CodexResumePolicy{
			Mode:             agentproto.CodexResumeApplyTargetProfile,
			ModelProviderID:  "codex_remote_profile_team",
			ModelMode:        agentproto.CodexThreadValueExplicit,
			Model:            "gpt-5.4",
			ReviewModelMode:  agentproto.CodexReviewModelExplicit,
			ReviewModel:      "gpt-5.4-review",
			ReasoningMode:    agentproto.CodexThreadValueExplicit,
			ReasoningEffort:  "high",
			ContextMode:      "extended_1m",
			ContextWindow:    1000000,
			AutoCompactLimit: 900000,
		},
	})
	if err != nil {
		t.Fatalf("translate command: %v", err)
	}
	payload := decodeSinglePayload(t, commands)
	params := payloadParams(t, payload, "thread/start")
	if params["modelProvider"] != "codex_remote_profile_team" || params["model"] != "gpt-5.4" {
		t.Fatalf("expected typed profile provider/model, got %#v", params)
	}
	config := payloadConfig(t, params)
	if config["model_reasoning_effort"] != "high" || config["reasoning_effort"] != "high" || config["review_model"] != "gpt-5.4-review" {
		t.Fatalf("expected explicit thread policy in config, got %#v", config)
	}
	if config["model_context_window"] != float64(1000000) || config["model_auto_compact_token_limit"] != float64(900000) {
		t.Fatalf("expected context policy in config, got %#v", config)
	}
	if params["cwd"] != "/tmp/project" {
		t.Fatalf("expected target cwd, got %#v", params)
	}
}

func TestTranslatePromptSendApplyTargetProfileDefaultPolicyOmitsModelReasoningAndTemplate(t *testing.T) {
	tr := NewTranslator("inst-1")
	if _, err := tr.ObserveClient([]byte(`{"method":"thread/start","params":{"cwd":"/tmp/old","modelProvider":"old-provider","model":"old-model","config":{"model_reasoning_effort":"low","review_model":"old-review"}}}`)); err != nil {
		t.Fatalf("seed local thread/start template: %v", err)
	}
	if _, err := tr.ObserveClient([]byte(`{"method":"thread/resume","params":{"threadId":"thread-current","cwd":"/tmp/current"}}`)); err != nil {
		t.Fatalf("seed current thread: %v", err)
	}

	commands, err := tr.TranslateCommand(agentproto.Command{
		Kind:   agentproto.CommandPromptSend,
		Origin: agentproto.Origin{ChatID: "surface-1"},
		Target: agentproto.Target{ThreadID: "thread-1", CWD: "/tmp/project"},
		Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}}},
		CodexResume: &agentproto.CodexResumePolicy{
			Mode:            agentproto.CodexResumeApplyTargetProfile,
			ModelProviderID: "openai",
			ModelMode:       agentproto.CodexThreadValueDefault,
			ReviewModelMode: agentproto.CodexReviewModelConfig,
			ReasoningMode:   agentproto.CodexThreadValueDefault,
			ContextMode:     "codex_default",
		},
	})
	if err != nil {
		t.Fatalf("translate command: %v", err)
	}
	params := payloadParams(t, decodeSinglePayload(t, commands), "thread/resume")
	if params["modelProvider"] != "openai" {
		t.Fatalf("expected target provider on resume, got %#v", params)
	}
	if _, exists := params["model"]; exists {
		t.Fatalf("default model policy must omit model, got %#v", params)
	}
	config := payloadConfig(t, params)
	for _, key := range []string{"model_reasoning_effort", "reasoning_effort", "review_model", "model_context_window", "model_auto_compact_token_limit"} {
		if _, exists := config[key]; exists {
			t.Fatalf("default policy must omit %s, got %#v", key, config)
		}
	}
}

func TestTranslatePromptSendApplyTargetProfilePolicyAddsTurnStartCollaborationSettings(t *testing.T) {
	tr := NewTranslator("inst-1")
	if _, err := tr.ObserveClient([]byte(`{"method":"turn/start","params":{"threadId":"thread-1","cwd":"/tmp/project","collaborationMode":{"mode":"custom","settings":{"model":"gpt-5.5","reasoning_effort":"xhigh"}}}}`)); err != nil {
		t.Fatalf("seed current turn template: %v", err)
	}

	commands, err := tr.TranslateCommand(agentproto.Command{
		Kind:   agentproto.CommandPromptSend,
		Origin: agentproto.Origin{ChatID: "surface-1"},
		Target: agentproto.Target{ThreadID: "thread-1", CWD: "/tmp/project"},
		Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}}},
		Overrides: agentproto.PromptOverrides{
			AccessMode: agentproto.AccessModeFullAccess,
			PlanMode:   "off",
		},
		CodexResume: &agentproto.CodexResumePolicy{
			Mode:            agentproto.CodexResumeApplyTargetProfile,
			ModelProviderID: "codex_remote_profile_deepseek",
			ModelMode:       agentproto.CodexThreadValueExplicit,
			Model:           "deepseek-v4-flash",
			ReasoningMode:   agentproto.CodexThreadValueExplicit,
			ReasoningEffort: "high",
			ContextMode:     "extended_1m",
		},
	})
	if err != nil {
		t.Fatalf("translate command: %v", err)
	}

	params := payloadParams(t, decodeSinglePayload(t, commands), "turn/start")
	if params["model"] != "deepseek-v4-flash" || params["effort"] != "high" {
		t.Fatalf("expected top-level profile policy, got %#v", params)
	}
	collaborationMode, _ := params["collaborationMode"].(map[string]any)
	settings, _ := collaborationMode["settings"].(map[string]any)
	if settings["model"] != "deepseek-v4-flash" || settings["reasoning_effort"] != "high" {
		t.Fatalf("expected profile policy in collaborationMode settings, got %#v", params["collaborationMode"])
	}
	if collaborationMode["mode"] != "default" {
		t.Fatalf("expected prompt plan override to preserve default mode, got %#v", params["collaborationMode"])
	}
}

func TestTranslateCompactAndChildRestartRestoreCarryCodexResumePolicy(t *testing.T) {
	tr := NewTranslator("inst-1")
	if _, err := tr.ObserveClient([]byte(`{"method":"thread/resume","params":{"threadId":"thread-current","cwd":"/tmp/current"}}`)); err != nil {
		t.Fatalf("seed current thread: %v", err)
	}
	policy := &agentproto.CodexResumePolicy{
		Mode:            agentproto.CodexResumePreserveThreadSettings,
		ModelProviderID: "codex_remote_profile_team",
		ModelMode:       agentproto.CodexThreadValuePreservedObserved,
		Model:           "observed-model",
		ReasoningMode:   agentproto.CodexThreadValuePreservedObserved,
		ReasoningEffort: "medium",
		ContextMode:     "codex_default",
	}
	commands, err := tr.TranslateCommand(agentproto.Command{
		Kind:        agentproto.CommandThreadCompactStart,
		Origin:      agentproto.Origin{Surface: "surface-1"},
		Target:      agentproto.Target{ThreadID: "thread-1", CWD: "/tmp/project"},
		CodexResume: policy,
	})
	if err != nil {
		t.Fatalf("translate compact command: %v", err)
	}
	params := payloadParams(t, decodeSinglePayload(t, commands), "thread/resume")
	if params["modelProvider"] != "codex_remote_profile_team" || params["model"] != "observed-model" {
		t.Fatalf("expected compact resume to carry preserve policy, got %#v", params)
	}
	if config := payloadConfig(t, params); config["model_reasoning_effort"] != "medium" {
		t.Fatalf("expected preserved reasoning in compact resume config, got %#v", config)
	}

	tr.PrepareChildRestartRestorePolicy(policy)
	frame, _, ok, err := tr.BuildChildRestartRestoreFrame("cmd-restart-1")
	if err != nil {
		t.Fatalf("BuildChildRestartRestoreFrame: %v", err)
	}
	if !ok {
		t.Fatal("expected child restart restore frame")
	}
	restoreParams := payloadParams(t, decodePayload(t, frame), "thread/resume")
	if restoreParams["modelProvider"] != "codex_remote_profile_team" || restoreParams["model"] != "observed-model" {
		t.Fatalf("expected child restart restore to carry preserve policy, got %#v", restoreParams)
	}
}

func TestObserveTurnStartedEmitsEffectiveContextEvidence(t *testing.T) {
	tr := NewTranslator("inst-1")
	_, err := tr.TranslateCommand(agentproto.Command{
		Kind:   agentproto.CommandPromptSend,
		Origin: agentproto.Origin{ChatID: "surface-1"},
		Target: agentproto.Target{ThreadID: "thread-1", CWD: "/tmp/project"},
		Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}}},
		CodexResume: &agentproto.CodexResumePolicy{
			Mode:             agentproto.CodexResumeApplyTargetProfile,
			ModelProviderID:  "codex_remote_profile_team",
			ModelMode:        agentproto.CodexThreadValueExplicit,
			Model:            "gpt-5.4",
			ReasoningMode:    agentproto.CodexThreadValueExplicit,
			ReasoningEffort:  "high",
			ContextMode:      "extended_1m",
			ContextWindow:    1000000,
			AutoCompactLimit: 900000,
		},
	})
	if err != nil {
		t.Fatalf("translate command: %v", err)
	}
	if _, err := tr.ObserveServer([]byte(`{"method":"thread/started","params":{"thread":{"id":"thread-1","modelProvider":"codex_remote_profile_team","model":"gpt-5.4","config":{"model_reasoning_effort":"high"}}}}`)); err != nil {
		t.Fatalf("observe thread started: %v", err)
	}

	result, err := tr.ObserveServer([]byte(`{"method":"turn/started","params":{"threadId":"thread-1","turn":{"id":"turn-1"},"modelContextWindow":272000}}`))
	if err != nil {
		t.Fatalf("observe turn started: %v", err)
	}
	if len(result.Events) != 1 || result.Events[0].CodexEffectiveThread == nil {
		t.Fatalf("expected effective thread contract on turn started, got %#v", result.Events)
	}
	effective := result.Events[0].CodexEffectiveThread
	if effective.ModelProviderID != "codex_remote_profile_team" || effective.Model != "gpt-5.4" || effective.ReasoningEffort != "high" {
		t.Fatalf("unexpected effective model contract: %#v", effective)
	}
	if effective.RequestedContextWindow != 1000000 || effective.EffectiveContextWindow != 272000 || effective.ContextStatus != agentproto.CodexContextPreferenceClamped {
		t.Fatalf("expected clamped context evidence, got %#v", effective)
	}
}

func TestObserveTurnStartedDoesNotForgeEffectiveProviderWithoutEvidence(t *testing.T) {
	tr := NewTranslator("inst-1")
	_, err := tr.TranslateCommand(agentproto.Command{
		Kind:   agentproto.CommandPromptSend,
		Origin: agentproto.Origin{ChatID: "surface-1"},
		Target: agentproto.Target{ThreadID: "thread-1", CWD: "/tmp/project"},
		Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}}},
		CodexResume: &agentproto.CodexResumePolicy{
			Mode:            agentproto.CodexResumeApplyTargetProfile,
			ModelProviderID: "codex_remote_profile_team",
			ModelMode:       agentproto.CodexThreadValueExplicit,
			Model:           "gpt-5.4",
			ReasoningMode:   agentproto.CodexThreadValueExplicit,
			ReasoningEffort: "high",
		},
	})
	if err != nil {
		t.Fatalf("translate command: %v", err)
	}

	result, err := tr.ObserveServer([]byte(`{"method":"turn/started","params":{"threadId":"thread-1","turn":{"id":"turn-1"},"modelContextWindow":272000}}`))
	if err != nil {
		t.Fatalf("observe turn started: %v", err)
	}
	if len(result.Events) != 1 {
		t.Fatalf("expected one turn started event, got %#v", result.Events)
	}
	event := result.Events[0]
	if event.CodexEffectiveThread != nil {
		t.Fatalf("must not forge effective contract without provider evidence, got %#v", event.CodexEffectiveThread)
	}
	if event.Problem == nil || event.Problem.Code != "codex_protocol_incomplete" {
		t.Fatalf("expected codex_protocol_incomplete problem, got %#v", event.Problem)
	}
}

func TestObserveTurnStartedBuildsEffectiveFromObservedThreadEvidence(t *testing.T) {
	tr := NewTranslator("inst-1")
	_, err := tr.TranslateCommand(agentproto.Command{
		Kind:   agentproto.CommandPromptSend,
		Origin: agentproto.Origin{ChatID: "surface-1"},
		Target: agentproto.Target{ThreadID: "thread-1", CWD: "/tmp/project"},
		Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}}},
		CodexResume: &agentproto.CodexResumePolicy{
			Mode:             agentproto.CodexResumeApplyTargetProfile,
			ModelProviderID:  "codex_remote_profile_team",
			ModelMode:        agentproto.CodexThreadValueExplicit,
			Model:            "gpt-5.4",
			ReasoningMode:    agentproto.CodexThreadValueExplicit,
			ReasoningEffort:  "high",
			ContextWindow:    1000000,
			AutoCompactLimit: 900000,
		},
	})
	if err != nil {
		t.Fatalf("translate command: %v", err)
	}
	if _, err := tr.ObserveServer([]byte(`{"method":"thread/started","params":{"thread":{"id":"thread-1","modelProvider":"codex_remote_profile_team","model":"gpt-5.4","config":{"model_reasoning_effort":"high"}}}}`)); err != nil {
		t.Fatalf("observe thread started: %v", err)
	}

	result, err := tr.ObserveServer([]byte(`{"method":"turn/started","params":{"threadId":"thread-1","turn":{"id":"turn-1"},"modelContextWindow":272000}}`))
	if err != nil {
		t.Fatalf("observe turn started: %v", err)
	}
	if len(result.Events) != 1 || result.Events[0].CodexEffectiveThread == nil {
		t.Fatalf("expected effective thread contract on turn started, got %#v", result.Events)
	}
	effective := result.Events[0].CodexEffectiveThread
	if effective.ModelProviderID != "codex_remote_profile_team" ||
		effective.Model != "gpt-5.4" ||
		effective.ReasoningEffort != "high" {
		t.Fatalf("expected observed provider/model/reasoning, got %#v", effective)
	}
	if effective.ModelMode != agentproto.CodexThreadValueExplicit || effective.ReasoningMode != agentproto.CodexThreadValueExplicit {
		t.Fatalf("expected explicit provenance from observed/request match, got %#v", effective)
	}
	if effective.EffectiveContextWindow != 272000 || effective.ContextStatus != agentproto.CodexContextPreferenceClamped {
		t.Fatalf("expected real context clamp evidence, got %#v", effective)
	}
}

func TestObserveTurnStartedReportsProtocolIncompleteOnProviderMismatch(t *testing.T) {
	tr := NewTranslator("inst-1")
	_, err := tr.TranslateCommand(agentproto.Command{
		Kind:   agentproto.CommandPromptSend,
		Origin: agentproto.Origin{ChatID: "surface-1"},
		Target: agentproto.Target{ThreadID: "thread-1", CWD: "/tmp/project"},
		Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}}},
		CodexResume: &agentproto.CodexResumePolicy{
			Mode:            agentproto.CodexResumeApplyTargetProfile,
			ModelProviderID: "codex_remote_profile_team",
			ModelMode:       agentproto.CodexThreadValueExplicit,
			Model:           "gpt-5.4",
		},
	})
	if err != nil {
		t.Fatalf("translate command: %v", err)
	}
	if _, err := tr.ObserveServer([]byte(`{"method":"thread/started","params":{"thread":{"id":"thread-1","modelProvider":"other-provider","model":"gpt-5.4"}}}`)); err != nil {
		t.Fatalf("observe thread started: %v", err)
	}

	result, err := tr.ObserveServer([]byte(`{"method":"turn/started","params":{"threadId":"thread-1","turn":{"id":"turn-1"}}}`))
	if err != nil {
		t.Fatalf("observe turn started: %v", err)
	}
	event := result.Events[0]
	if event.CodexEffectiveThread != nil {
		t.Fatalf("must not emit effective contract on provider mismatch, got %#v", event.CodexEffectiveThread)
	}
	if event.Problem == nil || event.Problem.Code != "codex_protocol_incomplete" {
		t.Fatalf("expected codex_protocol_incomplete on provider mismatch, got %#v", event.Problem)
	}
}

func decodeSinglePayload(t *testing.T, payloads [][]byte) map[string]any {
	t.Helper()
	if len(payloads) != 1 {
		t.Fatalf("expected one payload, got %d", len(payloads))
	}
	return decodePayload(t, payloads[0])
}

func decodePayload(t *testing.T, payload []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(payload, &out); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	return out
}

func payloadParams(t *testing.T, payload map[string]any, method string) map[string]any {
	t.Helper()
	if payload["method"] != method {
		t.Fatalf("expected %s payload, got %#v", method, payload)
	}
	params, _ := payload["params"].(map[string]any)
	if params == nil {
		t.Fatalf("missing params in %#v", payload)
	}
	return params
}

func payloadConfig(t *testing.T, params map[string]any) map[string]any {
	t.Helper()
	config, _ := params["config"].(map[string]any)
	if config == nil {
		t.Fatalf("missing config in %#v", params)
	}
	return config
}
