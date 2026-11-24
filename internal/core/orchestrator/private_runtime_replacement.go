package orchestrator

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

var ErrPrivateRuntimeProfileChanged = errors.New("private runtime profile changed")

// MaterializeRetiredPrivateReplacement uses only the checkpoint's previously
// confirmed navigation and the exact host-verified retired carrier identity.
// The historical instance is offline; no execution is inferred or recreated.
type PrivateRetiredContract struct {
	Profiles                  PrivateNativeProfiles
	CodexAdmissionRef         *state.CodexAdmissionRef
	ClaudeReasoningEffort     string
	OpenCodeAdmissionRef      *state.OpenCodeAdmissionRef
	OpenCodeRuntimeAccessMode string
}

func (s *Service) MaterializeRetiredPrivateReplacement(surface string, owner agentproto.NativeOwner, contract PrivateRetiredContract, cwd string) ([]eventcontract.Event, error) {
	v := s.root.Surfaces[surface]
	actual := contract.Profiles
	n, ok := s.privateNavigation[surface]
	if !s.config.PrivateApplication || v == nil || v.AttachedInstanceID != owner.InstanceID || owner.Validate() != nil {
		return nil, fmt.Errorf("retired private navigation changed")
	}
	if !ok && v.RouteMode == state.RouteModeNewThreadReady && v.SelectedThreadID == "" && v.PreparedThreadCWD != "" {
		n = PrivateNavigationSnapshot{Owner: owner, Actual: actual, CWD: v.PreparedThreadCWD, WorkspaceKey: v.ClaimedWorkspaceKey}
	} else if !ok || n.Owner != owner || n.Actual != actual || v.SelectedThreadID != n.ThreadID {
		return nil, fmt.Errorf("retired private navigation changed")
	}
	if n.WorkspaceKey == "" || n.CWD == "" {
		return nil, fmt.Errorf("retired private workspace missing")
	}
	if !privateProfilesCompatible(v, actual) {
		return nil, ErrPrivateRuntimeProfileChanged
	}
	cp, err := s.ExportPrivateCheckpoint(surface)
	if err != nil {
		return nil, err
	}
	if len(cp.Holds) != 0 || cp.ActiveQueueItemID != "" {
		return nil, fmt.Errorf("retired private execution remains unresolved")
	}
	old := s.root.Instances[owner.InstanceID]
	if old != nil && old.Online {
		return nil, fmt.Errorf("retired private carrier still online")
	}
	if old == nil {
		old = &state.InstanceRecord{InstanceID: owner.InstanceID, Source: "headless", Managed: true, Backend: owner.Backend, WorkspaceRoot: cwd, WorkspaceKey: n.WorkspaceKey, Threads: map[string]*state.ThreadRecord{}}
		s.UpsertInstance(old)
	}
	old.CodexProfileID = actual.Codex
	old.ClaudeProfileID = actual.Claude
	old.OpenCodeProfileID = actual.OpenCode
	if n.ThreadID != "" {
		old.Threads[n.ThreadID] = &state.ThreadRecord{ThreadID: n.ThreadID, CWD: n.CWD, WorkspaceKey: n.WorkspaceKey}
		if !s.claimKnownThread(v, old, n.ThreadID) {
			return nil, fmt.Errorf("retired private session claimed elsewhere")
		}
	}
	profile := ""
	switch owner.Backend {
	case agentproto.BackendCodex:
		profile = actual.Codex
	case agentproto.BackendClaude:
		profile = actual.Claude
	case agentproto.BackendOpenCode:
		profile = actual.OpenCode
	}
	events, err := s.PreparePrivateRuntimeReplacement(PrivateUpgradeResume{SurfaceID: surface, OldInstanceID: owner.InstanceID, ThreadID: n.ThreadID, CWD: n.CWD, ProfileID: profile, HistoryNamespace: owner.HistoryNamespace, Backend: owner.Backend})
	if err != nil {
		return nil, err
	}
	if v.PendingHeadless != nil {
		v.PendingHeadless.AutoRestore = true
		v.PendingHeadless.CodexAdmissionRef = state.NormalizeCodexAdmissionRef(contract.CodexAdmissionRef)
		v.PendingHeadless.ClaudeReasoningEffort = contract.ClaudeReasoningEffort
		v.PendingHeadless.OpenCodeAdmissionRef = state.NormalizeOpenCodeAdmissionRef(contract.OpenCodeAdmissionRef)
		v.PendingHeadless.OpenCodeRuntimeAccessMode = contract.OpenCodeRuntimeAccessMode
	}
	for i := range events {
		if events[i].DaemonCommand != nil {
			events[i].DaemonCommand.AutoRestore = true
			events[i].DaemonCommand.CodexAdmissionRef = state.NormalizeCodexAdmissionRef(contract.CodexAdmissionRef)
			events[i].DaemonCommand.ClaudeReasoningEffort = contract.ClaudeReasoningEffort
			events[i].DaemonCommand.OpenCodeAdmissionRef = state.NormalizeOpenCodeAdmissionRef(contract.OpenCodeAdmissionRef)
			events[i].DaemonCommand.OpenCodeRuntimeAccessMode = contract.OpenCodeRuntimeAccessMode
		}
	}
	return events, nil
}

