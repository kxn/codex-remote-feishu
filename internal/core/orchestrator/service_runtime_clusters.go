package orchestrator

import (
	"fmt"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type servicePickerRuntime struct {
	service                *Service
	nextPathPickerID       int
	nextTargetPickerID     int
	nextThreadHistoryID    int
	nextReviewPickerID     int
	nextCompactFlowID      int
	nextPlanProposalID     int
	nextLauncherFlowID     int
	pathPickerConsumers    map[string]PathPickerConsumer
	pathPickerEntryFilters map[string]PathPickerEntryFilter
}

func newServicePickerRuntime(service *Service) *servicePickerRuntime {
	return &servicePickerRuntime{
		service:                service,
		pathPickerConsumers:    map[string]PathPickerConsumer{},
		pathPickerEntryFilters: map[string]PathPickerEntryFilter{},
	}
}

func (r *servicePickerRuntime) registerPathPickerConsumer(kind string, consumer PathPickerConsumer) {
	if r == nil {
		return
	}
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return
	}
	if consumer == nil {
		delete(r.pathPickerConsumers, kind)
		return
	}
	r.pathPickerConsumers[kind] = consumer
}

func (r *servicePickerRuntime) lookupPathPickerConsumer(kind string) (PathPickerConsumer, bool) {
	if r == nil {
		return nil, false
	}
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return nil, false
	}
	consumer := r.pathPickerConsumers[kind]
	return consumer, consumer != nil
}

func (r *servicePickerRuntime) registerPathPickerEntryFilter(kind string, filter PathPickerEntryFilter) {
	if r == nil {
		return
	}
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return
	}
	if filter == nil {
		delete(r.pathPickerEntryFilters, kind)
		return
	}
	r.pathPickerEntryFilters[kind] = filter
}

func (r *servicePickerRuntime) lookupPathPickerEntryFilter(kind string) (PathPickerEntryFilter, bool) {
	if r == nil {
		return nil, false
	}
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return nil, false
	}
	filter := r.pathPickerEntryFilters[kind]
	return filter, filter != nil
}

func (r *servicePickerRuntime) nextPathPickerToken() string {
	r.nextPathPickerID++
	return fmt.Sprintf("picker-%d", r.nextPathPickerID)
}

func (r *servicePickerRuntime) nextTargetPickerToken() string {
	r.nextTargetPickerID++
	return fmt.Sprintf("target-picker-%d", r.nextTargetPickerID)
}

func (r *servicePickerRuntime) nextThreadHistoryToken() string {
	r.nextThreadHistoryID++
	return fmt.Sprintf("thread-history-%d", r.nextThreadHistoryID)
}

func (r *servicePickerRuntime) nextReviewPickerToken() string {
	r.nextReviewPickerID++
	return fmt.Sprintf("review-picker-%d", r.nextReviewPickerID)
}

func (r *servicePickerRuntime) nextCompactFlowToken() string {
	r.nextCompactFlowID++
	return fmt.Sprintf("compact-%d", r.nextCompactFlowID)
}

func (r *servicePickerRuntime) nextPlanProposalToken() string {
	r.nextPlanProposalID++
	return fmt.Sprintf("plan-proposal-%d", r.nextPlanProposalID)
}

func (r *servicePickerRuntime) nextLauncherFlowToken() string {
	r.nextLauncherFlowID++
	return fmt.Sprintf("launcher-flow-%d", r.nextLauncherFlowID)
}

func (r *servicePickerRuntime) recordSurfaceThreadHistory(surfaceID string, history agentproto.ThreadHistoryRecord) {
	if r == nil || r.service == nil {
		return
	}
	surface := r.service.root.Surfaces[strings.TrimSpace(surfaceID)]
	if surface == nil {
		return
	}
	cloned := cloneThreadHistoryRecord(history)
	surface.LastThreadHistory = &cloned
}

func (r *servicePickerRuntime) surfaceThreadHistory(surfaceID string) *agentproto.ThreadHistoryRecord {
	if r == nil || r.service == nil {
		return nil
	}
	surface := r.service.root.Surfaces[strings.TrimSpace(surfaceID)]
	if surface == nil || surface.LastThreadHistory == nil {
		return nil
	}
	cloned := cloneThreadHistoryRecord(*surface.LastThreadHistory)
	return &cloned
}

type serviceCatalogRuntime struct {
	service              *Service
	persistedThreads     PersistedThreadCatalog
	persistedThreadsLast []state.ThreadRecord
	persistedWorkspaces  map[string]time.Time
}

func newServiceCatalogRuntime(service *Service) *serviceCatalogRuntime {
	return &serviceCatalogRuntime{service: service}
}

func (r *serviceCatalogRuntime) setPersistedThreadCatalog(catalog PersistedThreadCatalog) {
	if r == nil {
		return
	}
	r.persistedThreads = catalog
	r.persistedThreadsLast = nil
	r.persistedWorkspaces = nil
}

