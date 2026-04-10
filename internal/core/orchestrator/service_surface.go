package orchestrator

import (
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type HeadlessRestoreAttempt struct {
	ThreadID    string
	ThreadTitle string
	ThreadCWD   string
}

type SurfaceResumeAttempt struct {
	InstanceID   string
	ThreadID     string
	WorkspaceKey string
}

type HeadlessRestoreStatus string

const (
	HeadlessRestoreStatusSkipped  HeadlessRestoreStatus = "skipped"
	HeadlessRestoreStatusWaiting  HeadlessRestoreStatus = "waiting"
	HeadlessRestoreStatusAttached HeadlessRestoreStatus = "attached"
	HeadlessRestoreStatusStarting HeadlessRestoreStatus = "starting"
	HeadlessRestoreStatusFailed   HeadlessRestoreStatus = "failed"
)

type HeadlessRestoreResult struct {
	Status      HeadlessRestoreStatus
	FailureCode string
}

type SurfaceResumeStatus string

const (
	SurfaceResumeStatusSkipped           SurfaceResumeStatus = "skipped"
	SurfaceResumeStatusWaiting           SurfaceResumeStatus = "waiting"
	SurfaceResumeStatusInstanceAttached  SurfaceResumeStatus = "instance_attached"
	SurfaceResumeStatusThreadAttached    SurfaceResumeStatus = "thread_attached"
	SurfaceResumeStatusWorkspaceAttached SurfaceResumeStatus = "workspace_attached"
	SurfaceResumeStatusFailed            SurfaceResumeStatus = "failed"
)

type SurfaceResumeResult struct {
	Status      SurfaceResumeStatus
	FailureCode string
}

type attachSurfaceToKnownThreadMode string

const (
	attachSurfaceToKnownThreadDefault         attachSurfaceToKnownThreadMode = "default"
	attachSurfaceToKnownThreadHeadlessRestore attachSurfaceToKnownThreadMode = "headless_restore"
	attachSurfaceToKnownThreadSurfaceResume   attachSurfaceToKnownThreadMode = "surface_resume"
)

type startHeadlessMode string

const (
	startHeadlessModeDefault         startHeadlessMode = "default"
	startHeadlessModeHeadlessRestore startHeadlessMode = "headless_restore"
)

type attachWorkspaceMode string

const (
	attachWorkspaceModeDefault       attachWorkspaceMode = "default"
	attachWorkspaceModeSurfaceResume attachWorkspaceMode = "surface_resume"
)

type attachInstanceMode string

const (
	attachInstanceModeDefault       attachInstanceMode = "default"
	attachInstanceModeSurfaceResume attachInstanceMode = "surface_resume"
)

type threadSelectionDisplayMode string

const (
	threadSelectionDisplayRecent    threadSelectionDisplayMode = "recent"
	threadSelectionDisplayAll       threadSelectionDisplayMode = "all"
	threadSelectionDisplayScopedAll threadSelectionDisplayMode = "scoped_all"
)

type threadSelectionPresentation struct {
	title               string
	views               []*mergedThreadView
	limit               int
	includeWorkspace    bool
	allowCrossWorkspace bool
	showScopedAllButton bool
	scopedAllButtonText string
	scopedAllStatus     string
	returnActionKind    string
	returnButtonText    string
	returnStatus        string
}

func (s *Service) ensureSurface(action control.Action) *state.SurfaceConsoleRecord {
	surface := s.root.Surfaces[action.SurfaceSessionID]
	if surface != nil {
		if action.GatewayID != "" {
			surface.GatewayID = action.GatewayID
		}
		if action.ChatID != "" {
			surface.ChatID = action.ChatID
		}
		if action.ActorUserID != "" {
			surface.ActorUserID = action.ActorUserID
		}
		if surface.PendingRequests == nil {
			surface.PendingRequests = map[string]*state.RequestPromptRecord{}
		}
		s.normalizeSurfaceProductMode(surface)
		s.surfaceCurrentWorkspaceKey(surface)
		surface.LastInboundAt = s.now()
		return surface
	}

	surface = &state.SurfaceConsoleRecord{
		SurfaceSessionID: action.SurfaceSessionID,
		Platform:         "feishu",
		GatewayID:        action.GatewayID,
		ChatID:           action.ChatID,
		ActorUserID:      action.ActorUserID,
		ProductMode:      state.ProductModeNormal,
		RouteMode:        state.RouteModeUnbound,
		DispatchMode:     state.DispatchModeNormal,
		LastInboundAt:    s.now(),
		QueueItems:       map[string]*state.QueueItemRecord{},
		StagedImages:     map[string]*state.StagedImageRecord{},
		PendingRequests:  map[string]*state.RequestPromptRecord{},
	}
	s.root.Surfaces[action.SurfaceSessionID] = surface
	return surface
}

func (s *Service) pendingHeadlessActionBlocked(surface *state.SurfaceConsoleRecord, action control.Action) []control.UIEvent {
	if surface == nil || surface.PendingHeadless == nil {
		return nil
	}
	switch action.Kind {
	case control.ActionStatus,
		control.ActionAutoContinueCommand,
		control.ActionModeCommand,
		control.ActionDebugCommand,
		control.ActionUpgradeCommand,
		control.ActionDetach,
		control.ActionKillInstance,
		control.ActionRemovedCommand,
		control.ActionReactionCreated,
		control.ActionMessageRecalled:
		return nil
	default:
		return notice(surface, headlessPendingNoticeCode(surface.PendingHeadless), headlessPendingNoticeText(surface.PendingHeadless))
	}
}

func (s *Service) expirePendingHeadless(surface *state.SurfaceConsoleRecord, pending *state.HeadlessLaunchRecord) []control.UIEvent {
	if surface == nil || pending == nil {
		return nil
	}
	surface.PendingHeadless = nil
	events := []control.UIEvent{}
	if surface.AttachedInstanceID == pending.InstanceID {
		events = append(events, s.finalizeDetachedSurface(surface)...)
	}
	events = append(events, control.UIEvent{
		Kind:             control.UIEventDaemonCommand,
		SurfaceSessionID: surface.SurfaceSessionID,
		DaemonCommand: &control.DaemonCommand{
			Kind:             control.DaemonCommandKillHeadless,
			SurfaceSessionID: surface.SurfaceSessionID,
			InstanceID:       pending.InstanceID,
			ThreadID:         pending.ThreadID,
			ThreadTitle:      pending.ThreadTitle,
			ThreadCWD:        pending.ThreadCWD,
		},
	})
	events = append(events, control.UIEvent{
		Kind:             control.UIEventNotice,
		SurfaceSessionID: surface.SurfaceSessionID,
		Notice:           pendingHeadlessTimeoutNotice(pending),
	})
	return events
}

func pendingHeadlessTimeoutNotice(pending *state.HeadlessLaunchRecord) *control.Notice {
	if pending != nil && pending.AutoRestore {
		return &control.Notice{
			Code:  "headless_restore_start_timeout",
			Title: "恢复失败",
			Text:  "之前的会话恢复超时，请稍后重试或尝试其他会话。",
		}
	}
	return &control.Notice{
		Code:  "headless_start_timeout",
		Title: "恢复超时",
		Text:  "后台恢复启动超时，已自动取消，请重新发送 /use 或 /useall 选择要恢复的会话。",
	}
}

func (s *Service) ensureThread(inst *state.InstanceRecord, threadID string) *state.ThreadRecord {
	if inst.Threads == nil {
		inst.Threads = map[string]*state.ThreadRecord{}
	}
	thread := inst.Threads[threadID]
	if thread != nil {
		return thread
	}
	thread = &state.ThreadRecord{ThreadID: threadID}
	inst.Threads[threadID] = thread
	return thread
}

func (s *Service) handleRemovedCommand(surface *state.SurfaceConsoleRecord, action control.Action) []control.UIEvent {
	command := control.LegacyActionCommand(action.Text)
	switch control.LegacyActionKey(action.Text) {
	case "newinstance":
		return notice(surface, "command_removed_newinstance", "`/newinstance` 已移除。请改用 `/use` 或 `/useall` 选择要恢复的会话；在默认 normal 模式下，系统会自动复用在线工作区，必要时在后台恢复。")
	case "killinstance":
		return notice(surface, "command_removed_killinstance", "`/killinstance` 已移除。请改用 `/detach` 取消当前恢复流程，或断开当前接管。")
	case "resume_headless_thread":
		return notice(surface, "selection_expired", "这个旧恢复卡片（来自已移除的 `/newinstance` 流程）已失效，请改用 `/use` 或 `/useall` 选择要恢复的会话；在默认 normal 模式下，系统会自动复用在线工作区，必要时在后台恢复。")
	default:
		if command == "" {
			return notice(surface, "command_removed", "这个旧命令已移除。请发送 `/help` 查看当前可用命令。")
		}
		return notice(surface, "command_removed", fmt.Sprintf("旧命令 `%s` 已移除。请发送 `/help` 查看当前可用命令。", command))
	}
}

func (s *Service) presentInstanceSelection(surface *state.SurfaceConsoleRecord) []control.UIEvent {
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
	contextTitle := ""
	contextText := ""
	for _, inst := range instances {
		if inst == nil {
			continue
		}
		current := surface != nil && surface.AttachedInstanceID == inst.InstanceID
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
		if !current && !busy && surface != nil && strings.TrimSpace(surface.AttachedInstanceID) != "" {
			buttonLabel = "切换"
		}
		option := control.SelectionOption{
			OptionID:    inst.InstanceID,
			Label:       instanceSelectionLabel(inst),
			ButtonLabel: buttonLabel,
			AgeText:     ageText,
			MetaText:    instanceSelectionMetaText(inst, ageText, busy),
			IsCurrent:   current,
			Disabled:    busy,
		}
		if current {
			contextTitle = "当前实例"
			contextText = s.instanceSelectionContextText(surface, inst)
			continue
		}
		entry := instanceSelectionEntry{
			option:       option,
			latestUsedAt: latestUsedAt,
			hasFocus:     instanceHasObservedFocus(inst),
		}
		if busy {
			unavailable = append(unavailable, entry)
			continue
		}
		available = append(available, entry)
	}
	sortInstanceSelectionEntries(available)
	sortInstanceSelectionEntries(unavailable)

	options := make([]control.SelectionOption, 0, len(available)+len(unavailable))
	appendIndexed := func(entries []instanceSelectionEntry) {
		for _, entry := range entries {
			entry.option.Index = len(options) + 1
			options = append(options, entry.option)
		}
	}
	appendIndexed(available)
	appendIndexed(unavailable)

	hint := ""
	if contextTitle != "" && len(options) == 0 {
		hint = "当前没有其他可接管实例。"
	}
	return []control.UIEvent{{
		Kind:             control.UIEventSelectionPrompt,
		SurfaceSessionID: surface.SurfaceSessionID,
		SelectionPrompt: &control.SelectionPrompt{
			Kind:         control.SelectionPromptAttachInstance,
			Layout:       "grouped_attach_instance",
			Title:        "在线 VS Code 实例",
			Hint:         hint,
			ContextTitle: contextTitle,
			ContextText:  contextText,
			Options:      options,
		},
	}}
}

type instanceSelectionEntry struct {
	option       control.SelectionOption
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
		return strings.TrimSpace(left.option.OptionID) < strings.TrimSpace(right.option.OptionID)
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
	for _, thread := range visibleThreads(inst) {
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

func (s *Service) presentWorkspaceSelection(surface *state.SurfaceConsoleRecord) []control.UIEvent {
	grouped := map[string][]*state.InstanceRecord{}
	for _, inst := range s.root.Instances {
		if inst == nil || !inst.Online {
			continue
		}
		for _, workspaceKey := range instanceWorkspaceSelectionKeys(inst) {
			grouped[workspaceKey] = append(grouped[workspaceKey], inst)
		}
	}
	views := s.mergedThreadViews(surface)
	visibleWorkspaces := s.normalModeListWorkspaceSetWithViews(surface, views)
	if len(visibleWorkspaces) == 0 {
		return notice(surface, "no_available_workspaces", "当前没有可接管的工作区。请先连接一个 VS Code 会话，或等待可恢复工作区出现。")
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

	currentWorkspace := s.surfaceCurrentWorkspaceKey(surface)
	available := make([]workspaceSelectionEntry, 0, len(visibleWorkspaces))
	unavailable := make([]workspaceSelectionEntry, 0, len(visibleWorkspaces))
	contextTitle := ""
	contextText := ""
	for workspaceKey := range visibleWorkspaces {
		workspaceKey = normalizeWorkspaceClaimKey(workspaceKey)
		instances := append([]*state.InstanceRecord(nil), grouped[workspaceKey]...)
		s.sortWorkspaceAttachInstances(surface, workspaceKey, instances)

		latestUsedAt := recoverableWorkspaces[workspaceKey]
		ageText := ""
		if !latestUsedAt.IsZero() {
			ageText = humanizeRelativeTime(s.now(), latestUsedAt)
		}
		hasVSCodeActivity := s.workspaceHasVSCodeActivity(instances)
		isCurrent := surface.AttachedInstanceID != "" && currentWorkspace != "" && currentWorkspace == workspaceKey
		busy := s.workspaceBusyOwnerForSurface(surface, workspaceKey) != nil
		attachable := s.resolveWorkspaceAttachInstanceFromCandidates(surface, workspaceKey, instances) != nil
		recoverableOnly := !attachable && len(instances) == 0 && recoverableWorkspaceSeen[workspaceKey]

		buttonLabel := ""
		actionKind := ""
		disabled := false
		switch {
		case isCurrent:
		case busy:
			disabled = true
		case attachable:
			if surface.AttachedInstanceID != "" {
				buttonLabel = "切换"
			}
		case recoverableOnly:
			buttonLabel = "恢复"
			actionKind = "show_workspace_threads"
		default:
			disabled = true
		}

		option := control.SelectionOption{
			OptionID:    workspaceKey,
			Label:       workspaceSelectionLabel(workspaceKey),
			ButtonLabel: buttonLabel,
			AgeText:     ageText,
			MetaText:    workspaceSelectionMetaText(ageText, hasVSCodeActivity, busy, !attachable && !recoverableOnly, recoverableOnly),
			ActionKind:  actionKind,
			IsCurrent:   isCurrent,
			Disabled:    disabled,
		}
		if isCurrent {
			contextTitle = "当前工作区"
			contextText = workspaceSelectionContextText(option.Label, ageText)
			continue
		}
		entry := workspaceSelectionEntry{
			option:       option,
			latestUsedAt: latestUsedAt,
		}
		if disabled {
			unavailable = append(unavailable, entry)
			continue
		}
		available = append(available, entry)
	}

	sortWorkspaceSelectionEntries(available)
	sortWorkspaceSelectionEntries(unavailable)

	options := make([]control.SelectionOption, 0, len(available)+len(unavailable))
	appendIndexed := func(entries []workspaceSelectionEntry) {
		for _, entry := range entries {
			entry.option.Index = len(options) + 1
			options = append(options, entry.option)
		}
	}
	appendIndexed(available)
	appendIndexed(unavailable)

	hint := ""
	if contextTitle != "" && len(options) == 0 {
		hint = "当前没有其他可接管工作区。"
	}

	return []control.UIEvent{{
		Kind:             control.UIEventSelectionPrompt,
		SurfaceSessionID: surface.SurfaceSessionID,
		SelectionPrompt: &control.SelectionPrompt{
			Kind:         control.SelectionPromptAttachWorkspace,
			Layout:       "grouped_attach_workspace",
			Title:        "工作区列表",
			Hint:         hint,
			ContextTitle: contextTitle,
			ContextText:  contextText,
			Options:      options,
		},
	}}
}

type workspaceSelectionEntry struct {
	option       control.SelectionOption
	latestUsedAt time.Time
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
		return strings.TrimSpace(left.option.OptionID) < strings.TrimSpace(right.option.OptionID)
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

func workspaceSelectionMetaText(ageText string, hasVSCodeActivity, busy, unavailable, recoverableOnly bool) string {
	parts := make([]string, 0, 2)
	if age := strings.TrimSpace(ageText); age != "" {
		parts = append(parts, age)
	}
	switch {
	case busy:
		parts = append(parts, "当前被其他飞书会话接管")
	case recoverableOnly:
		parts = append(parts, "后台可恢复")
	case unavailable:
		parts = append(parts, "当前暂不可接管")
	case hasVSCodeActivity:
		parts = append(parts, "有 VS Code 活动")
	}
	if len(parts) == 0 {
		return "可接管"
	}
	return strings.Join(parts, " · ")
}

func workspaceSelectionContextText(label, ageText string) string {
	label = strings.TrimSpace(label)
	parts := []string{label}
	if age := strings.TrimSpace(ageText); age != "" {
		parts[0] += " · " + age
	}
	parts = append(parts, "同工作区内继续工作请直接 /use 或 /new")
	return strings.Join(parts, "\n")
}

func instanceWorkspaceSelectionKeys(inst *state.InstanceRecord) []string {
	if inst == nil {
		return nil
	}
	seen := map[string]struct{}{}
	keys := []string{}
	for _, thread := range visibleThreads(inst) {
		if thread == nil {
			continue
		}
		if !threadBelongsToInstanceWorkspace(inst, thread) {
			continue
		}
		key := state.ResolveWorkspaceKey(thread.CWD)
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
	for _, thread := range visibleThreads(inst) {
		if thread == nil {
			continue
		}
		if !threadBelongsToInstanceWorkspace(inst, thread) {
			continue
		}
		if state.ResolveWorkspaceKey(thread.CWD) != workspaceKey {
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
	root := state.NormalizeWorkspaceKey(inst.WorkspaceRoot)
	cwd := state.NormalizeWorkspaceKey(thread.CWD)
	if root == "" || cwd == "" {
		return true
	}
	return cwd == root || strings.HasPrefix(cwd, root+string(filepath.Separator))
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

func (s *Service) attachWorkspace(surface *state.SurfaceConsoleRecord, workspaceKey string) []control.UIEvent {
	return s.attachWorkspaceWithMode(surface, workspaceKey, attachWorkspaceModeDefault)
}

func (s *Service) attachWorkspaceWithMode(surface *state.SurfaceConsoleRecord, workspaceKey string, mode attachWorkspaceMode) []control.UIEvent {
	workspaceKey = normalizeWorkspaceClaimKey(workspaceKey)
	if workspaceKey == "" {
		return notice(surface, "workspace_not_found", "目标工作区不存在。请重新发送 /list。")
	}
	currentWorkspace := s.surfaceCurrentWorkspaceKey(surface)
	if surface.AttachedInstanceID != "" && currentWorkspace == workspaceKey {
		return notice(surface, "workspace_already_attached", fmt.Sprintf("当前已接管工作区：%s。", workspaceKey))
	}
	if owner := s.workspaceBusyOwnerForSurface(surface, workspaceKey); owner != nil {
		return notice(surface, "workspace_busy", "目标 workspace 当前已被其他飞书会话接管，请等待对方 /detach。")
	}
	if surface.AttachedInstanceID != "" && currentWorkspace != "" && currentWorkspace != workspaceKey {
		if blocked := s.blockFreshThreadAttach(surface); blocked != nil {
			return blocked
		}
	}

	inst := s.resolveWorkspaceAttachInstance(surface, workspaceKey)
	if inst == nil {
		if len(s.workspaceOnlineInstances(workspaceKey)) == 0 {
			return notice(surface, "workspace_not_found", "目标工作区已失效，请重新发送 /list。")
		}
		return notice(surface, "workspace_instance_busy", "目标工作区当前暂时不可接管，请稍后重试。")
	}

	events := []control.UIEvent{}
	if surface.AttachedInstanceID != "" {
		events = append(events, s.discardDrafts(surface)...)
		events = append(events, s.finalizeDetachedSurface(surface)...)
	} else {
		events = append(events, s.discardDrafts(surface)...)
		clearSurfaceRequestCapture(surface)
		clearSurfaceRequests(surface)
		s.releaseSurfaceThreadClaim(surface)
		s.clearPreparedNewThread(surface)
		surface.PromptOverride = state.ModelConfigRecord{}
		surface.PendingHeadless = nil
		surface.ActiveQueueItemID = ""
		surface.DispatchMode = state.DispatchModeNormal
		surface.Abandoning = false
		delete(s.pausedUntil, surface.SurfaceSessionID)
		delete(s.abandoningUntil, surface.SurfaceSessionID)
	}

	if !s.claimWorkspace(surface, workspaceKey) {
		return append(events, notice(surface, "workspace_busy", "目标 workspace 当前已被其他飞书会话接管，请等待对方 /detach。")...)
	}
	if !s.claimInstance(surface, inst.InstanceID) {
		s.releaseSurfaceWorkspaceClaim(surface)
		return append(events, notice(surface, "workspace_instance_busy", "目标工作区当前暂时不可接管，请稍后重试。")...)
	}

	surface.AttachedInstanceID = inst.InstanceID
	s.surfaceCurrentWorkspaceKey(surface)
	surface.PendingHeadless = nil
	surface.ActiveQueueItemID = ""
	surface.DispatchMode = state.DispatchModeNormal
	surface.Abandoning = false
	delete(s.pausedUntil, surface.SurfaceSessionID)
	delete(s.abandoningUntil, surface.SurfaceSessionID)
	clearSurfaceRequests(surface)
	s.clearPreparedNewThread(surface)
	s.releaseSurfaceThreadClaim(surface)
	surface.PromptOverride = state.ModelConfigRecord{}
	surface.SelectedThreadID = ""
	surface.RouteMode = state.RouteModeUnbound
	surface.LastSelection = &state.SelectionAnnouncementRecord{
		ThreadID:  "",
		RouteMode: string(state.RouteModeUnbound),
		Title:     "未绑定会话",
		Preview:   "",
	}

	noticeCode := "workspace_attached"
	noticeText := fmt.Sprintf("已接管工作区 %s。请继续 /use 选择一个会话，或 /new 准备新会话。", workspaceKey)
	if currentWorkspace != "" && currentWorkspace != workspaceKey {
		noticeCode = "workspace_switched"
		noticeText = fmt.Sprintf("已切换到工作区 %s。请继续 /use 选择一个会话，或 /new 准备新会话。", workspaceKey)
	}
	visibleThreadCount := len(workspaceVisibleThreads(inst, workspaceKey))
	if mode == attachWorkspaceModeSurfaceResume {
		noticeCode = "surface_resume_workspace_attached"
		if visibleThreadCount == 0 {
			noticeText = fmt.Sprintf("之前的会话暂未恢复，已先回到工作区 %s。当前还没有可见会话；你可以直接 /new 准备新会话，或稍后发送 /use。", workspaceKey)
		} else {
			noticeText = fmt.Sprintf("之前的会话当前不可见，已先回到工作区 %s。请继续 /use 选择要恢复的会话，或 /new 准备新会话。", workspaceKey)
		}
	} else if visibleThreadCount == 0 {
		noticeText = fmt.Sprintf("已接管工作区 %s。当前还没有可见会话；你可以直接 /new 准备新会话，或稍后发送 /use。", workspaceKey)
	}
	events = append(events, control.UIEvent{
		Kind:             control.UIEventNotice,
		SurfaceSessionID: surface.SurfaceSessionID,
		Notice: &control.Notice{
			Code: noticeCode,
			Text: noticeText,
		},
	})
	if visibleThreadCount != 0 {
		events = append(events, s.autoPromptUseThread(surface, inst)...)
	}
	return events
}

func (s *Service) attachInstance(surface *state.SurfaceConsoleRecord, instanceID string) []control.UIEvent {
	return s.attachInstanceWithMode(surface, instanceID, attachInstanceModeDefault)
}

func (s *Service) attachInstanceWithMode(surface *state.SurfaceConsoleRecord, instanceID string, mode attachInstanceMode) []control.UIEvent {
	inst := s.root.Instances[instanceID]
	if inst == nil {
		return notice(surface, "instance_not_found", "实例不存在。")
	}
	productMode := s.normalizeSurfaceProductMode(surface)
	workspaceKey := instanceWorkspaceClaimKey(inst)
	switchingInstance := surface.AttachedInstanceID != "" && surface.AttachedInstanceID != instanceID
	if switchingInstance && productMode != state.ProductModeVSCode {
		return notice(surface, "attach_requires_detach", "当前会话已接管其他工作区，请先 /detach。")
	}
	if switchingInstance {
		if blocked := s.blockFreshThreadAttach(surface); blocked != nil {
			return blocked
		}
	}
	if surface.AttachedInstanceID == instanceID {
		if productMode != state.ProductModeVSCode && workspaceKey != "" {
			return notice(surface, "already_attached", fmt.Sprintf("当前已接管工作区：%s。", workspaceKey))
		}
		return notice(surface, "already_attached", fmt.Sprintf("当前已接管 %s。", inst.DisplayName))
	}
	if s.surfaceUsesWorkspaceClaims(surface) && workspaceKey == "" {
		return notice(surface, "workspace_key_missing", "当前无法确定目标对应的工作区，暂时不能在 normal 模式接管。请切到 `/mode vscode` 后再试。")
	}
	if owner := s.workspaceBusyOwnerForSurface(surface, workspaceKey); owner != nil {
		return notice(surface, "workspace_busy", "目标 workspace 当前已被其他飞书会话接管，请等待对方 /detach。")
	}
	if owner := s.instanceClaimSurface(instanceID); owner != nil && owner.SurfaceSessionID != surface.SurfaceSessionID {
		return notice(surface, "instance_busy", fmt.Sprintf("%s 当前已被其他飞书会话接管，请等待对方 /detach。", inst.DisplayName))
	}

	events := s.discardDrafts(surface)
	if surface.AttachedInstanceID != "" {
		events = append(events, s.finalizeDetachedSurface(surface)...)
	} else {
		clearSurfaceRequestCapture(surface)
		clearSurfaceRequests(surface)
		s.releaseSurfaceThreadClaim(surface)
		s.clearPreparedNewThread(surface)
		surface.PromptOverride = state.ModelConfigRecord{}
	}
	if !s.claimWorkspace(surface, workspaceKey) {
		if s.surfaceUsesWorkspaceClaims(surface) {
			return append(events, notice(surface, "workspace_busy", "目标 workspace 当前已被其他飞书会话接管，请等待对方 /detach。")...)
		}
		return append(events, notice(surface, "workspace_key_missing", "当前无法确定目标对应的工作区，暂时不能在 normal 模式接管。请切到 `/mode vscode` 后再试。")...)
	}
	if !s.claimInstance(surface, instanceID) {
		s.releaseSurfaceWorkspaceClaim(surface)
		return append(events, notice(surface, "instance_busy", fmt.Sprintf("%s 当前已被其他飞书会话接管，请等待对方 /detach。", inst.DisplayName))...)
	}
	s.surfaceCurrentWorkspaceKey(surface)
	surface.AttachedInstanceID = instanceID
	surface.PendingHeadless = nil
	surface.ActiveQueueItemID = ""
	surface.DispatchMode = state.DispatchModeNormal
	surface.Abandoning = false
	delete(s.pausedUntil, surface.SurfaceSessionID)
	delete(s.abandoningUntil, surface.SurfaceSessionID)

	if productMode == state.ProductModeVSCode {
		return append(events, s.attachVSCodeInstance(surface, inst, switchingInstance, mode)...)
	}

	initialThreadID := s.defaultAttachThread(inst)
	if initialThreadID != "" && s.claimThread(surface, inst, initialThreadID) {
		surface.SelectedThreadID = initialThreadID
		surface.RouteMode = state.RouteModePinned
	} else {
		surface.SelectedThreadID = ""
		surface.RouteMode = state.RouteModeUnbound
	}
	lastTitle := ""
	lastPreview := ""
	if surface.SelectedThreadID != "" {
		lastTitle = displayThreadTitle(inst, inst.Threads[surface.SelectedThreadID], surface.SelectedThreadID)
		lastPreview = threadPreview(inst.Threads[surface.SelectedThreadID])
	}
	surface.LastSelection = &state.SelectionAnnouncementRecord{
		ThreadID:  surface.SelectedThreadID,
		RouteMode: string(surface.RouteMode),
		Title:     lastTitle,
		Preview:   lastPreview,
	}

	title := "未绑定会话"
	text := s.attachedLeadText(surface, inst)
	if surface.SelectedThreadID != "" {
		title = displayThreadTitle(inst, inst.Threads[surface.SelectedThreadID], surface.SelectedThreadID)
		text = fmt.Sprintf("%s 当前输入目标：%s", text, title)
	} else if initialThreadID != "" {
		text = fmt.Sprintf("%s 默认会话当前已被其他飞书会话占用，请先通过 /use 选择可用会话。", text)
	} else if len(visibleThreads(inst)) != 0 {
		text = fmt.Sprintf("%s 当前还没有绑定会话，请先通过 /use 选择一个会话。", text)
	} else {
		if productMode == state.ProductModeVSCode {
			text = fmt.Sprintf("%s 当前没有可用会话，请等待 VS Code 切到会话后再 /use，或直接 /detach。", text)
		} else {
			text = fmt.Sprintf("%s 当前工作区还没有可用会话；你可以稍后再 /use，或直接 /new 准备新会话。", text)
		}
	}
	events = append(events, control.UIEvent{
		Kind:             control.UIEventNotice,
		SurfaceSessionID: surface.SurfaceSessionID,
		Notice: &control.Notice{
			Code: "attached",
			Text: text,
		},
	})
	if surface.SelectedThreadID != "" {
		events = append(events, s.replayThreadUpdate(surface, inst, surface.SelectedThreadID)...)
	}
	events = append(events, s.maybeRequestThreadRefresh(surface, inst, surface.SelectedThreadID)...)
	if surface.SelectedThreadID == "" {
		events = append(events, s.autoPromptUseThread(surface, inst)...)
	}
	return events
}

func (s *Service) attachVSCodeInstance(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, switched bool, mode attachInstanceMode) []control.UIEvent {
	if surface == nil || inst == nil {
		return nil
	}
	surface.SelectedThreadID = ""
	s.clearPreparedNewThread(surface)
	surface.RouteMode = state.RouteModeFollowLocal

	events := s.reevaluateFollowSurface(surface)
	if len(events) == 0 && surface.SelectedThreadID == "" {
		events = append(events, s.threadSelectionEvents(surface, "", string(state.RouteModeFollowLocal), "跟随当前 VS Code（等待中）", "")...)
	}

	verb := "已接管"
	if switched {
		verb = "已切换到"
	}
	noticeCode := "attached"
	text := fmt.Sprintf("%s %s。", verb, inst.DisplayName)
	if mode == attachInstanceModeSurfaceResume {
		noticeCode = "surface_resume_instance_attached"
		text = fmt.Sprintf("已恢复到 VS Code 实例 %s。", inst.DisplayName)
	}
	if surface.SelectedThreadID != "" {
		thread := s.ensureThread(inst, surface.SelectedThreadID)
		text = fmt.Sprintf("%s 当前跟随会话：%s", text, displayThreadTitle(inst, thread, surface.SelectedThreadID))
	} else if len(visibleThreads(inst)) != 0 {
		if mode == attachInstanceModeSurfaceResume {
			text = fmt.Sprintf("%s 当前还没有新的 VS Code 焦点；请先在 VS Code 里再说一句话，或发送 /use 选择当前实例已知会话。", text)
		} else {
			text = fmt.Sprintf("%s 已进入跟随模式；当前还没有可接管的 VS Code 焦点。请先在 VS Code 里实际操作一次会话，或发送 /use 选择当前实例已知会话。", text)
		}
	} else {
		if mode == attachInstanceModeSurfaceResume {
			text = fmt.Sprintf("%s 当前还没有观测到新的 VS Code 活动；请先在 VS Code 里再说一句话，或稍后重试。", text)
		} else {
			text = fmt.Sprintf("%s 已进入跟随模式；当前还没有观测到会话。请先在 VS Code 里实际操作一次会话，或稍后重试。", text)
		}
	}

	result := []control.UIEvent{{
		Kind:             control.UIEventNotice,
		SurfaceSessionID: surface.SurfaceSessionID,
		Notice: &control.Notice{
			Code: noticeCode,
			Text: text,
		},
	}}
	result = append(result, events...)
	if surface.SelectedThreadID != "" {
		result = append(result, s.replayThreadUpdate(surface, inst, surface.SelectedThreadID)...)
	}
	result = append(result, s.maybeRequestThreadRefresh(surface, inst, surface.SelectedThreadID)...)
	return result
}

func (s *Service) attachHeadlessInstance(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, pending *state.HeadlessLaunchRecord) []control.UIEvent {
	if surface == nil || inst == nil || pending == nil {
		return nil
	}
	if strings.TrimSpace(pending.ThreadID) != "" {
		view := s.mergedThreadView(surface, pending.ThreadID)
		if view == nil {
			thread := s.ensureThread(inst, pending.ThreadID)
			if strings.TrimSpace(thread.Name) == "" {
				thread.Name = strings.TrimSpace(pending.ThreadName)
			}
			if strings.TrimSpace(thread.Preview) == "" {
				thread.Preview = strings.TrimSpace(pending.ThreadPreview)
			}
			if strings.TrimSpace(thread.CWD) == "" {
				thread.CWD = strings.TrimSpace(pending.ThreadCWD)
			}
			view = &mergedThreadView{
				ThreadID: pending.ThreadID,
				Inst:     inst,
				Thread:   thread,
			}
		}
		mode := attachSurfaceToKnownThreadDefault
		if pending.AutoRestore {
			mode = attachSurfaceToKnownThreadHeadlessRestore
		}
		return s.attachSurfaceToKnownThread(surface, inst, view, mode)
	}
	surface.PendingHeadless = nil
	events := []control.UIEvent{}
	if surface.AttachedInstanceID == pending.InstanceID {
		events = append(events, s.finalizeDetachedSurface(surface)...)
	}
	events = append(events,
		control.UIEvent{
			Kind:             control.UIEventDaemonCommand,
			SurfaceSessionID: surface.SurfaceSessionID,
			DaemonCommand: &control.DaemonCommand{
				Kind:             control.DaemonCommandKillHeadless,
				SurfaceSessionID: surface.SurfaceSessionID,
				InstanceID:       pending.InstanceID,
			},
		},
		control.UIEvent{
			Kind:             control.UIEventNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice: &control.Notice{
				Code:  "command_removed_newinstance",
				Title: "旧恢复流程已移除",
				Text:  "旧版 `/newinstance` 恢复流程已移除。请改用 `/use` 或 `/useall` 选择要恢复的会话；当前后台恢复流程已自动结束。",
			},
		},
	)
	return events
}

func (s *Service) presentThreadSelection(surface *state.SurfaceConsoleRecord, showAll bool) []control.UIEvent {
	mode := threadSelectionDisplayRecent
	if showAll {
		mode = threadSelectionDisplayAll
	}
	return s.presentThreadSelectionMode(surface, mode)
}

func (s *Service) presentScopedThreadSelection(surface *state.SurfaceConsoleRecord) []control.UIEvent {
	return s.presentThreadSelectionMode(surface, threadSelectionDisplayScopedAll)
}

func (s *Service) presentWorkspaceThreadSelection(surface *state.SurfaceConsoleRecord, workspaceKey string) []control.UIEvent {
	workspaceKey = normalizeWorkspaceClaimKey(workspaceKey)
	if workspaceKey == "" {
		return notice(surface, "workspace_not_found", "目标工作区不存在。请重新发送 /useall。")
	}
	views := s.threadViewsVisibleInNormalList(surface, s.mergedThreadViews(surface))
	filtered := make([]*mergedThreadView, 0, len(views))
	for _, view := range views {
		if mergedThreadWorkspaceClaimKey(view) != workspaceKey {
			continue
		}
		filtered = append(filtered, view)
	}
	if len(filtered) == 0 {
		return notice(surface, "no_visible_threads", fmt.Sprintf("当前工作区 %s 还没有可恢复会话。", workspaceKey))
	}
	options := make([]control.SelectionOption, 0, len(filtered))
	for i, view := range filtered {
		status, disabled := s.threadSelectionStatus(surface, view, true)
		summary := s.threadSelectionSummary(surface, view)
		options = append(options, control.SelectionOption{
			Index:               i + 1,
			OptionID:            view.ThreadID,
			Label:               summary,
			Subtitle:            s.threadSelectionOptionSubtitle(surface, view, false, true),
			ButtonLabel:         summary,
			GroupKey:            workspaceKey,
			GroupLabel:          workspaceSelectionLabel(workspaceKey),
			AgeText:             humanizeRelativeTime(s.now(), threadLastUsedAt(view)),
			MetaText:            s.threadSelectionMetaText(surface, view, status),
			IsCurrent:           surface.SelectedThreadID == view.ThreadID && s.surfaceOwnsThread(surface, view.ThreadID),
			Disabled:            disabled,
			AllowCrossWorkspace: true,
		})
	}
	options = append(options, control.SelectionOption{
		Index:       len(options) + 1,
		ButtonLabel: "全部会话",
		Subtitle:    "回到跨工作区会话列表",
		ActionKind:  "show_all_threads",
	})
	return []control.UIEvent{{
		Kind:             control.UIEventSelectionPrompt,
		SurfaceSessionID: surface.SurfaceSessionID,
		SelectionPrompt: &control.SelectionPrompt{
			Kind:    control.SelectionPromptUseThread,
			Layout:  "workspace_grouped_useall",
			Title:   workspaceSelectionLabel(workspaceKey) + " 全部会话",
			Options: options,
		},
	}}
}

func (s *Service) presentThreadSelectionMode(surface *state.SurfaceConsoleRecord, mode threadSelectionDisplayMode) []control.UIEvent {
	if surface != nil && s.normalizeSurfaceProductMode(surface) == state.ProductModeVSCode && strings.TrimSpace(surface.AttachedInstanceID) == "" {
		return notice(surface, "not_attached_vscode", "vscode 模式下请先 /list 选择一个 VS Code 实例，再使用 /use 或 /useall。")
	}
	presentation := s.resolveThreadSelectionPresentation(surface, mode)
	if len(presentation.views) == 0 {
		if surface != nil && s.normalizeSurfaceProductMode(surface) == state.ProductModeVSCode && strings.TrimSpace(surface.AttachedInstanceID) != "" {
			return notice(surface, "no_visible_threads", "当前接管的 VS Code 实例还没有已知会话。请先在 VS Code 里实际操作一次会话，再重试。")
		}
		if workspaceKey := s.threadSelectionWorkspaceScope(surface); workspaceKey != "" {
			return notice(surface, "no_visible_threads", fmt.Sprintf("当前工作区 %s 还没有可恢复会话。你可以直接 /new、发送 /useall 查看其他 workspace 的会话，或先 /list 切换工作区。", workspaceKey))
		}
		return notice(surface, "no_visible_threads", "当前还没有可恢复会话。")
	}
	limit := presentation.limit
	if limit <= 0 || limit > len(presentation.views) {
		limit = len(presentation.views)
	}
	threads := presentation.views[:limit]
	options := make([]control.SelectionOption, 0, len(threads)+1)
	for i, view := range threads {
		status, disabled := s.threadSelectionStatus(surface, view, presentation.allowCrossWorkspace)
		summary := s.threadSelectionSummary(surface, view)
		workspaceKey := mergedThreadWorkspaceClaimKey(view)
		options = append(options, control.SelectionOption{
			Index:               i + 1,
			OptionID:            view.ThreadID,
			Label:               summary,
			Subtitle:            s.threadSelectionOptionSubtitle(surface, view, presentation.includeWorkspace, presentation.allowCrossWorkspace),
			ButtonLabel:         summary,
			GroupKey:            workspaceKey,
			GroupLabel:          workspaceSelectionLabel(workspaceKey),
			AgeText:             humanizeRelativeTime(s.now(), threadLastUsedAt(view)),
			MetaText:            s.threadSelectionMetaText(surface, view, status),
			IsCurrent:           surface.SelectedThreadID == view.ThreadID && s.surfaceOwnsThread(surface, view.ThreadID),
			Disabled:            disabled,
			AllowCrossWorkspace: presentation.allowCrossWorkspace,
		})
	}
	if presentation.showScopedAllButton {
		options = append(options, control.SelectionOption{
			Index:       len(options) + 1,
			ButtonLabel: presentation.scopedAllButtonText,
			Subtitle:    presentation.scopedAllStatus,
			ActionKind:  "show_scoped_threads",
		})
	}
	if strings.TrimSpace(presentation.returnActionKind) != "" {
		options = append(options, control.SelectionOption{
			Index:       len(options) + 1,
			ButtonLabel: presentation.returnButtonText,
			Subtitle:    presentation.returnStatus,
			ActionKind:  presentation.returnActionKind,
		})
	}
	return []control.UIEvent{{
		Kind:             control.UIEventSelectionPrompt,
		SurfaceSessionID: surface.SurfaceSessionID,
		SelectionPrompt: &control.SelectionPrompt{
			Kind:         control.SelectionPromptUseThread,
			Layout:       s.threadSelectionPromptLayout(surface, presentation),
			Title:        presentation.title,
			ContextTitle: s.threadSelectionContextTitle(surface, presentation),
			ContextText:  s.threadSelectionContextText(surface, presentation),
			ContextKey:   s.threadSelectionContextKey(surface, presentation),
			Options:      options,
		},
	}}
}

func (s *Service) threadSelectionPromptLayout(surface *state.SurfaceConsoleRecord, presentation threadSelectionPresentation) string {
	if surface != nil &&
		s.normalizeSurfaceProductMode(surface) == state.ProductModeVSCode &&
		strings.TrimSpace(surface.AttachedInstanceID) != "" {
		return "vscode_instance_threads"
	}
	if presentation.title == "全部会话" && presentation.includeWorkspace {
		return "workspace_grouped_useall"
	}
	return ""
}

func (s *Service) threadSelectionContextTitle(surface *state.SurfaceConsoleRecord, presentation threadSelectionPresentation) string {
	if surface == nil || strings.TrimSpace(surface.AttachedInstanceID) == "" {
		return ""
	}
	if s.normalizeSurfaceProductMode(surface) == state.ProductModeVSCode {
		return "当前实例"
	}
	if !presentation.includeWorkspace || presentation.title != "全部会话" {
		return ""
	}
	if workspaceKey := s.surfaceCurrentWorkspaceKey(surface); workspaceKey != "" {
		return "当前工作区"
	}
	return ""
}

func (s *Service) threadSelectionContextText(surface *state.SurfaceConsoleRecord, presentation threadSelectionPresentation) string {
	if surface != nil &&
		s.normalizeSurfaceProductMode(surface) == state.ProductModeVSCode &&
		strings.TrimSpace(surface.AttachedInstanceID) != "" {
		return s.vscodeThreadSelectionContextText(surface)
	}
	workspaceKey := s.threadSelectionContextKey(surface, presentation)
	if workspaceKey == "" {
		return ""
	}
	label := workspaceSelectionLabel(workspaceKey)
	if label == "" {
		label = workspaceKey
	}
	parts := []string{label}
	if latest := threadViewsLatestUsedAt(s.scopedMergedThreadViews(surface)); !latest.IsZero() {
		parts[0] += " · " + humanizeRelativeTime(s.now(), latest)
	}
	parts = append(parts, "同工作区内切换请直接用 /use")
	return strings.Join(parts, "\n")
}

func (s *Service) threadSelectionContextKey(surface *state.SurfaceConsoleRecord, presentation threadSelectionPresentation) string {
	if surface == nil || !presentation.includeWorkspace || strings.TrimSpace(surface.AttachedInstanceID) == "" {
		return ""
	}
	if s.normalizeSurfaceProductMode(surface) != state.ProductModeNormal {
		return ""
	}
	if presentation.title != "全部会话" {
		return ""
	}
	return s.surfaceCurrentWorkspaceKey(surface)
}

func (s *Service) threadSelectionSummary(surface *state.SurfaceConsoleRecord, view *mergedThreadView) string {
	if surface != nil &&
		s.normalizeSurfaceProductMode(surface) == state.ProductModeVSCode &&
		strings.TrimSpace(surface.AttachedInstanceID) != "" {
		return vscodeThreadSelectionButtonLabel(view.Thread, view.ThreadID)
	}
	return threadSelectionButtonLabel(view.Thread, view.ThreadID)
}

func vscodeThreadSelectionButtonLabel(thread *state.ThreadRecord, fallback string) string {
	source := threadDisplayBody(thread, 20)
	if source == "" {
		source = shortenThreadID(fallback)
	}
	if source == "" {
		source = "未命名会话"
	}
	return source
}

func (s *Service) vscodeInstanceSurfaceStatus(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord) string {
	if surface == nil || inst == nil || strings.TrimSpace(surface.AttachedInstanceID) != inst.InstanceID {
		return ""
	}
	if strings.TrimSpace(surface.SelectedThreadID) != "" {
		if surface.RouteMode == state.RouteModeFollowLocal {
			return "当前跟随中"
		}
		return "已接管"
	}
	if instanceHasObservedFocus(inst) {
		return "当前焦点可跟随"
	}
	return "等待 VS Code 焦点"
}

func (s *Service) vscodeThreadSelectionContextText(surface *state.SurfaceConsoleRecord) string {
	if surface == nil {
		return ""
	}
	inst := s.root.Instances[strings.TrimSpace(surface.AttachedInstanceID)]
	if inst == nil {
		return ""
	}
	label := instanceSelectionLabel(inst)
	status := s.vscodeInstanceSurfaceStatus(surface, inst)
	if status == "" {
		return label
	}
	return label + " · " + status
}

func (s *Service) resolveThreadSelectionPresentation(surface *state.SurfaceConsoleRecord, mode threadSelectionDisplayMode) threadSelectionPresentation {
	productMode := state.ProductModeNormal
	if surface != nil {
		productMode = s.normalizeSurfaceProductMode(surface)
	}
	switch productMode {
	case state.ProductModeVSCode:
		views := s.scopedMergedThreadViews(surface)
		presentation := threadSelectionPresentation{
			title:            "最近会话",
			views:            views,
			limit:            min(5, len(views)),
			includeWorkspace: false,
		}
		switch mode {
		case threadSelectionDisplayAll:
			presentation.title = "当前实例全部会话"
			presentation.limit = len(views)
		case threadSelectionDisplayScopedAll:
			presentation.title = "当前实例全部会话"
			presentation.limit = len(views)
			presentation.returnActionKind = "show_threads"
			presentation.returnButtonText = "最近会话"
			presentation.returnStatus = "回到当前实例最近 5 个会话"
		default:
			if len(views) > 5 {
				presentation.showScopedAllButton = true
				presentation.scopedAllButtonText = "当前实例全部会话"
				presentation.scopedAllStatus = "展开当前实例内的全部会话"
			}
		}
		return presentation
	default:
		attached := surface != nil && strings.TrimSpace(surface.AttachedInstanceID) != ""
		if !attached || mode == threadSelectionDisplayAll {
			views := s.threadViewsVisibleInNormalList(surface, s.mergedThreadViews(surface))
			return threadSelectionPresentation{
				title:               "全部会话",
				views:               views,
				limit:               len(views),
				includeWorkspace:    true,
				allowCrossWorkspace: true,
			}
		}
		views := s.scopedMergedThreadViews(surface)
		presentation := threadSelectionPresentation{
			title:            "最近会话",
			views:            views,
			limit:            min(5, len(views)),
			includeWorkspace: false,
		}
		if mode == threadSelectionDisplayScopedAll {
			presentation.title = "当前工作区全部会话"
			presentation.limit = len(views)
			presentation.returnActionKind = "show_threads"
			presentation.returnButtonText = "最近会话"
			presentation.returnStatus = "回到当前工作区最近 5 个会话"
			return presentation
		}
		if len(views) > 5 {
			presentation.showScopedAllButton = true
			presentation.scopedAllButtonText = "当前工作区全部会话"
			presentation.scopedAllStatus = "展开当前工作区内的全部会话"
		}
		return presentation
	}
}

func (s *Service) TryAutoRestoreHeadless(surfaceID string, attempt HeadlessRestoreAttempt, allowMissingThreadFailure bool) ([]control.UIEvent, HeadlessRestoreResult) {
	surface := s.root.Surfaces[strings.TrimSpace(surfaceID)]
	if surface == nil {
		return nil, HeadlessRestoreResult{Status: HeadlessRestoreStatusSkipped}
	}
	if s.normalizeSurfaceProductMode(surface) == state.ProductModeVSCode {
		return nil, HeadlessRestoreResult{Status: HeadlessRestoreStatusSkipped}
	}
	if strings.TrimSpace(surface.AttachedInstanceID) != "" || surface.PendingHeadless != nil {
		return nil, HeadlessRestoreResult{Status: HeadlessRestoreStatusSkipped}
	}
	view := s.headlessRestoreView(surface, attempt)
	if view == nil {
		if !allowMissingThreadFailure {
			return nil, HeadlessRestoreResult{Status: HeadlessRestoreStatusWaiting}
		}
		return []control.UIEvent{{
			Kind:             control.UIEventNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice:           headlessRestoreFailureNotice("thread_not_found"),
		}}, HeadlessRestoreResult{Status: HeadlessRestoreStatusFailed, FailureCode: "thread_not_found"}
	}
	target := s.resolveHeadlessRestoreTargetFromView(surface, view)
	switch target.Mode {
	case threadAttachFreeVisible, threadAttachReuseHeadless:
		return s.attachSurfaceToKnownThread(surface, target.Instance, target.View, attachSurfaceToKnownThreadHeadlessRestore), HeadlessRestoreResult{Status: HeadlessRestoreStatusAttached}
	case threadAttachCreateHeadless:
		return s.startHeadlessForResolvedThreadWithMode(surface, target.View, startHeadlessModeHeadlessRestore), HeadlessRestoreResult{Status: HeadlessRestoreStatusStarting}
	case threadAttachUnavailable:
		if target.NoticeCode == "thread_not_found" && !allowMissingThreadFailure {
			return nil, HeadlessRestoreResult{Status: HeadlessRestoreStatusWaiting}
		}
		failureCode := firstNonEmpty(strings.TrimSpace(target.NoticeCode), "thread_not_found")
		return []control.UIEvent{{
			Kind:             control.UIEventNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice:           headlessRestoreFailureNotice(failureCode),
		}}, HeadlessRestoreResult{Status: HeadlessRestoreStatusFailed, FailureCode: failureCode}
	default:
		return nil, HeadlessRestoreResult{Status: HeadlessRestoreStatusSkipped}
	}
}

func (s *Service) TryAutoResumeNormalSurface(surfaceID string, attempt SurfaceResumeAttempt, allowMissingTargetFailure bool) ([]control.UIEvent, SurfaceResumeResult) {
	surface := s.root.Surfaces[strings.TrimSpace(surfaceID)]
	if surface == nil {
		return nil, SurfaceResumeResult{Status: SurfaceResumeStatusSkipped}
	}
	if s.normalizeSurfaceProductMode(surface) != state.ProductModeNormal {
		return nil, SurfaceResumeResult{Status: SurfaceResumeStatusSkipped}
	}
	if strings.TrimSpace(surface.AttachedInstanceID) != "" || surface.PendingHeadless != nil {
		return nil, SurfaceResumeResult{Status: SurfaceResumeStatusSkipped}
	}

	failureCode := ""
	threadID := strings.TrimSpace(attempt.ThreadID)
	if threadID != "" {
		view := s.mergedThreadView(surface, threadID)
		if inst, code := s.resolveSurfaceResumeVisibleInstance(surface, view, strings.TrimSpace(attempt.InstanceID)); inst != nil {
			return s.attachSurfaceToKnownThread(surface, inst, view, attachSurfaceToKnownThreadSurfaceResume), SurfaceResumeResult{Status: SurfaceResumeStatusThreadAttached}
		} else if code != "" {
			failureCode = code
		}
		if !allowMissingTargetFailure {
			return nil, SurfaceResumeResult{Status: SurfaceResumeStatusWaiting}
		}
	}

	workspaceKey := normalizeWorkspaceClaimKey(attempt.WorkspaceKey)
	if workspaceKey != "" {
		if owner := s.workspaceBusyOwnerForSurface(surface, workspaceKey); owner != nil {
			return nil, SurfaceResumeResult{Status: SurfaceResumeStatusFailed, FailureCode: "workspace_busy"}
		}
		if inst := s.resolveWorkspaceAttachInstance(surface, workspaceKey); inst != nil {
			return s.attachWorkspaceWithMode(surface, workspaceKey, attachWorkspaceModeSurfaceResume), SurfaceResumeResult{Status: SurfaceResumeStatusWorkspaceAttached}
		}
		if len(s.workspaceOnlineInstances(workspaceKey)) == 0 {
			if !allowMissingTargetFailure {
				return nil, SurfaceResumeResult{Status: SurfaceResumeStatusWaiting}
			}
			return nil, SurfaceResumeResult{Status: SurfaceResumeStatusFailed, FailureCode: firstNonEmpty(failureCode, "workspace_not_found")}
		}
		return nil, SurfaceResumeResult{Status: SurfaceResumeStatusFailed, FailureCode: "workspace_instance_busy"}
	}

	if failureCode == "" {
		failureCode = "thread_not_found"
	}
	if !allowMissingTargetFailure && failureCode == "thread_not_found" {
		return nil, SurfaceResumeResult{Status: SurfaceResumeStatusWaiting}
	}
	return nil, SurfaceResumeResult{Status: SurfaceResumeStatusFailed, FailureCode: failureCode}
}

func (s *Service) TryAutoResumeVSCodeSurface(surfaceID, instanceID string) ([]control.UIEvent, SurfaceResumeResult) {
	surface := s.root.Surfaces[strings.TrimSpace(surfaceID)]
	if surface == nil {
		return nil, SurfaceResumeResult{Status: SurfaceResumeStatusSkipped}
	}
	if s.normalizeSurfaceProductMode(surface) != state.ProductModeVSCode {
		return nil, SurfaceResumeResult{Status: SurfaceResumeStatusSkipped}
	}
	if strings.TrimSpace(surface.AttachedInstanceID) != "" || surface.PendingHeadless != nil {
		return nil, SurfaceResumeResult{Status: SurfaceResumeStatusSkipped}
	}

	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return nil, SurfaceResumeResult{Status: SurfaceResumeStatusSkipped}
	}
	inst := s.root.Instances[instanceID]
	if inst == nil || !inst.Online || !isVSCodeInstance(inst) {
		return nil, SurfaceResumeResult{Status: SurfaceResumeStatusWaiting}
	}
	if owner := s.instanceClaimSurface(instanceID); owner != nil && owner.SurfaceSessionID != surface.SurfaceSessionID {
		return nil, SurfaceResumeResult{Status: SurfaceResumeStatusFailed, FailureCode: "instance_busy"}
	}
	return s.attachInstanceWithMode(surface, instanceID, attachInstanceModeSurfaceResume), SurfaceResumeResult{Status: SurfaceResumeStatusInstanceAttached}
}

func (s *Service) headlessRestoreView(surface *state.SurfaceConsoleRecord, attempt HeadlessRestoreAttempt) *mergedThreadView {
	threadID := strings.TrimSpace(attempt.ThreadID)
	if threadID == "" {
		return nil
	}
	view := s.mergedThreadView(surface, threadID)
	if view == nil {
		return s.syntheticHeadlessRestoreView(threadID, attempt.ThreadTitle, attempt.ThreadCWD)
	}
	cloned := *view
	thread := &state.ThreadRecord{ThreadID: threadID}
	if view.Thread != nil {
		copy := *view.Thread
		thread = &copy
	}
	if strings.TrimSpace(thread.Name) == "" {
		thread.Name = strings.TrimSpace(attempt.ThreadTitle)
	}
	if strings.TrimSpace(thread.CWD) == "" {
		thread.CWD = strings.TrimSpace(attempt.ThreadCWD)
	}
	cloned.Thread = thread
	return &cloned
}

func (s *Service) syntheticHeadlessRestoreView(threadID, threadTitle, threadCWD string) *mergedThreadView {
	threadID = strings.TrimSpace(threadID)
	threadCWD = strings.TrimSpace(threadCWD)
	threadTitle = strings.TrimSpace(threadTitle)
	if threadID == "" || threadCWD == "" {
		return nil
	}
	view := &mergedThreadView{
		ThreadID: threadID,
		Thread: &state.ThreadRecord{
			ThreadID: threadID,
			Name:     threadTitle,
			CWD:      threadCWD,
			Loaded:   true,
		},
	}
	if owner := s.threadClaimSurface(threadID); owner != nil {
		view.BusyOwner = owner
	}
	return view
}

func headlessRestoreFailureNotice(code string) *control.Notice {
	switch strings.TrimSpace(code) {
	case "workspace_busy":
		return &control.Notice{
			Code:  "headless_restore_workspace_busy",
			Title: "恢复失败",
			Text:  "之前的 workspace 当前被其他飞书会话占用，暂时无法恢复，请稍后重试或尝试其他会话。",
		}
	case "thread_busy":
		return &control.Notice{
			Code:  "headless_restore_thread_busy",
			Title: "恢复失败",
			Text:  "之前的会话当前被其他窗口占用，暂时无法恢复，请稍后重试或尝试其他会话。",
		}
	case "thread_cwd_missing":
		return &control.Notice{
			Code:  "headless_restore_thread_cwd_missing",
			Title: "恢复失败",
			Text:  "之前的会话缺少可恢复的工作目录，暂时无法自动恢复，请稍后重试或尝试其他会话。",
		}
	default:
		return &control.Notice{
			Code:  "headless_restore_thread_not_found",
			Title: "恢复失败",
			Text:  "暂时无法找到之前会话，请稍后重试或尝试其他会话。",
		}
	}
}

func surfaceResumeFailureNotice(code string) *control.Notice {
	switch strings.TrimSpace(code) {
	case "workspace_busy":
		return &control.Notice{
			Code:  "surface_resume_workspace_busy",
			Title: "恢复失败",
			Text:  "之前的工作区当前被其他飞书会话接管，暂时无法恢复。请稍后重试，或发送 /list 重新选择工作区。",
		}
	case "workspace_instance_busy":
		return &control.Notice{
			Code:  "surface_resume_workspace_instance_busy",
			Title: "恢复失败",
			Text:  "之前的工作区当前暂时不可接管。请稍后重试，或发送 /list 重新选择工作区。",
		}
	case "thread_busy":
		return &control.Notice{
			Code:  "surface_resume_thread_busy",
			Title: "恢复失败",
			Text:  "之前的会话当前被其他飞书会话占用，暂时无法直接恢复。请稍后重试，或发送 /use 选择其他会话。",
		}
	default:
		return &control.Notice{
			Code:  "surface_resume_target_not_found",
			Title: "恢复失败",
			Text:  "暂时无法恢复到之前会话。请稍后重试，或发送 /list 重新选择工作区。",
		}
	}
}

func NoticeForSurfaceResumeFailure(code string) *control.Notice {
	return surfaceResumeFailureNotice(code)
}

func vscodeSurfaceResumeFailureNotice(code string) *control.Notice {
	switch strings.TrimSpace(code) {
	case "instance_busy":
		return &control.Notice{
			Code:  "surface_resume_instance_busy",
			Title: "恢复失败",
			Text:  "之前的 VS Code 实例当前已被其他飞书会话接管，暂时无法恢复。请稍后重试，或发送 /list 重新选择实例。",
		}
	default:
		return &control.Notice{
			Code:  "surface_resume_instance_not_found",
			Title: "恢复失败",
			Text:  "暂时无法恢复到之前的 VS Code 实例。请稍后重试，或发送 /list 重新选择实例。",
		}
	}
}

func NoticeForVSCodeSurfaceResumeFailure(code string) *control.Notice {
	return vscodeSurfaceResumeFailureNotice(code)
}

func NoticeForVSCodeOpenPrompt(hadPreviousInstance bool) *control.Notice {
	if hadPreviousInstance {
		return &control.Notice{
			Code:  "surface_resume_open_vscode",
			Title: "请先打开 VS Code",
			Text:  "还没有找到之前的 VS Code 实例。请先打开 VS Code 中的 Codex，然后再回来使用。",
		}
	}
	return &control.Notice{
		Code:  "vscode_open_required",
		Title: "请先打开 VS Code",
		Text:  "当前还没有可用的 VS Code 实例。请先打开 VS Code 中的 Codex，然后再回来使用。",
	}
}

func (s *Service) useThread(surface *state.SurfaceConsoleRecord, threadID string, allowCrossWorkspace bool) []control.UIEvent {
	threadID = strings.TrimSpace(threadID)
	target := s.resolveThreadTargetWithScope(surface, threadID, allowCrossWorkspace)
	return s.executeResolvedThreadTarget(surface, threadID, target)
}

func (s *Service) executeResolvedThreadTarget(surface *state.SurfaceConsoleRecord, threadID string, target resolvedThreadTarget) []control.UIEvent {
	switch target.Mode {
	case threadAttachCurrentVisible:
		return s.useAttachedVisibleThread(surface, threadID)
	case threadAttachFreeVisible, threadAttachReuseHeadless:
		if blocked := s.blockFreshThreadAttach(surface); blocked != nil {
			return blocked
		}
		return s.attachSurfaceToKnownThread(surface, target.Instance, target.View, attachSurfaceToKnownThreadDefault)
	case threadAttachCreateHeadless:
		if blocked := s.blockFreshThreadAttach(surface); blocked != nil {
			return blocked
		}
		return s.startHeadlessForResolvedThread(surface, target.View)
	default:
		code := firstNonEmpty(target.NoticeCode, "thread_not_found")
		text := firstNonEmpty(target.NoticeText, "目标会话不存在或当前不可见。")
		return notice(surface, code, text)
	}
}

func (s *Service) useAttachedVisibleThread(surface *state.SurfaceConsoleRecord, threadID string) []control.UIEvent {
	return s.useAttachedVisibleThreadMode(surface, threadID, s.surfaceThreadPickRouteMode(surface))
}

func (s *Service) useAttachedVisibleThreadMode(surface *state.SurfaceConsoleRecord, threadID string, routeMode state.RouteMode) []control.UIEvent {
	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil {
		return notice(surface, "not_attached", s.notAttachedText(surface))
	}
	if (surface.RouteMode != routeMode || surface.SelectedThreadID != threadID) && surfaceHasRouteMutationRequestState(surface) {
		if blocked := s.blockRouteMutationForRequestState(surface); blocked != nil {
			return blocked
		}
	}
	events := []control.UIEvent{}
	if surface.RouteMode == state.RouteModeNewThreadReady {
		if blocked := s.blockPreparedNewThreadRouteExit(surface); blocked != nil {
			return blocked
		}
		events = append(events, s.discardDrafts(surface)...)
	} else if blocked := s.blockThreadSwitch(surface); blocked != nil {
		return blocked
	}
	thread := inst.Threads[threadID]
	if !threadVisible(thread) {
		return append(events, notice(surface, "thread_not_found", "目标会话不存在或当前不可见。")...)
	}
	if !threadBelongsToInstanceWorkspace(inst, thread) {
		fallback := s.resolveThreadTargetFromView(surface, s.mergedThreadView(surface, threadID))
		if fallback.Mode == threadAttachCurrentVisible {
			return append(events, notice(surface, "thread_not_found", "目标会话不存在或当前不可见。")...)
		}
		return append(events, s.executeResolvedThreadTarget(surface, threadID, fallback)...)
	}
	if owner := s.threadClaimSurface(threadID); owner != nil && owner.SurfaceSessionID != surface.SurfaceSessionID {
		switch s.threadKickStatus(inst, owner, threadID) {
		case threadKickIdle:
			return append(events, s.presentKickThreadPrompt(surface, inst, threadID, owner)...)
		case threadKickQueued:
			return append(events, notice(surface, "thread_busy_queued", "目标会话当前还有排队任务，暂时不能强踢。请等待对方队列清空，或切换到其他会话。")...)
		case threadKickRunning:
			return append(events, notice(surface, "thread_busy_running", "目标会话当前正在执行，暂时不能强踢。请等待执行完成，或切换到其他会话。")...)
		default:
			return append(events, notice(surface, "thread_busy", "目标会话当前已被其他飞书会话占用。")...)
		}
	}
	prevThreadID := surface.SelectedThreadID
	prevRouteMode := surface.RouteMode
	s.releaseSurfaceThreadClaim(surface)
	if !s.claimThread(surface, inst, threadID) {
		surface.RouteMode = state.RouteModeUnbound
		s.clearPreparedNewThread(surface)
		return append(events, notice(surface, "thread_busy", "目标会话当前已被其他飞书会话占用。")...)
	}
	events = append(events, s.discardStagedImagesForRouteChange(surface, prevThreadID, prevRouteMode, threadID, routeMode)...)
	surface.SelectedThreadID = threadID
	s.clearPreparedNewThread(surface)
	surface.RouteMode = routeMode
	title := threadID
	preview := ""
	thread = s.ensureThread(inst, threadID)
	s.touchThread(thread)
	title = displayThreadTitle(inst, thread, threadID)
	preview = threadPreview(thread)
	events = append(events, s.threadSelectionEvents(surface, threadID, string(surface.RouteMode), title, preview)...)
	events = append(events, s.replayThreadUpdate(surface, inst, threadID)...)
	if len(events) != 0 {
		return events
	}
	return notice(surface, "selection_unchanged", fmt.Sprintf("当前输入目标保持为：%s", title))
}

func (s *Service) attachSurfaceToKnownThread(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, view *mergedThreadView, mode attachSurfaceToKnownThreadMode) []control.UIEvent {
	if surface == nil || inst == nil || view == nil || strings.TrimSpace(view.ThreadID) == "" {
		return nil
	}
	workspaceKey := mergedThreadWorkspaceClaimKey(view)
	if s.surfaceUsesWorkspaceClaims(surface) && workspaceKey == "" {
		return notice(surface, "workspace_key_missing", "当前无法确定目标会话所属的 workspace，暂时不能在 normal 模式接管。请切到 `/mode vscode` 后再试。")
	}
	if owner := s.workspaceBusyOwnerForSurface(surface, workspaceKey); owner != nil {
		return attachSurfaceToKnownThreadWorkspaceBusyNotice(surface, mode)
	}
	if owner := s.instanceClaimSurface(inst.InstanceID); owner != nil && owner.SurfaceSessionID != surface.SurfaceSessionID {
		return attachSurfaceToKnownThreadInstanceBusyNotice(surface, inst, mode)
	}

	events := []control.UIEvent{}
	if surface.AttachedInstanceID != "" {
		events = append(events, s.discardDrafts(surface)...)
		events = append(events, s.finalizeDetachedSurface(surface)...)
	} else {
		events = append(events, s.discardDrafts(surface)...)
		clearSurfaceRequestCapture(surface)
		clearSurfaceRequests(surface)
		s.clearPreparedNewThread(surface)
		surface.PromptOverride = state.ModelConfigRecord{}
		surface.PendingHeadless = nil
		surface.ActiveQueueItemID = ""
		surface.DispatchMode = state.DispatchModeNormal
		surface.Abandoning = false
		delete(s.pausedUntil, surface.SurfaceSessionID)
		delete(s.abandoningUntil, surface.SurfaceSessionID)
	}
	if !s.claimWorkspace(surface, workspaceKey) {
		if s.surfaceUsesWorkspaceClaims(surface) {
			return append(events, attachSurfaceToKnownThreadWorkspaceBusyNotice(surface, mode)...)
		}
		return append(events, notice(surface, "workspace_key_missing", "当前无法确定目标会话所属的 workspace，暂时不能在 normal 模式接管。请切到 `/mode vscode` 后再试。")...)
	}

	if !s.claimInstance(surface, inst.InstanceID) {
		s.releaseSurfaceWorkspaceClaim(surface)
		return append(events, attachSurfaceToKnownThreadInstanceBusyNotice(surface, inst, mode)...)
	}
	s.surfaceCurrentWorkspaceKey(surface)
	surface.AttachedInstanceID = inst.InstanceID
	surface.PendingHeadless = nil
	surface.ActiveQueueItemID = ""
	surface.DispatchMode = state.DispatchModeNormal
	surface.Abandoning = false
	delete(s.pausedUntil, surface.SurfaceSessionID)
	delete(s.abandoningUntil, surface.SurfaceSessionID)
	clearSurfaceRequests(surface)
	s.clearPreparedNewThread(surface)
	surface.PromptOverride = state.ModelConfigRecord{}

	if isHeadlessInstance(inst) && strings.TrimSpace(threadCWD(view)) != "" {
		s.retargetManagedHeadlessInstance(inst, threadCWD(view))
	}

	thread := s.ensureThread(inst, view.ThreadID)
	if view.Thread != nil {
		if strings.TrimSpace(view.Thread.Name) != "" {
			thread.Name = strings.TrimSpace(view.Thread.Name)
		}
		if strings.TrimSpace(view.Thread.Preview) != "" {
			thread.Preview = strings.TrimSpace(view.Thread.Preview)
		}
		if strings.TrimSpace(view.Thread.CWD) != "" {
			thread.CWD = strings.TrimSpace(view.Thread.CWD)
		}
		if strings.TrimSpace(view.Thread.State) != "" {
			thread.State = strings.TrimSpace(view.Thread.State)
		}
		if strings.TrimSpace(view.Thread.ExplicitModel) != "" {
			thread.ExplicitModel = strings.TrimSpace(view.Thread.ExplicitModel)
		}
		if strings.TrimSpace(view.Thread.ExplicitReasoningEffort) != "" {
			thread.ExplicitReasoningEffort = strings.TrimSpace(view.Thread.ExplicitReasoningEffort)
		}
		thread.Loaded = thread.Loaded || view.Thread.Loaded
		thread.Archived = view.Thread.Archived
		thread.LastUsedAt = view.Thread.LastUsedAt
		thread.ListOrder = view.Thread.ListOrder
	}
	if mode == attachSurfaceToKnownThreadHeadlessRestore || mode == attachSurfaceToKnownThreadSurfaceResume {
		s.clearThreadReplay(inst, view.ThreadID)
	} else {
		s.adoptThreadReplay(inst, view.ThreadID)
	}
	s.touchThread(thread)
	s.releaseSurfaceThreadClaim(surface)
	if !s.claimKnownThread(surface, inst, view.ThreadID) {
		events = append(events, s.finalizeDetachedSurface(surface)...)
		return append(events, attachSurfaceToKnownThreadThreadBusyNotice(surface, mode)...)
	}
	surface.SelectedThreadID = view.ThreadID
	surface.RouteMode = state.RouteModePinned

	title := displayThreadTitle(inst, thread, view.ThreadID)
	preview := threadPreview(thread)
	if mode == attachSurfaceToKnownThreadHeadlessRestore {
		surface.LastSelection = &state.SelectionAnnouncementRecord{
			ThreadID:  view.ThreadID,
			RouteMode: string(surface.RouteMode),
			Title:     title,
			Preview:   preview,
		}
		events = append(events, control.UIEvent{
			Kind:             control.UIEventNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice: &control.Notice{
				Code:  "headless_restore_attached",
				Title: "会话已恢复",
				Text:  fmt.Sprintf("重连成功，已恢复到之前会话：%s", title),
			},
		})
	} else if mode == attachSurfaceToKnownThreadSurfaceResume {
		s.clearThreadReplay(inst, view.ThreadID)
		surface.LastSelection = &state.SelectionAnnouncementRecord{
			ThreadID:  view.ThreadID,
			RouteMode: string(surface.RouteMode),
			Title:     title,
			Preview:   preview,
		}
		events = append(events, control.UIEvent{
			Kind:             control.UIEventNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice: &control.Notice{
				Code:  "surface_resume_attached",
				Title: "会话已恢复",
				Text:  fmt.Sprintf("已恢复到之前会话：%s", title),
			},
		})
	} else {
		attachLead := s.attachedLeadText(surface, inst)
		events = append(events, control.UIEvent{
			Kind:             control.UIEventNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice: &control.Notice{
				Code: "attached",
				Text: fmt.Sprintf("%s 当前输入目标：%s", attachLead, title),
			},
		})
		events = append(events, s.threadSelectionEvents(surface, view.ThreadID, string(surface.RouteMode), title, preview)...)
		events = append(events, s.replayThreadUpdate(surface, inst, view.ThreadID)...)
	}
	events = append(events, s.maybeRequestThreadRefresh(surface, inst, view.ThreadID)...)
	return events
}

func (s *Service) startHeadlessForResolvedThread(surface *state.SurfaceConsoleRecord, view *mergedThreadView) []control.UIEvent {
	return s.startHeadlessForResolvedThreadWithMode(surface, view, startHeadlessModeDefault)
}

func attachSurfaceToKnownThreadInstanceBusyNotice(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, mode attachSurfaceToKnownThreadMode) []control.UIEvent {
	if mode == attachSurfaceToKnownThreadHeadlessRestore {
		return []control.UIEvent{{
			Kind:             control.UIEventNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice:           headlessRestoreFailureNotice("thread_busy"),
		}}
	}
	if mode == attachSurfaceToKnownThreadSurfaceResume {
		return []control.UIEvent{{
			Kind:             control.UIEventNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice:           surfaceResumeFailureNotice("workspace_instance_busy"),
		}}
	}
	if surface != nil && state.NormalizeProductMode(surface.ProductMode) == state.ProductModeNormal {
		return notice(surface, "workspace_instance_busy", "目标工作区当前暂时不可接管，请稍后重试。")
	}
	return notice(surface, "instance_busy", fmt.Sprintf("%s 当前已被其他飞书会话接管，请等待对方 /detach。", inst.DisplayName))
}

func attachSurfaceToKnownThreadThreadBusyNotice(surface *state.SurfaceConsoleRecord, mode attachSurfaceToKnownThreadMode) []control.UIEvent {
	if mode == attachSurfaceToKnownThreadHeadlessRestore {
		return []control.UIEvent{{
			Kind:             control.UIEventNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice:           headlessRestoreFailureNotice("thread_busy"),
		}}
	}
	if mode == attachSurfaceToKnownThreadSurfaceResume {
		return []control.UIEvent{{
			Kind:             control.UIEventNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice:           surfaceResumeFailureNotice("thread_busy"),
		}}
	}
	return notice(surface, "thread_busy", "目标会话当前已被其他飞书会话占用。")
}

func attachSurfaceToKnownThreadWorkspaceBusyNotice(surface *state.SurfaceConsoleRecord, mode attachSurfaceToKnownThreadMode) []control.UIEvent {
	if mode == attachSurfaceToKnownThreadHeadlessRestore {
		return []control.UIEvent{{
			Kind:             control.UIEventNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice:           headlessRestoreFailureNotice("workspace_busy"),
		}}
	}
	if mode == attachSurfaceToKnownThreadSurfaceResume {
		return []control.UIEvent{{
			Kind:             control.UIEventNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice:           surfaceResumeFailureNotice("workspace_busy"),
		}}
	}
	return notice(surface, "workspace_busy", "目标 workspace 当前已被其他飞书会话接管。")
}

func (s *Service) startHeadlessForResolvedThreadWithMode(surface *state.SurfaceConsoleRecord, view *mergedThreadView, mode startHeadlessMode) []control.UIEvent {
	if surface == nil || view == nil {
		return nil
	}
	cwd := strings.TrimSpace(threadCWD(view))
	if cwd == "" {
		if mode == startHeadlessModeHeadlessRestore {
			return []control.UIEvent{{
				Kind:             control.UIEventNotice,
				SurfaceSessionID: surface.SurfaceSessionID,
				Notice:           headlessRestoreFailureNotice("thread_cwd_missing"),
			}}
		}
		return notice(surface, "thread_cwd_missing", "目标会话缺少可恢复的工作目录，当前无法在后台恢复该会话。")
	}
	if owner := s.workspaceBusyOwnerForSurface(surface, cwd); owner != nil {
		if mode == startHeadlessModeHeadlessRestore {
			return []control.UIEvent{{
				Kind:             control.UIEventNotice,
				SurfaceSessionID: surface.SurfaceSessionID,
				Notice:           headlessRestoreFailureNotice("workspace_busy"),
			}}
		}
		return notice(surface, "workspace_busy", "目标 workspace 当前已被其他飞书会话接管。")
	}
	s.nextHeadlessID++
	instanceID := fmt.Sprintf("inst-headless-%d-%d", s.now().UnixNano(), s.nextHeadlessID)
	threadTitle := displayThreadTitle(view.Inst, view.Thread, view.ThreadID)
	threadPreview := ""
	threadName := ""
	sourceInstanceID := ""
	if view.Thread != nil {
		threadPreview = strings.TrimSpace(view.Thread.Preview)
		threadName = strings.TrimSpace(view.Thread.Name)
	}
	if view.Inst != nil {
		sourceInstanceID = view.Inst.InstanceID
	}

	events := []control.UIEvent{}
	if surface.AttachedInstanceID != "" {
		events = append(events, s.discardDrafts(surface)...)
		events = append(events, s.finalizeDetachedSurface(surface)...)
	} else {
		events = append(events, s.discardDrafts(surface)...)
		clearSurfaceRequestCapture(surface)
		clearSurfaceRequests(surface)
		s.clearPreparedNewThread(surface)
		surface.PromptOverride = state.ModelConfigRecord{}
	}
	if !s.claimWorkspace(surface, cwd) {
		if s.surfaceUsesWorkspaceClaims(surface) {
			if mode == startHeadlessModeHeadlessRestore {
				return append(events, control.UIEvent{
					Kind:             control.UIEventNotice,
					SurfaceSessionID: surface.SurfaceSessionID,
					Notice:           headlessRestoreFailureNotice("workspace_busy"),
				})
			}
			return append(events, notice(surface, "workspace_busy", "目标 workspace 当前已被其他飞书会话接管。")...)
		}
		return append(events, notice(surface, "workspace_key_missing", "当前无法确定目标会话所属的 workspace，暂时不能在 normal 模式恢复。请切到 `/mode vscode` 后再试。")...)
	}
	surface.PendingHeadless = &state.HeadlessLaunchRecord{
		InstanceID:       instanceID,
		ThreadID:         view.ThreadID,
		ThreadTitle:      threadTitle,
		ThreadName:       threadName,
		ThreadPreview:    threadPreview,
		ThreadCWD:        cwd,
		RequestedAt:      s.now(),
		ExpiresAt:        s.now().Add(s.config.HeadlessLaunchWait),
		Status:           state.HeadlessLaunchStarting,
		SourceInstanceID: sourceInstanceID,
		AutoRestore:      mode == startHeadlessModeHeadlessRestore,
	}
	if mode == startHeadlessModeDefault {
		events = append(events, control.UIEvent{
			Kind:             control.UIEventNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice: &control.Notice{
				Code:  "headless_starting",
				Title: "准备恢复会话",
				Text:  fmt.Sprintf("正在后台准备恢复会话：%s", threadTitle),
			},
		})
	}
	events = append(events, control.UIEvent{
		Kind:             control.UIEventDaemonCommand,
		SurfaceSessionID: surface.SurfaceSessionID,
		DaemonCommand: &control.DaemonCommand{
			Kind:             control.DaemonCommandStartHeadless,
			SurfaceSessionID: surface.SurfaceSessionID,
			InstanceID:       instanceID,
			ThreadID:         view.ThreadID,
			ThreadTitle:      threadTitle,
			ThreadCWD:        cwd,
			AutoRestore:      mode == startHeadlessModeHeadlessRestore,
		},
	})
	return events
}

func (s *Service) prepareNewThread(surface *state.SurfaceConsoleRecord) []control.UIEvent {
	if s.normalizeSurfaceProductMode(surface) != state.ProductModeNormal {
		return notice(surface, "new_thread_disabled_vscode", "当前处于 vscode 模式，`/new` 只在 normal 模式可用。请先 `/mode normal`，或继续通过 follow / `/use` 使用当前 VS Code 会话。")
	}
	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil {
		return notice(surface, "not_attached", s.notAttachedText(surface))
	}
	if surface.ActiveRequestCapture != nil {
		return notice(surface, "request_capture_waiting_text", "当前正在等待你发送一条文字处理意见，请先发送文本或重新处理确认卡片。")
	}
	if pending := activePendingRequest(surface); pending != nil {
		_ = pending
		return notice(surface, "request_pending", pendingRequestNoticeText(activePendingRequest(surface)))
	}
	if surface.RouteMode == state.RouteModeNewThreadReady {
		if blocked := s.blockPreparedNewThreadReprepare(surface); blocked != nil {
			return blocked
		}
		cwd := strings.TrimSpace(surface.PreparedThreadCWD)
		if cwd == "" {
			if fallbackCWD, fallbackThreadID, ok := s.prepareNewThreadBase(surface, inst); ok {
				surface.PreparedThreadCWD = fallbackCWD
				surface.PreparedFromThreadID = fallbackThreadID
				cwd = fallbackCWD
			}
		}
		if cwd == "" {
			return notice(surface, "new_thread_cwd_missing", "当前无法获取新会话的工作目录，请先重新 /use 一个有工作目录的会话。")
		}
		discarded := countPendingDrafts(surface)
		events := s.discardDrafts(surface)
		surface.PreparedAt = s.now()
		if discarded == 0 {
			return append(events, notice(surface, "already_new_thread_ready", "当前已经在新建会话待命状态。下一条文本会创建新会话。")...)
		}
		return append(events, notice(surface, "new_thread_ready_reset", fmt.Sprintf("已丢弃 %d 条未发送输入。下一条文本会创建新会话。", discarded))...)
	}
	cwd, threadID, ok := s.prepareNewThreadBase(surface, inst)
	if !ok {
		if s.normalizeSurfaceProductMode(surface) == state.ProductModeNormal {
			return notice(surface, "new_thread_cwd_missing", "当前工作区缺少可继承的工作目录，暂时无法新建会话。请先 /list 切换工作区，或稍后重试。")
		}
		return notice(surface, "new_thread_requires_bound_thread", "当前必须先绑定并接管一个会话，才能基于它的新建会话。请先 /use，或在 follow 模式下等到已跟随到会话。")
	}
	if blocked := s.blockNewThreadPreparation(surface); blocked != nil {
		return blocked
	}
	discarded := countPendingDrafts(surface)
	events := s.discardDrafts(surface)
	prevThreadID := surface.SelectedThreadID
	prevRouteMode := surface.RouteMode
	s.releaseSurfaceThreadClaim(surface)
	surface.RouteMode = state.RouteModeNewThreadReady
	surface.PreparedThreadCWD = cwd
	surface.PreparedFromThreadID = threadID
	surface.PreparedAt = s.now()
	events = append(events, s.discardStagedImagesForRouteChange(surface, prevThreadID, prevRouteMode, "", state.RouteModeNewThreadReady)...)
	events = append(events, s.threadSelectionEvents(surface, "", string(state.RouteModeNewThreadReady), preparedNewThreadSelectionTitle(), "")...)
	text := "已清空当前远端上下文。下一条文本会创建新会话。"
	if discarded > 0 {
		text = fmt.Sprintf("已清空当前远端上下文，并丢弃 %d 条未发送输入。下一条文本会创建新会话。", discarded)
	}
	return append(events, notice(surface, "new_thread_ready", text)...)
}

func (s *Service) prepareNewThreadBase(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord) (string, string, bool) {
	if surface == nil || inst == nil {
		return "", "", false
	}
	if s.normalizeSurfaceProductMode(surface) == state.ProductModeNormal {
		workspaceKey := s.surfaceCurrentWorkspaceKey(surface)
		if workspaceKey == "" {
			return "", "", false
		}
		threadID := strings.TrimSpace(surface.SelectedThreadID)
		if threadID == "" || !s.surfaceOwnsThread(surface, threadID) || !threadVisible(inst.Threads[threadID]) {
			return workspaceKey, "", true
		}
		return workspaceKey, threadID, true
	}

	threadID := strings.TrimSpace(surface.SelectedThreadID)
	if threadID == "" || !s.surfaceOwnsThread(surface, threadID) {
		return "", "", false
	}
	thread := inst.Threads[threadID]
	if !threadVisible(thread) {
		return "", "", false
	}
	cwd := strings.TrimSpace(thread.CWD)
	if cwd == "" {
		return "", "", false
	}
	return cwd, threadID, true
}

func preparedNewThreadSelectionTitle() string {
	return "新建会话（等待首条消息）"
}

func clearAutoContinueRuntime(surface *state.SurfaceConsoleRecord) {
	if surface == nil {
		return
	}
	surface.AutoContinue = state.AutoContinueRuntimeRecord{}
}

func parseProductMode(value string) (state.ProductMode, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "normal":
		return state.ProductModeNormal, true
	case "vscode", "vs-code", "vs_code":
		return state.ProductModeVSCode, true
	default:
		return "", false
	}
}

func (s *Service) handleModeCommand(surface *state.SurfaceConsoleRecord, action control.Action) []control.UIEvent {
	current := s.normalizeSurfaceProductMode(surface)
	parts := strings.Fields(strings.TrimSpace(action.Text))
	if len(parts) <= 1 {
		return []control.UIEvent{commandCatalogEvent(surface, s.buildModeCatalog(surface))}
	}
	if len(parts) != 2 {
		return notice(surface, "surface_mode_usage", "用法：/mode 查看当前状态；/mode normal；/mode vscode。")
	}
	target, ok := parseProductMode(parts[1])
	if !ok {
		return notice(surface, "surface_mode_usage", "用法：/mode 查看当前状态；/mode normal；/mode vscode。")
	}
	if target == current {
		return notice(surface, "surface_mode_current", fmt.Sprintf("当前已处于 %s 模式。", target))
	}
	inst := s.root.Instances[surface.AttachedInstanceID]
	if s.surfaceHasLiveRemoteWork(surface) || s.surfaceNeedsDelayedDetach(surface, inst) {
		return notice(surface, "surface_mode_busy", "当前仍有执行中的 turn、派发中的请求或排队消息，暂时不能切换模式。请等待完成、/stop，或先 /detach。")
	}

	events := s.discardDrafts(surface)
	pending := surface.PendingHeadless
	events = append(events, s.finalizeDetachedSurface(surface)...)
	if pending != nil {
		events = append(events, control.UIEvent{
			Kind:             control.UIEventDaemonCommand,
			SurfaceSessionID: surface.SurfaceSessionID,
			DaemonCommand: &control.DaemonCommand{
				Kind:             control.DaemonCommandKillHeadless,
				SurfaceSessionID: surface.SurfaceSessionID,
				InstanceID:       pending.InstanceID,
				ThreadID:         pending.ThreadID,
				ThreadTitle:      pending.ThreadTitle,
				ThreadCWD:        pending.ThreadCWD,
			},
		})
	}
	surface.ProductMode = target
	return append(events, notice(surface, "surface_mode_switched", fmt.Sprintf("已切换到 %s 模式。当前没有接管中的目标。", target))...)
}

func (s *Service) handleAutoContinueCommand(surface *state.SurfaceConsoleRecord, action control.Action) []control.UIEvent {
	parts := strings.Fields(strings.TrimSpace(action.Text))
	if len(parts) <= 1 {
		return []control.UIEvent{commandCatalogEvent(surface, s.buildAutoContinueCatalog(surface))}
	}
	if len(parts) != 2 {
		return notice(surface, "auto_continue_usage", "用法：`/autocontinue` 查看当前状态；`/autocontinue on`；`/autocontinue off`。")
	}

	switch strings.ToLower(parts[1]) {
	case "on", "enable", "enabled", "true":
		if surface.AutoContinue.Enabled {
			return notice(surface, "auto_continue_enabled", "当前飞书会话的 auto-continue 已开启。")
		}
		clearAutoContinueRuntime(surface)
		surface.AutoContinue.Enabled = true
		return notice(surface, "auto_continue_enabled", "已开启当前飞书会话的 auto-continue。daemon 重启后会恢复为关闭。")
	case "off", "disable", "disabled", "false":
		clearAutoContinueRuntime(surface)
		return notice(surface, "auto_continue_disabled", "已关闭当前飞书会话的 auto-continue。")
	default:
		return notice(surface, "auto_continue_usage", "用法：`/autocontinue` 查看当前状态；`/autocontinue on`；`/autocontinue off`。")
	}
}

func (s *Service) handleModelCommand(surface *state.SurfaceConsoleRecord, action control.Action) []control.UIEvent {
	parts := strings.Fields(strings.TrimSpace(action.Text))
	if len(parts) <= 1 {
		return []control.UIEvent{commandCatalogEvent(surface, s.buildModelCatalog(surface))}
	}
	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil {
		return notice(surface, "not_attached", s.notAttachedText(surface))
	}
	if len(parts) == 2 && isClearCommand(parts[1]) {
		surface.PromptOverride.Model = ""
		surface.PromptOverride.ReasoningEffort = ""
		surface.PromptOverride = compactPromptOverride(surface.PromptOverride)
		return notice(surface, "surface_override_cleared", "已清除飞书临时模型覆盖。之后从飞书发送的消息将恢复使用底层真实配置。")
	}
	if len(parts) > 3 {
		return notice(surface, "surface_override_usage", "用法：`/model` 查看当前配置；`/model <模型>`；`/model <模型> <推理强度>`；`/model clear`。")
	}
	override := surface.PromptOverride
	override.Model = parts[1]
	if len(parts) == 3 {
		if !looksLikeReasoningEffort(parts[2]) {
			return notice(surface, "surface_override_usage", "推理强度建议使用 `low`、`medium`、`high` 或 `xhigh`。")
		}
		override.ReasoningEffort = strings.ToLower(parts[2])
	}
	surface.PromptOverride = override
	summary := s.resolveNextPromptSummary(inst, surface, "", "", state.ModelConfigRecord{})
	return notice(surface, "surface_override_updated", formatOverrideNotice(summary, "已更新飞书临时模型覆盖。"))
}

func (s *Service) handleReasoningCommand(surface *state.SurfaceConsoleRecord, action control.Action) []control.UIEvent {
	parts := strings.Fields(strings.TrimSpace(action.Text))
	if len(parts) <= 1 {
		return []control.UIEvent{commandCatalogEvent(surface, s.buildReasoningCatalog(surface))}
	}
	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil {
		return notice(surface, "not_attached", s.notAttachedText(surface))
	}
	if len(parts) == 2 && isClearCommand(parts[1]) {
		surface.PromptOverride.ReasoningEffort = ""
		surface.PromptOverride = compactPromptOverride(surface.PromptOverride)
		return notice(surface, "surface_override_reasoning_cleared", "已清除飞书临时推理强度覆盖。")
	}
	if len(parts) != 2 || !looksLikeReasoningEffort(parts[1]) {
		return notice(surface, "surface_override_usage", "用法：`/reasoning` 查看当前配置；`/reasoning <推理强度>`；`/reasoning clear`。")
	}
	surface.PromptOverride.ReasoningEffort = strings.ToLower(parts[1])
	summary := s.resolveNextPromptSummary(inst, surface, "", "", state.ModelConfigRecord{})
	return notice(surface, "surface_override_updated", formatOverrideNotice(summary, "已更新飞书临时推理强度覆盖。"))
}

func (s *Service) handleAccessCommand(surface *state.SurfaceConsoleRecord, action control.Action) []control.UIEvent {
	parts := strings.Fields(strings.TrimSpace(action.Text))
	if len(parts) <= 1 {
		return []control.UIEvent{commandCatalogEvent(surface, s.buildAccessCatalog(surface))}
	}
	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil {
		return notice(surface, "not_attached", s.notAttachedText(surface))
	}
	if len(parts) != 2 {
		return notice(surface, "surface_access_usage", "用法：`/access` 查看当前配置；`/access full`；`/access confirm`；`/access clear`。")
	}
	if isClearCommand(parts[1]) {
		surface.PromptOverride.AccessMode = ""
		surface.PromptOverride = compactPromptOverride(surface.PromptOverride)
		summary := s.resolveNextPromptSummary(inst, surface, "", "", state.ModelConfigRecord{})
		return notice(surface, "surface_access_reset", formatOverrideNotice(summary, "已恢复飞书默认执行权限。"))
	}
	mode := agentproto.NormalizeAccessMode(parts[1])
	if mode == "" {
		return notice(surface, "surface_access_usage", "执行权限建议使用 `full` 或 `confirm`。")
	}
	surface.PromptOverride.AccessMode = mode
	surface.PromptOverride = compactPromptOverride(surface.PromptOverride)
	summary := s.resolveNextPromptSummary(inst, surface, "", "", state.ModelConfigRecord{})
	return notice(surface, "surface_access_updated", formatOverrideNotice(summary, "已更新飞书执行权限模式。"))
}

func (s *Service) handleText(surface *state.SurfaceConsoleRecord, action control.Action) []control.UIEvent {
	text := strings.TrimSpace(action.Text)
	if text == "" && len(action.Inputs) == 0 {
		return nil
	}

	if surface.ActiveRequestCapture != nil {
		if text == "" {
			return notice(surface, "request_capture_waiting_text", "当前反馈模式只接受文本，请发送一条文字处理意见。")
		}
		return s.consumeCapturedRequestFeedback(surface, action, text)
	}
	if pending := activePendingRequest(surface); pending != nil {
		return notice(surface, "request_pending", pendingRequestNoticeText(pending))
	}
	if surface.ActiveCommandCapture != nil {
		if text == "" {
			return notice(surface, "command_capture_waiting_text", "当前输入模式只接受文本，请发送一条模型名，或重新打开 `/model` 卡片。")
		}
		return s.consumeCapturedCommandInput(surface, text)
	}

	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil {
		return notice(surface, "not_attached", s.notAttachedText(surface))
	}
	if blocked := s.unboundInputBlocked(surface); blocked != nil {
		return blocked
	}
	if surface.RouteMode == state.RouteModeNewThreadReady && s.preparedNewThreadHasPendingCreate(surface) {
		return notice(surface, "new_thread_first_input_pending", "当前新会话的首条消息已经在排队或发送中；请等待它落地后再继续发送。")
	}

	threadID, cwd, routeMode, createThread := freezeRoute(inst, surface)
	inputs, stagedMessageIDs := s.consumeStagedInputs(surface)
	messageInputs := append([]agentproto.Input{}, action.Inputs...)
	if len(messageInputs) == 0 {
		messageInputs = []agentproto.Input{{Type: agentproto.InputText, Text: text}}
	}
	inputs = append(inputs, messageInputs...)
	if !createThread && threadID == "" {
		s.restoreStagedInputs(surface, stagedMessageIDs)
		return notice(surface, "thread_not_ready", "当前还没有可发送的目标会话。请先 /use 重新选择会话；normal 模式可 /new，如需跟随 VS Code 请先 /mode vscode 再 /follow。")
	}
	if createThread && strings.TrimSpace(cwd) == "" {
		s.restoreStagedInputs(surface, stagedMessageIDs)
		return notice(surface, "new_thread_cwd_missing", "当前无法获取新会话的工作目录，请先重新 /use 一个有工作目录的会话。")
	}
	return s.enqueueQueueItem(surface, action.MessageID, action.Text, stagedMessageIDs, inputs, threadID, cwd, routeMode, surface.PromptOverride, false)
}

func (s *Service) stageImage(surface *state.SurfaceConsoleRecord, action control.Action) []control.UIEvent {
	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil {
		return notice(surface, "not_attached", s.notAttachedText(surface))
	}
	if blocked := s.unboundInputBlocked(surface); blocked != nil {
		return blocked
	}
	if surface.ActiveRequestCapture != nil {
		return notice(surface, "request_capture_waiting_text", "当前正在等待你发送一条文字处理意见，请先发送文本或重新处理确认卡片。")
	}
	if surface.ActiveCommandCapture != nil {
		return notice(surface, "command_capture_waiting_text", "当前正在等待你发送一条模型名，请先发送文本，或重新打开 `/model` 卡片。")
	}
	if pending := activePendingRequest(surface); pending != nil {
		_ = pending
		return notice(surface, "request_pending", pendingRequestNoticeText(pending))
	}
	if surface.RouteMode == state.RouteModeNewThreadReady && s.preparedNewThreadHasPendingCreate(surface) {
		return notice(surface, "new_thread_first_input_pending", "当前新会话的首条消息已经在排队或发送中；如需带图，请等它创建完成后再发送下一条。")
	}
	s.nextImageID++
	image := &state.StagedImageRecord{
		ImageID:          fmt.Sprintf("img-%d", s.nextImageID),
		SurfaceSessionID: surface.SurfaceSessionID,
		SourceMessageID:  action.MessageID,
		LocalPath:        action.LocalPath,
		MIMEType:         action.MIMEType,
		State:            state.ImageStaged,
	}
	surface.StagedImages[image.ImageID] = image
	return []control.UIEvent{{
		Kind:             control.UIEventPendingInput,
		SurfaceSessionID: surface.SurfaceSessionID,
		PendingInput: &control.PendingInputState{
			QueueItemID:     image.ImageID,
			SourceMessageID: image.SourceMessageID,
			Status:          string(image.State),
			QueueOn:         true,
		},
	}}
}

func (s *Service) handleReactionCreated(surface *state.SurfaceConsoleRecord, action control.Action) []control.UIEvent {
	if surface == nil || !isThumbsUpReaction(action.ReactionType) {
		return nil
	}
	targetMessageID := strings.TrimSpace(action.TargetMessageID)
	if targetMessageID == "" {
		return nil
	}
	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil || inst.ActiveTurnID == "" || inst.ActiveThreadID == "" {
		return nil
	}
	for index, queueID := range surface.QueuedQueueItemIDs {
		item := surface.QueueItems[queueID]
		if item == nil || item.Status != state.QueueItemQueued || item.SourceMessageID != targetMessageID {
			continue
		}
		if item.FrozenThreadID == "" || item.FrozenThreadID != inst.ActiveThreadID {
			return nil
		}
		item.Status = state.QueueItemSteering
		surface.QueuedQueueItemIDs = removeString(surface.QueuedQueueItemIDs, item.ID)
		s.pendingSteers[item.ID] = &pendingSteerBinding{
			InstanceID:       inst.InstanceID,
			SurfaceSessionID: surface.SurfaceSessionID,
			QueueItemID:      item.ID,
			SourceMessageID:  item.SourceMessageID,
			ThreadID:         inst.ActiveThreadID,
			TurnID:           inst.ActiveTurnID,
			QueueIndex:       index,
		}
		return []control.UIEvent{{
			Kind:             control.UIEventAgentCommand,
			SurfaceSessionID: surface.SurfaceSessionID,
			Command: &agentproto.Command{
				Kind: agentproto.CommandTurnSteer,
				Origin: agentproto.Origin{
					Surface:   surface.SurfaceSessionID,
					UserID:    surface.ActorUserID,
					ChatID:    surface.ChatID,
					MessageID: item.SourceMessageID,
				},
				Target: agentproto.Target{
					ThreadID: inst.ActiveThreadID,
					TurnID:   inst.ActiveTurnID,
				},
				Prompt: agentproto.Prompt{
					Inputs: item.Inputs,
				},
			},
		}}
	}
	return nil
}

func (s *Service) handleMessageRecalled(surface *state.SurfaceConsoleRecord, targetMessageID string) []control.UIEvent {
	targetMessageID = strings.TrimSpace(targetMessageID)
	if surface == nil || targetMessageID == "" {
		return nil
	}
	if activeID := surface.ActiveQueueItemID; activeID != "" {
		if item := surface.QueueItems[activeID]; item != nil && queueItemHasSourceMessage(item, targetMessageID) {
			switch item.Status {
			case state.QueueItemDispatching, state.QueueItemRunning:
				return []control.UIEvent{{
					Kind:             control.UIEventNotice,
					SurfaceSessionID: surface.SurfaceSessionID,
					Notice: &control.Notice{
						Code:     "message_recall_too_late",
						Title:    "无法撤回排队",
						Text:     "这条输入已经开始执行，不能通过撤回取消。若要中断当前 turn，请发送 `/stop`。",
						ThemeKey: "system",
					},
				}}
			}
		}
	}
	for _, queueID := range surface.QueuedQueueItemIDs {
		item := surface.QueueItems[queueID]
		if item == nil || item.Status != state.QueueItemQueued || !queueItemHasSourceMessage(item, targetMessageID) {
			continue
		}
		item.Status = state.QueueItemDiscarded
		s.markImagesForMessages(surface, queueItemSourceMessageIDs(item), state.ImageDiscarded)
		surface.QueuedQueueItemIDs = removeString(surface.QueuedQueueItemIDs, item.ID)
		return s.pendingInputEvents(surface, control.PendingInputState{
			QueueItemID: item.ID,
			Status:      string(item.Status),
			QueueOff:    true,
			ThumbsDown:  true,
		}, queueItemSourceMessageIDs(item))
	}
	for _, image := range surface.StagedImages {
		if image.SourceMessageID == targetMessageID && image.State == state.ImageStaged {
			image.State = state.ImageCancelled
			return s.pendingInputEvents(surface, control.PendingInputState{
				QueueItemID: image.ImageID,
				Status:      string(image.State),
				QueueOff:    true,
				ThumbsDown:  true,
			}, []string{image.SourceMessageID})
		}
	}
	return nil
}

func isThumbsUpReaction(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.ReplaceAll(normalized, "_", "")
	normalized = strings.ReplaceAll(normalized, "-", "")
	return normalized == "thumbsup"
}

func (s *Service) stopSurface(surface *state.SurfaceConsoleRecord) []control.UIEvent {
	var events []control.UIEvent
	discarded := countPendingDrafts(surface)
	inst := s.root.Instances[surface.AttachedInstanceID]
	notice := control.Notice{
		Code:     "stop_no_active_turn",
		Title:    "没有正在运行的推理",
		Text:     "当前没有正在运行的推理。",
		ThemeKey: "system",
	}
	if inst != nil && inst.ActiveTurnID != "" {
		events = append(events, control.UIEvent{
			Kind:             control.UIEventAgentCommand,
			SurfaceSessionID: surface.SurfaceSessionID,
			Command: &agentproto.Command{
				Kind: agentproto.CommandTurnInterrupt,
				Origin: agentproto.Origin{
					Surface: surface.SurfaceSessionID,
					UserID:  surface.ActorUserID,
					ChatID:  surface.ChatID,
				},
				Target: agentproto.Target{
					ThreadID: inst.ActiveThreadID,
					TurnID:   inst.ActiveTurnID,
				},
			},
		})
		notice = control.Notice{
			Code:     "stop_requested",
			Title:    "已发送停止请求",
			Text:     "已向当前运行中的 turn 发送停止请求。",
			ThemeKey: "system",
		}
	} else if surface.ActiveQueueItemID != "" {
		if inst != nil && !inst.Online {
			notice = s.stopOfflineNotice(surface)
		} else {
			notice = control.Notice{
				Code:     "stop_not_interruptible",
				Title:    "当前还不能停止",
				Text:     "当前请求正在派发，尚未进入可中断状态。",
				ThemeKey: "system",
			}
		}
	}

	events = append(events, s.discardDrafts(surface)...)
	clearSurfaceRequests(surface)
	if discarded > 0 {
		notice.Text += fmt.Sprintf(" 已清空 %d 条排队或暂存输入。", discarded)
	}
	events = append(events, control.UIEvent{
		Kind:             control.UIEventNotice,
		SurfaceSessionID: surface.SurfaceSessionID,
		Notice:           &notice,
	})
	return events
}

func (s *Service) cancelPendingHeadlessLaunch(surface *state.SurfaceConsoleRecord, notice *control.Notice) []control.UIEvent {
	if surface == nil || surface.PendingHeadless == nil {
		return nil
	}
	pending := surface.PendingHeadless
	events := s.discardDrafts(surface)
	events = append(events, s.finalizeDetachedSurface(surface)...)
	events = append(events, control.UIEvent{
		Kind:             control.UIEventDaemonCommand,
		SurfaceSessionID: surface.SurfaceSessionID,
		DaemonCommand: &control.DaemonCommand{
			Kind:             control.DaemonCommandKillHeadless,
			SurfaceSessionID: surface.SurfaceSessionID,
			InstanceID:       pending.InstanceID,
			ThreadID:         pending.ThreadID,
			ThreadTitle:      pending.ThreadTitle,
			ThreadCWD:        pending.ThreadCWD,
		},
	})
	if notice != nil {
		events = append(events, control.UIEvent{
			Kind:             control.UIEventNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice:           notice,
		})
	}
	return events
}

func (s *Service) killHeadlessInstance(surface *state.SurfaceConsoleRecord) []control.UIEvent {
	if surface == nil {
		return nil
	}
	if surface.PendingHeadless != nil {
		return s.cancelPendingHeadlessLaunch(surface, &control.Notice{
			Code:  "headless_cancelled",
			Title: "取消恢复流程",
			Text:  "已取消当前恢复流程。",
		})
	}
	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil {
		return notice(surface, "headless_not_found", "当前没有可结束的后台恢复流程。")
	}
	if !isHeadlessInstance(inst) {
		return notice(surface, "headless_kill_forbidden", "当前接管的是 VS Code 实例，不需要结束后台恢复。")
	}
	instanceID := inst.InstanceID
	threadID := surface.SelectedThreadID
	threadTitle := displayThreadTitle(inst, inst.Threads[threadID], threadID)
	threadCWD := ""
	if thread := inst.Threads[threadID]; thread != nil {
		threadCWD = thread.CWD
	}
	events := s.discardDrafts(surface)
	surface.PendingHeadless = nil
	events = append(events, s.finalizeDetachedSurface(surface)...)
	events = append(events,
		control.UIEvent{
			Kind:             control.UIEventDaemonCommand,
			SurfaceSessionID: surface.SurfaceSessionID,
			DaemonCommand: &control.DaemonCommand{
				Kind:             control.DaemonCommandKillHeadless,
				SurfaceSessionID: surface.SurfaceSessionID,
				InstanceID:       instanceID,
				ThreadID:         threadID,
				ThreadTitle:      threadTitle,
				ThreadCWD:        threadCWD,
			},
		},
		control.UIEvent{
			Kind:             control.UIEventNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice: &control.Notice{
				Code:  "headless_kill_requested",
				Title: "结束后台恢复",
				Text:  "已请求结束当前后台恢复，并断开当前接管。",
			},
		},
	)
	return events
}

func (s *Service) detach(surface *state.SurfaceConsoleRecord) []control.UIEvent {
	if surface.PendingHeadless != nil {
		return s.cancelPendingHeadlessLaunch(surface, &control.Notice{
			Code:  "detached",
			Title: "已取消恢复流程",
			Text:  fmt.Sprintf("已取消当前恢复流程。%s", s.detachedNoneText(surface)),
		})
	}
	if surface.AttachedInstanceID == "" {
		return notice(surface, "detached", s.detachedNoneText(surface))
	}
	events := s.discardDrafts(surface)
	clearSurfaceRequests(surface)
	surface.PendingHeadless = nil
	surface.PromptOverride = state.ModelConfigRecord{}
	surface.DispatchMode = state.DispatchModeNormal
	delete(s.handoffUntil, surface.SurfaceSessionID)
	delete(s.pausedUntil, surface.SurfaceSessionID)
	inst := s.root.Instances[surface.AttachedInstanceID]
	if s.surfaceNeedsDelayedDetach(surface, inst) {
		surface.Abandoning = true
		s.abandoningUntil[surface.SurfaceSessionID] = s.now().Add(s.config.DetachAbandonWait)
		if binding := s.remoteBindingForSurface(surface); binding != nil && binding.TurnID != "" {
			events = append(events, control.UIEvent{
				Kind:             control.UIEventAgentCommand,
				SurfaceSessionID: surface.SurfaceSessionID,
				Command: &agentproto.Command{
					Kind: agentproto.CommandTurnInterrupt,
					Origin: agentproto.Origin{
						Surface: surface.SurfaceSessionID,
						UserID:  surface.ActorUserID,
						ChatID:  surface.ChatID,
					},
					Target: agentproto.Target{
						ThreadID: binding.ThreadID,
						TurnID:   binding.TurnID,
					},
				},
			})
		}
		return append(events, notice(surface, "detach_pending", s.detachPendingText(surface))...)
	}
	events = append(events, s.finalizeDetachedSurface(surface)...)
	return append(events, notice(surface, "detached", s.detachedText(surface))...)
}
