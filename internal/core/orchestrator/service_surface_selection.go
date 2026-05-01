package orchestrator

import (
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/gitmeta"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) presentInstanceSelection(surface *state.SurfaceConsoleRecord) []eventcontract.Event {
	return s.presentInstanceSelectionWithInline(surface, false)
}

func (s *Service) presentInstanceSelectionWithInline(surface *state.SurfaceConsoleRecord, inline bool) []eventcontract.Event {
	return s.presentInstanceSelectionWithAction(surface, control.Action{}, inline)
}

func (s *Service) presentInstanceSelectionWithAction(surface *state.SurfaceConsoleRecord, action control.Action, inline bool) []eventcontract.Event {
	_ = inline
	familyID, variantID, backend := s.catalogProvenanceForAction(surface, action)
	instances := make([]*state.InstanceRecord, 0, len(s.root.Instances))
	for _, inst := range s.root.Instances {
		if inst.Online && isVSCodeInstance(inst) {
			instances = append(instances, inst)
		}
	}
	if len(instances) == 0 {
		return notice(surface, "no_online_instances", "当前没有在线 VS Code 实例。请先在 VS Code 中打开 Codex 会话。")
	}
	available := make([]instanceSelectionEntry, 0, len(instances))
	unavailable := make([]instanceSelectionEntry, 0, len(instances))
	var current *control.FeishuInstanceSelectionCurrent
	for _, inst := range instances {
		if inst == nil {
			continue
		}
		isCurrent := surface != nil && surface.AttachedInstanceID == inst.InstanceID
		busy := false
		if owner := s.instanceClaimSurface(inst.InstanceID); owner != nil && (surface == nil || owner.SurfaceSessionID != surface.SurfaceSessionID) {
			busy = true
		}
		latestUsedAt := instanceLatestVisibleThreadUsedAt(inst)
		ageText := ""
		if !latestUsedAt.IsZero() {
			ageText = humanizeRelativeTime(s.now(), latestUsedAt)
		}
		buttonLabel := ""
		if !isCurrent && !busy && surface != nil && strings.TrimSpace(surface.AttachedInstanceID) != "" {
			buttonLabel = "切换"
		}
		entry := control.FeishuInstanceSelectionEntry{
			InstanceID:  inst.InstanceID,
			Label:       instanceSelectionLabel(inst),
			ButtonLabel: buttonLabel,
			MetaText:    instanceSelectionMetaText(inst, ageText, busy),
			HasFocus:    instanceHasObservedFocus(inst),
			Disabled:    busy,
		}
		if isCurrent {
			current = &control.FeishuInstanceSelectionCurrent{
				InstanceID:  inst.InstanceID,
				Label:       instanceSelectionLabel(inst),
				ContextText: s.instanceSelectionContextText(surface, inst),
			}
			continue
		}
		sorted := instanceSelectionEntry{
			entry:        entry,
			latestUsedAt: latestUsedAt,
			hasFocus:     instanceHasObservedFocus(inst),
		}
		if busy {
			unavailable = append(unavailable, sorted)
			continue
		}
		available = append(available, sorted)
	}
	sortInstanceSelectionEntries(available)
	sortInstanceSelectionEntries(unavailable)

	entries := make([]control.FeishuInstanceSelectionEntry, 0, len(available)+len(unavailable))
	appendEntries := func(items []instanceSelectionEntry) {
		for _, item := range items {
			entries = append(entries, item.entry)
		}
	}
	appendEntries(available)
	appendEntries(unavailable)

	return []eventcontract.Event{s.selectionViewEvent(surface, control.FeishuSelectionView{
		PromptKind:       control.SelectionPromptAttachInstance,
		CatalogFamilyID:  familyID,
		CatalogVariantID: variantID,
		CatalogBackend:   backend,
		Instance: &control.FeishuInstanceSelectionView{
			Current: current,
			Entries: entries,
		},
	})}
}

type instanceSelectionEntry struct {
	entry        control.FeishuInstanceSelectionEntry
	latestUsedAt time.Time
	hasFocus     bool
}

