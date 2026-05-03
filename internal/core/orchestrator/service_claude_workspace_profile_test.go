package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestClaudeWorkspaceProfileSnapshotPartitionsAndClearsUnsupportedModelOverride(t *testing.T) {
	now := time.Date(2026, 4, 29, 8, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := svc.ensureSurface(control.Action{
		Kind:             control.ActionStatus,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	surface.ProductMode = state.ProductModeNormal
	surface.Backend = agentproto.BackendClaude
	surface.ClaudeProfileID = "profile-a"
	surface.ClaimedWorkspaceKey = "/data/dl/repo-a"
	surface.PlanMode = state.PlanModeSettingOn
	surface.PromptOverride = state.ModelConfigRecord{
		Model:           "should-not-persist",
		ReasoningEffort: "high",
		AccessMode:      agentproto.AccessModeConfirm,
	}
	svc.persistCurrentClaudeWorkspaceProfileSnapshot(surface)

	surface.ClaudeProfileID = "profile-b"
	surface.ClaimedWorkspaceKey = "/data/dl/repo-b"
	surface.PlanMode = state.PlanModeSettingOff
	surface.PromptOverride = state.ModelConfigRecord{
		ReasoningEffort: "medium",
		AccessMode:      agentproto.AccessModeFullAccess,
	}
	svc.persistCurrentClaudeWorkspaceProfileSnapshot(surface)

	keyA := state.ClaudeWorkspaceProfileSnapshotStorageKey("/data/dl/repo-a", agentproto.BackendClaude, "profile-a")
	keyB := state.ClaudeWorkspaceProfileSnapshotStorageKey("/data/dl/repo-b", agentproto.BackendClaude, "profile-b")
	if got := svc.root.ClaudeWorkspaceProfileSnapshots[keyA]; got != (state.ClaudeWorkspaceProfileSnapshotRecord{
		ReasoningEffort: "high",
		AccessMode:      agentproto.AccessModeConfirm,
		PlanMode:        state.PlanModeSettingOn,
	}) {
		t.Fatalf("unexpected profile A snapshot: %#v", got)
	}
	if got := svc.root.ClaudeWorkspaceProfileSnapshots[keyB]; got != (state.ClaudeWorkspaceProfileSnapshotRecord{
		ReasoningEffort: "medium",
		AccessMode:      agentproto.AccessModeFullAccess,
		PlanMode:        state.PlanModeSettingOff,
	}) {
		t.Fatalf("unexpected profile B snapshot: %#v", got)
	}

	surface.ClaudeProfileID = "profile-a"
	surface.ClaimedWorkspaceKey = "/data/dl/repo-a"
	surface.PlanMode = state.PlanModeSettingOff
	surface.PromptOverride = state.ModelConfigRecord{Model: "clear-me"}
	svc.restoreCurrentClaudeWorkspaceProfileSnapshot(surface)
	if surface.PlanMode != state.PlanModeSettingOn {
		t.Fatalf("expected plan mode restored from snapshot, got %q", surface.PlanMode)
	}
	if surface.PromptOverride.Model != "" || surface.PromptOverride.ReasoningEffort != "high" || surface.PromptOverride.AccessMode != agentproto.AccessModeConfirm {
		t.Fatalf("expected restored snapshot to clear model and restore reasoning/access, got %#v", surface.PromptOverride)
	}

	surface.ClaudeProfileID = "profile-c"
	surface.ClaimedWorkspaceKey = "/data/dl/repo-c"
	surface.PlanMode = state.PlanModeSettingOn
	surface.PromptOverride = state.ModelConfigRecord{
		Model:           "clear-me-too",
		ReasoningEffort: "high",
		AccessMode:      agentproto.AccessModeConfirm,
	}
	svc.restoreCurrentClaudeWorkspaceProfileSnapshot(surface)
	if surface.PlanMode != state.PlanModeSettingOff {
		t.Fatalf("expected missing snapshot to reset plan mode, got %q", surface.PlanMode)
	}
	if surface.PromptOverride != (state.ModelConfigRecord{}) {
		t.Fatalf("expected missing snapshot to clear prompt override, got %#v", surface.PromptOverride)
	}
}

func TestClaudeReasoningCommandUsesMaxAndClearDeletesWorkspaceProfileSnapshot(t *testing.T) {
	now := time.Date(2026, 5, 3, 9, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	workspaceKey := "/data/dl/repo"
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:      "inst-claude-1",
		WorkspaceRoot:   workspaceKey,
		WorkspaceKey:    workspaceKey,
		Backend:         agentproto.BackendClaude,
		ClaudeProfileID: "devseek",
		Online:          true,
		Threads:         map[string]*state.ThreadRecord{},
	})
	svc.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	surface := svc.root.Surfaces["surface-1"]
	surface.ProductMode = state.ProductModeNormal
	surface.Backend = agentproto.BackendClaude
	surface.ClaudeProfileID = "devseek"
	surface.AttachedInstanceID = "inst-claude-1"

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReasoningCommand,
		SurfaceSessionID: "surface-1",
		Text:             "/reasoning xhigh",
	})
	if len(events) != 1 {
		t.Fatalf("expected invalid reasoning feedback, got %#v", events)
	}
	if surface.PromptOverride.ReasoningEffort != "" {
		t.Fatalf("expected claude xhigh to be rejected, got %#v", surface.PromptOverride)
	}
	if summary := commandCatalogSummaryText(commandCatalogFromEvent(t, events[0])); !strings.Contains(summary, "max") {
		t.Fatalf("expected claude reasoning hint to mention max, got %q", summary)
	}

	events = svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReasoningCommand,
		SurfaceSessionID: "surface-1",
		Text:             "/reasoning max",
	})
	if len(events) != 1 || surface.PromptOverride.ReasoningEffort != "max" {
		t.Fatalf("expected max reasoning to apply, events=%#v override=%#v", events, surface.PromptOverride)
	}
	key := state.ClaudeWorkspaceProfileSnapshotStorageKey(workspaceKey, agentproto.BackendClaude, "devseek")
	if got := svc.root.ClaudeWorkspaceProfileSnapshots[key]; got.ReasoningEffort != "max" {
		t.Fatalf("expected max reasoning snapshot, got %#v", got)
	}

	events = svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReasoningCommand,
		SurfaceSessionID: "surface-1",
		Text:             "/reasoning clear",
	})
	if len(events) != 1 || surface.PromptOverride != (state.ModelConfigRecord{}) {
		t.Fatalf("expected clear reasoning to reset override, events=%#v override=%#v", events, surface.PromptOverride)
	}
	if _, ok := svc.root.ClaudeWorkspaceProfileSnapshots[key]; ok {
		t.Fatalf("expected clear reasoning to delete empty snapshot, got %#v", svc.root.ClaudeWorkspaceProfileSnapshots[key])
	}
}

