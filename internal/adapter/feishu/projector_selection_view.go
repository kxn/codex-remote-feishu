package feishu

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

// SelectionPromptFromView projects the UI-owned selection view into the prompt
// shape currently consumed by the Feishu card renderer.
func SelectionPromptFromView(view control.FeishuSelectionView, ctx *control.FeishuUISelectionContext) (control.SelectionPrompt, bool) {
	switch {
	case view.Workspace != nil && view.PromptKind == control.SelectionPromptAttachWorkspace:
		return workspaceSelectionPromptFromView(*view.Workspace, ctx), true
	case view.Thread != nil && view.PromptKind == control.SelectionPromptUseThread:
		return threadSelectionPromptFromView(*view.Thread), true
	default:
		return control.SelectionPrompt{}, false
	}
}

func workspaceSelectionPromptFromView(view control.FeishuWorkspaceSelectionView, ctx *control.FeishuUISelectionContext) control.SelectionPrompt {
	limit := len(view.Entries)
	if !view.Expanded && view.RecentLimit > 0 && limit > view.RecentLimit {
		limit = view.RecentLimit
	}
	entries := view.Entries[:limit]
	available := make([]control.SelectionOption, 0, len(entries))
	unavailable := make([]control.SelectionOption, 0, len(entries))
	for _, entry := range entries {
		disabled := entry.Busy || (!entry.Attachable && !entry.RecoverableOnly)
		buttonLabel := ""
		actionKind := ""
		switch {
		case entry.Busy:
			disabled = true
		case entry.Attachable:
			if ctx != nil && strings.TrimSpace(ctx.Surface.AttachedInstanceID) != "" {
				buttonLabel = "切换"
			}
		case entry.RecoverableOnly:
			buttonLabel = "恢复"
			actionKind = "show_workspace_threads"
		default:
			disabled = true
		}
		option := control.SelectionOption{
			OptionID:    entry.WorkspaceKey,
			Label:       firstNonEmpty(strings.TrimSpace(entry.WorkspaceLabel), strings.TrimSpace(entry.WorkspaceKey)),
			ButtonLabel: buttonLabel,
			AgeText:     strings.TrimSpace(entry.AgeText),
			MetaText:    workspaceSelectionMetaText(strings.TrimSpace(entry.AgeText), entry.HasVSCodeActivity, entry.Busy, !entry.Attachable && !entry.RecoverableOnly, entry.RecoverableOnly),
			ActionKind:  actionKind,
			Disabled:    disabled,
		}
		if disabled {
			unavailable = append(unavailable, option)
			continue
		}
		available = append(available, option)
	}

	options := make([]control.SelectionOption, 0, len(available)+len(unavailable)+1)
	appendIndexed := func(entries []control.SelectionOption) {
		for _, option := range entries {
			option.Index = len(options) + 1
			options = append(options, option)
		}
	}
	appendIndexed(available)
	appendIndexed(unavailable)

	hiddenCount := len(view.Entries) - limit
	if !view.Expanded && hiddenCount > 0 {
		options = append(options, control.SelectionOption{
			Index:       len(options) + 1,
			Label:       "全部工作区",
			ButtonLabel: "全部工作区",
			ActionKind:  "show_all_workspaces",
			MetaText:    fmt.Sprintf("还有 %d 个工作区未显示", hiddenCount),
		})
	} else if view.Expanded && len(view.Entries) > view.RecentLimit {
		options = append(options, control.SelectionOption{
			Index:       len(options) + 1,
			Label:       "最近工作区",
			ButtonLabel: "最近工作区",
			ActionKind:  "show_recent_workspaces",
			MetaText:    fmt.Sprintf("回到最近 %d 个工作区", view.RecentLimit),
		})
	}

	hint := ""
	if view.Current != nil && len(options) == 0 {
		hint = "当前没有其他可接管工作区。"
	}

	prompt := control.SelectionPrompt{
		Kind:    control.SelectionPromptAttachWorkspace,
		Layout:  "grouped_attach_workspace",
		Title:   "工作区列表",
		Hint:    hint,
		Options: options,
	}
	if view.Expanded {
		prompt.Title = "全部工作区"
	}
	if view.Current != nil {
		prompt.ContextTitle = "当前工作区"
		prompt.ContextText = workspaceSelectionContextText(
			firstNonEmpty(strings.TrimSpace(view.Current.WorkspaceLabel), strings.TrimSpace(view.Current.WorkspaceKey)),
			strings.TrimSpace(view.Current.AgeText),
		)
		prompt.ContextKey = strings.TrimSpace(view.Current.WorkspaceKey)
	}
	return prompt
}