func sortInstanceSelectionEntries(entries []instanceSelectionEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		left := entries[i]
		right := entries[j]
		if left.hasFocus != right.hasFocus {
			return left.hasFocus
		}
		switch {
		case left.latestUsedAt.IsZero() && right.latestUsedAt.IsZero():
		case left.latestUsedAt.IsZero():
			return false
		case right.latestUsedAt.IsZero():
			return true
		case !left.latestUsedAt.Equal(right.latestUsedAt):
			return left.latestUsedAt.After(right.latestUsedAt)
		}
		return strings.TrimSpace(left.entry.InstanceID) < strings.TrimSpace(right.entry.InstanceID)
	})
}

func instanceSelectionLabel(inst *state.InstanceRecord) string {
	if inst == nil {
		return ""
	}
	label := strings.TrimSpace(inst.ShortName)
	if label == "" {
		label = strings.TrimSpace(filepath.Base(inst.WorkspaceKey))
	}
	if label == "" || label == "." || label == string(filepath.Separator) {
		label = strings.TrimSpace(inst.DisplayName)
	}
	if label == "" {
		label = strings.TrimSpace(inst.InstanceID)
	}
	return label
}

func instanceLatestVisibleThreadUsedAt(inst *state.InstanceRecord) time.Time {
	if inst == nil {
		return time.Time{}
	}
	latest := time.Time{}
	for _, thread := range ordinaryVisibleThreads(inst) {
		if thread == nil || !thread.LastUsedAt.After(latest) {
			continue
		}
		latest = thread.LastUsedAt
	}
	return latest
}

func instanceHasObservedFocus(inst *state.InstanceRecord) bool {
	return inst != nil && strings.TrimSpace(inst.ObservedFocusedThreadID) != ""
}

func instanceSelectionMetaText(inst *state.InstanceRecord, ageText string, busy bool) string {
	parts := make([]string, 0, 2)
	if age := strings.TrimSpace(ageText); age != "" {
		parts = append(parts, age)
	}
	switch {
	case busy:
		parts = append(parts, "当前被其他飞书会话接管")
	case instanceHasObservedFocus(inst):
		parts = append(parts, "当前焦点可跟随")
	default:
		parts = append(parts, "等待 VS Code 焦点")
	}
	return strings.Join(parts, " · ")
}

func (s *Service) instanceSelectionContextText(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord) string {
	label := instanceSelectionLabel(inst)
	stateText := s.vscodeInstanceSurfaceStatus(surface, inst)
	if stateText == "" {
		stateText = "等待 VS Code 焦点"
	}
	return strings.Join([]string{
		label + " · " + stateText,
		"焦点切换仍会自动跟随，换实例才用 /list",
	}, "\n")
}

func (s *Service) presentWorkspaceSelection(surface *state.SurfaceConsoleRecord) []eventcontract.Event {
	return s.presentWorkspaceSelectionPage(surface, 1)
}

func (s *Service) presentAllWorkspaceSelection(surface *state.SurfaceConsoleRecord) []eventcontract.Event {
	return s.presentWorkspaceSelectionPage(surface, 1)
}

const workspaceSelectionPageSize = 8

func (s *Service) presentWorkspaceSelectionPage(surface *state.SurfaceConsoleRecord, page int) []eventcontract.Event {
	model, events := s.buildWorkspaceSelectionModel(surface, page)
	if len(events) != 0 {
		return events
	}
	if model == nil {
		return nil
	}
	return []eventcontract.Event{s.selectionViewEvent(surface, control.FeishuSelectionView{
		PromptKind: control.SelectionPromptAttachWorkspace,
		Workspace:  model,
	})}
}

