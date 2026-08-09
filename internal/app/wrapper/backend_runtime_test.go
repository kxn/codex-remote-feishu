package wrapper

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/adapter/claude"
	"github.com/kxn/codex-remote-feishu/internal/adapter/codex"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestNewBackendRuntimeOpenCodeDoesNotFallBackToCodex(t *testing.T) {
	runtime := newBackendRuntime(Config{
		Backend:       agentproto.BackendOpenCode,
		InstanceID:    "inst-opencode",
		WorkspaceRoot: "/tmp/work",
	})
	if runtime.Backend() != agentproto.BackendOpenCode {
		t.Fatalf("runtime backend = %q, want %q", runtime.Backend(), agentproto.BackendOpenCode)
	}
	if _, ok := runtime.(*codexBackendRuntime); ok {
		t.Fatal("opencode backend runtime must not fall back to codex runtime")
	}
	opencodeRuntime, ok := runtime.(*opencodeBackendRuntime)
	if !ok {
		t.Fatalf("runtime = %T, want *opencodeBackendRuntime", runtime)
	}
	if opencodeRuntime.translator == nil {
		t.Fatal("opencode backend runtime must own an ACP translator")
	}
	caps := runtime.Capabilities()
	if !caps.SessionCatalog || !caps.RequestRespond || !caps.RequiresCWDForResume || caps.VSCodeMode || caps.TurnSteer {
		t.Fatalf("unexpected opencode runtime capabilities: %#v", caps)
	}
	if session, err := runtime.Launch(context.Background(), nil, nil, nil); err != nil || session != nil {
		t.Fatalf("opencode launch with nil app = %#v, %v; want nil session without startup failure", session, err)
	}
	translated, err := runtime.TranslateCommand(agentproto.Command{
		CommandID: "cmd-opencode",
		Kind:      agentproto.CommandPromptSend,
		Target: agentproto.Target{
			ExecutionMode: agentproto.PromptExecutionModeStartNew,
			CWD:           "/tmp/work",
		},
		Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}}},
	})
	if err != nil {
		t.Fatalf("opencode TranslateCommand: %v", err)
	}
	if len(translated.Phases) != 1 || len(translated.Phases[0].OutboundToChild) != 1 {
		t.Fatalf("opencode translated phases = %#v, want one session/new frame", translated.Phases)
	}
	var payload map[string]any
	if err := json.Unmarshal(translated.Phases[0].OutboundToChild[0], &payload); err != nil {
		t.Fatalf("unmarshal opencode frame: %v", err)
	}
	if payload["method"] != "session/new" {
		t.Fatalf("opencode first method = %#v, want session/new", payload["method"])
	}
}

func TestNewBackendRuntimeUnknownDoesNotFallBackToCodex(t *testing.T) {
	runtime := newBackendRuntime(Config{
		Backend:    agentproto.Backend("mystery"),
		InstanceID: "inst-mystery",
	})
	if runtime.Backend() == agentproto.BackendCodex {
		t.Fatalf("unknown backend runtime fell back to codex")
	}
	if _, ok := runtime.(*codexBackendRuntime); ok {
		t.Fatal("unknown backend runtime must not use codex runtime")
	}
	_, err := runtime.TranslateCommand(agentproto.Command{Kind: agentproto.CommandPromptSend})
	if err == nil || !strings.Contains(err.Error(), "backend_unsupported") {
		t.Fatalf("unknown backend TranslateCommand error = %v, want backend_unsupported", err)
	}
}

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
	}, nil); err != nil {
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

func TestCodexBackendRuntimePrepareChildRestartStoresResumePolicy(t *testing.T) {
	runtime := &codexBackendRuntime{translator: codex.NewTranslator("inst-1")}
	if _, err := runtime.translator.ObserveClient([]byte(`{"method":"thread/resume","params":{"threadId":"thread-1","cwd":"/tmp/project"}}`)); err != nil {
		t.Fatalf("seed thread: %v", err)
	}
	policy := &agentproto.CodexResumePolicy{
		Mode:            agentproto.CodexResumeApplyTargetProfile,
		ModelProviderID: "codex_remote_profile_team",
		ModelMode:       agentproto.CodexThreadValueExplicit,
		Model:           "gpt-5.5",
		ReasoningMode:   agentproto.CodexThreadValueExplicit,
		ReasoningEffort: "high",
	}
	if err := runtime.PrepareChildRestart("cmd-restart", agentproto.PromptDispatchPlan{}, policy); err != nil {
		t.Fatalf("PrepareChildRestart: %v", err)
	}
	frame, _, ok, err := runtime.BuildChildRestartRestoreFrame("cmd-restart")
	if err != nil {
		t.Fatalf("BuildChildRestartRestoreFrame: %v", err)
	}
	if !ok {
		t.Fatal("expected restore frame")
	}
	var payload map[string]any
	if err := json.Unmarshal(frame, &payload); err != nil {
		t.Fatalf("unmarshal restore frame: %v", err)
	}
	params, _ := payload["params"].(map[string]any)
	if params["modelProvider"] != "codex_remote_profile_team" || params["model"] != "gpt-5.5" {
		t.Fatalf("expected restart restore policy in frame, got %#v", params)
	}
	config, _ := params["config"].(map[string]any)
	if config["model_reasoning_effort"] != "high" {
		t.Fatalf("expected restart restore reasoning in frame, got %#v", config)
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
	}, nil); err != nil {
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
