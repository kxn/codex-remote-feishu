package orchestrator

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func threadSelectionEvent(surface *state.SurfaceConsoleRecord, threadID, routeMode, title, preview string) control.UIEvent {
	return control.UIEvent{
		Kind:             control.UIEventThreadSelectionChange,
		GatewayID:        surface.GatewayID,
		SurfaceSessionID: surface.SurfaceSessionID,
		ThreadSelection: &control.ThreadSelectionChanged{
			ThreadID:  threadID,
			RouteMode: routeMode,
			Title:     title,
			Preview:   preview,
		},
	}
}

func (s *Service) touchThread(thread *state.ThreadRecord) {
	if thread == nil {
		return
	}
	thread.LastUsedAt = s.now()
}

func (s *Service) pendingInputEvents(surface *state.SurfaceConsoleRecord, pending control.PendingInputState, sourceMessageIDs []string) []control.UIEvent {
	if surface == nil {
		return nil
	}
	messageIDs := uniqueStrings(sourceMessageIDs)
	if len(messageIDs) == 0 && pending.SourceMessageID != "" {
		messageIDs = []string{pending.SourceMessageID}
	}
	if len(messageIDs) == 0 {
		return nil
	}
	events := make([]control.UIEvent, 0, len(messageIDs))
	for _, messageID := range messageIDs {
		pendingCopy := pending
		pendingCopy.SourceMessageID = messageID
		events = append(events, control.UIEvent{
			Kind:             control.UIEventPendingInput,
			GatewayID:        surface.GatewayID,
			SurfaceSessionID: surface.SurfaceSessionID,
			PendingInput:     &pendingCopy,
		})
	}
	return events
}

func appendPendingInputTyping(events []control.UIEvent, primaryMessageID string, typingOn bool) []control.UIEvent {
	if primaryMessageID == "" {
		return events
	}
	for i := range events {
		pending := events[i].PendingInput
		if pending == nil || pending.SourceMessageID != primaryMessageID {
			continue
		}
		pending.TypingOn = typingOn
		pending.TypingOff = !typingOn
		return events
	}
	return events
}

func queueItemSourceMessageIDs(item *state.QueueItemRecord) []string {
	if item == nil {
		return nil
	}
	return uniqueStrings(append([]string{item.SourceMessageID}, item.SourceMessageIDs...))
}

func queueItemHasSourceMessage(item *state.QueueItemRecord, messageID string) bool {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" || item == nil {
		return false
	}
	for _, candidate := range queueItemSourceMessageIDs(item) {
		if candidate == messageID {
			return true
		}
	}
	return false
}

func (s *Service) markImagesForMessages(surface *state.SurfaceConsoleRecord, sourceMessageIDs []string, next state.ImageState) {
	if surface == nil || len(surface.StagedImages) == 0 {
		return
	}
	targets := map[string]struct{}{}
	for _, messageID := range uniqueStrings(sourceMessageIDs) {
		if messageID == "" {
			continue
		}
		targets[messageID] = struct{}{}
	}
	if len(targets) == 0 {
		return
	}
	for _, image := range surface.StagedImages {
		if image == nil {
			continue
		}
		if _, ok := targets[image.SourceMessageID]; ok {
			image.State = next
		}
	}
}

func countPendingDrafts(surface *state.SurfaceConsoleRecord) int {
	if surface == nil {
		return 0
	}
	total := 0
	for _, image := range surface.StagedImages {
		if image != nil && image.State == state.ImageStaged {
			total++
		}
	}
	for _, queueID := range surface.QueuedQueueItemIDs {
		if item := surface.QueueItems[queueID]; item != nil && item.Status == state.QueueItemQueued {
			total++
		}
	}
	return total
}

func removeString(values []string, target string) []string {
	out := values[:0]
	for _, value := range values {
		if value != target {
			out = append(out, value)
		}
	}
	return out
}

