package projector

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
)

func TargetPickerElements(view control.FeishuTargetPickerView, daemonLifecycleID string) []map[string]any {
	view = control.NormalizeFeishuTargetPickerView(view)
	if view.Phase != frontstagecontract.PhaseEditing {
		return targetPickerStageElements(view, daemonLifecycleID)
	}
	return targetPickerEditingElements(view, daemonLifecycleID)
}

func targetPickerStageElements(view control.FeishuTargetPickerView, daemonLifecycleID string) []map[string]any {
	view = control.NormalizeFeishuTargetPickerView(view)
	elements := make([]map[string]any, 0, 8)
	elements = append(elements, targetPickerHeaderElements(view.StageLabel, view.Question)...)
	if bodySections := targetPickerBodySectionsForView(view); len(bodySections) != 0 {
		elements = appendCardTextSections(elements, bodySections)
	}
	if noticeSections := targetPickerNoticeSections(view); len(noticeSections) != 0 {
		if len(targetPickerBodySectionsForView(view)) != 0 {
			elements = append(elements, cardDividerElement())
		}
		elements = appendCardTextSections(elements, noticeSections)
	}
	if view.Phase != frontstagecontract.PhaseProcessing || view.ActionPolicy != frontstagecontract.ActionPolicyCancelOnly {
		return elements
	}
	return appendCardFooterButtonGroup(elements, []map[string]any{
		cardCallbackButtonElement(
			strings.TrimSpace(firstNonEmpty(view.ProcessingCancelLabel, "取消")),
			"default",
			stampActionValue(targetPickerPayload(view, actionPayloadTargetPicker(cardActionKindTargetPickerCancel, view.PickerID)), daemonLifecycleID),
			false,
			"",
		),
	})
}

func targetPickerBodySectionsForView(view control.FeishuTargetPickerView) []control.FeishuCardTextSection {
	if len(view.BodySections) != 0 {
		return view.BodySections
	}
	return view.StatusSections
}

func targetPickerNoticeSections(view control.FeishuTargetPickerView) []control.FeishuCardTextSection {
	if len(view.NoticeSections) != 0 {
		return view.NoticeSections
	}
	sections := make([]control.FeishuCardTextSection, 0, len(view.StatusSections)+2)
	if text := strings.TrimSpace(view.StatusText); text != "" {
		label := strings.TrimSpace(view.StatusTitle)
		if label == "" {
			label = "说明"
		}
		sections = append(sections, control.FeishuCardTextSection{
			Label: label,
			Lines: []string{text},
		})
	}
	sections = append(sections, view.StatusSections...)
	if footer := strings.TrimSpace(view.StatusFooter); footer != "" {
		sections = append(sections, control.FeishuCardTextSection{
			Label: "下一步",
			Lines: []string{footer},
		})
	}
	return sections
}

func TargetPickerTheme(view control.FeishuTargetPickerView) string {
	switch view.Stage {
	case control.FeishuTargetPickerStageSucceeded, control.FeishuTargetPickerStageCancelled:
		return cardThemeInfo
	case control.FeishuTargetPickerStageFailed:
		return cardThemeError
	default:
		return cardThemeInfo
	}
}

func targetPickerModePageElements(view control.FeishuTargetPickerView, daemonLifecycleID string) []map[string]any {
	return targetPickerChoicePageElements(
		targetPickerModeChoiceItems(view, daemonLifecycleID),
	)
}

func targetPickerLockedWorkspaceSections(view control.FeishuTargetPickerView) []control.FeishuCardTextSection {
	lines := make([]string, 0, 2)
	if label := strings.TrimSpace(view.SelectedWorkspaceLabel); label != "" {
		lines = append(lines, label)
	}
	if meta := strings.TrimSpace(view.SelectedWorkspaceMeta); meta != "" {
		lines = append(lines, meta)
	}
	if len(lines) == 0 {
		return nil
	}
	return []control.FeishuCardTextSection{{
		Label: "当前工作区",
		Lines: lines,
	}}
}

func targetPickerSourcePageElements(view control.FeishuTargetPickerView, daemonLifecycleID string) []map[string]any {
	return targetPickerChoicePageElements(
		targetPickerSourceChoiceItems(view, daemonLifecycleID),
	)
}

