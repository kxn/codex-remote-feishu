package orchestrator

import "github.com/kxn/codex-remote-feishu/internal/core/state"

// Global snapshots can contain this thread on unrelated headless instances.
// Visibility must use the final merged workspace, after persisted metadata has
// been applied, rather than the reporting instance's copied workspace label.
func visibleInstanceMatchesMergedThread(inst *state.InstanceRecord, view *mergedThreadView) bool {
	return inst != nil && view != nil &&
		(!isHeadlessInstance(inst) || instanceWorkspaceClaimKey(inst) == mergedThreadWorkspaceClaimKey(view))
}

func (s *Service) refreshMergedThreadVisibility(surface *state.SurfaceConsoleRecord, view *mergedThreadView, instances []*state.InstanceRecord) {
	view.CurrentVisible = false
	view.AnyVisibleInst = nil
	view.FreeVisibleInst = nil
	view.CompatibleAnyVisibleInst = nil
	view.CompatibleFreeVisibleInst = nil
	for _, inst := range instances {
		if !visibleInstanceMatchesMergedThread(inst, view) || state.EffectiveInstanceBackend(inst) != view.Backend {
			continue
		}
		thread := inst.Threads[view.ThreadID]
		if !ordinaryThreadVisible(thread) || !threadBelongsToInstanceWorkspaceForTarget(inst, thread) {
			continue
		}
		if surface != nil && inst.InstanceID == surface.AttachedInstanceID {
			view.CurrentVisible = true
		}
		if !inst.Online {
			continue
		}
		owner := s.instanceClaimSurface(inst.InstanceID)
		free := owner == nil || (surface != nil && owner.SurfaceSessionID == surface.SurfaceSessionID)
		if betterVisibleThreadInstance(surface, view.AnyVisibleInst, inst, thread) {
			view.AnyVisibleInst = inst
		}
		if free && betterVisibleThreadInstance(surface, view.FreeVisibleInst, inst, thread) {
			view.FreeVisibleInst = inst
		}
		if s.surfaceInstanceCompatibility(surface, inst).Compatible {
			if betterVisibleThreadInstance(surface, view.CompatibleAnyVisibleInst, inst, thread) {
				view.CompatibleAnyVisibleInst = inst
			}
			if free && betterVisibleThreadInstance(surface, view.CompatibleFreeVisibleInst, inst, thread) {
				view.CompatibleFreeVisibleInst = inst
			}
		}
	}
}