// RememberRetiredPrivateNavigation records a previously applied native thread
// fact under the host's exact retired carrier authority; it does not connect it.
func (s *Service) RememberRetiredPrivateNavigation(surface string, owner agentproto.NativeOwner, actual PrivateNativeProfiles, event agentproto.Event) error {
	v := s.root.Surfaces[surface]
	if v == nil || v.RouteMode != state.RouteModePinned || v.AttachedInstanceID != owner.InstanceID || v.SelectedThreadID == "" || event.Kind != agentproto.EventThreadDiscovered || event.ThreadID != v.SelectedThreadID || (!filepath.IsAbs(event.CWD) || filepath.Clean(event.CWD) != event.CWD) || (event.NativeBinding != nil && event.NativeBinding.Owner != owner) || !privateProfilesCompatible(v, actual) {
		return fmt.Errorf("retired session evidence mismatch")
	}
	s.privateNavigation[surface] = PrivateNavigationSnapshot{Owner: owner, Actual: actual, ThreadID: event.ThreadID, CWD: event.CWD, WorkspaceKey: v.ClaimedWorkspaceKey, CodexProfileID: v.CodexProfileID, ClaudeProfileID: v.ClaudeProfileID, OpenCodeProfileID: v.OpenCodeProfileID}
	return nil
}

// PrivateReplacementInputTarget freezes the only route that may receive input
// during a normal retired-carrier replacement.
type PrivateReplacementInputTarget struct {
	SourceInstanceID, ReplacementInstanceID, ThreadID, CWD string
}

func (s *Service) PrivateReplacementInputTarget(surface string) (PrivateReplacementInputTarget, bool) {
	v := s.root.Surfaces[surface]
	if !s.config.PrivateApplication || v == nil || v.PendingHeadless == nil {
		return PrivateReplacementInputTarget{}, false
	}
	p := v.PendingHeadless
	if p.Purpose != state.HeadlessLaunchPurposePrivateUpgrade || !p.AutoRestore || p.SourceInstanceID != v.AttachedInstanceID || p.ThreadID != v.SelectedThreadID || p.InstanceID == "" || p.ThreadCWD == "" {
		return PrivateReplacementInputTarget{}, false
	}
	return PrivateReplacementInputTarget{p.SourceInstanceID, p.InstanceID, p.ThreadID, p.ThreadCWD}, true
}

func (s *Service) privateReplacementInputAction(action control.Action) bool {
	_, ok := s.PrivateReplacementInputTarget(action.SurfaceSessionID)
	return ok && (action.Kind == control.ActionTextMessage || action.Kind == control.ActionImageMessage || action.Kind == control.ActionFileMessage)
}

func (s *Service) handlePrivateReplacementRouteAction(action control.Action) ([]eventcontract.Event, bool) {
	if action.Kind != control.ActionDetach && action.Kind != control.ActionWorkspaceDetach && action.Kind != control.ActionNewThread {
		return nil, false
	}
	if _, ok := s.PrivateReplacementInputTarget(action.SurfaceSessionID); !ok {
		return nil, false
	}
	v := s.root.Surfaces[action.SurfaceSessionID]
	cp, err := s.ExportPrivateCheckpoint(action.SurfaceSessionID)
	if err != nil {
		return nil, false
	}
	if action.Kind == control.ActionNewThread {
		return s.cancelPrivateReplacementForNew(v, cp)
	}
	return s.detachPendingPrivateReplacement(v, cp)
}

// A new session cancels only the in-flight replacement while retaining the
// retired workspace so the existing /new transition can prepare its route.
func (s *Service) cancelPrivateReplacementForNew(v *state.SurfaceConsoleRecord, cp PrivateCheckpoint) ([]eventcontract.Event, bool) {
	if v == nil || v.PendingHeadless == nil || cp.ActiveQueueItemID != "" || len(cp.Holds) != 1 || cp.Holds[0].Kind != "headless_launch" {
		return nil, false
	}
	target, ok := s.PrivateReplacementInputTarget(v.SurfaceSessionID)
	if !ok {
		return nil, false
	}
	hold := cp.Holds[0]
	var pending state.HeadlessLaunchRecord
	if json.Unmarshal(hold.Frozen, &pending) != nil || pending.InstanceID != target.ReplacementInstanceID || pending.SourceInstanceID != target.SourceInstanceID {
		return nil, false
	}
	hold.InstanceID = pending.InstanceID
	s.privateDetachedExecutions[v.SurfaceSessionID] = append(s.privateDetachedExecutions[v.SurfaceSessionID], PrivateDetachedExecution{InstanceID: pending.InstanceID, Holds: []PrivateRecoveryHold{hold}})
	delete(s.privateRecoveryHolds, v.SurfaceSessionID)
	delete(s.privateOfflineStops, v.SurfaceSessionID)
	s.consumeSurfacePendingHeadlessLaunch(v, pending.InstanceID)
	events := s.discardDrafts(v)
	events = append(events, eventcontract.Event{Kind: eventcontract.KindDaemonCommand, SurfaceSessionID: v.SurfaceSessionID, DaemonCommand: &control.DaemonCommand{Kind: control.DaemonCommandKillHeadless, SurfaceSessionID: v.SurfaceSessionID, InstanceID: pending.InstanceID}})
	events = append(events, s.prepareNewThread(v)...)
	return events, true
}
