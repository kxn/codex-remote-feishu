package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type contractResolutionMode string

const (
	contractResolutionUnavailable    contractResolutionMode = "unavailable"
	contractResolutionCurrentVisible contractResolutionMode = "current_visible"
	contractResolutionAttachVisible  contractResolutionMode = "attach_visible"
	contractResolutionReuseManaged   contractResolutionMode = "reuse_managed"
	contractResolutionRestartManaged contractResolutionMode = "restart_managed"
	contractResolutionCreateHeadless contractResolutionMode = "create_headless"
)

type contractResolutionSubjectKind string

const (
	contractResolutionSubjectThread    contractResolutionSubjectKind = "thread"
	contractResolutionSubjectWorkspace contractResolutionSubjectKind = "workspace"
)

type contractResolutionContext struct {
	SubjectKind        contractResolutionSubjectKind
	ThreadID           string
	WorkspaceKey       string
	Backend            agentproto.Backend
	CurrentVisibleOK   bool
	BusyOwnerPresent   bool
	CWD                string
	VisibleCandidates  []*state.InstanceRecord
	PreferredManaged   *state.InstanceRecord
	AllowDirectVisible func(*state.InstanceRecord) bool
	NotFoundCode       string
	NotFoundText       string
	CWDMissingCode     string
	CWDMissingText     string
	BusyCode           string
	BusyText           string
}

type contractResolution struct {
	Mode             contractResolutionMode
	Instance         *state.InstanceRecord
	RestartInstance  *state.InstanceRecord
	NoticeCode       string
	NoticeText       string
	IncompatibleSeen bool
}

func (resolution contractResolution) allowsDirectAttach() bool {
	switch resolution.Mode {
	case contractResolutionAttachVisible, contractResolutionReuseManaged, contractResolutionRestartManaged:
		return true
	default:
		return false
	}
}

func (s *Service) resolveHeadlessContract(surface *state.SurfaceConsoleRecord, ctx contractResolutionContext) contractResolution {
	if ctx.CurrentVisibleOK {
		return contractResolution{Mode: contractResolutionCurrentVisible}
	}
	if ctx.BusyOwnerPresent {
		return contractResolution{
			Mode:       contractResolutionUnavailable,
			NoticeCode: firstNonEmpty(ctx.BusyCode, "thread_busy"),
			NoticeText: firstNonEmpty(ctx.BusyText, "目标当前已被其他飞书会话占用。"),
		}
	}
	incompatibleSeen := false
	for _, inst := range ctx.VisibleCandidates {
		if inst == nil {
			continue
		}
		if ctx.AllowDirectVisible != nil && !ctx.AllowDirectVisible(inst) {
			continue
		}
		compat := s.surfaceInstanceCompatibility(surface, inst)
		if compat.Compatible {
			return contractResolution{
				Mode:     contractResolutionAttachVisible,
				Instance: inst,
			}
		}
		if compat.Visible {
			if isHeadlessInstance(inst) {
				if ctx.PreferredManaged == nil {
					ctx.PreferredManaged = inst
				}
			}
			incompatibleSeen = true
		}
	}
	if ctx.PreferredManaged != nil && strings.TrimSpace(ctx.CWD) != "" {
		if headlessThreadWorkspaceMustMatch(ctx.PreferredManaged) && !cwdBelongsToInstanceWorkspace(ctx.PreferredManaged, ctx.CWD) {
			return contractResolution{
				Mode:             contractResolutionRestartManaged,
				RestartInstance:  ctx.PreferredManaged,
				IncompatibleSeen: incompatibleSeen,
			}
		}
		if s.surfaceInstanceCompatibleForAttach(surface, ctx.PreferredManaged) {
			return contractResolution{
				Mode:     contractResolutionReuseManaged,
				Instance: ctx.PreferredManaged,
			}
		}
		return contractResolution{
			Mode:             contractResolutionRestartManaged,
			RestartInstance:  ctx.PreferredManaged,
			IncompatibleSeen: true,
		}
	}
	if strings.TrimSpace(ctx.CWD) == "" {
		return contractResolution{
			Mode:       contractResolutionUnavailable,
			NoticeCode: firstNonEmpty(ctx.CWDMissingCode, "thread_cwd_missing"),
			NoticeText: firstNonEmpty(ctx.CWDMissingText, "目标缺少可恢复的工作目录，当前无法直接接管。"),
		}
	}
	return contractResolution{
		Mode:             contractResolutionCreateHeadless,
		IncompatibleSeen: incompatibleSeen,
	}
}

