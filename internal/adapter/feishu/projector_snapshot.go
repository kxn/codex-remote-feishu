package feishu

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const (
	snapshotStatusTitleLimit   = 28
	snapshotStatusPreviewLimit = 24
)

func projectSnapshotElements(snapshot control.Snapshot, daemonBinary, currentDirectory string, worktree *gitWorktreeSummary) []map[string]any {
	return appendCardTextSections(nil, snapshotSections(snapshot, daemonBinary, currentDirectory, worktree))
}

func snapshotSections(snapshot control.Snapshot, daemonBinary, currentDirectory string, worktree *gitWorktreeSummary) []control.FeishuCardTextSection {
	lines := []string{snapshotLine("当前模式", displaySnapshotMode(snapshot.ProductMode))}
	if planMode := snapshotPlanModeText(snapshot.NextPrompt, snapshot.Dispatch); planMode != "" {
		lines = append(lines, snapshotLine("Plan mode", planMode))
	}
	if observedPlanMode := snapshotObservedThreadPlanModeText(snapshot.NextPrompt); observedPlanMode != "" {
		lines = append(lines, snapshotLine("会话最近本地模式", observedPlanMode))
	}
	if daemonBinary = strings.TrimSpace(daemonBinary); daemonBinary != "" {
		lines = append(lines, snapshotLine("当前二进制", daemonBinary))
	}
	if currentDirectory = strings.TrimSpace(currentDirectory); currentDirectory != "" {
		lines = append(lines, snapshotLine("当前目录", currentDirectory))
	}
	if snapshot.Attachment.InstanceID == "" {
		lines = append(lines, snapshotLine("接管对象类型", "无"))
		lines = append(lines, snapshotLine("已接管", "无"))
	} else {
		lines = append(lines, snapshotLine("接管对象类型", displayAttachmentObjectType(snapshot.Attachment.ObjectType)))
		lines = append(lines, snapshotLine("已接管", formatInstanceLabel(snapshot.Attachment.DisplayName, snapshot.Attachment.Source, snapshot.Attachment.Managed)))
		if snapshot.Attachment.Abandoning {
			lines = append(lines, snapshotLine("状态", "正在断开，等待当前 turn 收尾"))
		}
		switch {
		case snapshot.Attachment.SelectedThreadTitle != "":
			lines = append(lines, snapshotLine("当前输入目标", compactSnapshotStatusText(snapshot.Attachment.SelectedThreadTitle, snapshotStatusTitleLimit)))
		case snapshot.Attachment.SelectedThreadID != "":
			lines = append(lines, snapshotLine("当前输入目标", "未命名会话"))
		case snapshot.Attachment.RouteMode == "new_thread_ready":
			lines = append(lines, snapshotLine("当前输入目标", "新建会话（等待首条消息）"))
		case snapshot.Attachment.RouteMode == "follow_local":
			lines = append(lines, snapshotLine("当前输入目标", "跟随当前 VS Code（等待中）"))
		default:
			lines = append(lines, snapshotLine("当前输入目标", "未绑定会话"))
		}
		if first := strings.TrimSpace(snapshot.Attachment.SelectedThreadFirstUserMessage); first != "" {
			lines = append(lines, snapshotLine("会话起点", compactSnapshotStatusText(first, snapshotStatusPreviewLimit)))
		}
		if lastUser := strings.TrimSpace(snapshot.Attachment.SelectedThreadLastUserMessage); lastUser != "" {
			lines = append(lines, snapshotLine("最近用户", compactSnapshotStatusText(lastUser, snapshotStatusPreviewLimit)))
		}
		if lastAssistant := strings.TrimSpace(snapshot.Attachment.SelectedThreadLastAssistantMessage); lastAssistant != "" {
			lines = append(lines, snapshotLine("最近回复", compactSnapshotStatusText(lastAssistant, snapshotStatusPreviewLimit)))
		}
		if age := strings.TrimSpace(snapshot.Attachment.SelectedThreadAgeText); age != "" && age != "时间未知" {
			lines = append(lines, snapshotLine("最近活跃", age))
		}
		if dispatch := snapshotDispatchText(snapshot.Dispatch); dispatch != "" {
			lines = append(lines, snapshotLine("执行状态", dispatch))
		}
		if gate := snapshotGateText(snapshot.Gate); gate != "" {
			lines = append(lines, snapshotLine("输入门禁", gate))
		}
		if snapshot.Attachment.PID > 0 {
			lines = append(lines, snapshotLine("实例 PID", fmt.Sprintf("%d", snapshot.Attachment.PID)))
		}
		lines = append(lines, snapshotLine("下条飞书消息", formatSnapshotEffectivePromptPlain(snapshot.NextPrompt)))
		if snapshotShouldShowPromptCWD(snapshotCurrentDirectory(snapshot), snapshot.NextPrompt.CWD) {
			lines = append(lines, snapshotLine("工作目录", strings.TrimSpace(snapshot.NextPrompt.CWD)))
		}
	}
	lines = append(lines, formatSnapshotGitFieldsPlain(worktree)...)
	if autoWhip := snapshotAutoWhipTextPlain(snapshot.AutoWhip); autoWhip != "" {
		lines = append(lines, snapshotLine("AutoWhip", autoWhip))
	}
	if autoContinue := snapshotAutoContinueTextPlain(snapshot.AutoContinue); autoContinue != "" {
		lines = append(lines, snapshotLine("自动继续", autoContinue))
	}

	sections := []control.FeishuCardTextSection{{Lines: lines}}
	if permissionGaps := formatSnapshotPermissionGapsPlain(snapshot.PermissionGaps); len(permissionGaps) != 0 {
		sections = append(sections, control.FeishuCardTextSection{
			Label: "已知缺权限",
			Lines: permissionGaps,
		})
	}
	if headlessLines := pendingHeadlessSectionLines(snapshot.PendingHeadless); len(headlessLines) != 0 {
		sections = append(sections, control.FeishuCardTextSection{
			Label: "后台恢复中",
			Lines: headlessLines,
		})
	}
	return sections
}

