package orchestrator

import (
	"fmt"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/core/threadcatalogcontract"
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
	persistedThreads     threadcatalogcontract.PersistedThreadCatalog
	persistedThreadsLast map[agentproto.Backend][]state.ThreadRecord
	persistedWorkspaces  map[agentproto.Backend]map[string]time.Time
}

func newServiceCatalogRuntime(service *Service) *serviceCatalogRuntime {
	return &serviceCatalogRuntime{service: service}
}

func (r *serviceCatalogRuntime) setPersistedThreadCatalog(catalog threadcatalogcontract.PersistedThreadCatalog) {
	if r == nil {
		return
	}
	r.persistedThreads = catalog
	r.persistedThreadsLast = map[agentproto.Backend][]state.ThreadRecord{}
	r.persistedWorkspaces = map[agentproto.Backend]map[string]time.Time{}
}

func (r *serviceCatalogRuntime) recentPersistedThreads(limit int) []state.ThreadRecord {
	return r.recentPersistedThreadsForBackend(agentproto.BackendCodex, limit)
}

func (r *serviceCatalogRuntime) recentPersistedThreadsForBackend(backend agentproto.Backend, limit int) []state.ThreadRecord {
	if r == nil || r.persistedThreads == nil {
		return nil
	}
	backend = agentproto.NormalizeBackend(backend)
	threads, err := r.loadPersistedThreadsForBackend(backend, limit)
	if err != nil {
		if len(r.persistedThreadsLast[backend]) == 0 {
			return nil
		}
		return clonePersistedThreads(r.persistedThreadsLast[backend])
	}
	if r.persistedThreadsLast == nil {
		r.persistedThreadsLast = map[agentproto.Backend][]state.ThreadRecord{}
	}
	r.persistedThreadsLast[backend] = clonePersistedThreads(threads)
	return clonePersistedThreads(threads)
}

func (r *serviceCatalogRuntime) recentPersistedWorkspaces(limit int) map[string]time.Time {
	return r.recentPersistedWorkspacesForBackend(agentproto.BackendCodex, limit)
}

func (r *serviceCatalogRuntime) recentPersistedWorkspacesForBackend(backend agentproto.Backend, limit int) map[string]time.Time {
	if r == nil || r.persistedThreads == nil {
		return nil
	}
	backend = agentproto.NormalizeBackend(backend)
	workspaces, err := r.loadPersistedWorkspacesForBackend(backend, limit)
	if err == nil {
		normalized := normalizePersistedWorkspaceRecency(workspaces)
		if r.persistedWorkspaces == nil {
			r.persistedWorkspaces = map[agentproto.Backend]map[string]time.Time{}
		}
		r.persistedWorkspaces[backend] = clonePersistedWorkspaceRecency(normalized)
		return normalized
	}
	if cached := r.persistedWorkspaces[backend]; len(cached) > 0 {
		return clonePersistedWorkspaceRecency(cached)
	}
	return workspaceRecencyFromThreads(r.recentPersistedThreadsForBackend(backend, persistedRecentThreadLimit))
}

func (r *serviceCatalogRuntime) persistedThreadByIDForBackend(backend agentproto.Backend, threadID string) (*state.ThreadRecord, error) {
	if r == nil || r.persistedThreads == nil {
		return nil, nil
	}
	backend = agentproto.NormalizeBackend(backend)
	if backendCatalog, ok := r.persistedThreads.(threadcatalogcontract.BackendAwarePersistedThreadCatalog); ok {
		return backendCatalog.ThreadByIDForBackend(backend, threadID)
	}
	if backend != agentproto.BackendCodex {
		return nil, nil
	}
	return r.persistedThreads.ThreadByID(threadID)
}

func (r *serviceCatalogRuntime) loadPersistedThreadsForBackend(backend agentproto.Backend, limit int) ([]state.ThreadRecord, error) {
	if backendCatalog, ok := r.persistedThreads.(threadcatalogcontract.BackendAwarePersistedThreadCatalog); ok {
		return backendCatalog.RecentThreadsForBackend(backend, limit)
	}
	if backend != agentproto.BackendCodex {
		return nil, nil
	}
	return r.persistedThreads.RecentThreads(limit)
}

func (r *serviceCatalogRuntime) loadPersistedWorkspacesForBackend(backend agentproto.Backend, limit int) (map[string]time.Time, error) {
	if backendCatalog, ok := r.persistedThreads.(threadcatalogcontract.BackendAwarePersistedThreadCatalog); ok {
		return backendCatalog.RecentWorkspacesForBackend(backend, limit)
	}
	if backend != agentproto.BackendCodex {
		return nil, nil
	}
	return r.persistedThreads.RecentWorkspaces(limit)
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

func (r *serviceTurnRuntime) pendingRemoteBinding(instanceID string) *remoteTurnBinding {
	if r == nil {
		return nil
	}
	return r.pendingRemote[strings.TrimSpace(instanceID)]
}

func (r *serviceTurnRuntime) bindPendingRemote(instanceID string, binding *remoteTurnBinding) {
	if r == nil {
		return
	}
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" || binding == nil {
		return
	}
	r.pendingRemote[instanceID] = binding
}

func (r *serviceTurnRuntime) clearPendingRemote(instanceID string) {
	if r == nil {
		return
	}
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return
	}
	delete(r.pendingRemote, instanceID)
}

func (r *serviceTurnRuntime) forEachPendingRemote(fn func(*remoteTurnBinding)) {
	if r == nil || fn == nil {
		return
	}
	for _, binding := range r.pendingRemote {
		if binding != nil {
			fn(binding)
		}
	}
}

