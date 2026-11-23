package orchestrator

import (
	"encoding/json"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type PrivateUpgradeResume struct {
	SurfaceID, OldInstanceID, ThreadID, CWD, ProfileID, HistoryNamespace string
	Backend                                                              agentproto.Backend
}

// The host freezes previous in the same durable maintenance recovery record as
// the launch intent. Pausing never clears queued input or a user's prior pause.
func (s *Service) SuspendPrivateUpgradeDispatch(surfaceID, oldInstanceID string) (state.DispatchMode, error) {
	v := s.root.Surfaces[surfaceID]
	if !s.config.PrivateApplication || v == nil || oldInstanceID == "" || v.AttachedInstanceID != oldInstanceID {
		return "", fmt.Errorf("private upgrade dispatch owner changed")
	}
	// Pausing admission is valid while a turn is active. It does not settle that
	// turn; replacement preparation and resumption require independent proof.
	previous := v.DispatchMode
	if previous == "" {
		previous = state.DispatchModeNormal
	}
	if previous != state.DispatchModeNormal && previous != state.DispatchModePausedForLocal {
		return "", fmt.Errorf("private upgrade must wait for current dispatch transition")
	}
	v.DispatchMode = state.DispatchModePausedForLocal
	return previous, nil
}

// Called only after the host commits global maintenance release, under the
// Processor and its exact replacement-carrier authority. Its events are saved
// with the release projection, so a restart cannot repeat queued dispatch.
func (s *Service) ResumePrivateUpgradeDispatch(surfaceID, newInstanceID string, previous state.DispatchMode) ([]eventcontract.Event, error) {
	v, inst := s.root.Surfaces[surfaceID], s.root.Instances[newInstanceID]
	if !s.config.PrivateApplication || v == nil || inst == nil || newInstanceID == "" || v.AttachedInstanceID != newInstanceID || !inst.Online || !isHeadlessInstance(inst) || v.PendingHeadless != nil || v.ActiveQueueItemID != "" || (previous != state.DispatchModeNormal && previous != state.DispatchModePausedForLocal) {
		return nil, fmt.Errorf("private upgrade replacement is not ready")
	}
	cp, err := s.ExportPrivateCheckpoint(surfaceID)
	if err != nil {
		return nil, err
	}
	if len(cp.Holds) != 0 {
		return nil, fmt.Errorf("private upgrade replacement remains unresolved")
	}
	delete(s.privateRecoveryHolds, surfaceID)
	v.DispatchMode = previous
	return s.dispatchNext(v), nil
}

// PreparePrivateUpgradeResume is a carrier replacement, not a change of work
// target. The caller must first prove retirement through the maintenance owner
// and commit the returned effect with its immutable recovery intent. Unlike
// directory/thread selection, this transition never discards queued drafts,
// staged media or per-conversation settings, and never invents a terminal turn.
func (s *Service) PreparePrivateUpgradeResume(req PrivateUpgradeResume) ([]eventcontract.Event, error) {
	if !s.config.PrivateApplication || req.SurfaceID == "" || req.OldInstanceID == "" || req.CWD == "" || req.HistoryNamespace == "" || req.Backend != agentproto.BackendCodex {
		return nil, fmt.Errorf("invalid private upgrade recovery")
	}
	v, old := s.root.Surfaces[req.SurfaceID], s.root.Instances[req.OldInstanceID]
	if v == nil || old == nil || v.AttachedInstanceID != req.OldInstanceID || v.SelectedThreadID != req.ThreadID || v.PendingHeadless != nil || !isHeadlessInstance(old) || state.EffectiveInstanceBackend(old) != req.Backend || s.surfaceBackend(v) != req.Backend {
		return nil, fmt.Errorf("private upgrade recovery no longer owns selected carrier")
	}
	policy, ok := s.privateThreadPolicies[req.SurfaceID]
	if !ok || policy.InstanceNamespaces[req.OldInstanceID] != req.HistoryNamespace || (req.ThreadID != "" && !s.privateThreadVisible(req.SurfaceID, req.Backend, req.OldInstanceID, req.ThreadID)) {
		return nil, fmt.Errorf("private upgrade history namespace changed")
	}
	contract := s.headlessLaunchContract(v)
	if contract.Backend != req.Backend || contract.CodexProfileID != req.ProfileID || state.NormalizeCodexProfileID(old.CodexProfileID) != req.ProfileID || normalizeWorkspaceClaimKey(old.WorkspaceRoot) != normalizeWorkspaceClaimKey(req.CWD) {
		return nil, fmt.Errorf("private upgrade launch settings changed")
	}
	cp, err := s.ExportPrivateCheckpoint(req.SurfaceID)
	if err != nil {
		return nil, err
	}
	if len(cp.Holds) != 0 || cp.ActiveQueueItemID != "" || old.ActiveTurnID != "" {
		return nil, fmt.Errorf("private upgrade has unresolved execution")
	}
	workspace := s.surfaceCurrentWorkspaceKeyRaw(v)
	if workspace == "" {
		return nil, fmt.Errorf("private upgrade lost selected workspace")
	}
	s.nextHeadlessID++
	id := fmt.Sprintf("inst-headless-%d-%d", s.now().UnixNano(), s.nextHeadlessID)
	pending := &state.HeadlessLaunchRecord{InstanceID: id, SourceInstanceID: old.InstanceID, ThreadID: req.ThreadID, WorkspaceKey: workspace, ThreadCWD: req.CWD, Backend: contract.Backend, CodexProfileID: contract.CodexProfileID, CodexAdmissionRef: state.NormalizeCodexAdmissionRef(contract.CodexAdmissionRef), CodexConnectionContract: state.CloneCodexConnectionContract(contract.CodexConnectionContract), CodexThreadPolicy: state.CloneCodexThreadPolicy(contract.CodexThreadPolicy), RequestedAt: s.now(), ExpiresAt: s.now().Add(s.config.HeadlessLaunchWait), Status: state.HeadlessLaunchStarting, Purpose: state.HeadlessLaunchPurposePrivateUpgrade, PrepareNewThread: v.RouteMode == state.RouteModeNewThreadReady}
	if thread := old.Threads[req.ThreadID]; thread != nil {
		pending.ThreadTitle, pending.ThreadName, pending.ThreadPreview = displayThreadTitle(old, thread), thread.Name, thread.Preview
	}
	old.Online = false
	v.PendingHeadless = pending
	command := &control.DaemonCommand{Kind: control.DaemonCommandStartHeadless, GatewayID: v.GatewayID, SurfaceSessionID: v.SurfaceSessionID, InstanceID: id, ThreadID: req.ThreadID, ThreadTitle: pending.ThreadTitle, WorkspaceKey: workspace, ThreadCWD: req.CWD}
	s.applyHeadlessLaunchContract(command, contract)
	return []eventcontract.Event{{Kind: eventcontract.KindDaemonCommand, GatewayID: v.GatewayID, SurfaceSessionID: v.SurfaceSessionID, DaemonCommand: command}}, nil
}

// RestorePrivateUpgradeHeadlessProjection restores only the frozen pending
// replacement matched by the host to its committed recovery effect. It is not
// permission to launch, release a native hold, or create another generation.
func (s *Service) RestorePrivateUpgradeHeadlessProjection(req PrivateUpgradeResume, newInstanceID string) error {
	v := s.root.Surfaces[req.SurfaceID]
	if !s.config.PrivateApplication || v == nil || newInstanceID == "" || v.AttachedInstanceID != req.OldInstanceID || v.SelectedThreadID != req.ThreadID {
		return fmt.Errorf("private upgrade recovery target changed")
	}
	match := func(p *state.HeadlessLaunchRecord) bool {
		return p != nil && p.Purpose == state.HeadlessLaunchPurposePrivateUpgrade && p.InstanceID == newInstanceID && p.SourceInstanceID == req.OldInstanceID && p.ThreadID == req.ThreadID && p.ThreadCWD == req.CWD && p.Backend == req.Backend && p.CodexProfileID == req.ProfileID
	}
	if v.PendingHeadless != nil {
		if match(v.PendingHeadless) {
			return nil
		}
		return fmt.Errorf("another private headless launch is pending")
	}
	held, ok := s.privateRecoveryHolds[req.SurfaceID]
	if !ok || len(held.Holds) != 1 || held.Holds[0].Kind != "headless_launch" {
		return fmt.Errorf("private upgrade headless evidence absent or execution unresolved")
	}
	var pending state.HeadlessLaunchRecord
	if json.Unmarshal(held.Holds[0].Frozen, &pending) != nil || !match(&pending) {
		return fmt.Errorf("private upgrade headless evidence mismatch")
	}
	v.PendingHeadless = &pending
	delete(s.privateRecoveryHolds, req.SurfaceID)
	return nil
}

func (s *Service) attachPrivateUpgradeInstance(v *state.SurfaceConsoleRecord, inst *state.InstanceRecord, pending *state.HeadlessLaunchRecord) []eventcontract.Event {
	if !s.config.PrivateApplication || v.AttachedInstanceID != pending.SourceInstanceID || v.SelectedThreadID != pending.ThreadID || v.ActiveQueueItemID != "" || inst.InstanceID != pending.InstanceID || state.EffectiveInstanceBackend(inst) != pending.Backend {
		return notice(v, "private_upgrade_restore_changed", "运行环境已连接，但原会话选择发生变化；升级恢复仍在核对，未切换当前工作。")
	}
	if pending.ThreadID != "" {
		thread := s.ensureThread(inst, pending.ThreadID)
		thread.WorkspaceKey, thread.CWD = pending.WorkspaceKey, pending.ThreadCWD
		thread.Name, thread.Preview = pending.ThreadName, pending.ThreadPreview
	}
	next := surfaceRouteCoreState{AttachedInstanceID: inst.InstanceID, WorkspaceKey: pending.WorkspaceKey, RouteMode: v.RouteMode, SelectedThreadID: v.SelectedThreadID, PreparedThreadCWD: v.PreparedThreadCWD, PreparedFromThreadID: v.PreparedFromThreadID, ThreadClaimPolicy: surfaceRouteThreadClaimKnown}
	if !s.transitionSurfaceRouteCore(v, inst, next) {
		return notice(v, "private_upgrade_restore_conflict", "原会话暂时不能重新连接，升级恢复会保留当前工作并继续核对。")
	}
	s.applyPendingHeadlessRuntimeToInstance(v, inst, pending)
	s.consumeSurfacePendingHeadlessLaunch(v, pending.InstanceID)
	return nil
}