func (p *Projector) projectSnapshotElements(snapshot control.Snapshot) []map[string]any {
	daemonBinary := ""
	if p != nil {
		daemonBinary = p.snapshotBinary
	}
	var worktree *gitWorktreeSummary
	if p != nil && p.readGitWorktree != nil {
		if cwd := snapshotGitProbeCWD(snapshot); cwd != "" {
			worktree = p.readGitWorktree(cwd)
		}
	}
	return projectSnapshotElements(snapshot, daemonBinary, formatSnapshotCurrentDirectoryPlain(snapshotCurrentDirectory(snapshot), gitBranchFromWorktree(worktree)), worktree)
}

func formatSnapshotGitFieldsPlain(worktree *gitWorktreeSummary) []string {
	if worktree == nil {
		return nil
	}
	lines := make([]string, 0, 1)
	if status := formatSnapshotGitWorktreeStatusPlain(worktree); status != "" {
		lines = append(lines, snapshotLine("Git 工作区", status))
	}
	return lines
}

func snapshotGitProbeCWD(snapshot control.Snapshot) string {
	if cwd := strings.TrimSpace(snapshotCurrentDirectory(snapshot)); cwd != "" && filepath.IsAbs(cwd) {
		return cwd
	}
	if cwd := strings.TrimSpace(snapshotCurrentDirectory(snapshot)); strings.HasPrefix(cwd, "/") {
		return cwd
	}
	return ""
}

func snapshotCurrentDirectory(snapshot control.Snapshot) string {
	return firstNonEmpty(
		strings.TrimSpace(snapshot.NextPrompt.CWD),
		strings.TrimSpace(snapshot.PendingHeadless.ThreadCWD),
		strings.TrimSpace(snapshot.WorkspaceKey),
	)
}