type targetPickerChoiceItem struct {
	Label    string
	MetaText string
	Payload  map[string]any
	Selected bool
	Disabled bool
}

func targetPickerModeChoiceItems(view control.FeishuTargetPickerView, daemonLifecycleID string) []targetPickerChoiceItem {
	items := make([]targetPickerChoiceItem, 0, len(view.ModeOptions))
	for _, option := range view.ModeOptions {
		label := strings.TrimSpace(option.Label)
		if label == "" || option.Value == "" {
			continue
		}
		available := option.Available || strings.TrimSpace(option.UnavailableReason) == ""
		metaText := strings.TrimSpace(option.MetaText)
		if !available && strings.TrimSpace(option.UnavailableReason) != "" {
			metaText = strings.TrimSpace(firstNonEmpty(metaText, option.UnavailableReason))
		}
		items = append(items, targetPickerChoiceItem{
			Label:    label,
			MetaText: metaText,
			Payload:  stampActionValue(targetPickerPayload(view, actionPayloadTargetPickerValue(cardActionKindTargetPickerSelectMode, view.PickerID, string(option.Value))), daemonLifecycleID),
			Selected: option.Selected,
			Disabled: !available,
		})
	}
	return items
}

func targetPickerSourceChoiceItems(view control.FeishuTargetPickerView, daemonLifecycleID string) []targetPickerChoiceItem {
	items := make([]targetPickerChoiceItem, 0, len(view.SourceOptions))
	for _, option := range view.SourceOptions {
		label := strings.TrimSpace(option.Label)
		value := strings.TrimSpace(string(option.Value))
		if label == "" || value == "" {
			continue
		}
		metaText := strings.TrimSpace(option.MetaText)
		if !option.Available && strings.TrimSpace(option.UnavailableReason) != "" {
			metaText = strings.TrimSpace(firstNonEmpty(metaText, option.UnavailableReason))
		}
		items = append(items, targetPickerChoiceItem{
			Label:    label,
			MetaText: metaText,
			Payload:  stampActionValue(targetPickerPayload(view, actionPayloadTargetPickerValue(cardActionKindTargetPickerSelectSource, view.PickerID, value)), daemonLifecycleID),
			Selected: option.Value == view.SelectedSource,
			Disabled: !option.Available,
		})
	}
	return items
}

func targetPickerChoicePageElements(items []targetPickerChoiceItem) []map[string]any {
	elements := make([]map[string]any, 0, len(items)*2)
	for _, item := range items {
		label := strings.TrimSpace(item.Label)
		if label == "" || len(item.Payload) == 0 {
			continue
		}
		buttonType := "default"
		if item.Selected {
			buttonType = "primary"
		}
		elements = append(elements, cardCallbackButtonElement(
			label,
			buttonType,
			item.Payload,
			item.Disabled,
			"fill",
		))
		if metaText := strings.TrimSpace(item.MetaText); metaText != "" {
			if block := cardPlainTextBlockElement(metaText); len(block) != 0 {
				elements = append(elements, block)
			}
		}
	}
	return elements
}

func targetPickerEditingFooterButtons(view control.FeishuTargetPickerView, daemonLifecycleID string) []map[string]any {
	buttons := []map[string]any{
		cardCallbackButtonElement("取消", "default", stampActionValue(targetPickerPayload(view, actionPayloadTargetPicker(cardActionKindTargetPickerCancel, view.PickerID)), daemonLifecycleID), false, ""),
	}
	if view.CanGoBack {
		if back := targetPickerBackButtonElement(view, daemonLifecycleID); len(back) != 0 {
			buttons = append(buttons, back)
		}
	}
	if view.Page == control.FeishuTargetPickerPageMode || view.Page == control.FeishuTargetPickerPageSource {
		return buttons
	}
	buttons = append(buttons, cardCallbackButtonElement(strings.TrimSpace(firstNonEmpty(view.ConfirmLabel, "确认")), "primary", stampActionValue(targetPickerPayload(view, actionPayloadTargetPicker(cardActionKindTargetPickerConfirm, view.PickerID)), daemonLifecycleID), targetPickerConfirmDisabled(view), "fill"))
	return buttons
}

