package wrapper

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/adapter/claude"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestClaudeBackendRuntimeRestartPlanUsesPersistedResumeTarget(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)
	workspaceRoot := filepath.Join(t.TempDir(), "ws-resume-plan")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	writeWrapperClaudeSessionFile(t, configDir, workspaceRoot, "resume-session-1", []map[string]any{
		{"type": "system", "cwd": workspaceRoot, "session_id": "resume-session-1", "model": "mimo-v2.5-pro"},
		{"type": "session-title", "title": "Persisted resume target"},
		{"type": "user", "message": map[string]any{"role": "user", "content": "resume me"}},
	})

	runtime := &claudeBackendRuntime{
		workspaceRoot: workspaceRoot,
	}
	plan, err := runtime.restartPlanForCommand(agentproto.Command{
		CommandID: "cmd-prompt-claude-resume",
		Kind:      agentproto.CommandPromptSend,
		Target: agentproto.Target{
			ThreadID: "resume-session-1",
			CWD:      workspaceRoot,
		},
		Prompt: agentproto.Prompt{
			Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "resume this session"}},
		},
	})
	if err != nil {
		t.Fatalf("restartPlanForCommand: %v", err)
	}
	if plan == nil {
		t.Fatal("expected persisted resume target to require restart plan")
	}
	if plan.DispatchPlan.ExecutionThreadID != "resume-session-1" {
		t.Fatalf("restart target thread = %q, want resume-session-1", plan.DispatchPlan.ExecutionThreadID)
	}
	if plan.DispatchPlan.CWD != workspaceRoot {
		t.Fatalf("restart target cwd = %q, want %q", plan.DispatchPlan.CWD, workspaceRoot)
	}
}

func TestClaudeBackendRuntimePrepareChildRestartStoresResolvedResumeTarget(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)
	workspaceRoot := filepath.Join(t.TempDir(), "ws-restart-prepare")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	writeWrapperClaudeSessionFile(t, configDir, workspaceRoot, "resume-session-1", []map[string]any{
		{"type": "system", "cwd": workspaceRoot, "session_id": "resume-session-1", "model": "mimo-v2.5-pro"},
		{"type": "session-title", "title": "Persisted resume target"},
		{"type": "user", "message": map[string]any{"role": "user", "content": "resume me"}},
	})

	runtime := &claudeBackendRuntime{
		workspaceRoot: workspaceRoot,
	}
	if err := runtime.PrepareChildRestart("cmd-prompt-claude-resume", agentproto.PromptDispatchPlan{
		ExecutionThreadID: "resume-session-1",
		CWD:               workspaceRoot,
	}); err != nil {
		t.Fatalf("PrepareChildRestart: %v", err)
	}
	if runtime.pendingLaunchResume == nil {
		t.Fatal("expected pending launch resume target to be stored")
	}
	if runtime.pendingLaunchResume.ThreadID != "resume-session-1" {
		t.Fatalf("pending launch resume thread = %q, want resume-session-1", runtime.pendingLaunchResume.ThreadID)
	}
	if runtime.pendingLaunchResume.CWD != workspaceRoot {
		t.Fatalf("pending launch resume cwd = %q, want %q", runtime.pendingLaunchResume.CWD, workspaceRoot)
	}
}

func TestClaudeBackendRuntimeRestartPlanExplicitStartNewDropsCurrentSession(t *testing.T) {
	workspaceRoot := filepath.Join(t.TempDir(), "ws-start-new")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	runtime := &claudeBackendRuntime{
		workspaceRoot: workspaceRoot,
		expectedResumeThread: &claudeLaunchResumeTarget{
			ThreadID: "resume-session-1",
			CWD:      workspaceRoot,
		},
	}
	plan, err := runtime.restartPlanForCommand(agentproto.Command{
		CommandID: "cmd-prompt-claude-start-new",
		Kind:      agentproto.CommandPromptSend,
		Target: agentproto.Target{
			CWD:                   workspaceRoot,
			ExecutionMode:         agentproto.PromptExecutionModeStartNew,
			CreateThreadIfMissing: true,
		},
		Prompt: agentproto.Prompt{
			Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "start a fresh session"}},
		},
	})
	if err != nil {
		t.Fatalf("restartPlanForCommand: %v", err)
	}
	if plan == nil {
		t.Fatal("expected explicit start_new to require a child restart away from the current session")
	}
	if plan.DispatchPlan.ExecutionThreadID != "" {
		t.Fatalf("restart target thread = %q, want empty fresh launch target", plan.DispatchPlan.ExecutionThreadID)
	}
	if plan.DispatchPlan.ExecutionMode != agentproto.PromptExecutionModeStartNew {
		t.Fatalf("restart target mode = %q, want %q", plan.DispatchPlan.ExecutionMode, agentproto.PromptExecutionModeStartNew)
	}
}

func TestClaudeBackendRuntimePrepareChildRestartExplicitStartNewClearsResumeTarget(t *testing.T) {
	workspaceRoot := filepath.Join(t.TempDir(), "ws-start-new-prepare")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	runtime := &claudeBackendRuntime{
		workspaceRoot: workspaceRoot,
		expectedResumeThread: &claudeLaunchResumeTarget{
			ThreadID: "resume-session-1",
			CWD:      workspaceRoot,
		},
	}
	if err := runtime.PrepareChildRestart("cmd-prompt-claude-start-new", agentproto.PromptDispatchPlan{
		CWD:           workspaceRoot,
		ExecutionMode: agentproto.PromptExecutionModeStartNew,
	}); err != nil {
		t.Fatalf("PrepareChildRestart: %v", err)
	}
	if runtime.pendingLaunchResume != nil {
		t.Fatalf("pending launch resume = %#v, want nil for fresh child launch", runtime.pendingLaunchResume)
	}
}

func TestClaudeBackendRuntimeRestartPlanExplicitStartNewWithoutCurrentSessionSkipsRestart(t *testing.T) {
	workspaceRoot := filepath.Join(t.TempDir(), "ws-start-new-no-resume")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	runtime := &claudeBackendRuntime{
		workspaceRoot: workspaceRoot,
		translator:    claude.NewTranslator("inst-1"),
	}
	plan, err := runtime.restartPlanForCommand(agentproto.Command{
		CommandID: "cmd-prompt-claude-start-new-fresh",
		Kind:      agentproto.CommandPromptSend,
		Target: agentproto.Target{
			CWD:                   workspaceRoot,
			ExecutionMode:         agentproto.PromptExecutionModeStartNew,
			CreateThreadIfMissing: true,
		},
		Prompt: agentproto.Prompt{
			Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "start in an already fresh child"}},
		},
	})
	if err != nil {
		t.Fatalf("restartPlanForCommand: %v", err)
	}
	if plan != nil {
		t.Fatalf("restart plan = %#v, want nil when no resumed session is active", plan)
	}
}