func insertString(values []string, index int, value string) []string {
	if strings.TrimSpace(value) == "" {
		return values
	}
	if index < 0 {
		index = 0
	}
	if index > len(values) {
		index = len(values)
	}
	values = append(values, "")
	copy(values[index+1:], values[index:])
	values[index] = value
	return values
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func isDigits(value string) bool {
	_, err := strconv.Atoi(value)
	return err == nil
}

func threadTitle(inst *state.InstanceRecord, thread *state.ThreadRecord, fallback string) string {
	short := threadWorkspaceLabel(inst, thread)
	if thread == nil {
		if fallback == "" {
			return short
		}
		if short == "" {
			return control.ShortenThreadID(fallback)
		}
		return fmt.Sprintf("%s · %s", short, control.ShortenThreadID(fallback))
	}
	if body := threadDisplayBody(thread, 40); body != "" {
		if short == "" {
			return body
		}
		return fmt.Sprintf("%s · %s", short, body)
	}
	if thread.CWD != "" {
		base := filepath.Base(thread.CWD)
		switch {
		case base == "", base == ".", base == string(filepath.Separator), base == short:
			if short == "" {
				return control.ShortenThreadID(fallback)
			}
			return fmt.Sprintf("%s · %s", short, control.ShortenThreadID(fallback))
		default:
			return fmt.Sprintf("%s · %s · %s", short, base, control.ShortenThreadID(fallback))
		}
	}
	if fallback == "" {
		return short
	}
	if short == "" {
		return control.ShortenThreadID(fallback)
	}
	return fmt.Sprintf("%s · %s", short, control.ShortenThreadID(fallback))
}

func threadWorkspaceLabel(inst *state.InstanceRecord, thread *state.ThreadRecord) string {
	if thread != nil {
		if short := state.WorkspaceShortName(thread.CWD); short != "" {
			return short
		}
	}
	if inst != nil {
		if short := state.WorkspaceShortName(inst.WorkspaceKey); short != "" {
			return short
		}
		if short := state.WorkspaceShortName(inst.WorkspaceRoot); short != "" {
			return short
		}
		if short := strings.TrimSpace(inst.ShortName); short != "" {
			return short
		}
		if short := strings.TrimSpace(inst.DisplayName); short != "" {
			return short
		}
	}
	return ""
}

func threadDisplayBody(thread *state.ThreadRecord, limit int) string {
	if thread == nil {
		return ""
	}
	if name := threadDisplayName(thread); name != "" {
		return truncateThreadDisplayText(name, limit)
	}
	if preview := previewOfText(thread.Preview); preview != "" {
		return truncateThreadDisplayText(preview, limit)
	}
	return ""
}

func threadDisplayName(thread *state.ThreadRecord) string {
	if thread == nil {
		return ""
	}
	name := strings.Join(strings.Fields(strings.TrimSpace(thread.Name)), " ")
	switch strings.ToLower(name) {
	case "", "新会话", "新聊天", "new chat", "new thread":
		return ""
	default:
		return name
	}
}

func truncateThreadDisplayText(text string, limit int) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if text == "" || limit <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "..."
}

func displayThreadTitle(inst *state.InstanceRecord, thread *state.ThreadRecord, fallback string) string {
	title := threadTitle(inst, thread, fallback)
	if inst == nil || fallback == "" {
		return title
	}
	shortID := control.ShortenThreadID(fallback)
	if strings.Contains(title, shortID) {
		return title
	}
	if duplicateThreadTitle(inst, title) {
		return fmt.Sprintf("%s · %s", title, shortID)
	}
	return title
}

func duplicateThreadTitle(inst *state.InstanceRecord, title string) bool {
	if inst == nil || title == "" {
		return false
	}
	count := 0
	for threadID, thread := range inst.Threads {
		if !threadVisible(thread) {
			continue
		}
		if threadTitle(inst, thread, threadID) != title {
			continue
		}
		count++
		if count > 1 {
			return true
		}
	}
	return false
}

func threadPreview(thread *state.ThreadRecord) string {
	if thread == nil {
		return ""
	}
	return previewSnippet(thread.Preview)
}

func threadSelectionButtonLabel(thread *state.ThreadRecord, fallback string) string {
	source := threadDisplayBody(thread, 20)
	if source == "" {
		source = control.ShortenThreadID(fallback)
	}
	if source == "" {
		source = "未命名会话"
	}
	workspace := threadWorkspaceLabel(nil, thread)
	if workspace == "" {
		return source
	}
	return workspace + " · " + source
}

func (s *Service) maybeRequestThreadRefresh(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, threadID string) []control.UIEvent {
	if surface == nil || inst == nil || surface.AttachedInstanceID != inst.InstanceID {
		return nil
	}
	if s.threadRefreshes[inst.InstanceID] || !threadNeedsRefresh(inst.Threads[threadID]) {
		return nil
	}
	s.threadRefreshes[inst.InstanceID] = true
	return []control.UIEvent{{
		Kind:             control.UIEventAgentCommand,
		SurfaceSessionID: surface.SurfaceSessionID,
		Command: &agentproto.Command{
			Kind: agentproto.CommandThreadsRefresh,
			Origin: agentproto.Origin{
				Surface: surface.SurfaceSessionID,
				UserID:  surface.ActorUserID,
				ChatID:  surface.ChatID,
			},
		},
	}}
}

func threadNeedsRefresh(thread *state.ThreadRecord) bool {
	if thread == nil || !threadVisible(thread) {
		return false
	}
	return !thread.Loaded || (strings.TrimSpace(thread.Name) == "" && strings.TrimSpace(thread.Preview) == "")
}