func targetPickerBackButtonElement(view control.FeishuTargetPickerView, daemonLifecycleID string) map[string]any {
	label := strings.TrimSpace(firstNonEmpty(view.BackLabel, "上一步"))
	if commandText := strings.TrimSpace(view.BackCommandText); commandText != "" {
		action, ok := control.ParseFeishuTextAction(commandText)
		if !ok {
			return nil
		}
		return cardCallbackButtonElement(
			label,
			"default",
			stampActionValue(actionPayloadPageAction(string(action.Kind), control.FeishuActionArgumentText(action.Text)), daemonLifecycleID),
			false,
			"",
		)
	}
	return cardCallbackButtonElement(label, "default", stampActionValue(targetPickerPayload(view, actionPayloadTargetPicker(cardActionKindTargetPickerBack, view.PickerID)), daemonLifecycleID), false, "")
}

func targetPickerLocalDirectoryElements(view control.FeishuTargetPickerView, daemonLifecycleID string) []map[string]any {
	elements := []map[string]any{
		{
			"tag":     "markdown",
			"content": targetPickerFieldMarkdown("目录", strings.TrimSpace(view.LocalDirectoryPath), "未选择"),
		},
	}
	elements = append(elements, cardButtonGroupElement([]map[string]any{
		cardCallbackButtonElement(
			"选择目录",
			"default",
			stampActionValue(actionPayloadTargetPickerValue(cardActionKindTargetPickerOpenPathPicker, view.PickerID, control.FeishuTargetPickerPathFieldLocalDirectory), daemonLifecycleID),
			false,
			"",
		),
	}))
	return elements
}

func targetPickerGitURLElements(view control.FeishuTargetPickerView, daemonLifecycleID string) []map[string]any {
	if form := targetPickerGitURLFormElement(view, daemonLifecycleID); len(form) != 0 {
		return []map[string]any{form}
	}
	return nil
}

func targetPickerWorktreeElements(view control.FeishuTargetPickerView, daemonLifecycleID string) []map[string]any {
	lane := targetPickerWorktreeWorkspaceLane(view)
	plan := targetPickerPlanSingleLaneForm(view, daemonLifecycleID, lane, func(page paginatedSelectPage) []map[string]any {
		form := targetPickerWorktreeFormElement(view, daemonLifecycleID, targetPickerPaginatedLaneElements(lane, daemonLifecycleID, page))
		if len(form) == 0 {
			return nil
		}
		return []map[string]any{form}
	})
	form := targetPickerWorktreeFormElement(view, daemonLifecycleID, targetPickerPaginatedLaneElements(lane, daemonLifecycleID, plan.Page))
	if len(form) == 0 {
		return nil
	}
	return []map[string]any{form}
}

func targetPickerGitURLFormElement(view control.FeishuTargetPickerView, daemonLifecycleID string) map[string]any {
	elements := make([]map[string]any, 0, 5)
	if row := targetPickerGitParentDirFormRow(view, daemonLifecycleID); len(row) != 0 {
		elements = append(elements, row)
	}
	elements = append(elements, targetPickerInputElement(
		control.FeishuTargetPickerGitRepoURLFieldName,
		"Git 仓库地址",
		"支持 HTTPS 或 SSH，例如 https://github.com/org/repo.git",
		strings.TrimSpace(view.GitRepoURL),
	))
	elements = append(elements, targetPickerInputElement(
		control.FeishuTargetPickerGitDirectoryNameFieldName,
		"本地目录名（可选）",
		"不填写时，将根据仓库地址自动生成",
		strings.TrimSpace(view.GitDirectoryName),
	))
	if footer := targetPickerInlineFormFooterElements(view, daemonLifecycleID, "克隆并继续"); len(footer) != 0 {
		elements = append(elements, footer...)
	}
	return map[string]any{
		"tag":      "form",
		"name":     "target_picker_git_form_" + strings.TrimSpace(view.PickerID),
		"elements": elements,
	}
}