func threadSelectionPromptFromView(view control.FeishuThreadSelectionView) control.SelectionPrompt {
	switch view.Mode {
	case control.FeishuThreadSelectionNormalWorkspaceView:
		return threadWorkspacePromptFromView(view)
	case control.FeishuThreadSelectionNormalGlobalRecent:
		return threadGlobalPromptFromView(view, false)
	case control.FeishuThreadSelectionNormalGlobalAll:
		return threadGlobalPromptFromView(view, true)
	case control.FeishuThreadSelectionNormalScopedAll:
		return threadScopedPromptFromView(view, true)
	case control.FeishuThreadSelectionNormalScopedRecent:
		return threadScopedPromptFromView(view, false)
	case control.FeishuThreadSelectionVSCodeAll, control.FeishuThreadSelectionVSCodeScopedAll:
		return threadVSCodePromptFromView(view, true)
	default:
		return threadVSCodePromptFromView(view, false)
	}
}

func threadWorkspacePromptFromView(view control.FeishuThreadSelectionView) control.SelectionPrompt {
	options := make([]control.SelectionOption, 0, len(view.Entries)+1)
	for _, entry := range view.Entries {
		options = append(options, threadSelectionOption(entry, false))
	}
	options = append(options, control.SelectionOption{
		Index:       len(options) + 1,
		ButtonLabel: "全部会话",
		Subtitle:    "回到跨工作区会话列表",
		ActionKind:  "show_all_threads",
	})
	title := "全部会话"
	if view.Workspace != nil {
		title = firstNonEmpty(strings.TrimSpace(view.Workspace.WorkspaceLabel), strings.TrimSpace(view.Workspace.WorkspaceKey), "工作区") + " 全部会话"
	}
	return control.SelectionPrompt{
		Kind:    control.SelectionPromptUseThread,
		Layout:  "workspace_grouped_useall",
		Title:   title,
		Options: options,
	}
}

func threadGlobalPromptFromView(view control.FeishuThreadSelectionView, expanded bool) control.SelectionPrompt {
	entries := append([]control.FeishuThreadSelectionEntry(nil), view.Entries...)
	excludeWorkspaceKey := ""
	if view.CurrentWorkspace != nil {
		excludeWorkspaceKey = strings.TrimSpace(view.CurrentWorkspace.WorkspaceKey)
	}
	if !expanded {
		filtered, _ := filterThreadSelectionEntriesToRecentWorkspaceGroups(entries, excludeWorkspaceKey, view.RecentLimit)
		entries = filtered
	}
	options := make([]control.SelectionOption, 0, len(entries)+1)
	for _, entry := range entries {
		options = append(options, threadSelectionOption(entry, true))
	}
	totalGroups := countThreadSelectionEntryWorkspaceGroups(view.Entries, excludeWorkspaceKey)
	visibleGroups := countThreadSelectionEntryWorkspaceGroups(entries, excludeWorkspaceKey)
	if !expanded && totalGroups > visibleGroups {
		options = append(options, control.SelectionOption{
			Index:       len(options) + 1,
			ButtonLabel: "全部工作区",
			Subtitle:    fmt.Sprintf("还有 %d 个工作区未显示", totalGroups-visibleGroups),
			ActionKind:  "show_all_thread_workspaces",
		})
	} else if expanded && totalGroups > view.RecentLimit {
		options = append(options, control.SelectionOption{
			Index:       len(options) + 1,
			ButtonLabel: "最近工作区",
			Subtitle:    fmt.Sprintf("回到最近 %d 个工作区", view.RecentLimit),
			ActionKind:  "show_recent_thread_workspaces",
		})
	}
	prompt := control.SelectionPrompt{
		Kind:    control.SelectionPromptUseThread,
		Layout:  "workspace_grouped_useall",
		Title:   "全部会话",
		Options: options,
	}
	if view.CurrentWorkspace != nil {
		prompt.ContextTitle = "当前工作区"
		prompt.ContextKey = strings.TrimSpace(view.CurrentWorkspace.WorkspaceKey)
		line := firstNonEmpty(strings.TrimSpace(view.CurrentWorkspace.WorkspaceLabel), strings.TrimSpace(view.CurrentWorkspace.WorkspaceKey))
		if age := strings.TrimSpace(view.CurrentWorkspace.AgeText); age != "" {
			line += " · " + age
		}
		prompt.ContextText = strings.Join([]string{line, "同工作区内切换请直接用 /use"}, "\n")
	}
	return prompt
}