func gitBranchFromWorktree(worktree *gitWorktreeSummary) string {
	if worktree == nil {
		return ""
	}
	return strings.TrimSpace(worktree.Branch)
}

func formatSnapshotCurrentDirectoryPlain(path, gitBranch string) string {
	path = strings.TrimSpace(path)
	gitBranch = strings.TrimSpace(gitBranch)
	switch {
	case path != "" && gitBranch != "":
		return path + " · Git " + gitBranch
	case path != "":
		return path
	case gitBranch != "":
		return "Git " + gitBranch
	default:
		return ""
	}
}

func snapshotLine(label, value string) string {
	label = strings.TrimSpace(label)
	value = strings.TrimSpace(value)
	if label == "" {
		return value
	}
	if value == "" {
		return label + "："
	}
	return label + "：" + value
}

func compactSnapshotStatusText(text string, limit int) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if text == "" || limit <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "..."
}

func displaySnapshotMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "vscode", "vs-code", "vs_code":
		return "vscode"
	default:
		return "normal"
	}
}

func displaySnapshotValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return "未知"
	}
	return value
}

func displaySnapshotAccessMode(value string) string {
	if strings.TrimSpace(value) == "" {
		return "未知"
	}
	return agentproto.DisplayAccessModeShort(value)
}

func formatSnapshotEffectivePromptPlain(summary control.PromptRouteSummary) string {
	return strings.Join([]string{
		"Plan " + displaySnapshotPlanMode(summary.EffectivePlanMode),
		"模型 " + displaySnapshotValue(summary.EffectiveModel),
		"推理 " + displaySnapshotValue(summary.EffectiveReasoningEffort),
		"权限 " + displaySnapshotAccessMode(summary.EffectiveAccessMode),
	}, "，")
}

func displaySnapshotPlanMode(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "on") {
		return "开启"
	}
	return "关闭"
}

func snapshotPlanModeText(summary control.PromptRouteSummary, dispatch control.DispatchSummary) string {
	mode := displaySnapshotPlanMode(summary.EffectivePlanMode)
	if strings.EqualFold(strings.TrimSpace(summary.EffectivePlanMode), "on") &&
		(strings.TrimSpace(dispatch.ActiveItemStatus) != "" || dispatch.QueuedCount > 0) {
		return mode + "（当前执行中的这一轮不受影响）"
	}
	return mode
}

func snapshotObservedThreadPlanModeText(summary control.PromptRouteSummary) string {
	if strings.TrimSpace(summary.ObservedThreadPlanMode) == "" {
		return ""
	}
	return displaySnapshotPlanMode(summary.ObservedThreadPlanMode)
}

func snapshotShouldShowPromptCWD(workspaceKey, cwd string) bool {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return false
	}
	workspaceKey = strings.TrimSpace(workspaceKey)
	if workspaceKey == "" {
		return true
	}
	return state.NormalizeWorkspaceKey(workspaceKey) != state.NormalizeWorkspaceKey(cwd)
}

func snapshotGateText(summary control.GateSummary) string {
	switch summary.Kind {
	case "request_capture":
		return "正在等待一条文字处理意见；下一条文本不会发到当前会话"
	case "pending_request":
		if summary.PendingRequestCount > 1 {
			return fmt.Sprintf("有 %d 个待处理请求；普通文本和图片会先被拦住", summary.PendingRequestCount)
		}
		return "有 1 个待处理请求；普通文本和图片会先被拦住"
	default:
		return ""
	}
}

func snapshotAutoWhipTextPlain(summary control.AutoWhipSummary) string {
	stateText := "关闭"
	if summary.Enabled {
		stateText = "开启"
	}
	parts := []string{stateText}
	if summary.ConsecutiveCount > 0 {
		parts = append(parts, fmt.Sprintf("连续 %d 次", summary.ConsecutiveCount))
	}
	if summary.PendingReason != "" {
		label := summary.PendingReason
		switch summary.PendingReason {
		case "incomplete_stop":
			label = "等待继续补打一轮"
		}
		parts = append(parts, label)
	}
	if !summary.PendingDueAt.IsZero() {
		parts = append(parts, "计划于 "+summary.PendingDueAt.Format("2006-01-02 15:04:05 MST"))
	}
	return strings.Join(parts, "，")
}