func targetPickerGitParentDirFormRow(view control.FeishuTargetPickerView, daemonLifecycleID string) map[string]any {
	openPathButton := cardFormActionButtonElement(
		"选择目录",
		"default",
		stampActionValue(targetPickerPayload(view, actionPayloadTargetPickerValue(cardActionKindTargetPickerOpenPathPicker, view.PickerID, control.FeishuTargetPickerPathFieldGitParentDir)), daemonLifecycleID),
		false,
		"",
	)
	if len(openPathButton) == 0 {
		return nil
	}
	openPathButton["name"] = "target_picker_open_path"
	return map[string]any{
		"tag":                "column_set",
		"horizontal_spacing": "small",
		"columns": []map[string]any{
			{
				"tag":            "column",
				"width":          "weighted",
				"weight":         5,
				"vertical_align": "center",
				"elements": []map[string]any{{
					"tag":     "markdown",
					"content": targetPickerGitParentDirMarkdown(view),
				}},
			},
			{
				"tag":            "column",
				"width":          "auto",
				"vertical_align": "center",
				"elements":       []map[string]any{openPathButton},
			},
		},
	}
}

func targetPickerWorktreeFormElement(view control.FeishuTargetPickerView, daemonLifecycleID string, workspaceElements []map[string]any) map[string]any {
	elements := make([]map[string]any, 0, len(workspaceElements)+6)
	if intro := cardPlainTextBlockElement("从一个已接入的 Git 工作区派生新的并行工作区；创建成功后会自动接入并进入新会话。"); len(intro) != 0 {
		elements = append(elements, intro)
	}
	elements = append(elements, workspaceElements...)
	elements = append(elements, targetPickerInputElement(
		control.FeishuTargetPickerWorktreeBranchFieldName,
		"新分支名",
		"例如 feat/login",
		strings.TrimSpace(view.WorktreeBranchName),
	))
	elements = append(elements, targetPickerInputElement(
		control.FeishuTargetPickerWorktreeDirectoryFieldName,
		"本地目录名（可选）",
		"不填写时按分支名自动生成",
		strings.TrimSpace(view.WorktreeDirectoryName),
	))
	elements = append(elements, map[string]any{
		"tag":     "markdown",
		"content": targetPickerFieldMarkdown("目标目录", strings.TrimSpace(view.WorktreeFinalPath), "待生成"),
	})
	if footer := targetPickerInlineFormFooterElements(view, daemonLifecycleID, "创建并进入"); len(footer) != 0 {
		elements = append(elements, footer...)
	}
	return map[string]any{
		"tag":      "form",
		"name":     "target_picker_worktree_form_" + strings.TrimSpace(view.PickerID),
		"elements": elements,
	}
}

func targetPickerInlineFormFooterElements(view control.FeishuTargetPickerView, daemonLifecycleID string, defaultConfirmLabel string) []map[string]any {
	buttons := []map[string]any{}
	cancelButton := cardFormActionButtonElement(
		"取消",
		"default",
		stampActionValue(targetPickerPayload(view, actionPayloadTargetPicker(cardActionKindTargetPickerCancel, view.PickerID)), daemonLifecycleID),
		false,
		"",
	)
	if len(cancelButton) != 0 {
		cancelButton["name"] = "target_picker_cancel"
		buttons = append(buttons, cancelButton)
	}
	if view.CanGoBack {
		backButton := cardFormActionButtonElement(
			strings.TrimSpace(firstNonEmpty(view.BackLabel, "上一步")),
			"default",
			stampActionValue(targetPickerPayload(view, actionPayloadTargetPicker(cardActionKindTargetPickerBack, view.PickerID)), daemonLifecycleID),
			false,
			"",
		)
		if len(backButton) != 0 {
			backButton["name"] = "target_picker_back"
			buttons = append(buttons, backButton)
		}
	}
	confirmButton := cardFormActionButtonElement(
		strings.TrimSpace(firstNonEmpty(view.ConfirmLabel, defaultConfirmLabel)),
		"primary",
		stampActionValue(targetPickerPayload(view, actionPayloadTargetPicker(cardActionKindTargetPickerConfirm, view.PickerID)), daemonLifecycleID),
		targetPickerConfirmDisabled(view),
		"",
	)
	if len(confirmButton) != 0 {
		confirmButton["name"] = "target_picker_confirm"
		buttons = append(buttons, confirmButton)
	}
	group := cardButtonGroupElement(buttons)
	if len(group) == 0 {
		return nil
	}
	return []map[string]any{
		cardDividerElement(),
		group,
	}
}