func (r *serviceCatalogRuntime) recentPersistedThreads(limit int) []state.ThreadRecord {
	if r == nil || r.persistedThreads == nil {
		return nil
	}
	threads, err := r.persistedThreads.RecentThreads(limit)
	if err != nil {
		if len(r.persistedThreadsLast) == 0 {
			return nil
		}
		return clonePersistedThreads(r.persistedThreadsLast)
	}
	r.persistedThreadsLast = clonePersistedThreads(threads)
	return clonePersistedThreads(threads)
}

func (r *serviceCatalogRuntime) recentPersistedWorkspaces(limit int) map[string]time.Time {
	if r == nil || r.persistedThreads == nil {
		return nil
	}
	workspaces, err := r.persistedThreads.RecentWorkspaces(limit)
	if err == nil {
		normalized := normalizePersistedWorkspaceRecency(workspaces)
		r.persistedWorkspaces = clonePersistedWorkspaceRecency(normalized)
		return normalized
	}
	if len(r.persistedWorkspaces) > 0 {
		return clonePersistedWorkspaceRecency(r.persistedWorkspaces)
	}
	return workspaceRecencyFromThreads(r.recentPersistedThreads(persistedRecentThreadLimit))
}

type serviceTurnRuntime struct {
	service       *Service
	pendingRemote map[string]*remoteTurnBinding
	activeRemote  map[string]*remoteTurnBinding
	pendingSteers map[string]*pendingSteerBinding
	compactTurns  map[string]*compactTurnBinding
}

func newServiceTurnRuntime(service *Service) *serviceTurnRuntime {
	return &serviceTurnRuntime{
		service:       service,
		pendingRemote: map[string]*remoteTurnBinding{},
		activeRemote:  map[string]*remoteTurnBinding{},
		pendingSteers: map[string]*pendingSteerBinding{},
		compactTurns:  map[string]*compactTurnBinding{},
	}
}

func (r *serviceTurnRuntime) instanceHasCompact(instanceID string) bool {
	if r == nil || strings.TrimSpace(instanceID) == "" {
		return false
	}
	return r.compactTurns[instanceID] != nil
}

func (r *serviceTurnRuntime) surfaceHasPendingCompact(surface *state.SurfaceConsoleRecord) bool {
	if r == nil || surface == nil || strings.TrimSpace(surface.AttachedInstanceID) == "" {
		return false
	}
	binding := r.compactTurns[surface.AttachedInstanceID]
	return binding != nil && binding.SurfaceSessionID == surface.SurfaceSessionID
}

func (r *serviceTurnRuntime) isCompactTurn(instanceID, threadID, turnID string) bool {
	if r == nil || strings.TrimSpace(instanceID) == "" || strings.TrimSpace(turnID) == "" {
		return false
	}
	binding := r.compactTurns[instanceID]
	if binding == nil || binding.TurnID != turnID {
		return false
	}
	return binding.ThreadID == "" || threadID == "" || binding.ThreadID == threadID
}

type serviceProgressRuntime struct {
	service             *Service
	turnPlanSnapshots   map[string]*turnPlanSnapshotRecord
	mcpToolCallProgress map[string]*mcpToolCallProgressRecord
	pendingTurnText     map[string]*completedTextItem
	pendingPlanProposal map[string]*completedTextItem
	turnFileChanges     map[string]*turnFileChangeSummary
	turnDiffSnapshots   map[string]*control.TurnDiffSnapshot
}

func newServiceProgressRuntime(service *Service) *serviceProgressRuntime {
	return &serviceProgressRuntime{
		service:             service,
		turnPlanSnapshots:   map[string]*turnPlanSnapshotRecord{},
		mcpToolCallProgress: map[string]*mcpToolCallProgressRecord{},
		pendingTurnText:     map[string]*completedTextItem{},
		pendingPlanProposal: map[string]*completedTextItem{},
		turnFileChanges:     map[string]*turnFileChangeSummary{},
		turnDiffSnapshots:   map[string]*control.TurnDiffSnapshot{},
	}
}

func (r *serviceProgressRuntime) instanceHasCompact(instanceID string) bool {
	if r == nil || r.service == nil {
		return false
	}
	return r.service.turns.instanceHasCompact(instanceID)
}

func (r *serviceProgressRuntime) surfaceHasPendingCompact(surface *state.SurfaceConsoleRecord) bool {
	if r == nil || r.service == nil {
		return false
	}
	return r.service.turns.surfaceHasPendingCompact(surface)
}

func (r *serviceProgressRuntime) isCompactTurn(instanceID, threadID, turnID string) bool {
	if r == nil || r.service == nil {
		return false
	}
	return r.service.turns.isCompactTurn(instanceID, threadID, turnID)
}
