package codex

import (
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"testing"
)

func TestTurnModelEvidenceInvalidatesChangedRequest(t *testing.T) {
	for _, tc := range []struct {
		name, model, effort, fresh, wantModel, wantEffort string
	}{
		{name: "model and effort changed", model: "new-model", effort: "high"},
		{name: "model changed", model: "new-model"},
		{name: "effort changed", effort: "high", wantModel: "old-model"},
		{name: "unchanged explicit", model: "old-model", effort: "low", wantModel: "old-model", wantEffort: "low"},
		{name: "defaults preserve observations", wantModel: "old-model", wantEffort: "low"},
		{name: "fresh evidence", model: "new-model", effort: "high", fresh: `{"method":"thread/settings/updated","params":{"threadId":"thread-1","settings":{"model":"actual-model","reasoningEffort":"medium"}}}`, wantModel: "actual-model", wantEffort: "medium"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr := NewTranslator("inst-1")
			if _, err := tr.ObserveServer([]byte(`{"method":"thread/started","params":{"thread":{"id":"thread-1","modelProvider":"provider","model":"old-model","config":{"model_reasoning_effort":"low"}}}}`)); err != nil {
				t.Fatal(err)
			}
			commands, err := tr.TranslateCommand(agentproto.Command{Kind: agentproto.CommandPromptSend, Origin: agentproto.Origin{Surface: "surface-1"}, Target: agentproto.Target{ThreadID: "thread-1", CWD: "/tmp/project"}, Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "next"}}}, Overrides: agentproto.PromptOverrides{Model: tc.model, ReasoningEffort: tc.effort}, CodexResume: &agentproto.CodexResumePolicy{Mode: agentproto.CodexResumePreserveThreadSettings, ModelProviderID: "provider", ModelMode: agentproto.CodexThreadValueDefault, ReasoningMode: agentproto.CodexThreadValueDefault}})
			if err != nil {
				t.Fatal(err)
			}
			params := payloadParams(t, decodeSinglePayload(t, commands), "turn/start")
			if tc.model != "" && params["model"] != tc.model {
				t.Fatalf("outgoing model=%v", params["model"])
			}
			if tc.effort != "" && params["effort"] != tc.effort {
				t.Fatalf("outgoing effort=%v", params["effort"])
			}
			if tc.fresh != "" {
				if _, err := tr.ObserveServer([]byte(tc.fresh)); err != nil {
					t.Fatal(err)
				}
			}
			started, err := tr.ObserveServer([]byte(`{"method":"turn/started","params":{"threadId":"thread-1","turn":{"id":"turn-new"}}}`))
			if err != nil {
				t.Fatal(err)
			}
			effective := started.Events[0].CodexEffectiveThread
			if effective == nil || effective.Model != tc.wantModel || effective.ReasoningEffort != tc.wantEffort {
				t.Fatalf("effective=%#v, want %q/%q", effective, tc.wantModel, tc.wantEffort)
			}
		})
	}
}

func TestLocalRequestsInvalidateTurnModelEvidence(t *testing.T) {
	for _, method := range []string{"turn/start", "thread/resume"} {
		t.Run(method, func(t *testing.T) {
			tr := NewTranslator("inst-1")
			tr.mergeObservedThread("thread-1", "provider", "old-model", "low")
			if _, err := tr.ObserveClient([]byte(`{"id":"local","method":"` + method + `","params":{"threadId":"thread-1","model":"new-model","effort":"high"}}`)); err != nil {
				t.Fatal(err)
			}
			observed := tr.observedThreads["thread-1"]
			if observed.Model != "" || observed.ReasoningEffort != "" || observed.ModelProviderID != "provider" {
				t.Fatalf("stale observation retained: %#v", observed)
			}
		})
	}
}

func TestResumeRequestsInvalidateModelEvidence(t *testing.T) {
	for _, kind := range []agentproto.CommandKind{agentproto.CommandPromptSend, agentproto.CommandThreadCompactStart, "restart"} {
		t.Run(string(kind), func(t *testing.T) {
			tr := NewTranslator("inst-1")
			tr.mergeObservedThread("thread-1", "provider", "old-model", "low")
			policy := &agentproto.CodexResumePolicy{Mode: agentproto.CodexResumePreserveThreadSettings, ModelProviderID: "provider", ModelMode: agentproto.CodexThreadValueExplicit, Model: "new-model"}
			if kind == "restart" {
				tr.currentThreadID = "thread-1"
				tr.PrepareChildRestartRestorePolicy(policy)
				if _, _, _, err := tr.BuildChildRestartRestoreFrame("restart"); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := tr.TranslateCommand(agentproto.Command{Kind: kind, Target: agentproto.Target{ThreadID: "thread-1"}, CodexResume: policy}); err != nil {
					t.Fatal(err)
				}
			}
			observed := tr.observedThreads["thread-1"]
			if observed.Model != "" || observed.ReasoningEffort != "" {
				t.Fatalf("resume retained pre-change evidence: %#v", observed)
			}
		})
	}
}