func targetPickerInlineGitFormTerminalButtons(view control.FeishuTargetPickerView, daemonLifecycleID string) []map[string]any {
	return []map[string]any{
		cardCallbackButtonElement(
			"取消",
			"default",
			stampActionValue(targetPickerPayload(view, actionPayloadTargetPicker(cardActionKindTargetPickerCancel, view.PickerID)), daemonLifecycleID),
			false,
			"",
		),
	}
}

func targetPickerPayload(view control.FeishuTargetPickerView, payload map[string]any) map[string]any {
	return actionPayloadWithCatalog(payload, view.CatalogFamilyID, view.CatalogVariantID, string(view.CatalogBackend))
}

func targetPickerInputElement(name, label, placeholder, value string) map[string]any {
	input := map[string]any{
		"tag":  "input",
		"name": strings.TrimSpace(name),
		"label": map[string]any{
			"tag":     "plain_text",
			"content": strings.TrimSpace(label),
		},
		"label_position": "left",
	}
	if strings.TrimSpace(placeholder) != "" {
		input["placeholder"] = map[string]any{
			"tag":     "plain_text",
			"content": strings.TrimSpace(placeholder),
		}
	}
	if strings.TrimSpace(value) != "" {
		input["default_value"] = strings.TrimSpace(value)
	}
	return input
}

func targetPickerUsesInlineForm(view control.FeishuTargetPickerView) bool {
	return view.Page == control.FeishuTargetPickerPageGit || view.Page == control.FeishuTargetPickerPageWorktree
}

func targetPickerConfirmDisabled(view control.FeishuTargetPickerView) bool {
	if view.ConfirmValidatesOnSubmit {
		return false
	}
	return !view.CanConfirm
}

func targetPickerGitParentDirMarkdown(view control.FeishuTargetPickerView) string {
	return targetPickerFieldMarkdown("落地父目录", strings.TrimSpace(view.GitParentDir), "未选择")
}

func targetPickerFieldMarkdown(label, value, placeholder string) string {
	if strings.TrimSpace(value) == "" {
		value = strings.TrimSpace(firstNonEmpty(placeholder, "未填写"))
	}
	return fmt.Sprintf("**%s**\n%s", strings.TrimSpace(label), formatNeutralTextTag(value))
}

func TargetPickerMessageElements(messages []control.FeishuTargetPickerMessage) []map[string]any {
	if len(messages) == 0 {
		return nil
	}
	elements := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		text := strings.TrimSpace(message.Text)
		if text == "" {
			continue
		}
		if label := targetPickerMessageLevelLabel(message.Level); label != "" {
			elements = append(elements, map[string]any{
				"tag":     "markdown",
				"content": label,
			})
		}
		if block := cardPlainTextBlockElement(text); len(block) != 0 {
			elements = append(elements, block)
		}
	}
	return elements
}

func targetPickerMessageLevelLabel(level control.FeishuTargetPickerMessageLevel) string {
	switch level {
	case control.FeishuTargetPickerMessageDanger:
		return "<font color='red'>请先处理这个问题</font>"
	case control.FeishuTargetPickerMessageWarning:
		return "**请注意**"
	default:
		return ""
	}
}

func targetPickerWorkspaceOptions(options []control.FeishuTargetPickerWorkspaceOption) []map[string]any {
	result := make([]map[string]any, 0, len(options))
	for _, option := range options {
		value := strings.TrimSpace(option.Value)
		if value == "" {
			continue
		}
		result = append(result, map[string]any{
			"text":  cardPlainText(targetPickerOptionLabel(option.Label, option.MetaText)),
			"value": value,
		})
	}
	return result
}

func targetPickerSessionOptions(options []control.FeishuTargetPickerSessionOption) []map[string]any {
	result := make([]map[string]any, 0, len(options))
	for _, option := range options {
		value := strings.TrimSpace(option.Value)
		if value == "" {
			continue
		}
		result = append(result, map[string]any{
			"text":  cardPlainText(targetPickerOptionLabel(option.Label, option.MetaText)),
			"value": value,
		})
	}
	return result
}

func targetPickerOptionLabel(label, meta string) string {
	label = strings.TrimSpace(label)
	meta = strings.TrimSpace(meta)
	if label == "" {
		return meta
	}
	if meta == "" {
		return label
	}
	if line := strings.TrimSpace(strings.Split(meta, "\n")[0]); line != "" {
		return label + " · " + line
	}
	return label
}
