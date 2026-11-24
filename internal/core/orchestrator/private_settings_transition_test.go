package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestPrivateModePreviewPreservesAttachedRouteUntilReceipt(t *testing.T) {
	now := time.Date(2026, 4, 29, 3, 0, 0, 0, time.UTC)
	s := newServiceForTest(&now)
	s.config.PrivateApplication = true
	const surfaceID = "feishu:app-1:user:user-1"
	s.MaterializeSurface(surfaceID, "app-1", "chat-1", "user-1")
	s.UpsertInstance(&state.InstanceRecord{InstanceID: "inst", WorkspaceRoot: "/data/dl/repo", WorkspaceKey: "/data/dl/repo", Backend: agentproto.BackendCodex, Online: true, Threads: map[string]*state.ThreadRecord{}})
	s.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: surfaceID, GatewayID: "app-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst"})
	a := control.Action{Kind: control.ActionModeCommand, Text: "/mode claude", SurfaceSessionID: surfaceID, GatewayID: "app-1", ChatID: "chat-1", ActorUserID: "user-1"}
	proposal, events, err := s.PreviewPrivateSettingsTransition(a)
	if err != nil || proposal == nil || proposal.Backend != agentproto.BackendClaude || len(events) != 0 {
		t.Fatalf("preview: %#v %#v %v", proposal, events, err)
	}
	v := s.root.Surfaces[surfaceID]
	if v.AttachedInstanceID != "inst" || v.PendingHeadless != nil || v.Backend != agentproto.BackendCodex {
		t.Fatalf("preview changed route: %#v", v)
	}
	events = s.ApplySurfaceAction(a)
	if v.Backend != agentproto.BackendClaude || v.AttachedInstanceID != "" || v.PendingHeadless == nil {
		t.Fatalf("receipt transition did not execute: %#v", v)
	}
	found := false
	for _, event := range events {
		if event.DaemonCommand != nil && event.DaemonCommand.Kind == control.DaemonCommandStartHeadless {
			found = true
		}
	}
	if !found {
		t.Fatal("missing legacy headless continuation")
	}
}

func TestPrivateReasoningPreviewDoesNotWriteClaudeWorkspaceSnapshot(t *testing.T) {
	now := time.Date(2026, 5, 3, 9, 2, 0, 0, time.UTC)
	s := newServiceForTest(&now)
	s.config.PrivateApplication = true
	const surfaceID = "feishu:app-1:user:user-1"
	s.MaterializeSurface(surfaceID, "app-1", "chat-1", "user-1")
	v := s.root.Surfaces[surfaceID]
	v.ProductMode, v.Backend, v.ClaudeProfileID = state.ProductModeNormal, agentproto.BackendClaude, "profile"
	v.AttachedInstanceID = "instance"
	s.UpsertInstance(&state.InstanceRecord{InstanceID: "instance", WorkspaceRoot: "/workspace", WorkspaceKey: "/workspace", Backend: agentproto.BackendClaude, ClaudeProfileID: "profile", Online: true, Threads: map[string]*state.ThreadRecord{}})
	s.MaterializeBotCapabilitySettings([]state.BotCapabilitySettingsRecord{{GatewayID: "app-1", ProductMode: state.ProductModeNormal, Backend: agentproto.BackendClaude, ClaudeProfileID: "profile"}})
	key := state.ClaudeWorkspaceProfileSnapshotStorageKey("/workspace", agentproto.BackendClaude, "profile")
	s.root.ClaudeWorkspaceProfileSnapshots[key] = state.ClaudeWorkspaceProfileSnapshotRecord{ReasoningEffort: "low"}
	proposal, _, err := s.PreviewPrivateSettingsTransition(control.Action{Kind: control.ActionReasoningCommand, Text: "/reasoning high", SurfaceSessionID: surfaceID, GatewayID: "app-1", ChatID: "chat-1", ActorUserID: "user-1"})
	if err != nil || proposal == nil || proposal.PromptOverride.ReasoningEffort != "high" {
		t.Fatalf("preview proposal: %#v %v", proposal, err)
	}
	if got := s.root.ClaudeWorkspaceProfileSnapshots[key].ReasoningEffort; got != "low" {
		t.Fatalf("preview changed shared workspace snapshot: %q", got)
	}
	if got := s.BotCapabilitySettings()[0].PromptOverride.ReasoningEffort; got != "" {
		t.Fatalf("preview changed authoritative projection: %q", got)
	}
}