func (r *serviceTurnRuntime) activeRemoteBinding(instanceID string) *remoteTurnBinding {
	if r == nil {
		return nil
	}
	return r.activeRemote[strings.TrimSpace(instanceID)]
}

func (r *serviceTurnRuntime) bindActiveRemote(instanceID string, binding *remoteTurnBinding) {
	if r == nil {
		return
	}
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" || binding == nil {
		return
	}
	r.activeRemote[instanceID] = binding
}

func (r *serviceTurnRuntime) clearActiveRemote(instanceID string) {
	if r == nil {
		return
	}
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return
	}
	delete(r.activeRemote, instanceID)
}

func (r *serviceTurnRuntime) forEachActiveRemote(fn func(*remoteTurnBinding)) {
	if r == nil || fn == nil {
		return
	}
	for _, binding := range r.activeRemote {
		if binding != nil {
			fn(binding)
		}
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

func (r *serviceTurnRuntime) pendingSteerBinding(key string) *pendingSteerBinding {
	if r == nil {
		return nil
	}
	return r.pendingSteers[strings.TrimSpace(key)]
}

func (r *serviceTurnRuntime) bindPendingSteer(key string, binding *pendingSteerBinding) {
	if r == nil {
		return
	}
	key = strings.TrimSpace(key)
	if key == "" || binding == nil {
		return
	}
	r.pendingSteers[key] = binding
}

func (r *serviceTurnRuntime) clearPendingSteer(key string) {
	if r == nil {
		return
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	delete(r.pendingSteers, key)
}

func (r *serviceTurnRuntime) forEachPendingSteer(fn func(string, *pendingSteerBinding)) {
	if r == nil || fn == nil {
		return
	}
	for key, binding := range r.pendingSteers {
		if binding != nil {
			fn(key, binding)
		}
	}
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

func (r *serviceProgressRuntime) pendingTurnTextItem(instanceID, threadID, turnID string) *completedTextItem {
	if r == nil {
		return nil
	}
	return r.pendingTurnText[turnRenderKey(instanceID, threadID, turnID)]
}

func (r *serviceProgressRuntime) storePendingTurnText(item *completedTextItem) *completedTextItem {
	if r == nil || item == nil {
		return nil
	}
	key := turnRenderKey(item.InstanceID, item.ThreadID, item.TurnID)
	previous := r.pendingTurnText[key]
	r.pendingTurnText[key] = item
	return previous
}

func (r *serviceProgressRuntime) takePendingTurnText(instanceID, threadID, turnID string) *completedTextItem {
	if r == nil {
		return nil
	}
	key := turnRenderKey(instanceID, threadID, turnID)
	pending := r.pendingTurnText[key]
	delete(r.pendingTurnText, key)
	return pending
}

func (r *serviceProgressRuntime) storePendingPlanProposal(item *completedTextItem) {
	if r == nil || item == nil {
		return
	}
	r.pendingPlanProposal[turnRenderKey(item.InstanceID, item.ThreadID, item.TurnID)] = item
}

func (r *serviceProgressRuntime) pendingPlanProposalItem(instanceID, threadID, turnID string) *completedTextItem {
	if r == nil {
		return nil
	}
	return r.pendingPlanProposal[turnRenderKey(instanceID, threadID, turnID)]
}

func (r *serviceProgressRuntime) takePendingPlanProposal(instanceID, threadID, turnID string) *completedTextItem {
	if r == nil {
		return nil
	}
	key := turnRenderKey(instanceID, threadID, turnID)
	pending := r.pendingPlanProposal[key]
	delete(r.pendingPlanProposal, key)
	return pending
}

func (r *serviceProgressRuntime) clearPendingTextForTurn(instanceID, threadID, turnID string) {
	if r == nil || strings.TrimSpace(turnID) == "" {
		return
	}
	key := turnRenderKey(instanceID, threadID, turnID)
	delete(r.pendingTurnText, key)
	delete(r.pendingPlanProposal, key)
}

func (r *serviceProgressRuntime) clearPendingTextForInstance(instanceID string) {
	if r == nil {
		return
	}
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return
	}
	for key, item := range r.pendingTurnText {
		if item != nil && item.InstanceID == instanceID {
			delete(r.pendingTurnText, key)
		}
	}
	for key, item := range r.pendingPlanProposal {
		if item != nil && item.InstanceID == instanceID {
			delete(r.pendingPlanProposal, key)
		}
	}
}

func (r *serviceProgressRuntime) upsertTurnPlanSnapshot(record *turnPlanSnapshotRecord) bool {
	if r == nil || record == nil || record.Snapshot == nil {
		return false
	}
	key := turnPlanSnapshotKey(record.SurfaceSessionID, record.InstanceID, record.ThreadID, record.TurnID)
	if existing := r.turnPlanSnapshots[key]; existing != nil && equalTurnPlanSnapshot(existing.Snapshot, record.Snapshot) {
		return false
	}
	r.turnPlanSnapshots[key] = record
	return true
}

func (r *serviceProgressRuntime) clearTurnPlanSnapshots(instanceID, threadID, turnID string) {
	if r == nil {
		return
	}
	for key, record := range r.turnPlanSnapshots {
		if record == nil {
			continue
		}
		if record.InstanceID != instanceID {
			continue
		}
		if threadID != "" && record.ThreadID != threadID {
			continue
		}
		if turnID != "" && record.TurnID != turnID {
			continue
		}
		delete(r.turnPlanSnapshots, key)
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
