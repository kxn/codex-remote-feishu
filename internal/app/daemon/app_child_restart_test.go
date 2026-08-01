package daemon

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/renderer"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestRestartRelayChildCodexSendsRestartCommand(t *testing.T) {
	app := &App{}
	var (
		gotInstance string
		gotCommand  agentproto.Command
	)
	app.sendAgentCommand = func(instanceID string, command agentproto.Command) error {
		gotInstance = instanceID
		gotCommand = command
		return nil
	}

	if err := app.restartRelayChildCodex("inst-1"); err != nil {
		t.Fatalf("restartRelayChildCodex: %v", err)
	}
	if gotInstance != "inst-1" {
		t.Fatalf("expected instance inst-1, got %q", gotInstance)
	}
	if gotCommand.Kind != agentproto.CommandProcessChildRestart {
		t.Fatalf("expected process.child.restart command, got %#v", gotCommand)
	}
	if gotCommand.CommandID == "" {
		t.Fatal("expected generated command id")
	}
}

func TestNewRelayChildCodexRestartCommandGeneratesCommand(t *testing.T) {
	app := &App{sendAgentCommand: func(string, agentproto.Command) error { return nil }}

	command, err := app.newRelayChildCodexRestartCommand("inst-1")
	if err != nil {
		t.Fatalf("newRelayChildCodexRestartCommand: %v", err)
	}
	if command.Kind != agentproto.CommandProcessChildRestart {
		t.Fatalf("expected process.child.restart command, got %#v", command)
	}
	if command.CommandID == "" {
		t.Fatal("expected generated command id")
	}
}

func TestNewRelayChildCodexRestartCommandCarriesCodexResumePolicy(t *testing.T) {
	now := time.Date(2026, 8, 1, 11, 0, 0, 0, time.UTC)
	svc := orchestrator.NewService(func() time.Time { return now }, orchestrator.Config{}, renderer.NewPlanner())
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID: "inst-1",
		Backend:    agentproto.BackendCodex,
		CodexConnectionContract: &state.CodexConnectionContract{
			ConnectionContractID: "conn-team-r4",
			ModelProviderID:      "codex_remote_profile_team",
		},
		CodexThreadPolicy: &state.CodexThreadPolicy{
			ThreadPolicyID:  "thread-policy-r7",
			ModelMode:       state.CodexThreadValueExplicit,
			Model:           "gpt-5.4",
			ReasoningMode:   state.CodexThreadValueExplicit,
			ReasoningEffort: "high",
			ContextMode:     state.CodexContextModePrice272K,
			ContextWindow:   272000,
		},
	})
	app := &App{
		service:          svc,
		sendAgentCommand: func(string, agentproto.Command) error { return nil },
	}

	command, err := app.newRelayChildCodexRestartCommand("inst-1")
	if err != nil {
		t.Fatalf("newRelayChildCodexRestartCommand: %v", err)
	}
	if command.CodexResume == nil || command.CodexResume.ModelProviderID != "codex_remote_profile_team" ||
		command.CodexResume.Model != "gpt-5.4" || command.CodexResume.ReasoningEffort != "high" {
		t.Fatalf("expected codex resume policy on restart command, got %#v", command.CodexResume)
	}
}

func TestNewRelayChildCodexRestartCommandPreservesActiveThreadSettings(t *testing.T) {
	now := time.Date(2026, 8, 1, 11, 5, 0, 0, time.UTC)
	svc := orchestrator.NewService(func() time.Time { return now }, orchestrator.Config{}, renderer.NewPlanner())
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:     "inst-1",
		Backend:        agentproto.BackendCodex,
		ActiveThreadID: "thread-1",
		CodexConnectionContract: &state.CodexConnectionContract{
			ConnectionContractID: "conn-team-r4",
			ModelProviderID:      "codex_remote_profile_team",
		},
		CodexThreadPolicy: &state.CodexThreadPolicy{
			ThreadPolicyID:   "thread-policy-r7",
			ModelMode:        state.CodexThreadValueExplicit,
			Model:            "target-profile-model",
			ReasoningMode:    state.CodexThreadValueExplicit,
			ReasoningEffort:  "high",
			ContextMode:      state.CodexContextModeExtended,
			ContextWindow:    1000000,
			AutoCompactLimit: 900000,
		},
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {
				ThreadID: "thread-1",
				CodexEffectiveThread: &agentproto.CodexEffectiveThreadContract{
					ConnectionContractID: "conn-team-r4",
					ModelProviderID:      "codex_remote_profile_team",
					Model:                "observed-model",
					ReasoningEffort:      "medium",
				},
			},
		},
	})
	app := &App{
		service:          svc,
		sendAgentCommand: func(string, agentproto.Command) error { return nil },
	}

	command, err := app.newRelayChildCodexRestartCommand("inst-1")
	if err != nil {
		t.Fatalf("newRelayChildCodexRestartCommand: %v", err)
	}
	if command.CodexResume == nil ||
		command.CodexResume.Mode != agentproto.CodexResumePreserveThreadSettings ||
		command.CodexResume.Model != "observed-model" ||
		command.CodexResume.ReasoningEffort != "medium" ||
		command.CodexResume.ContextWindow != 1000000 {
		t.Fatalf("expected restart preserve policy, got %#v", command.CodexResume)
	}
}