func (s *Service) buildWorkspaceSelectionModel(surface *state.SurfaceConsoleRecord, page int) (*control.FeishuWorkspaceSelectionView, []eventcontract.Event) {
	grouped := map[string][]*state.InstanceRecord{}
	targetBackend, filterByBackend := s.normalModeThreadBackend(surface)
	for _, inst := range s.root.Instances {
		if inst == nil || !inst.Online {
			continue
		}
		if filterByBackend && state.EffectiveInstanceBackend(inst) != targetBackend {
			continue
		}
		for _, workspaceKey := range instanceWorkspaceSelectionKeys(inst) {
			grouped[workspaceKey] = append(grouped[workspaceKey], inst)
		}
	}
	views := s.mergedThreadViews(surface)
	visibleWorkspaces := s.normalModeListWorkspaceSetWithViews(surface, views)
	if len(visibleWorkspaces) == 0 {
		return nil, notice(surface, "no_available_workspaces", "当前没有可接管的工作区。请先连接一个 VS Code 会话，或等待可恢复工作区出现。")
	}
	recoverableWorkspaces := map[string]time.Time{}
	recoverableWorkspaceSeen := map[string]bool{}
	for _, view := range views {
		workspaceKey := mergedThreadWorkspaceClaimKey(view)
		if workspaceKey == "" {
			continue
		}
		recoverableWorkspaceSeen[workspaceKey] = true
		usedAt := threadLastUsedAt(view)
		if current, ok := recoverableWorkspaces[workspaceKey]; !ok || usedAt.After(current) {
			recoverableWorkspaces[workspaceKey] = usedAt
		}
	}
	s.mergeWorkspaceSelectionRecencyFromOnlineThreads(surface, recoverableWorkspaces, recoverableWorkspaceSeen, visibleWorkspaces)
	s.mergeWorkspaceSelectionRecencyFromPersistedWorkspaces(surface, recoverableWorkspaces, recoverableWorkspaceSeen, visibleWorkspaces)

	currentWorkspace := s.surfaceCurrentWorkspaceKey(surface)
	entries := make([]workspaceSelectionEntry, 0, len(visibleWorkspaces))
	seenWorkspaceKeys := map[string]struct{}{}
	var current *control.FeishuWorkspaceSelectionCurrent
	for workspaceKey := range visibleWorkspaces {
		workspaceKey = normalizeWorkspaceClaimKey(workspaceKey)
		if workspaceKey == "" {
			continue
		}
		if _, exists := seenWorkspaceKeys[workspaceKey]; exists {
			continue
		}
		seenWorkspaceKeys[workspaceKey] = struct{}{}
		instances := append([]*state.InstanceRecord(nil), grouped[workspaceKey]...)
		s.sortWorkspaceAttachInstances(surface, workspaceKey, instances)

		latestUsedAt := recoverableWorkspaces[workspaceKey]
		ageText := ""
		if !latestUsedAt.IsZero() {
			ageText = humanizeRelativeTime(s.now(), latestUsedAt)
		}
		label := workspaceSelectionLabel(workspaceKey)
		hasVSCodeActivity := s.workspaceHasVSCodeActivity(instances)
		isCurrent := surface.AttachedInstanceID != "" && currentWorkspace != "" && currentWorkspace == workspaceKey
		busy := s.workspaceBusyOwnerForSurface(surface, workspaceKey) != nil
		attachable := s.resolveWorkspaceAttachInstanceFromCandidates(surface, workspaceKey, instances) != nil
		recoverableOnly := !attachable && len(instances) == 0 && recoverableWorkspaceSeen[workspaceKey]

		if isCurrent {
			current = &control.FeishuWorkspaceSelectionCurrent{
				WorkspaceKey:   workspaceKey,
				WorkspaceLabel: label,
				AgeText:        ageText,
			}
			continue
		}
		entry := workspaceSelectionEntry{
			workspaceKey:      workspaceKey,
			latestUsedAt:      latestUsedAt,
			label:             label,
			ageText:           ageText,
			hasVSCodeActivity: hasVSCodeActivity,
			busy:              busy,
			attachable:        attachable,
			recoverableOnly:   recoverableOnly,
		}
		entries = append(entries, entry)
	}

	sortWorkspaceSelectionEntries(entries)
	page, totalPages := paginatePage(page, len(entries), workspaceSelectionPageSize)
	start, end := pageBounds(page, workspaceSelectionPageSize, len(entries))
	model := &control.FeishuWorkspaceSelectionView{
		Page:       page,
		PageSize:   workspaceSelectionPageSize,
		TotalPages: totalPages,
		Current:    current,
		Entries:    make([]control.FeishuWorkspaceSelectionEntry, 0, maxInt(end-start, 0)),
	}
	for _, entry := range entries[start:end] {
		model.Entries = append(model.Entries, control.FeishuWorkspaceSelectionEntry{
			WorkspaceKey:      entry.workspaceKey,
			WorkspaceLabel:    entry.label,
			AgeText:           entry.ageText,
			HasVSCodeActivity: entry.hasVSCodeActivity,
			Busy:              entry.busy,
			Attachable:        entry.attachable,
			RecoverableOnly:   entry.recoverableOnly,
		})
	}
	return model, nil
}