func TestClaudeReasoningAndAccessCommandsShareWorkspaceProfileSnapshotContract(t *testing.T) {
	now := time.Date(2026, 5, 3, 9, 2, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	workspaceKey := "/data/dl/repo"
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:      "inst-claude-1",
		WorkspaceRoot:   workspaceKey,
		WorkspaceKey:    workspaceKey,
		Backend:         agentproto.BackendClaude,
		ClaudeProfileID: "devseek",
		Online:          true,
		Threads:         map[string]*state.ThreadRecord{},
	})
	svc.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	surface := svc.root.Surfaces["surface-1"]
	surface.ProductMode = state.ProductModeNormal
	surface.Backend = agentproto.BackendClaude
	surface.ClaudeProfileID = "devseek"
	surface.AttachedInstanceID = "inst-claude-1"

	key := state.ClaudeWorkspaceProfileSnapshotStorageKey(workspaceKey, agentproto.BackendClaude, "devseek")

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReasoningCommand,
		SurfaceSessionID: "surface-1",
		Text:             "/reasoning max",
	})
	if len(events) != 1 || surface.PromptOverride.ReasoningEffort != "max" {
		t.Fatalf("expected reasoning max to apply, events=%#v override=%#v", events, surface.PromptOverride)
	}
	if got := svc.root.ClaudeWorkspaceProfileSnapshots[key]; got != (state.ClaudeWorkspaceProfileSnapshotRecord{
		ReasoningEffort: "max",
		PlanMode:        state.PlanModeSettingOff,
	}) {
		t.Fatalf("expected reasoning-only snapshot, got %#v", got)
	}

	events = svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAccessCommand,
		SurfaceSessionID: "surface-1",
		Text:             "/access confirm",
	})
	if len(events) != 1 || surface.PromptOverride.AccessMode != agentproto.AccessModeConfirm {
		t.Fatalf("expected access confirm to apply, events=%#v override=%#v", events, surface.PromptOverride)
	}
	if got := svc.root.ClaudeWorkspaceProfileSnapshots[key]; got != (state.ClaudeWorkspaceProfileSnapshotRecord{
		ReasoningEffort: "max",
		AccessMode:      agentproto.AccessModeConfirm,
		PlanMode:        state.PlanModeSettingOff,
	}) {
		t.Fatalf("expected reasoning+access snapshot, got %#v", got)
	}

	events = svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAccessCommand,
		SurfaceSessionID: "surface-1",
		Text:             "/access clear",
	})
	if len(events) != 1 || surface.PromptOverride.AccessMode != "" {
		t.Fatalf("expected access clear to reset override, events=%#v override=%#v", events, surface.PromptOverride)
	}
	if got := svc.root.ClaudeWorkspaceProfileSnapshots[key]; got != (state.ClaudeWorkspaceProfileSnapshotRecord{
		ReasoningEffort: "max",
		PlanMode:        state.PlanModeSettingOff,
	}) {
		t.Fatalf("expected reasoning-only snapshot after access clear, got %#v", got)
	}

	events = svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReasoningCommand,
		SurfaceSessionID: "surface-1",
		Text:             "/reasoning clear",
	})
	if len(events) != 1 || surface.PromptOverride != (state.ModelConfigRecord{}) {
		t.Fatalf("expected reasoning clear to reset override, events=%#v override=%#v", events, surface.PromptOverride)
	}
	if _, ok := svc.root.ClaudeWorkspaceProfileSnapshots[key]; ok {
		t.Fatalf("expected empty snapshot to be deleted, got %#v", svc.root.ClaudeWorkspaceProfileSnapshots[key])
	}
}