func TestPrivateClaudeSnapshotOverlayReceiptUsesTokenNotValue(t *testing.T) {
	s := newServiceForTest(nil)
	a := PrivateClaudeSnapshotChange{SurfaceID: "s", Key: "workspace", Token: "one", After: state.ClaudeWorkspaceProfileSnapshotRecord{AccessMode: agentproto.AccessModeConfirm}}
	b := a
	b.Token, b.After.AccessMode = "two", agentproto.AccessModeFullAccess
	last := a
	last.Token = "three"
	s.SetPrivateClaudeSnapshotOverlay(a)
	s.SetPrivateClaudeSnapshotOverlay(b)
	s.SetPrivateClaudeSnapshotOverlay(last)
	s.ClearPrivateClaudeSnapshotOverlay(a)
	if got := s.privateClaudeSnapshotOverlay["s"]["workspace"]; got.Token != "three" {
		t.Fatalf("old identical value cleared newer overlay: %#v", got)
	}
	s.ClearPrivateClaudeSnapshotOverlay(last)
	if _, ok := s.privateClaudeSnapshotOverlay["s"]["workspace"]; ok {
		t.Fatal("matching receipt did not clear overlay")
	}
}

func TestPrivateProfilePreviewKeepsWorkspaceUntilReceipt(t *testing.T) {
	now := time.Date(2026, 5, 1, 11, 10, 0, 0, time.UTC)
	s := newServiceForTest(&now)
	s.config.PrivateApplication = true
	const surfaceID = "feishu:app-1:user:user-1"
	s.MaterializeSurfaceResumeWithCodexProfile(surfaceID, "app-1", "chat-1", "user-1", state.ProductModeNormal, agentproto.BackendCodex, "default", "", "", state.PlanModeSettingOff)
	materializeTestCodexProfiles(s, state.CodexProfileSummary{ID: "team-proxy", Name: "Team Proxy"})
	v := s.root.Surfaces[surfaceID]
	originalProfile := v.CodexProfileID
	v.ClaimedWorkspaceKey = "/data/dl/repo"
	v.RouteMode = state.RouteModeNewThreadReady
	v.PreparedThreadCWD = "/data/dl/repo"
	a := control.Action{Kind: control.ActionCodexProfileCommand, Text: "/codexprofile team-proxy", GatewayID: "app-1", SurfaceSessionID: surfaceID, ChatID: "chat-1", ActorUserID: "user-1"}
	proposal, events, err := s.PreviewPrivateSettingsTransition(a)
	if err != nil || proposal == nil || proposal.CodexProfileID != "team-proxy" || len(events) != 0 {
		t.Fatalf("preview: %#v %v", proposal, err)
	}
	if v.PendingHeadless != nil || v.CodexProfileID != originalProfile || v.RouteMode != state.RouteModeNewThreadReady {
		t.Fatalf("preview changed workspace: pending=%#v profile=%q route=%q", v.PendingHeadless, v.CodexProfileID, v.RouteMode)
	}
	events = s.ApplySurfaceAction(a)
	if v.CodexProfileID != "team-proxy" || v.PendingHeadless == nil || !v.PendingHeadless.PrepareNewThread {
		t.Fatalf("profile transition: %#v", v)
	}
	found := false
	for _, event := range events {
		if event.DaemonCommand != nil && event.DaemonCommand.Kind == control.DaemonCommandStartHeadless {
			found = true
		}
	}
	if !found {
		t.Fatal("missing profile restart")
	}
}
