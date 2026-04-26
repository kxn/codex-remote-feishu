package projector

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

type selectionRenderModel struct {
	Kind             control.SelectionPromptKind
	Layout           string
	ViewMode         string
	Title            string
	Hint             string
	ContextTitle     string
	ContextText      string
	ContextKey       string
	Page             int
	TotalPages       int
	ReturnPage       int
	CatalogFamilyID  string
	CatalogVariantID string
	CatalogBackend   agentproto.Backend
	Options          []control.SelectionOption
}

func selectionRenderModelFromView(view control.FeishuSelectionView, ctx *control.FeishuUISelectionContext) (selectionRenderModel, bool) {
	semantics := control.DeriveFeishuSelectionSemantics(view)
	var model selectionRenderModel
	var ok bool
	switch {
	case view.Instance != nil && view.PromptKind == control.SelectionPromptAttachInstance:
		model, ok = instanceSelectionRenderModelFromView(*view.Instance, ctx, semantics), true
	case view.Workspace != nil && view.PromptKind == control.SelectionPromptAttachWorkspace:
		model, ok = workspaceSelectionRenderModelFromView(*view.Workspace, ctx, semantics), true
	case view.Thread != nil && view.PromptKind == control.SelectionPromptUseThread:
		model, ok = threadSelectionRenderModelFromView(*view.Thread, semantics), true
	case view.KickThread != nil && view.PromptKind == control.SelectionPromptKickThread:
		model, ok = kickThreadSelectionRenderModelFromView(*view.KickThread, semantics), true
	default:
		return selectionRenderModel{}, false
	}
	model.CatalogFamilyID = strings.TrimSpace(view.CatalogFamilyID)
	model.CatalogVariantID = strings.TrimSpace(view.CatalogVariantID)
	model.CatalogBackend = view.CatalogBackend
	if ctx != nil {
		if model.CatalogFamilyID == "" {
			model.CatalogFamilyID = strings.TrimSpace(ctx.CatalogFamilyID)
		}
		if model.CatalogVariantID == "" {
			model.CatalogVariantID = strings.TrimSpace(ctx.CatalogVariantID)
		}
		if model.CatalogBackend == "" {
			model.CatalogBackend = ctx.CatalogBackend
		}
	}
	return model, ok
}

func instanceSelectionRenderModelFromView(view control.FeishuInstanceSelectionView, _ *control.FeishuUISelectionContext, semantics control.FeishuSelectionSemantics) selectionRenderModel {
	options := make([]control.SelectionOption, 0, len(view.Entries))
	for index, entry := range view.Entries {
		options = append(options, control.SelectionOption{
			Index:       index + 1,
			OptionID:    strings.TrimSpace(entry.InstanceID),
			Label:       strings.TrimSpace(entry.Label),
			ButtonLabel: strings.TrimSpace(entry.ButtonLabel),
			MetaText:    strings.TrimSpace(entry.MetaText),
			Disabled:    entry.Disabled,
		})
	}
	model := selectionRenderModel{
		Kind:         semantics.PromptKind,
		Layout:       strings.TrimSpace(semantics.Layout),
		Title:        strings.TrimSpace(semantics.Title),
		ContextTitle: strings.TrimSpace(semantics.ContextTitle),
		ContextText:  strings.TrimSpace(semantics.ContextText),
		Options:      options,
	}
	return model
}

func workspaceSelectionRenderModelFromView(view control.FeishuWorkspaceSelectionView, ctx *control.FeishuUISelectionContext, semantics control.FeishuSelectionSemantics) selectionRenderModel {
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
			actionKind = cardActionKindShowWorkspaceThreads
		default:
			disabled = true
		}
		option := control.SelectionOption{
			OptionID:    strings.TrimSpace(entry.WorkspaceKey),
			Label:       firstNonEmpty(strings.TrimSpace(entry.WorkspaceLabel), strings.TrimSpace(entry.WorkspaceKey)),
			ButtonLabel: buttonLabel,
			AgeText:     strings.TrimSpace(entry.AgeText),
			MetaText:    control.FormatFeishuWorkspaceSelectionMetaText(strings.TrimSpace(entry.AgeText), entry.HasVSCodeActivity, entry.Busy, !entry.Attachable && !entry.RecoverableOnly, entry.RecoverableOnly),
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

	model := selectionRenderModel{
		Kind:         semantics.PromptKind,
		Layout:       strings.TrimSpace(semantics.Layout),
		Title:        strings.TrimSpace(semantics.Title),
		Hint:         hint,
		ViewMode:     strings.TrimSpace(semantics.ViewMode),
		Page:         view.Page,
		TotalPages:   view.TotalPages,
		ContextTitle: strings.TrimSpace(semantics.ContextTitle),
		ContextText:  strings.TrimSpace(semantics.ContextText),
		ContextKey:   strings.TrimSpace(semantics.ContextKey),
		Options:      options,
	}
	return model
}

func threadSelectionRenderModelFromView(view control.FeishuThreadSelectionView, semantics control.FeishuSelectionSemantics) selectionRenderModel {
	switch view.Mode {
	case control.FeishuThreadSelectionNormalWorkspaceView:
		return threadWorkspaceRenderModelFromView(view, semantics)
	case control.FeishuThreadSelectionNormalGlobalRecent, control.FeishuThreadSelectionNormalGlobalAll:
		return threadGlobalRenderModelFromView(view, semantics)
	case control.FeishuThreadSelectionNormalScopedAll, control.FeishuThreadSelectionNormalScopedRecent:
		return threadScopedRenderModelFromView(view, semantics)
	case control.FeishuThreadSelectionVSCodeAll, control.FeishuThreadSelectionVSCodeScopedAll, control.FeishuThreadSelectionVSCodeRecent:
		return threadVSCodeRenderModelFromView(view, semantics)
	default:
		return threadVSCodeRenderModelFromView(view, semantics)
	}
}