func snapshotAutoContinueTextPlain(summary control.AutoContinueSummary) string {
	if !summary.Enabled {
		return "关闭"
	}
	parts := []string{"开启"}
	switch summary.State {
	case "":
		parts = append(parts, "空闲")
	case "scheduled":
		if summary.AttemptCount > 0 {
			parts = append(parts, fmt.Sprintf("等待第 %d 次自动继续", summary.AttemptCount))
		} else {
			parts = append(parts, "等待自动继续")
		}
	case "running":
		if summary.AttemptCount > 0 {
			parts = append(parts, fmt.Sprintf("第 %d 次自动继续进行中", summary.AttemptCount))
		} else {
			parts = append(parts, "自动继续进行中")
		}
	case "failed":
		parts = append(parts, "本轮已停止")
	case "cancelled":
		parts = append(parts, "当前已停止")
	case "completed":
		parts = append(parts, "本轮已完成")
	default:
		parts = append(parts, summary.State)
	}
	if summary.ConsecutiveDryFailureCount > 0 {
		parts = append(parts, fmt.Sprintf("连续空失败 %d 次", summary.ConsecutiveDryFailureCount))
	}
	if !summary.PendingDueAt.IsZero() {
		parts = append(parts, "计划于 "+summary.PendingDueAt.Format("2006-01-02 15:04:05 MST"))
	}
	return strings.Join(parts, "，")
}

func snapshotDispatchText(summary control.DispatchSummary) string {
	if !summary.InstanceOnline && summary.DispatchMode == "" && summary.ActiveItemStatus == "" && summary.QueuedCount == 0 {
		return ""
	}
	if !summary.InstanceOnline {
		if summary.QueuedCount > 0 {
			return fmt.Sprintf("实例离线，已保留接管关系；%d 条排队消息会在恢复后继续", summary.QueuedCount)
		}
		return "实例离线，已保留接管关系，等待恢复"
	}
	switch summary.DispatchMode {
	case "paused_for_local":
		if summary.QueuedCount > 0 {
			return fmt.Sprintf("本地 VS Code 占用中；%d 条飞书消息继续排队", summary.QueuedCount)
		}
		return "本地 VS Code 占用中；新的飞书消息会先排队"
	case "handoff_wait":
		if summary.QueuedCount > 0 {
			return fmt.Sprintf("等待本地 turn handoff；%d 条排队消息稍后继续派发", summary.QueuedCount)
		}
		return "等待本地 turn handoff；稍后自动恢复远端派发"
	}
	switch summary.ActiveItemStatus {
	case "running":
		if summary.QueuedCount > 0 {
			return fmt.Sprintf("当前 1 条执行中，另有 %d 条排队", summary.QueuedCount)
		}
		return "当前 1 条执行中"
	case "dispatching":
		if summary.QueuedCount > 0 {
			return fmt.Sprintf("当前 1 条派发中，另有 %d 条排队", summary.QueuedCount)
		}
		return "当前 1 条派发中"
	}
	if summary.QueuedCount > 0 {
		return fmt.Sprintf("当前 %d 条排队", summary.QueuedCount)
	}
	return "空闲"
}

func displayAttachmentObjectType(value string) string {
	switch strings.TrimSpace(value) {
	case "workspace":
		return "工作区"
	case "vscode_instance":
		return "VS Code 实例"
	case "headless_instance":
		return "headless 实例"
	case "instance":
		return "实例"
	default:
		return "未知"
	}
}

