package mockfeishu

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/render"
)

type Recorder struct {
	Events           []eventcontract.Event
	Notices          []string
	Blocks           []render.Block
	TypingOnFor      []string
	TypingOffFor     []string
	ThumbsUpFor      []string
	ThumbsDownFor    []string
	SelectionPrompts []control.FeishuDirectSelectionPrompt
}

func NewRecorder() *Recorder {
	return &Recorder{}
}

func (r *Recorder) Apply(events []eventcontract.Event) {
	r.ApplyEvents(events)
}

func (r *Recorder) ApplyEvents(events []eventcontract.Event) {
	for _, event := range events {
		event = event.Normalized()
		r.Events = append(r.Events, event)
		switch payload := event.Payload.(type) {
		case eventcontract.NoticePayload:
			r.Notices = append(r.Notices, payload.Notice.Text)
		case eventcontract.BlockCommittedPayload:
			r.Blocks = append(r.Blocks, payload.Block)
		case eventcontract.PendingInputPayload:
			if payload.State.TypingOn {
				r.TypingOnFor = append(r.TypingOnFor, payload.State.SourceMessageID)
			}
			if payload.State.TypingOff {
				r.TypingOffFor = append(r.TypingOffFor, payload.State.SourceMessageID)
			}
			if payload.State.ThumbsUp {
				r.ThumbsUpFor = append(r.ThumbsUpFor, payload.State.SourceMessageID)
			}
			if payload.State.ThumbsDown {
				r.ThumbsDownFor = append(r.ThumbsDownFor, payload.State.SourceMessageID)
			}
		case eventcontract.SelectionPayload:
			if prompt, ok := selectionPromptFromView(payload.View, payload.Context); ok {
				r.SelectionPrompts = append(r.SelectionPrompts, prompt)
			}
		}
	}
}

func selectionPromptFromView(view control.FeishuSelectionView, ctx *control.FeishuUISelectionContext) (control.FeishuDirectSelectionPrompt, bool) {
	semantics := control.DeriveFeishuSelectionSemantics(view)
	prompt := control.FeishuDirectSelectionPrompt{
		Kind:         view.PromptKind,
		Layout:       firstNonEmpty(selectionContextLayout(ctx), strings.TrimSpace(semantics.Layout)),
		ViewMode:     firstNonEmpty(selectionContextViewMode(ctx), strings.TrimSpace(semantics.ViewMode)),
		Title:        firstNonEmpty(selectionContextTitle(ctx), strings.TrimSpace(semantics.Title)),
		ContextTitle: firstNonEmpty(selectionContextLabel(ctx), strings.TrimSpace(semantics.ContextTitle)),
		ContextText:  firstNonEmpty(selectionContextText(ctx), strings.TrimSpace(semantics.ContextText)),
		ContextKey:   firstNonEmpty(selectionContextKey(ctx), strings.TrimSpace(semantics.ContextKey)),
	}
	switch {
	case view.Instance != nil && view.PromptKind == control.SelectionPromptAttachInstance:
		options := make([]control.SelectionOption, 0, len(view.Instance.Entries))
		for index, entry := range view.Instance.Entries {
			options = append(options, control.SelectionOption{
				Index:       index + 1,
				OptionID:    strings.TrimSpace(entry.InstanceID),
				Label:       strings.TrimSpace(entry.Label),
				ButtonLabel: strings.TrimSpace(entry.ButtonLabel),
				MetaText:    strings.TrimSpace(entry.MetaText),
				Disabled:    entry.Disabled,
			})
		}
		prompt.Options = options
		return prompt, true
	case view.Workspace != nil && view.PromptKind == control.SelectionPromptAttachWorkspace:
		prompt.Page = view.Workspace.Page
		prompt.TotalPages = view.Workspace.TotalPages
		options := make([]control.SelectionOption, 0, len(view.Workspace.Entries))
		for index, entry := range view.Workspace.Entries {
			disabled := entry.Busy || (!entry.Attachable && !entry.RecoverableOnly)
			actionKind := ""
			buttonLabel := ""
			switch {
			case entry.RecoverableOnly:
				actionKind = "show_workspace_threads"
				buttonLabel = "恢复"
			case entry.Attachable:
				buttonLabel = "切换"
			}
			options = append(options, control.SelectionOption{
				Index:       index + 1,
				OptionID:    strings.TrimSpace(entry.WorkspaceKey),
				Label:       firstNonEmpty(strings.TrimSpace(entry.WorkspaceLabel), strings.TrimSpace(entry.WorkspaceKey)),
				ButtonLabel: buttonLabel,
				AgeText:     strings.TrimSpace(entry.AgeText),
				MetaText:    control.FormatFeishuWorkspaceSelectionMetaText(strings.TrimSpace(entry.AgeText), entry.HasVSCodeActivity, entry.Busy, !entry.Attachable && !entry.RecoverableOnly, entry.RecoverableOnly),
				ActionKind:  actionKind,
				Disabled:    disabled,
			})
		}
		prompt.Options = options
		return prompt, true
	case view.Thread != nil && view.PromptKind == control.SelectionPromptUseThread:
		prompt.Page = view.Thread.Page
		prompt.TotalPages = view.Thread.TotalPages
		prompt.ReturnPage = view.Thread.ReturnPage
		options := make([]control.SelectionOption, 0, len(view.Thread.Entries))
		for index, entry := range view.Thread.Entries {
			options = append(options, control.SelectionOption{
				Index:               index + 1,
				OptionID:            strings.TrimSpace(entry.ThreadID),
				Label:               threadPromptLabel(view.Thread.Mode, entry),
				ButtonLabel:         threadPromptLabel(view.Thread.Mode, entry),
				MetaText:            threadPromptMeta(entry),
				AgeText:             strings.TrimSpace(entry.AgeText),
				GroupKey:            strings.TrimSpace(entry.WorkspaceKey),
				GroupLabel:          strings.TrimSpace(entry.WorkspaceLabel),
				IsCurrent:           entry.Current,
				Disabled:            entry.Disabled,
				AllowCrossWorkspace: entry.AllowCrossWorkspace,
			})
		}
		prompt.Options = options
		return prompt, true
	case view.KickThread != nil && view.PromptKind == control.SelectionPromptKickThread:
		prompt.Hint = strings.TrimSpace(view.KickThread.Hint)
		prompt.Options = []control.SelectionOption{
			{
				Index:       1,
				OptionID:    "cancel",
				Label:       "保留当前状态，不执行强踢。",
				ButtonLabel: firstNonEmpty(strings.TrimSpace(view.KickThread.CancelLabel), "取消"),
			},
			{
				Index:       2,
				OptionID:    strings.TrimSpace(view.KickThread.ThreadID),
				Label:       strings.TrimSpace(view.KickThread.ThreadLabel),
				Subtitle:    strings.TrimSpace(view.KickThread.ThreadSubtitle),
				ButtonLabel: firstNonEmpty(strings.TrimSpace(view.KickThread.ConfirmLabel), "强踢并占用"),
			},
		}
		return prompt, true
	default:
		return control.FeishuDirectSelectionPrompt{}, false
	}
}

