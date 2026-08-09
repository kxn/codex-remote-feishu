package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestOpenCodeHeadlessObservedConfigDoesNotPersistWorkspaceDefaults(t *testing.T) {
	now := time.Date(2026, 8, 9, 11, 20, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	workspaceKey := "/data/dl/droid"
	svc.MaterializeSurfaceResumeContract("surface-1", "app-1", "chat-1", "user-1", state.HeadlessOpenCodeSurfaceBackendContract("op_team"), "", "")
	surface := svc.root.Surfaces["surface-1"]
	surface.OpenCodeAdmissionRef = &state.OpenCodeAdmissionRef{ProfileRef: state.OpenCodeProfileRef{ID: "op_team", Revision: 7}}
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           workspaceKey,
		WorkspaceKey:            workspaceKey,
		ShortName:               "droid",
		Backend:                 agentproto.BackendOpenCode,
		OpenCodeProfileID:       "op_team",
		OpenCodeAdmissionRef:    state.NormalizeOpenCodeAdmissionRef(surface.OpenCodeAdmissionRef),
		Source:                  "headless",
		Managed:                 true,
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: workspaceKey},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:            agentproto.EventConfigObserved,
		ThreadID:        "thread-1",
		CWD:             workspaceKey,
		ConfigScope:     "cwd_default",
		Model:           "codex_remote_opencode_op_team/kimi-k2",
		ReasoningEffort: "high",
		AccessMode:      agentproto.AccessModeConfirm,
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:        agentproto.EventConfigObserved,
		ThreadID:    "thread-1",
		CWD:         workspaceKey,
		ConfigScope: "thread",
		AccessMode:  agentproto.AccessModeAcceptEdits,
	})

	if defaults := svc.root.WorkspaceDefaults[state.WorkspaceDefaultsStorageKey(workspaceKey, state.InstanceBackendContract{
		Backend:           agentproto.BackendOpenCode,
		OpenCodeProfileID: "op_team",
	})]; defaults != (state.ModelConfigRecord{}) {
		t.Fatalf("expected opencode observed config not to persist workspace defaults, got %#v", defaults)
	}
	if defaults := svc.root.Instances["inst-1"].CWDDefaults[workspaceKey]; defaults != (state.ModelConfigRecord{}) {
		t.Fatalf("expected opencode observed config not to become instance cwd defaults, got %#v", defaults)
	}
	if got := svc.root.Instances["inst-1"].Threads["thread-1"].ObservedAccessMode; got != agentproto.AccessModeAcceptEdits {
		t.Fatalf("expected thread observed access mode to remain available, got %q", got)
	}
}
