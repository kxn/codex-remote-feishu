package orchestrator

import (
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/gitmeta"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// Only the surface-owned local UI records are persisted. These records contain
// no live process or network state; expiry remains the original expiry.
type privateCheckpointUI struct {
	ModelWait              *PrivateModelWait
	UpgradeFlow            *PrivateUpgradeFlow
	CodexContractVersion   string
	MCPOAuth               map[string]privateMCPOAuthRequest
	AsyncUserInputs        map[string]*asyncUserInputRecord
	AsyncUserInputSequence uint64
	OwnerFlow              *activeOwnerCardFlowRecord
	PathPicker             *activePathPickerRecord
	TargetPicker           *activeTargetPickerRecord
	TargetOptions          *privateTargetOptions
	ThreadHistory          *activeThreadHistoryRecord
	WorkspacePage          *activeWorkspacePageRecord
}
type privateTargetOptions struct {
	Workspaces          []privateWorkspaceOption
	Sessions            map[string][]control.FeishuTargetPickerSessionOption
	DefaultSessions     map[string]string
	CurrentWorkspaceKey string
}
type privateWorkspaceOption struct {
	WorkspaceKey                                                       string
	LatestUsedAt                                                       time.Time
	Label                                                              string
	GitInfo                                                            gitmeta.WorkspaceInfo
	AgeText                                                            string
	HasVSCodeActivity, Busy, Attachable, WorktreeBase, RecoverableOnly bool
}

func (s *Service) exportPrivateCheckpointUI(v *state.SurfaceConsoleRecord) *privateCheckpointUI {
	r := s.surfaceUIRuntimeState(v)
	if r == nil {
		return nil
	}
	u := &privateCheckpointUI{ModelWait: r.PrivateModelWait, UpgradeFlow: r.PrivateUpgradeFlow, CodexContractVersion: r.PrivateCodexContractVersion, MCPOAuth: r.PrivateMCPOAuth, AsyncUserInputs: r.AsyncUserInputs, AsyncUserInputSequence: r.AsyncUserInputSequence, OwnerFlow: r.ActiveOwnerCardFlow, PathPicker: r.ActivePathPicker, ThreadHistory: r.ActiveThreadHistory, WorkspacePage: r.ActiveWorkspacePage}
	if r.ActiveTargetPicker != nil {
		copy := *r.ActiveTargetPicker
		if options := copy.OptionSnapshot; options != nil {
			o := &privateTargetOptions{Sessions: options.Sessions, DefaultSessions: options.DefaultSessions, CurrentWorkspaceKey: options.CurrentWorkspaceKey}
			for _, w := range options.Workspaces {
				o.Workspaces = append(o.Workspaces, privateWorkspaceOption{w.workspaceKey, w.latestUsedAt, w.label, w.gitInfo, w.ageText, w.hasVSCodeActivity, w.busy, w.attachable, w.worktreeBase, w.recoverableOnly})
			}
			u.TargetOptions = o
		}
		copy.OptionSnapshot = nil
		u.TargetPicker = &copy
	}
	return u
}

func validatePrivateCheckpointUI(u *privateCheckpointUI, owner string) error {
	if u == nil {
		return nil
	}
	for id, req := range u.MCPOAuth {
		if id == "" || req.InstanceID == "" || req.ServerName == "" {
			return fmt.Errorf("invalid private MCP OAuth request")
		}
	}
	for id, item := range u.AsyncUserInputs {
		if item == nil || id == "" || id != item.ID || (item.ActorUserID != "" && item.ActorUserID != owner) || item.Sequence > u.AsyncUserInputSequence {
			return fmt.Errorf("invalid private async input")
		}
	}
	if u.UpgradeFlow != nil && (u.UpgradeFlow.ID == "" || u.UpgradeFlow.OwnerUserID != owner) {
		return fmt.Errorf("invalid private upgrade flow owner")
	}
	if u.OwnerFlow != nil && (u.OwnerFlow.FlowID == "" || u.OwnerFlow.OwnerUserID != owner) {
		return fmt.Errorf("invalid private owner flow")
	}
	if u.PathPicker != nil && (u.PathPicker.PickerID == "" || u.PathPicker.OwnerUserID != owner || (u.PathPicker.OwnerFlowID != "" && (u.OwnerFlow == nil || u.PathPicker.OwnerFlowID != u.OwnerFlow.FlowID))) {
		return fmt.Errorf("invalid private path picker")
	}
	if u.TargetPicker != nil && (u.TargetPicker.PickerID == "" || u.TargetPicker.OwnerUserID != owner || u.OwnerFlow == nil || u.OwnerFlow.Kind != ownerCardFlowKindTargetPicker) {
		return fmt.Errorf("invalid private target picker")
	}
	if u.TargetOptions != nil && u.TargetPicker == nil {
		return fmt.Errorf("orphan private target options")
	}
	if h := u.ThreadHistory; h != nil && h.QueryCommandID != "" && (h.QueryInstanceID == "" || h.ThreadID == "") {
		return fmt.Errorf("invalid private history query")
	}
	if u.WorkspacePage != nil && u.WorkspacePage.OwnerUserID != owner {
		return fmt.Errorf("invalid private workspace page")
	}
	return nil
}

func (s *Service) restorePrivateCheckpointUI(v *state.SurfaceConsoleRecord, u *privateCheckpointUI) {
	if u == nil {
		return
	}
	r := s.ensureSurfaceUIRuntime(v)
	r.PrivateModelWait = u.ModelWait
	r.PrivateMCPOAuth = u.MCPOAuth
	r.AsyncUserInputs, r.AsyncUserInputSequence = u.AsyncUserInputs, u.AsyncUserInputSequence
	r.PrivateUpgradeFlow = u.UpgradeFlow
	r.PrivateCodexContractVersion = u.CodexContractVersion
	r.ActiveOwnerCardFlow, r.ActivePathPicker, r.ActiveTargetPicker = u.OwnerFlow, u.PathPicker, u.TargetPicker
	r.ActiveThreadHistory, r.ActiveWorkspacePage = u.ThreadHistory, u.WorkspacePage
	if u.TargetOptions != nil {
		o := &targetPickerOptionSnapshot{Sessions: u.TargetOptions.Sessions, DefaultSessions: u.TargetOptions.DefaultSessions, CurrentWorkspaceKey: u.TargetOptions.CurrentWorkspaceKey}
		for _, w := range u.TargetOptions.Workspaces {
			o.Workspaces = append(o.Workspaces, workspaceSelectionEntry{workspaceKey: w.WorkspaceKey, latestUsedAt: w.LatestUsedAt, label: w.Label, gitInfo: w.GitInfo, ageText: w.AgeText, hasVSCodeActivity: w.HasVSCodeActivity, busy: w.Busy, attachable: w.Attachable, worktreeBase: w.WorktreeBase, recoverableOnly: w.RecoverableOnly})
		}
		r.ActiveTargetPicker.OptionSnapshot = o
	}
}

// PrivateTargetPickerUsesGitPreview identifies the legacy worktree page whose
// view builder launches git; it must be observed outside a core transaction.
func (s *Service) PrivateTargetPickerUsesGitPreview(surfaceID string) bool {
	v := s.root.Surfaces[surfaceID]
	if v == nil {
		return false
	}
	r := s.activeTargetPicker(v)
	return r != nil && targetPickerEffectivePage(r) == control.FeishuTargetPickerPageWorktree
}

// PrivateRestoredPickerCardMatches authorizes an old lifecycle stamp only for
// the exact persisted card, picker and authenticated surface owner. Expired
// flows or unbound cards cannot be revived by an inbound callback.
func (s *Service) PrivateRestoredPickerCardMatches(a control.Action) bool {
	if !s.config.PrivateApplication || a.Inbound == nil || !a.Inbound.CardCallback || a.Inbound.OpenMessageID == "" {
		return false
	}
	v := s.root.Surfaces[a.SurfaceSessionID]
	if v == nil || v.GatewayID != a.GatewayID || v.ChatID != a.ChatID || v.ActorUserID != a.ActorUserID {
		return false
	}
	if f := s.activeOwnerCardFlow(v); f != nil && f.Kind == ownerCardFlowKindCommandMenu && f.OwnerUserID == a.ActorUserID && f.MessageID == a.Inbound.OpenMessageID && (f.ExpiresAt.IsZero() || f.ExpiresAt.After(s.now())) {
		switch a.Kind {
		case control.ActionShowCommandMenu, control.ActionShowCommandHelp, control.ActionModeCommand, control.ActionModelCommand, control.ActionReasoningCommand, control.ActionAccessCommand, control.ActionPlanCommand, control.ActionVerboseCommand, control.ActionAutoContinueCommand, control.ActionAutoWhipCommand:
			return true
		}
	}
	if a.PickerID == "" {
		return false
	}
	if p := s.activePathPicker(v); p != nil && p.PickerID == a.PickerID && p.OwnerUserID == a.ActorUserID && p.MessageID == a.Inbound.OpenMessageID && (p.ExpiresAt.IsZero() || p.ExpiresAt.After(s.now())) {
		return true
	}
	if p := s.activeTargetPicker(v); p != nil && p.PickerID == a.PickerID && p.OwnerUserID == a.ActorUserID && (p.ExpiresAt.IsZero() || p.ExpiresAt.After(s.now())) {
		f := s.activeOwnerCardFlow(v)
		return f != nil && f.Kind == ownerCardFlowKindTargetPicker && f.OwnerUserID == a.ActorUserID && f.MessageID == a.Inbound.OpenMessageID && (f.ExpiresAt.IsZero() || f.ExpiresAt.After(s.now()))
	}
	return false
}