func formatInstanceLabel(displayName, source string, managed bool) string {
	label := strings.TrimSpace(displayName)
	if label == "" {
		label = "未知实例"
	}
	if strings.EqualFold(strings.TrimSpace(source), "headless") {
		_ = managed
		return label
	}
	return label
}

func formatSnapshotPermissionGapsPlain(gaps []control.PermissionGapSummary) []string {
	if len(gaps) == 0 {
		return nil
	}
	lines := make([]string, 0, len(gaps)*2)
	for _, gap := range gaps {
		scope := strings.TrimSpace(gap.Scope)
		if scope == "" {
			continue
		}
		line := scope
		if source := strings.TrimSpace(gap.SourceAPI); source != "" {
			line += " · 来源 " + source
		}
		if !gap.LastSeenAt.IsZero() {
			line += " · 最近命中 " + gap.LastSeenAt.Format("2006-01-02 15:04:05 MST")
		}
		lines = append(lines, line)
		if url := strings.TrimSpace(gap.ApplyURL); url != "" {
			lines = append(lines, "申请链接："+url)
		}
	}
	return lines
}

func formatSnapshotGitWorktreeStatusPlain(summary *gitWorktreeSummary) string {
	if summary == nil {
		return ""
	}
	if !summary.Dirty {
		return "干净"
	}
	parts := []string{"有改动"}
	if summary.ModifiedCount > 0 {
		parts = append(parts, fmt.Sprintf("%d修改", summary.ModifiedCount))
	}
	if summary.UntrackedCount > 0 {
		parts = append(parts, fmt.Sprintf("%d未跟踪", summary.UntrackedCount))
	}
	return strings.Join(parts, " ")
}

func pendingHeadlessSectionLines(summary control.PendingHeadlessSummary) []string {
	lines := []string{}
	if title := strings.TrimSpace(summary.ThreadTitle); title != "" {
		lines = append(lines, snapshotLine("目标会话", compactSnapshotStatusText(title, snapshotStatusTitleLimit)))
	}
	if cwd := strings.TrimSpace(summary.ThreadCWD); cwd != "" {
		lines = append(lines, snapshotLine("启动目录", cwd))
	}
	if summary.PID > 0 {
		lines = append(lines, snapshotLine("进程 PID", fmt.Sprintf("%d", summary.PID)))
	}
	if !summary.ExpiresAt.IsZero() {
		lines = append(lines, snapshotLine("启动超时", summary.ExpiresAt.Format("2006-01-02 15:04:05 MST")))
	}
	return lines
}

func noticeThemeKey(notice control.Notice) string {
	key := strings.ToLower(strings.TrimSpace(notice.ThemeKey))
	switch {
	case key == cardThemeError || strings.Contains(key, "error") || strings.Contains(key, "fail"):
		return cardThemeError
	case key == cardThemeSuccess || key == "normal" || key == "ok":
		return cardThemeSuccess
	case key == cardThemeApproval || strings.Contains(key, "approval"):
		return cardThemeApproval
	case key == cardThemeFinal:
		return cardThemeFinal
	}

	title := strings.TrimSpace(notice.Title)
	code := strings.ToLower(strings.TrimSpace(notice.Code))
	text := strings.TrimSpace(notice.Text)
	if containsAny(title, "错误", "失败", "无法", "拒绝", "离线", "过期", "失效") ||
		containsAny(code, "error", "failed", "rejected", "offline", "expired", "invalid") ||
		containsAny(text, "链路错误", "创建失败", "连接失败") {
		return cardThemeError
	}
	if strings.HasPrefix(title, "已") ||
		containsAny(title, "成功", "就绪", "完成") ||
		containsAny(code, "attached", "detached", "follow", "cleared", "requested") ||
		strings.HasPrefix(text, "已") {
		return cardThemeSuccess
	}
	return cardThemeInfo
}

func containsAny(value string, parts ...string) bool {
	for _, part := range parts {
		if strings.Contains(value, part) {
			return true
		}
	}
	return false
}