func threadWorkspaceRenderModelFromView(view control.FeishuThreadSelectionView, semantics control.FeishuSelectionSemantics) selectionRenderModel {
	options := make([]control.SelectionOption, 0, len(view.Entries))
	for _, entry := range view.Entries {
		options = append(options, threadSelectionOption(entry, false))
	}
	return selectionRenderModel{
		Kind:         semantics.PromptKind,
		Layout:       strings.TrimSpace(semantics.Layout),
		Title:        strings.TrimSpace(semantics.Title),
		ViewMode:     strings.TrimSpace(semantics.ViewMode),
		Page:         view.Page,
		TotalPages:   view.TotalPages,
		ReturnPage:   view.ReturnPage,
		ContextTitle: strings.TrimSpace(semantics.ContextTitle),
		ContextText:  strings.TrimSpace(semantics.ContextText),
		ContextKey:   strings.TrimSpace(semantics.ContextKey),
		Options:      options,
	}
}

func threadGlobalRenderModelFromView(view control.FeishuThreadSelectionView, semantics control.FeishuSelectionSemantics) selectionRenderModel {
	options := make([]control.SelectionOption, 0, len(view.Entries))
	for _, entry := range view.Entries {
		options = append(options, threadSelectionOption(entry, true))
	}
	model := selectionRenderModel{
		Kind:         semantics.PromptKind,
		Layout:       strings.TrimSpace(semantics.Layout),
		Title:        strings.TrimSpace(semantics.Title),
		ViewMode:     strings.TrimSpace(semantics.ViewMode),
		Page:         view.Page,
		TotalPages:   view.TotalPages,
		ContextTitle: strings.TrimSpace(semantics.ContextTitle),
		ContextText:  strings.TrimSpace(semantics.ContextText),
		ContextKey:   strings.TrimSpace(semantics.ContextKey),
		Options:      options,
	}
	return model
}

func threadScopedRenderModelFromView(view control.FeishuThreadSelectionView, semantics control.FeishuSelectionSemantics) selectionRenderModel {
	options := make([]control.SelectionOption, 0, len(view.Entries))
	for _, entry := range view.Entries {
		options = append(options, threadSelectionOption(entry, false))
	}
	return selectionRenderModel{
		Kind:         semantics.PromptKind,
		Layout:       strings.TrimSpace(semantics.Layout),
		Title:        strings.TrimSpace(semantics.Title),
		ViewMode:     strings.TrimSpace(semantics.ViewMode),
		Page:         view.Page,
		TotalPages:   view.TotalPages,
		ContextTitle: strings.TrimSpace(semantics.ContextTitle),
		ContextText:  strings.TrimSpace(semantics.ContextText),
		ContextKey:   strings.TrimSpace(semantics.ContextKey),
		Options:      options,
	}
}

func threadVSCodeRenderModelFromView(view control.FeishuThreadSelectionView, semantics control.FeishuSelectionSemantics) selectionRenderModel {
	options := make([]control.SelectionOption, 0, len(view.Entries))
	for _, entry := range view.Entries {
		options = append(options, vscodeThreadSelectionOption(entry))
	}
	model := selectionRenderModel{
		Kind:         semantics.PromptKind,
		Layout:       strings.TrimSpace(semantics.Layout),
		Title:        strings.TrimSpace(semantics.Title),
		ViewMode:     strings.TrimSpace(semantics.ViewMode),
		Page:         view.Page,
		TotalPages:   view.TotalPages,
		ContextTitle: strings.TrimSpace(semantics.ContextTitle),
		ContextText:  strings.TrimSpace(semantics.ContextText),
		ContextKey:   strings.TrimSpace(semantics.ContextKey),
		Options:      options,
	}
	return model
}

func kickThreadSelectionRenderModelFromView(view control.FeishuKickThreadSelectionView, semantics control.FeishuSelectionSemantics) selectionRenderModel {
	threadID := strings.TrimSpace(view.ThreadID)
	return selectionRenderModel{
		Kind:   semantics.PromptKind,
		Layout: strings.TrimSpace(semantics.Layout),
		Title:  strings.TrimSpace(semantics.Title),
		Hint:   strings.TrimSpace(view.Hint),
		Options: []control.SelectionOption{
			{
				Index:       1,
				OptionID:    "cancel",
				Label:       "保留当前状态，不执行强踢。",
				ButtonLabel: firstNonEmpty(strings.TrimSpace(view.CancelLabel), "取消"),
			},
			{
				Index:       2,
				OptionID:    threadID,
				Label:       strings.TrimSpace(view.ThreadLabel),
				Subtitle:    strings.TrimSpace(view.ThreadSubtitle),
				ButtonLabel: firstNonEmpty(strings.TrimSpace(view.ConfirmLabel), "强踢并占用"),
			},
		},
	}
}

func vscodeThreadSelectionOption(entry control.FeishuThreadSelectionEntry) control.SelectionOption {
	option := threadSelectionOption(entry, false)
	label := firstNonEmpty(strings.TrimSpace(entry.Summary), strings.TrimSpace(entry.ThreadID))
	option.Label = label
	option.ButtonLabel = label
	return option
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
	return base
}