func (s *Service) visibleThreadInstancesForView(view *mergedThreadView) []*state.InstanceRecord {
	if view == nil {
		return nil
	}
	seen := map[string]struct{}{}
	instances := make([]*state.InstanceRecord, 0, 4)
	appendInst := func(inst *state.InstanceRecord) {
		if inst == nil {
			return
		}
		if _, ok := seen[inst.InstanceID]; ok {
			return
		}
		seen[inst.InstanceID] = struct{}{}
		instances = append(instances, inst)
	}
	appendInst(view.CompatibleFreeVisibleInst)
	appendInst(view.CompatibleAnyVisibleInst)
	appendInst(view.FreeVisibleInst)
	appendInst(view.AnyVisibleInst)
	return instances
}

func (s *Service) reusableManagedHeadlessForResolution(surface *state.SurfaceConsoleRecord, cwd string, backend agentproto.Backend) *state.InstanceRecord {
	cwd = strings.TrimSpace(cwd)
	backend = agentproto.NormalizeBackend(backend)
	var candidates []*state.InstanceRecord
	for _, inst := range s.Instances() {
		if inst == nil || !inst.Online || !isHeadlessInstance(inst) {
			continue
		}
		if backend != "" && state.EffectiveInstanceBackend(inst) != backend {
			continue
		}
		if owner := s.instanceClaimSurface(inst.InstanceID); owner != nil && (surface == nil || owner.SurfaceSessionID != surface.SurfaceSessionID) {
			continue
		}
		candidates = append(candidates, inst)
	}
	if len(candidates) == 0 {
		return nil
	}
	var bestCompatible *state.InstanceRecord
	var bestIncompatible *state.InstanceRecord
	for _, inst := range candidates {
		if s.surfaceInstanceCompatibleForAttach(surface, inst) {
			if bestCompatible == nil || reusableHeadlessScore(surface, inst, cwd) > reusableHeadlessScore(surface, bestCompatible, cwd) ||
				(reusableHeadlessScore(surface, inst, cwd) == reusableHeadlessScore(surface, bestCompatible, cwd) && inst.InstanceID < bestCompatible.InstanceID) {
				bestCompatible = inst
			}
			continue
		}
		if bestIncompatible == nil || reusableHeadlessScore(surface, inst, cwd) > reusableHeadlessScore(surface, bestIncompatible, cwd) ||
			(reusableHeadlessScore(surface, inst, cwd) == reusableHeadlessScore(surface, bestIncompatible, cwd) && inst.InstanceID < bestIncompatible.InstanceID) {
			bestIncompatible = inst
		}
	}
	if bestCompatible != nil {
		return bestCompatible
	}
	return bestIncompatible
}