func threadScopedPromptFromView(view control.FeishuThreadSelectionView, expanded bool) control.SelectionPrompt {
	entries := append([]control.FeishuThreadSelectionEntry(nil), view.Entries...)
	limit := len(entries)
	if !expanded && view.RecentLimit > 0 && limit > view.RecentLimit {
		limit = view.RecentLimit
	}
	options := make([]control.SelectionOption, 0, limit+1)
	for _, entry := range entries[:limit] {
		options = append(options, threadSelectionOption(entry, false))
	}
	if !expanded && len(entries) > limit {
		options = append(options, control.SelectionOption{
			Index:       len(options) + 1,
			ButtonLabel: "当前工作区全部会话",
			Subtitle:    "展开当前工作区内的全部会话",
			ActionKind:  "show_scoped_threads",
		})
	} else if expanded {
		options = append(options, control.SelectionOption{
			Index:       len(options) + 1,
			ButtonLabel: "最近会话",
			Subtitle:    fmt.Sprintf("回到当前工作区最近 %d 个会话", view.RecentLimit),
			ActionKind:  "show_threads",
		})
	}
	title := "最近会话"
	if expanded {
		title = "当前工作区全部会话"
	}
	return control.SelectionPrompt{
		Kind:    control.SelectionPromptUseThread,
		Title:   title,
		Options: options,
	}
}

func threadVSCodePromptFromView(view control.FeishuThreadSelectionView, expanded bool) control.SelectionPrompt {
	entries := append([]control.FeishuThreadSelectionEntry(nil), view.Entries...)
	limit := len(entries)
	if !expanded && view.RecentLimit > 0 && limit > view.RecentLimit {
		limit = view.RecentLimit
	}
	options := make([]control.SelectionOption, 0, limit+1)
	for _, entry := range entries[:limit] {
		options = append(options, threadSelectionOption(entry, false))
	}
	if !expanded && len(entries) > limit {
		options = append(options, control.SelectionOption{
			Index:       len(options) + 1,
			ButtonLabel: "当前实例全部会话",
			Subtitle:    "展开当前实例内的全部会话",
			ActionKind:  "show_scoped_threads",
		})
	} else if expanded {
		options = append(options, control.SelectionOption{
			Index:       len(options) + 1,
			ButtonLabel: "最近会话",
			Subtitle:    fmt.Sprintf("回到当前实例最近 %d 个会话", view.RecentLimit),
			ActionKind:  "show_threads",
		})
	}
	title := "最近会话"
	if expanded {
		title = "当前实例全部会话"
	}
	prompt := control.SelectionPrompt{
		Kind:    control.SelectionPromptUseThread,
		Layout:  "vscode_instance_threads",
		Title:   title,
		Options: options,
	}
	if view.CurrentInstance != nil {
		prompt.ContextTitle = "当前实例"
		prompt.ContextText = strings.TrimSpace(view.CurrentInstance.Label)
		if status := strings.TrimSpace(view.CurrentInstance.Status); status != "" {
			prompt.ContextText += " · " + status
		}
	}
	return prompt
}