func TestClaudeModelCommandIsRejectedBecauseModelLivesInProfile(t *testing.T) {
	now := time.Date(2026, 5, 3, 9, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	workspaceKey := "/data/dl/repo"
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:      "inst-claude-1",
		WorkspaceRoot:   workspaceKey,
		WorkspaceKey:    workspaceKey,
		Backend:         agentproto.BackendClaude,
		ClaudeProfileID: "devseek",
		Online:          true,
		Threads:         map[string]*state.ThreadRecord{},
	})
	svc.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	surface := svc.root.Surfaces["surface-1"]
	surface.ProductMode = state.ProductModeNormal
	surface.Backend = agentproto.BackendClaude
	surface.ClaudeProfileID = "devseek"
	surface.AttachedInstanceID = "inst-claude-1"

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModelCommand,
		SurfaceSessionID: "surface-1",
		Text:             "/model claude-sonnet",
	})
	if len(events) != 1 || events[0].Notice == nil {
		t.Fatalf("expected model command rejection notice, got %#v", events)
	}
	if !strings.Contains(events[0].Notice.Text, "Claude 配置") {
		t.Fatalf("expected profile guidance, got %#v", events[0].Notice)
	}
	if surface.PromptOverride.Model != "" {
		t.Fatalf("expected rejected model command not to mutate override, got %#v", surface.PromptOverride)
	}
}

func TestClaudePromptSummaryIgnoresModelOverridesAndDefaults(t *testing.T) {
	now := time.Date(2026, 5, 3, 9, 8, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	workspaceKey := "/data/dl/repo"
	inst := &state.InstanceRecord{
		InstanceID:      "inst-claude-1",
		WorkspaceRoot:   workspaceKey,
		WorkspaceKey:    workspaceKey,
		Backend:         agentproto.BackendClaude,
		ClaudeProfileID: "devseek",
		Online:          true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {
				ThreadID:      "thread-1",
				CWD:           workspaceKey,
				ExplicitModel: "legacy-thread-model",
				Loaded:        true,
			},
		},
		CWDDefaults: map[string]state.ModelConfigRecord{
			workspaceKey: {
				Model:           "legacy-cwd-model",
				ReasoningEffort: "high",
			},
		},
	}
	svc.UpsertInstance(inst)
	svc.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	surface := svc.root.Surfaces["surface-1"]
	surface.ProductMode = state.ProductModeNormal
	surface.Backend = agentproto.BackendClaude
	surface.ClaudeProfileID = "devseek"
	surface.AttachedInstanceID = "inst-claude-1"
	surface.SelectedThreadID = "thread-1"
	surface.PromptOverride = state.ModelConfigRecord{
		Model:           "stale-model-override",
		ReasoningEffort: "max",
	}

	summary := svc.resolveNextPromptSummary(inst, surface, "", "", state.ModelConfigRecord{})
	if summary.BaseModel != "" || summary.OverrideModel != "" || summary.EffectiveModel != "" {
		t.Fatalf("expected claude prompt summary to ignore model config, got %#v", summary)
	}
	if summary.EffectiveReasoningEffort != "max" {
		t.Fatalf("expected claude reasoning override to remain effective, got %#v", summary)
	}
}