func (s *Service) resolveThreadContract(surface *state.SurfaceConsoleRecord, view *mergedThreadView, currentVisible bool, directVisibleOnly bool) contractResolution {
	if view == nil {
		return contractResolution{
			Mode:       contractResolutionUnavailable,
			NoticeCode: "thread_not_found",
			NoticeText: "目标会话不存在或当前不可见。",
		}
	}
	allowVisible := func(inst *state.InstanceRecord) bool {
		if inst == nil {
			return false
		}
		if directVisibleOnly && !s.surfaceInstanceCompatibleForAttach(surface, inst) {
			return false
		}
		return true
	}
	return s.resolveHeadlessContract(surface, contractResolutionContext{
		SubjectKind:        contractResolutionSubjectThread,
		ThreadID:           view.ThreadID,
		WorkspaceKey:       mergedThreadWorkspaceClaimKey(view),
		Backend:            view.Backend,
		CurrentVisibleOK:   currentVisible,
		BusyOwnerPresent:   view.BusyOwner != nil,
		CWD:                threadCWD(view),
		VisibleCandidates:  s.visibleThreadInstancesForView(view),
		PreferredManaged:   s.reusableManagedHeadlessForResolution(surface, threadCWD(view), view.Backend),
		AllowDirectVisible: allowVisible,
		NotFoundCode:       "thread_not_found",
		NotFoundText:       "目标会话不存在或当前不可见。",
		CWDMissingCode:     "thread_cwd_missing",
		CWDMissingText:     "目标会话缺少可恢复的工作目录，当前无法直接接管。",
		BusyCode:           "thread_busy",
		BusyText:           "目标会话当前已被其他飞书会话占用。",
	})
}

func (s *Service) resolveWorkspaceContract(surface *state.SurfaceConsoleRecord, workspaceKey string, backend agentproto.Backend) contractResolution {
	workspaceKey = normalizeWorkspaceClaimKey(workspaceKey)
	backend = agentproto.NormalizeBackend(backend)
	if workspaceKey == "" {
		return contractResolution{
			Mode:       contractResolutionUnavailable,
			NoticeCode: "workspace_not_found",
			NoticeText: "目标工作区不存在。请重新发送 /list。",
		}
	}
	if owner := s.workspaceBusyOwnerForSurface(surface, workspaceKey); owner != nil {
		return contractResolution{
			Mode:       contractResolutionUnavailable,
			NoticeCode: "workspace_busy",
			NoticeText: "目标 workspace 当前已被其他飞书会话接管，请等待对方 /detach。",
		}
	}
	instances := s.workspaceOnlineInstancesForBackend(workspaceKey, backend)
	if len(instances) == 0 {
		return contractResolution{
			Mode:       contractResolutionCreateHeadless,
			NoticeCode: "workspace_not_found",
			NoticeText: "目标工作区已失效，请重新发送 /list。",
		}
	}
	s.sortWorkspaceAttachInstances(surface, workspaceKey, instances)
	var visibleCandidates []*state.InstanceRecord
	var incompatibleSeen bool
	for _, inst := range instances {
		if inst == nil {
			continue
		}
		visibleCandidates = append(visibleCandidates, inst)
		if compat := s.surfaceInstanceCompatibility(surface, inst); compat.Visible && !compat.Compatible {
			incompatibleSeen = true
		}
	}
	resolved := s.resolveHeadlessContract(surface, contractResolutionContext{
		SubjectKind:        contractResolutionSubjectWorkspace,
		WorkspaceKey:       workspaceKey,
		Backend:            backend,
		CWD:                workspaceKey,
		VisibleCandidates:  visibleCandidates,
		PreferredManaged:   s.reusableManagedHeadlessForResolution(surface, workspaceKey, backend),
		AllowDirectVisible: func(inst *state.InstanceRecord) bool { return inst != nil },
		NotFoundCode:       "workspace_not_found",
		NotFoundText:       "目标工作区已失效，请重新发送 /list。",
		CWDMissingCode:     "workspace_key_missing",
		CWDMissingText:     "当前无法确定目标对应的工作区，暂时不能在 headless 模式接管。请切到 `/mode vscode` 后再试。",
		BusyCode:           "workspace_instance_busy",
		BusyText:           "目标工作区当前暂时不可接管，请稍后重试。",
	})
	if resolved.Mode == contractResolutionCreateHeadless {
		resolved.IncompatibleSeen = resolved.IncompatibleSeen || incompatibleSeen
	}
	return resolved
}