type workspaceSelectionEntry struct {
	workspaceKey      string
	latestUsedAt      time.Time
	label             string
	gitInfo           gitmeta.WorkspaceInfo
	ageText           string
	hasVSCodeActivity bool
	busy              bool
	attachable        bool
	recoverableOnly   bool
}

func sortWorkspaceSelectionEntries(entries []workspaceSelectionEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		left := entries[i]
		right := entries[j]
		switch {
		case left.latestUsedAt.IsZero() && right.latestUsedAt.IsZero():
		case left.latestUsedAt.IsZero():
			return false
		case right.latestUsedAt.IsZero():
			return true
		case !left.latestUsedAt.Equal(right.latestUsedAt):
			return left.latestUsedAt.After(right.latestUsedAt)
		}
		return strings.TrimSpace(left.workspaceKey) < strings.TrimSpace(right.workspaceKey)
	})
}

func (s *Service) workspaceLatestVisibleThreadUsedAt(instances []*state.InstanceRecord, workspaceKey string) time.Time {
	workspaceKey = normalizeWorkspaceClaimKey(workspaceKey)
	latest := time.Time{}
	for _, inst := range instances {
		for _, thread := range workspaceVisibleThreads(inst, workspaceKey) {
			if thread == nil || !thread.LastUsedAt.After(latest) {
				continue
			}
			latest = thread.LastUsedAt
		}
	}
	return latest
}

