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

func formatSnapshot(snapshot control.Snapshot, daemonBinary, currentDirectory string, worktree *gitWorktreeSummary) string {
	lines := []string{}
	lines = append(lines, snapshotField("当前模式", formatNeutralTextTag(displaySnapshotMode(snapshot.ProductMode))))
	if daemonBinary = strings.TrimSpace(daemonBinary); daemonBinary != "" {
		lines = append(lines, snapshotField("当前二进制", formatNeutralTextTag(daemonBinary)))
	}
	if currentDirectory = strings.TrimSpace(currentDirectory); currentDirectory != "" {
		lines = append(lines, snapshotField("当前目录", currentDirectory))
	}
	if snapshot.Attachment.InstanceID == "" {
		lines = append(lines, snapshotField("接管对象类型", "无"))
		lines = append(lines, snapshotField("已接管", "无"))
	} else {
		lines = append(lines, snapshotField("接管对象类型", formatNeutralTextTag(displayAttachmentObjectType(snapshot.Attachment.ObjectType))))
		lines = append(lines, snapshotField("已接管", formatInstanceLabel(snapshot.Attachment.DisplayName, snapshot.Attachment.Source, snapshot.Attachment.Managed)))
		if snapshot.Attachment.Abandoning {
			lines = append(lines, snapshotField("状态", "正在断开，等待当前 turn 收尾"))
		}
		switch {
		case snapshot.Attachment.SelectedThreadTitle != "":
			lines = append(lines, snapshotField("当前输入目标", compactSnapshotStatusText(snapshot.Attachment.SelectedThreadTitle, snapshotStatusTitleLimit)))
			if short := control.ShortenThreadID(snapshot.Attachment.SelectedThreadID); short != "" {
				lines = append(lines, snapshotField("会话 ID", short))
			}
		case snapshot.Attachment.SelectedThreadID != "":
			lines = append(lines, snapshotField("当前输入目标", snapshot.Attachment.SelectedThreadID))
		case snapshot.Attachment.RouteMode == "new_thread_ready":
			lines = append(lines, snapshotField("当前输入目标", "新建会话（等待首条消息）"))
		case snapshot.Attachment.RouteMode == "follow_local":
			lines = append(lines, snapshotField("当前输入目标", "跟随当前 VS Code（等待中）"))
		default:
			lines = append(lines, snapshotField("当前输入目标", "未绑定会话"))
		}
		if preview := strings.TrimSpace(snapshot.Attachment.SelectedThreadPreview); preview != "" {
			lines = append(lines, snapshotField("最近信息", compactSnapshotStatusText(preview, snapshotStatusPreviewLimit)))
		}
		if dispatch := snapshotDispatchText(snapshot.Dispatch); dispatch != "" {
			lines = append(lines, snapshotField("执行状态", dispatch))
		}
		if gate := snapshotGateText(snapshot.Gate); gate != "" {
			lines = append(lines, snapshotField("输入门禁", gate))
		}
		if snapshot.Attachment.PID > 0 {
			lines = append(lines, snapshotField("实例 PID", formatNeutralTextTag(fmt.Sprintf("%d", snapshot.Attachment.PID))))
		}
		lines = append(lines, "")
		lines = append(lines, snapshotField("下条飞书消息", formatSnapshotEffectivePrompt(snapshot.NextPrompt)))
		if snapshotShouldShowPromptCWD(snapshotCurrentDirectory(snapshot), snapshot.NextPrompt.CWD) {
			lines = append(lines, snapshotField("工作目录", formatNeutralTextTag(snapshot.NextPrompt.CWD)))
		}
	}
	lines = append(lines, formatSnapshotGitFields(worktree)...)
	if autoContinue := snapshotAutoContinueText(snapshot.AutoContinue); autoContinue != "" {
		lines = append(lines, snapshotField("autowhip", autoContinue))
	}
	if snapshot.PendingHeadless.InstanceID != "" {
		lines = append(lines, "")
		lines = append(lines, "**后台恢复中：**")
		if snapshot.PendingHeadless.ThreadTitle != "" {
			lines = append(lines, fmt.Sprintf("- %s", snapshotField("目标会话", snapshot.PendingHeadless.ThreadTitle)))
		}
		if snapshot.PendingHeadless.ThreadCWD != "" {
			lines = append(lines, fmt.Sprintf("- %s", snapshotField("启动目录", formatNeutralTextTag(snapshot.PendingHeadless.ThreadCWD))))
		}
		if snapshot.PendingHeadless.PID > 0 {
			lines = append(lines, fmt.Sprintf("- %s", snapshotField("进程 PID", formatNeutralTextTag(fmt.Sprintf("%d", snapshot.PendingHeadless.PID)))))
		}
		if !snapshot.PendingHeadless.ExpiresAt.IsZero() {
			lines = append(lines, fmt.Sprintf("- %s", snapshotField("启动超时", formatNeutralTextTag(snapshot.PendingHeadless.ExpiresAt.Format("2006-01-02 15:04:05 MST")))))
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func (p *Projector) formatSnapshot(snapshot control.Snapshot) string {
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
	return formatSnapshot(snapshot, daemonBinary, formatSnapshotCurrentDirectory(snapshotCurrentDirectory(snapshot), gitBranchFromWorktree(worktree)), worktree)
}

func formatSnapshotGitFields(worktree *gitWorktreeSummary) []string {
	if worktree == nil {
		return nil
	}
	lines := make([]string, 0, 1)
	if status := formatSnapshotGitWorktreeStatus(worktree); status != "" {
		lines = append(lines, snapshotField("Git 工作区", status))
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

func formatSnapshotGitWorktreeStatus(summary *gitWorktreeSummary) string {
	if summary == nil {
		return ""
	}
	if !summary.Dirty {
		return formatNeutralTextTag("干净")
	}
	parts := []string{formatNeutralTextTag("有改动")}
	if summary.ModifiedCount > 0 {
		parts = append(parts, formatNeutralTextTag(fmt.Sprintf("%d修改", summary.ModifiedCount)))
	}
	if summary.UntrackedCount > 0 {
		parts = append(parts, formatNeutralTextTag(fmt.Sprintf("%d未跟踪", summary.UntrackedCount)))
	}
	return strings.Join(parts, " ")
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

func formatSnapshotCurrentDirectory(path, gitBranch string) string {
	path = strings.TrimSpace(path)
	gitBranch = strings.TrimSpace(gitBranch)
	switch {
	case path != "" && gitBranch != "":
		return formatNeutralTextTag(path) + " · Git " + formatNeutralTextTag(gitBranch)
	case path != "":
		return formatNeutralTextTag(path)
	case gitBranch != "":
		return "Git " + formatNeutralTextTag(gitBranch)
	default:
		return ""
	}
}

func snapshotField(label, value string) string {
	return fmt.Sprintf("**%s：** %s", label, value)
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

func formatSnapshotEffectivePrompt(summary control.PromptRouteSummary) string {
	return strings.Join([]string{
		"模型 " + formatNeutralTextTag(displaySnapshotValue(summary.EffectiveModel)),
		"推理 " + formatNeutralTextTag(displaySnapshotValue(summary.EffectiveReasoningEffort)),
		"权限 " + formatNeutralTextTag(displaySnapshotAccessMode(summary.EffectiveAccessMode)),
	}, "，")
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

func snapshotAutoContinueText(summary control.AutoContinueSummary) string {
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
		case "retryable_failure":
			label = "等待重试上游不稳定"
		}
		parts = append(parts, label)
	}
	if !summary.PendingDueAt.IsZero() {
		parts = append(parts, "计划于 "+formatNeutralTextTag(summary.PendingDueAt.Format("2006-01-02 15:04:05 MST")))
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