func selectionContextTitle(ctx *control.FeishuUISelectionContext) string {
	if ctx == nil {
		return ""
	}
	return strings.TrimSpace(ctx.Title)
}

func selectionContextLabel(ctx *control.FeishuUISelectionContext) string {
	if ctx == nil {
		return ""
	}
	return strings.TrimSpace(ctx.ContextTitle)
}

func selectionContextText(ctx *control.FeishuUISelectionContext) string {
	if ctx == nil {
		return ""
	}
	return strings.TrimSpace(ctx.ContextText)
}

func selectionContextKey(ctx *control.FeishuUISelectionContext) string {
	if ctx == nil {
		return ""
	}
	return strings.TrimSpace(ctx.ContextKey)
}

func selectionContextLayout(ctx *control.FeishuUISelectionContext) string {
	if ctx == nil {
		return ""
	}
	return strings.TrimSpace(ctx.Layout)
}

func selectionContextViewMode(ctx *control.FeishuUISelectionContext) string {
	if ctx == nil {
		return ""
	}
	return strings.TrimSpace(ctx.ViewMode)
}

func threadPromptLabel(mode control.FeishuThreadSelectionViewMode, entry control.FeishuThreadSelectionEntry) string {
	if mode == control.FeishuThreadSelectionVSCodeRecent || mode == control.FeishuThreadSelectionVSCodeAll || mode == control.FeishuThreadSelectionVSCodeScopedAll {
		if text := strings.TrimSpace(entry.FirstUserMessage); text != "" {
			return text
		}
		if text := strings.TrimSpace(entry.LastUserMessage); text != "" {
			return text
		}
		if text := strings.TrimSpace(entry.LastAssistantMessage); text != "" {
			return text
		}
		if parts := strings.SplitN(strings.TrimSpace(entry.Summary), " · ", 2); len(parts) == 2 {
			return strings.TrimSpace(parts[1])
		}
	}
	return strings.TrimSpace(firstNonEmpty(entry.Summary, entry.ThreadID))
}

func threadPromptMeta(entry control.FeishuThreadSelectionEntry) string {
	status := strings.TrimSpace(entry.Status)
	if entry.Current {
		parts := []string{firstNonEmpty(status, "当前跟随中")}
		if age := strings.TrimSpace(entry.AgeText); age != "" {
			parts = append(parts, age)
		}
		return strings.Join(parts, " · ")
	}
	parts := make([]string, 0, 2)
	if entry.VSCodeFocused {
		parts = append(parts, "VS Code 当前焦点")
	}
	if age := strings.TrimSpace(entry.AgeText); age != "" {
		parts = append(parts, age)
	}
	if len(parts) != 0 {
		return strings.Join(parts, " · ")
	}
	return status
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