func instanceWorkspaceSelectionKeys(inst *state.InstanceRecord) []string {
	if inst == nil {
		return nil
	}
	seen := map[string]struct{}{}
	keys := []string{}
	for _, thread := range ordinaryVisibleThreads(inst) {
		if thread == nil {
			continue
		}
		if !threadBelongsToInstanceWorkspace(inst, thread) {
			continue
		}
		key := normalizeWorkspaceClaimKey(thread.CWD)
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		if key := instanceWorkspaceClaimKey(inst); key != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

func instanceSupportsWorkspaceSelectionKey(inst *state.InstanceRecord, workspaceKey string) bool {
	workspaceKey = normalizeWorkspaceClaimKey(workspaceKey)
	if workspaceKey == "" || inst == nil {
		return false
	}
	for _, candidate := range instanceWorkspaceSelectionKeys(inst) {
		if candidate == workspaceKey {
			return true
		}
	}
	return false
}

func workspaceVisibleThreads(inst *state.InstanceRecord, workspaceKey string) []*state.ThreadRecord {
	workspaceKey = normalizeWorkspaceClaimKey(workspaceKey)
	if workspaceKey == "" || inst == nil {
		return nil
	}
	threads := []*state.ThreadRecord{}
	for _, thread := range ordinaryVisibleThreads(inst) {
		if thread == nil {
			continue
		}
		if !threadBelongsToInstanceWorkspace(inst, thread) {
			continue
		}
		if normalizeWorkspaceClaimKey(thread.CWD) != workspaceKey {
			continue
		}
		threads = append(threads, thread)
	}
	return threads
}

func threadBelongsToInstanceWorkspace(inst *state.InstanceRecord, thread *state.ThreadRecord) bool {
	if inst == nil || thread == nil {
		return false
	}
	if isVSCodeInstance(inst) {
		return true
	}
	root := normalizeWorkspaceClaimKey(inst.WorkspaceRoot)
	cwd := normalizeWorkspaceClaimKey(thread.CWD)
	if root == "" || cwd == "" {
		return true
	}
	return cwd == root || strings.HasPrefix(cwd, root+"/")
}

func (s *Service) workspaceOnlineInstances(workspaceKey string) []*state.InstanceRecord {
	workspaceKey = normalizeWorkspaceClaimKey(workspaceKey)
	if workspaceKey == "" {
		return nil
	}
	instances := []*state.InstanceRecord{}
	for _, inst := range s.root.Instances {
		if inst == nil || !inst.Online || !instanceSupportsWorkspaceSelectionKey(inst, workspaceKey) {
			continue
		}
		instances = append(instances, inst)
	}
	return instances
}

func (s *Service) workspaceOnlineInstancesForBackend(workspaceKey string, backend agentproto.Backend) []*state.InstanceRecord {
	backend = agentproto.NormalizeBackend(backend)
	instances := s.workspaceOnlineInstances(workspaceKey)
	if len(instances) == 0 {
		return nil
	}
	filtered := make([]*state.InstanceRecord, 0, len(instances))
	for _, inst := range instances {
		if inst == nil || state.EffectiveInstanceBackend(inst) != backend {
			continue
		}
		filtered = append(filtered, inst)
	}
	return filtered
}

func (s *Service) workspaceOnlineInstancesForSurfaceBackend(surface *state.SurfaceConsoleRecord, workspaceKey string, backend agentproto.Backend) []*state.InstanceRecord {
	instances := s.workspaceOnlineInstancesForBackend(workspaceKey, backend)
	if len(instances) == 0 {
		return nil
	}
	if surface == nil || backend != agentproto.BackendCodex {
		return instances
	}
	filtered := make([]*state.InstanceRecord, 0, len(instances))
	for _, inst := range instances {
		if inst == nil || !s.instanceMatchesSurfaceCodexProvider(surface, inst) {
			continue
		}
		filtered = append(filtered, inst)
	}
	return filtered
}

func (s *Service) sortWorkspaceAttachInstances(surface *state.SurfaceConsoleRecord, workspaceKey string, instances []*state.InstanceRecord) {
	sort.Slice(instances, func(i, j int) bool {
		left := instances[i]
		right := instances[j]

		leftCurrent := surface != nil && left != nil && left.InstanceID == surface.AttachedInstanceID
		rightCurrent := surface != nil && right != nil && right.InstanceID == surface.AttachedInstanceID
		if leftCurrent != rightCurrent {
			return leftCurrent
		}

		leftOwner := s.instanceClaimSurface(left.InstanceID)
		rightOwner := s.instanceClaimSurface(right.InstanceID)
		leftFree := leftOwner == nil || (surface != nil && leftOwner.SurfaceSessionID == surface.SurfaceSessionID)
		rightFree := rightOwner == nil || (surface != nil && rightOwner.SurfaceSessionID == surface.SurfaceSessionID)
		if leftFree != rightFree {
			return leftFree
		}

		leftScopedVisible := len(workspaceVisibleThreads(left, workspaceKey))
		rightScopedVisible := len(workspaceVisibleThreads(right, workspaceKey))
		if leftScopedVisible != rightScopedVisible {
			return leftScopedVisible > rightScopedVisible
		}

		leftExact := instanceWorkspaceClaimKey(left) == workspaceKey
		rightExact := instanceWorkspaceClaimKey(right) == workspaceKey
		if leftExact != rightExact {
			return leftExact
		}

		leftHeadless := isHeadlessInstance(left)
		rightHeadless := isHeadlessInstance(right)
		if leftHeadless != rightHeadless {
			return leftHeadless
		}

		leftVSCode := isVSCodeInstance(left)
		rightVSCode := isVSCodeInstance(right)
		if leftVSCode != rightVSCode {
			return leftVSCode
		}

		return left.InstanceID < right.InstanceID
	})
}

func (s *Service) resolveWorkspaceAttachInstanceFromCandidates(surface *state.SurfaceConsoleRecord, workspaceKey string, instances []*state.InstanceRecord) *state.InstanceRecord {
	if len(instances) == 0 {
		return nil
	}
	s.sortWorkspaceAttachInstances(surface, workspaceKey, instances)
	for _, inst := range instances {
		owner := s.instanceClaimSurface(inst.InstanceID)
		if owner == nil || (surface != nil && owner.SurfaceSessionID == surface.SurfaceSessionID) {
			return inst
		}
	}
	return nil
}

func (s *Service) resolveWorkspaceAttachInstance(surface *state.SurfaceConsoleRecord, workspaceKey string) *state.InstanceRecord {
	instances := s.workspaceOnlineInstances(workspaceKey)
	if surface != nil && s.surfaceIsHeadless(surface) {
		instances = s.workspaceOnlineInstancesForSurfaceBackend(surface, workspaceKey, s.surfaceBackend(surface))
	}
	return s.resolveWorkspaceAttachInstanceFromCandidates(surface, workspaceKey, instances)
}

func (s *Service) resolveWorkspaceAttachInstanceForBackend(surface *state.SurfaceConsoleRecord, workspaceKey string, backend agentproto.Backend) *state.InstanceRecord {
	instances := s.workspaceOnlineInstancesForSurfaceBackend(surface, workspaceKey, backend)
	return s.resolveWorkspaceAttachInstanceFromCandidates(surface, workspaceKey, instances)
}

func (s *Service) workspaceHasVSCodeActivity(instances []*state.InstanceRecord) bool {
	for _, inst := range instances {
		if inst == nil || !isVSCodeInstance(inst) {
			continue
		}
		if strings.TrimSpace(inst.ObservedFocusedThreadID) != "" || strings.TrimSpace(inst.ActiveThreadID) != "" {
			return true
		}
	}
	return false
}

func workspaceSelectionLabel(workspaceKey string) string {
	if label := strings.TrimSpace(filepath.Base(workspaceKey)); label != "" && label != "." && label != string(filepath.Separator) {
		return label
	}
	return workspaceKey
}

func (s *Service) mergeWorkspaceSelectionRecencyFromOnlineThreads(surface *state.SurfaceConsoleRecord, latest map[string]time.Time, seen map[string]bool, visible map[string]struct{}) {
	if s == nil {
		return
	}
	targetBackend, filterByBackend := s.normalModeThreadBackend(surface)
	for _, inst := range s.root.Instances {
		if inst == nil || !inst.Online {
			continue
		}
		if filterByBackend && state.EffectiveInstanceBackend(inst) != targetBackend {
			continue
		}
		for _, thread := range ordinaryVisibleThreads(inst) {
			mergeWorkspaceSelectionThreadRecency(latest, seen, visible, thread)
		}
	}
}

func (s *Service) mergeWorkspaceSelectionRecencyFromPersistedWorkspaces(surface *state.SurfaceConsoleRecord, latest map[string]time.Time, seen map[string]bool, visible map[string]struct{}) {
	if s == nil || s.catalog.persistedThreads == nil {
		return
	}
	workspaces := s.catalog.recentPersistedWorkspaces(persistedRecentWorkspaceLimit)
	if backend, filterByBackend := s.normalModeThreadBackend(surface); filterByBackend {
		workspaces = s.catalog.recentPersistedWorkspacesForBackend(backend, persistedRecentWorkspaceLimit)
	}
	for workspaceKey, usedAt := range workspaces {
		workspaceKey = normalizeWorkspaceClaimKey(workspaceKey)
		if workspaceKey == "" || workspaceSelectionInternalProbeWorkspace(workspaceKey) {
			continue
		}
		seen[workspaceKey] = true
		if visible != nil {
			visible[workspaceKey] = struct{}{}
		}
		if current, ok := latest[workspaceKey]; !ok || usedAt.After(current) {
			latest[workspaceKey] = usedAt
		}
	}
}

func mergeWorkspaceSelectionThreadRecency(latest map[string]time.Time, seen map[string]bool, visible map[string]struct{}, thread *state.ThreadRecord) {
	workspaceKey, usedAt := workspaceSelectionThreadKeyAndUsedAt(thread)
	if workspaceKey == "" {
		return
	}
	seen[workspaceKey] = true
	if visible != nil {
		visible[workspaceKey] = struct{}{}
	}
	if current, ok := latest[workspaceKey]; !ok || usedAt.After(current) {
		latest[workspaceKey] = usedAt
	}
}

func workspaceSelectionThreadKeyAndUsedAt(thread *state.ThreadRecord) (string, time.Time) {
	if !ordinaryThreadVisible(thread) {
		return "", time.Time{}
	}
	workspaceKey := normalizeWorkspaceClaimKey(thread.CWD)
	if workspaceKey == "" || workspaceSelectionInternalProbeWorkspace(workspaceKey) {
		return "", time.Time{}
	}
	return workspaceKey, thread.LastUsedAt
}

func workspaceSelectionInternalProbeWorkspace(workspaceKey string) bool {
	workspaceKey = state.NormalizeWorkspaceKey(workspaceKey)
	if workspaceKey == "" {
		return false
	}
	return strings.Contains(workspaceKey, "/_tmp-codex-thread-latency-") || strings.Contains(workspaceKey, "/_tmp-codex-appserver-")
}