func TestFreshClaudeWorkspaceRestoresReasoningBeforeLaunchContract(t *testing.T) {
	now := time.Date(2026, 5, 3, 9, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	workspaceKey := "/data/dl/repo"
	svc.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	surface := svc.root.Surfaces["surface-1"]
	surface.ProductMode = state.ProductModeNormal
	surface.Backend = agentproto.BackendClaude
	surface.ClaudeProfileID = "devseek"
	svc.MaterializeClaudeWorkspaceProfileSnapshots(map[string]state.ClaudeWorkspaceProfileSnapshotRecord{
		state.ClaudeWorkspaceProfileSnapshotStorageKey(workspaceKey, agentproto.BackendClaude, "devseek"): {
			ReasoningEffort: "max",
		},
	})

	events := svc.startFreshWorkspaceHeadlessWithOptions(surface, workspaceKey, false)
	if surface.PendingHeadless == nil || surface.PendingHeadless.ClaudeReasoningEffort != "max" {
		t.Fatalf("expected pending launch to carry restored reasoning, got %#v", surface.PendingHeadless)
	}
	if surface.PromptOverride.ReasoningEffort != "max" {
		t.Fatalf("expected surface override restored before launch, got %#v", surface.PromptOverride)
	}
	start := startHeadlessCommandFromEvents(events)
	if start == nil || start.ClaudeReasoningEffort != "max" {
		t.Fatalf("expected daemon start command to carry restored reasoning, got %#v", events)
	}
}

func TestClaudeThreadRestoreRestoresReasoningBeforeLaunchContract(t *testing.T) {
	now := time.Date(2026, 5, 3, 9, 15, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	workspaceKey := "/data/dl/repo"
	svc.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	surface := svc.root.Surfaces["surface-1"]
	surface.ProductMode = state.ProductModeNormal
	surface.Backend = agentproto.BackendClaude
	surface.ClaudeProfileID = "devseek"
	svc.MaterializeClaudeWorkspaceProfileSnapshots(map[string]state.ClaudeWorkspaceProfileSnapshotRecord{
		state.ClaudeWorkspaceProfileSnapshotStorageKey(workspaceKey, agentproto.BackendClaude, "devseek"): {
			ReasoningEffort: "max",
		},
	})

	events := svc.startHeadlessForResolvedThreadWithMode(surface, &mergedThreadView{
		ThreadID: "thread-1",
		Backend:  agentproto.BackendClaude,
		Thread: &state.ThreadRecord{
			ThreadID: "thread-1",
			Name:     "主线程",
			CWD:      workspaceKey,
			Loaded:   true,
		},
	}, startHeadlessModeDefault)
	if surface.PendingHeadless == nil || surface.PendingHeadless.ClaudeReasoningEffort != "max" {
		t.Fatalf("expected pending restore to carry restored reasoning, got %#v", surface.PendingHeadless)
	}
	start := startHeadlessCommandFromEvents(events)
	if start == nil || start.ClaudeReasoningEffort != "max" {
		t.Fatalf("expected daemon start command to carry restored reasoning, got %#v", events)
	}
}

func startHeadlessCommandFromEvents(events []eventcontract.Event) *control.DaemonCommand {
	for _, event := range events {
		if event.DaemonCommand != nil && event.DaemonCommand.Kind == control.DaemonCommandStartHeadless {
			return event.DaemonCommand
		}
	}
	return nil
}
