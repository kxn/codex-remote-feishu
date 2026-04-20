package feishu

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

// FeishuDirectSelectionPromptFromView projects the UI-owned selection view into the prompt
// shape currently consumed by the Feishu card renderer.
func FeishuDirectSelectionPromptFromView(view control.FeishuSelectionView, ctx *control.FeishuUISelectionContext) (control.FeishuDirectSelectionPrompt, bool) {
	switch {
	case view.Workspace != nil && view.PromptKind == control.SelectionPromptAttachWorkspace:
		return workspaceSelectionPromptFromView(*view.Workspace, ctx), true
	case view.Thread != nil && view.PromptKind == control.SelectionPromptUseThread:
		return threadSelectionPromptFromView(*view.Thread), true
	default:
		return control.FeishuDirectSelectionPrompt{}, false
	}
}

func workspaceSelectionPromptFromView(view control.FeishuWorkspaceSelectionView, ctx *control.FeishuUISelectionContext) control.FeishuDirectSelectionPrompt {
	available := make([]control.SelectionOption, 0, len(view.Entries))
	unavailable := make([]control.SelectionOption, 0, len(view.Entries))
	for _, entry := range view.Entries {
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

	options := make([]control.SelectionOption, 0, len(available)+len(unavailable))
	appendIndexed := func(entries []control.SelectionOption) {
		for _, option := range entries {
			option.Index = len(options) + 1
			options = append(options, option)
		}
	}
	appendIndexed(available)
	appendIndexed(unavailable)

	hint := ""
	if view.Current != nil && len(options) == 0 {
		hint = "当前没有其他可接管工作区。"
	}

	prompt := control.FeishuDirectSelectionPrompt{
		Kind:       control.SelectionPromptAttachWorkspace,
		Layout:     "grouped_attach_workspace",
		Title:      "工作区列表",
		Hint:       hint,
		ViewMode:   "paged",
		Page:       view.Page,
		TotalPages: view.TotalPages,
		Options:    options,
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

func threadSelectionPromptFromView(view control.FeishuThreadSelectionView) control.FeishuDirectSelectionPrompt {
	switch view.Mode {
	case control.FeishuThreadSelectionNormalWorkspaceView:
		return threadWorkspacePromptFromView(view)
	case control.FeishuThreadSelectionNormalGlobalRecent, control.FeishuThreadSelectionNormalGlobalAll:
		return threadGlobalPromptFromView(view)
	case control.FeishuThreadSelectionNormalScopedAll, control.FeishuThreadSelectionNormalScopedRecent:
		return threadScopedPromptFromView(view)
	case control.FeishuThreadSelectionVSCodeAll, control.FeishuThreadSelectionVSCodeScopedAll:
		return threadVSCodePromptFromView(view)
	default:
		return threadVSCodePromptFromView(view)
	}
}

func threadWorkspacePromptFromView(view control.FeishuThreadSelectionView) control.FeishuDirectSelectionPrompt {
	options := make([]control.SelectionOption, 0, len(view.Entries))
	for _, entry := range view.Entries {
		options = append(options, threadSelectionOption(entry, false))
	}
	title := "全部会话"
	if view.Workspace != nil {
		title = firstNonEmpty(strings.TrimSpace(view.Workspace.WorkspaceLabel), strings.TrimSpace(view.Workspace.WorkspaceKey), "工作区") + " 全部会话"
	}
	return control.FeishuDirectSelectionPrompt{
		Kind:       control.SelectionPromptUseThread,
		Layout:     "workspace_grouped_useall",
		Title:      title,
		ViewMode:   string(view.Mode),
		Page:       view.Page,
		TotalPages: view.TotalPages,
		ReturnPage: view.ReturnPage,
		ContextKey: strings.TrimSpace(firstNonEmpty(
			func() string {
				if view.Workspace == nil {
					return ""
				}
				return view.Workspace.WorkspaceKey
			}(),
		)),
		Options: options,
	}
}

func threadGlobalPromptFromView(view control.FeishuThreadSelectionView) control.FeishuDirectSelectionPrompt {
	options := make([]control.SelectionOption, 0, len(view.Entries))
	for _, entry := range view.Entries {
		options = append(options, threadSelectionOption(entry, true))
	}
	prompt := control.FeishuDirectSelectionPrompt{
		Kind:       control.SelectionPromptUseThread,
		Layout:     "workspace_grouped_useall",
		Title:      "全部会话",
		ViewMode:   string(view.Mode),
		Page:       view.Page,
		TotalPages: view.TotalPages,
		Options:    options,
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

func threadScopedPromptFromView(view control.FeishuThreadSelectionView) control.FeishuDirectSelectionPrompt {
	options := make([]control.SelectionOption, 0, len(view.Entries))
	for _, entry := range view.Entries {
		options = append(options, threadSelectionOption(entry, false))
	}
	title := "最近会话"
	if view.Mode == control.FeishuThreadSelectionNormalScopedAll {
		title = "当前工作区全部会话"
	}
	return control.FeishuDirectSelectionPrompt{
		Kind:       control.SelectionPromptUseThread,
		Title:      title,
		ViewMode:   string(view.Mode),
		Page:       view.Page,
		TotalPages: view.TotalPages,
		Options:    options,
	}
}

func threadVSCodePromptFromView(view control.FeishuThreadSelectionView) control.FeishuDirectSelectionPrompt {
	options := make([]control.SelectionOption, 0, len(view.Entries))
	for _, entry := range view.Entries {
		options = append(options, threadSelectionOption(entry, false))
	}
	title := "最近会话"
	if view.Mode == control.FeishuThreadSelectionVSCodeAll || view.Mode == control.FeishuThreadSelectionVSCodeScopedAll {
		title = "当前实例全部会话"
	}
	prompt := control.FeishuDirectSelectionPrompt{
		Kind:       control.SelectionPromptUseThread,
		Layout:     "vscode_instance_threads",
		Title:      title,
		ViewMode:   string(view.Mode),
		Page:       view.Page,
		TotalPages: view.TotalPages,
		Options:    options,
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
	base := ""
	status := strings.TrimSpace(entry.Status)
	if entry.Current {
		parts := []string{firstNonEmpty(status, "已接管")}
		if age := strings.TrimSpace(entry.AgeText); age != "" {
			parts = append(parts, age)
		}
		base = strings.Join(parts, " · ")
	} else if status != "" && (strings.Contains(status, "其他飞书会话接管") || strings.Contains(status, "不可接管") || strings.Contains(status, "不存在") || strings.Contains(status, "切换工作区") || strings.Contains(status, "VS Code 占用中")) {
		base = status
	} else {
		parts := make([]string, 0, 2)
		if entry.VSCodeFocused {
			parts = append(parts, "VS Code 当前焦点")
		}
		if age := strings.TrimSpace(entry.AgeText); age != "" && age != "时间未知" {
			parts = append(parts, age)
		}
		if len(parts) != 0 {
			base = strings.Join(parts, " · ")
		} else {
			base = firstNonEmpty(status, strings.TrimSpace(entry.AgeText), "时间未知")
		}
	}
	if hint := threadSelectionConversationHint(entry); hint != "" {
		return base + "\n" + hint
	}
	return base
}

func threadSelectionConversationHint(entry control.FeishuThreadSelectionEntry) string {
	if lastUser := strings.TrimSpace(entry.LastUserMessage); lastUser != "" {
		return "最近用户：" + lastUser
	}
	if firstUser := strings.TrimSpace(entry.FirstUserMessage); firstUser != "" {
		return "会话起点：" + firstUser
	}
	if lastAssistant := strings.TrimSpace(entry.LastAssistantMessage); lastAssistant != "" {
		return "最近回复：" + lastAssistant
	}
	return ""
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
	parts = append(parts, "同工作区内继续工作可 /use，或直接发送文本（也可 /new）")
	return strings.Join(parts, "\n")
}