func threadSelectionOption(entry control.FeishuThreadSelectionEntry, includeWorkspace bool) control.SelectionOption {
	lines := make([]string, 0, 2)
	if includeWorkspace {
		if workspaceKey := strings.TrimSpace(entry.WorkspaceKey); workspaceKey != "" {
			lines = append(lines, workspaceKey)
		}
	}
	if status := strings.TrimSpace(entry.Status); status != "" {
		lines = append(lines, status)
	}
	return control.SelectionOption{
		OptionID:            entry.ThreadID,
		Label:               firstNonEmpty(strings.TrimSpace(entry.Summary), strings.TrimSpace(entry.ThreadID)),
		Subtitle:            strings.Join(lines, "\n"),
		ButtonLabel:         firstNonEmpty(strings.TrimSpace(entry.Summary), strings.TrimSpace(entry.ThreadID)),
		GroupKey:            strings.TrimSpace(entry.WorkspaceKey),
		GroupLabel:          firstNonEmpty(strings.TrimSpace(entry.WorkspaceLabel), strings.TrimSpace(entry.WorkspaceKey)),
		AgeText:             strings.TrimSpace(entry.AgeText),
		MetaText:            threadSelectionMetaText(entry),
		IsCurrent:           entry.Current,
		Disabled:            entry.Disabled,
		AllowCrossWorkspace: entry.AllowCrossWorkspace,
	}
}

func threadSelectionMetaText(entry control.FeishuThreadSelectionEntry) string {
	status := strings.TrimSpace(entry.Status)
	if entry.Current {
		parts := []string{firstNonEmpty(status, "已接管")}
		if age := strings.TrimSpace(entry.AgeText); age != "" {
			parts = append(parts, age)
		}
		return strings.Join(parts, " · ")
	}
	if status != "" && (strings.Contains(status, "其他飞书会话接管") || strings.Contains(status, "不可接管") || strings.Contains(status, "不存在") || strings.Contains(status, "切换工作区") || strings.Contains(status, "VS Code 占用中")) {
		return status
	}
	parts := make([]string, 0, 2)
	if entry.VSCodeFocused {
		parts = append(parts, "VS Code 当前焦点")
	}
	if age := strings.TrimSpace(entry.AgeText); age != "" && age != "时间未知" {
		parts = append(parts, age)
	}
	if len(parts) != 0 {
		return strings.Join(parts, " · ")
	}
	return firstNonEmpty(status, strings.TrimSpace(entry.AgeText), "时间未知")
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

func countThreadSelectionEntryWorkspaceGroups(entries []control.FeishuThreadSelectionEntry, excludeWorkspaceKey string) int {
	excludeWorkspaceKey = strings.TrimSpace(excludeWorkspaceKey)
	seen := map[string]struct{}{}
	for _, entry := range entries {
		workspaceKey := strings.TrimSpace(entry.WorkspaceKey)
		if workspaceKey == "" || workspaceKey == excludeWorkspaceKey {
			continue
		}
		seen[workspaceKey] = struct{}{}
	}
	return len(seen)
}

func filterThreadSelectionEntriesToRecentWorkspaceGroups(entries []control.FeishuThreadSelectionEntry, excludeWorkspaceKey string, limit int) ([]control.FeishuThreadSelectionEntry, int) {
	if len(entries) == 0 {
		return nil, 0
	}
	excludeWorkspaceKey = strings.TrimSpace(excludeWorkspaceKey)
	seenGroups := map[string]struct{}{}
	visibleGroups := map[string]struct{}{}
	filtered := make([]control.FeishuThreadSelectionEntry, 0, len(entries))
	for _, entry := range entries {
		workspaceKey := strings.TrimSpace(entry.WorkspaceKey)
		if workspaceKey == "" {
			continue
		}
		if workspaceKey == excludeWorkspaceKey {
			filtered = append(filtered, entry)
			continue
		}
		if _, ok := seenGroups[workspaceKey]; !ok {
			seenGroups[workspaceKey] = struct{}{}
			if len(visibleGroups) < limit {
				visibleGroups[workspaceKey] = struct{}{}
			}
		}
		if _, ok := visibleGroups[workspaceKey]; ok {
			filtered = append(filtered, entry)
		}
	}
	return filtered, len(visibleGroups)
}