func threadSelectionSubtitle(thread *state.ThreadRecord, threadID string) string {
	if thread != nil && thread.CWD != "" {
		return thread.CWD
	}
	if short := control.ShortenThreadID(threadID); short != "" {
		return "会话 ID " + short
	}
	return ""
}

func headlessPendingNoticeCode(pending *state.HeadlessLaunchRecord) string {
	_ = pending
	return "headless_starting"
}

func headlessPendingNoticeText(pending *state.HeadlessLaunchRecord) string {
	_ = pending
	return "恢复流程仍在进行中，请等待完成，或执行 /detach 取消。"
}

func isInternalHelperEvent(event agentproto.Event) bool {
	return event.TrafficClass == agentproto.TrafficClassInternalHelper || event.Initiator.Kind == agentproto.InitiatorInternalHelper
}

func threadVisible(thread *state.ThreadRecord) bool {
	return thread != nil && !thread.Archived && thread.TrafficClass != agentproto.TrafficClassInternalHelper
}

func visibleThreads(inst *state.InstanceRecord) []*state.ThreadRecord {
	if inst == nil {
		return nil
	}
	threads := make([]*state.ThreadRecord, 0, len(inst.Threads))
	for _, thread := range inst.Threads {
		if threadVisible(thread) {
			threads = append(threads, thread)
		}
	}
	sortVisibleThreads(threads)
	return threads
}

func sortVisibleThreads(threads []*state.ThreadRecord) {
	sort.SliceStable(threads, func(i, j int) bool {
		left := threads[i]
		right := threads[j]
		switch {
		case left == nil:
			return false
		case right == nil:
			return true
		case !left.LastUsedAt.Equal(right.LastUsedAt):
			return left.LastUsedAt.After(right.LastUsedAt)
		case left.ListOrder == 0 && right.ListOrder != 0:
			return false
		case left.ListOrder != 0 && right.ListOrder == 0:
			return true
		case left.ListOrder != right.ListOrder:
			return left.ListOrder < right.ListOrder
		default:
			return left.ThreadID < right.ThreadID
		}
	})
}

func previewSnippet(text string) string {
	text = previewOfText(text)
	if text == "" {
		return ""
	}
	runes := []rune(text)
	if len(runes) > 40 {
		return string(runes[:40]) + "..."
	}
	return text
}

func isClearCommand(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "clear", "reset":
		return true
	default:
		return false
	}
}

func looksLikeReasoningEffort(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "low", "medium", "high", "xhigh":
		return true
	default:
		return false
	}
}

func formatOverrideNotice(summary control.PromptRouteSummary, prefix string) string {
	lines := []string{prefix}
	lines = append(lines, fmt.Sprintf("当前生效模型：%s", displayConfigValue(summary.EffectiveModel, summary.EffectiveModelSource)))
	lines = append(lines, fmt.Sprintf("当前推理强度：%s", displayConfigValue(summary.EffectiveReasoningEffort, summary.EffectiveReasoningEffortSource)))
	lines = append(lines, fmt.Sprintf("当前执行权限：%s", agentproto.DisplayAccessModeShort(summary.EffectiveAccessMode)))
	if summary.ThreadTitle != "" {
		lines = append(lines, fmt.Sprintf("当前输入目标：%s", summary.ThreadTitle))
	} else if summary.CreateThread {
		lines = append(lines, "当前输入目标：新建会话")
	} else if summary.RouteMode == string(state.RouteModeFollowLocal) {
		lines = append(lines, "当前输入目标：跟随当前 VS Code（等待中）")
	} else {
		lines = append(lines, "当前输入目标：未就绪，请先 /use 选择会话；normal 模式可直接发送文本开启新会话（也可 /new 先进入待命），如需跟随 VS Code 请先 /mode vscode 再 /follow")
	}
	lines = append(lines, "说明：覆盖会持续作用于之后从飞书发出的消息，直到 clear、/detach、/mode 切换或接管清理；不会同步 VS Code。")
	return strings.Join(lines, "\n")
}

func displayConfigValue(value, source string) string {
	if strings.TrimSpace(value) == "" {
		return "未知"
	}
	return value
}

func configSourceLabel(value string) string {
	switch value {
	case "thread":
		return "会话配置"
	case "workspace_default":
		return "工作区默认配置"
	case "cwd_default":
		return "工作目录默认配置"
	case "surface_override":
		return "飞书临时覆盖"
	case "surface_default":
		return "飞书默认"
	default:
		return "未知"
	}
}

func previewOfText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "```") {
			continue
		}
		return line
	}
	return text
}

func normalizeSourceMessagePreview(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	return strings.Join(strings.Fields(text), " ")
}
