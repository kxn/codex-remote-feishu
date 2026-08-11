package projector

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu/cardkit"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	frontstagecontract "github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

const (
	commandCatalogRootLabel               = "菜单首页"
	menuHomeProductName                   = "Codex Remote Feishu"
	menuHomeGitHubLabel                   = "kxn/codex-remote-feishu"
	menuHomeGitHubURL                     = "https://github.com/kxn/codex-remote-feishu"
	menuHomeUsageGuideURL                 = "https://my.feishu.cn/docx/PTncdNBf1oS9N5xBikBcGi2enzc"
	menuHomeVersionFallback               = "dev"
	commandCatalogPaginatedSelectPageSize = 10
)

type PageRenderOptions struct {
	MenuHomeVersion string
}

func PageElementsWithOptions(view control.FeishuPageView, daemonLifecycleID string, opts PageRenderOptions) []map[string]any {
	view = control.NormalizeFeishuPageView(view)
	elements := make([]map[string]any, 0, len(view.Sections)*3+len(view.SummarySections)*2+len(view.NoticeSections)*2+3)
	if breadcrumb := commandCatalogBreadcrumbMarkdown(view, opts); breadcrumb != "" {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": breadcrumb,
		})
	}
	bodySections := control.BuildFeishuPageBodySections(view)
	if len(bodySections) != 0 {
		elements = cardkit.AppendTextSections(elements, bodySections)
	} else {
		elements = cardkit.AppendTextSections(elements, cloneNormalizedCardSections(view.SummarySections))
	}
	hasBusinessContent := len(bodySections) != 0
	for _, section := range view.Sections {
		title := strings.TrimSpace(section.Title)
		if title != "" {
			elements = append(elements, map[string]any{
				"tag":     "markdown",
				"content": "**" + title + "**",
			})
		}
		for _, entry := range section.Entries {
			renderedCompactButtons := false
			if view.DisplayStyle == control.CommandCatalogDisplayCompactButtons && view.Interactive && len(entry.Buttons) > 0 {
				elements = append(elements, pageCompactButtonElements(entry.Buttons, view.CatalogBackend, daemonLifecycleID)...)
				renderedCompactButtons = true
				if entry.Form == nil {
					continue
				}
			}
			elements = cardkit.AppendTextSections(elements, commandCatalogEntryFallbackSections(entry))
			if view.Interactive && len(entry.Buttons) > 0 && !renderedCompactButtons {
				if group := cardkit.ButtonGroupElement(pageButtons(entry.Buttons, view.CatalogBackend, daemonLifecycleID)); len(group) != 0 {
					elements = append(elements, group)
				}
			}
			if view.Interactive && entry.Form != nil {
				if formElements := pageFormElements(*entry.Form, view.CatalogBackend, daemonLifecycleID); len(formElements) != 0 {
					elements = append(elements, formElements...)
				}
			}
		}
		hasBusinessContent = true
	}
	if noticeSections := control.BuildFeishuPageNoticeSections(view); len(noticeSections) != 0 {
		if hasBusinessContent {
			elements = append(elements, cardDividerElement())
		}
		elements = cardkit.AppendTextSections(elements, noticeSections)
	}
	if len(view.RelatedButtons) > 0 {
		elements = appendCardFooterButtonGroup(elements, pageButtons(view.RelatedButtons, view.CatalogBackend, daemonLifecycleID))
	}
	return elements
}

func pageFormElements(form control.CommandCatalogForm, pageBackend controlBackend, daemonLifecycleID string) []map[string]any {
	field := form.Field
	formName := commandCatalogFormName(form)
	submitValue := cloneActionPayload(form.SubmitValue)
	if len(submitValue) == 0 {
		actionKind, ok := control.ActionKindForFeishuCommandID(strings.TrimSpace(form.CommandID))
		if !ok {
			return nil
		}
		submitValue = actionPayloadPageSubmit(
			string(actionKind),
			control.FeishuActionArgumentText(form.CommandText),
			strings.TrimSpace(field.Name),
		)
		if resolved, ok := resolveCatalogActionForPage(control.Action{
			Kind:             actionKind,
			Text:             strings.TrimSpace(form.CommandText),
			CommandID:        strings.TrimSpace(form.CommandID),
			CatalogFamilyID:  strings.TrimSpace(form.CatalogFamilyID),
			CatalogVariantID: strings.TrimSpace(form.CatalogVariantID),
			CatalogBackend:   pageCatalogBackend(pageBackend, form.CatalogBackend),
		}, pageCatalogBackend(pageBackend, form.CatalogBackend)); ok {
			submitValue = actionPayloadWithCatalog(submitValue, resolved.FamilyID, resolved.VariantID, string(resolved.Backend))
		}
	}
	submitValue = stampActionValue(submitValue, daemonLifecycleID)
	submitButton := cardFormSubmitButtonElement(xutil.FirstNonEmpty(strings.TrimSpace(form.SubmitLabel), "执行"), submitValue)
	if len(submitButton) != 0 {
		submitButton["name"] = commandCatalogSubmitButtonName(formName)
	}
	field = paginatedCommandCatalogFormField(field, form)
	elements := []map[string]any{{
		"tag":  "form",
		"name": formName,
		"elements": []map[string]any{
			commandCatalogFormFieldElement(field),
			submitButton,
		},
	}}
	if group := commandCatalogFormPaginationButtons(form, pageBackend, daemonLifecycleID); len(group) != 0 {
		elements = append(elements, group)
	}
	return elements
}

func paginatedCommandCatalogFormField(field control.CommandCatalogFormField, form control.CommandCatalogForm) control.CommandCatalogFormField {
	if !form.Paginated || field.Kind != control.CommandCatalogFormFieldSelectStatic || len(field.Options) <= commandCatalogPaginatedSelectPageSize {
		return field
	}
	cursor := normalizeCommandCatalogFormCursor(form.Cursor, len(field.Options))
	end := cursor + commandCatalogPaginatedSelectPageSize
	if end > len(field.Options) {
		end = len(field.Options)
	}
	field.Options = append([]control.CommandCatalogFormFieldOption(nil), field.Options[cursor:end]...)
	return field
}

func commandCatalogFormPaginationButtons(form control.CommandCatalogForm, pageBackend controlBackend, daemonLifecycleID string) map[string]any {
	if !form.Paginated || len(form.Field.Options) <= commandCatalogPaginatedSelectPageSize {
		return nil
	}
	actionKind, ok := control.ActionKindForFeishuCommandID(strings.TrimSpace(form.CommandID))
	if !ok {
		return nil
	}
	cursor := normalizeCommandCatalogFormCursor(form.Cursor, len(form.Field.Options))
	buttons := make([]map[string]any, 0, 2)
	if cursor > 0 {
		buttons = append(buttons, commandCatalogFormPageButton("上一页", form, actionKind, pageBackend, daemonLifecycleID, xutil.MaxInt(cursor-commandCatalogPaginatedSelectPageSize, 0)))
	}
	if next := cursor + commandCatalogPaginatedSelectPageSize; next < len(form.Field.Options) {
		buttons = append(buttons, commandCatalogFormPageButton("下一页", form, actionKind, pageBackend, daemonLifecycleID, next))
	}
	return cardkit.ButtonGroupElement(buttons)
}

func commandCatalogFormPageButton(label string, form control.CommandCatalogForm, actionKind control.ActionKind, pageBackend controlBackend, daemonLifecycleID string, cursor int) map[string]any {
	payload := actionPayloadPageLocalAction(string(actionKind), strings.TrimSpace(form.Field.DefaultValue))
	payload[frontstagecontract.CardActionPayloadKeyCursor] = cursor
	if resolved, ok := resolveCatalogActionForPage(control.Action{
		Kind:             actionKind,
		Text:             strings.TrimSpace(form.CommandText),
		CommandID:        strings.TrimSpace(form.CommandID),
		CatalogFamilyID:  strings.TrimSpace(form.CatalogFamilyID),
		CatalogVariantID: strings.TrimSpace(form.CatalogVariantID),
		CatalogBackend:   pageCatalogBackend(pageBackend, form.CatalogBackend),
	}, pageCatalogBackend(pageBackend, form.CatalogBackend)); ok {
		payload = actionPayloadWithCatalog(payload, resolved.FamilyID, resolved.VariantID, string(resolved.Backend))
	}
	return cardCallbackButtonElement(label, "default", stampActionValue(payload, daemonLifecycleID), false, "")
}

func normalizeCommandCatalogFormCursor(cursor, total int) int {
	if cursor < 0 {
		return 0
	}
	if total <= 0 || cursor < total {
		return cursor
	}
	lastPage := (total - 1) / commandCatalogPaginatedSelectPageSize * commandCatalogPaginatedSelectPageSize
	return xutil.MaxInt(lastPage, 0)
}

type controlBackend = agentproto.Backend

func pageButtons(buttons []control.CommandCatalogButton, pageBackend controlBackend, daemonLifecycleID string) []map[string]any {
	actions := make([]map[string]any, 0, len(buttons))
	defaultType := "default"
	if len(buttons) == 1 {
		defaultType = "primary"
	}
	for _, button := range buttons {
		label := strings.TrimSpace(button.Label)
		payload := map[string]any{}
		switch button.Kind {
		case "", control.CommandCatalogButtonAction:
			commandText := strings.TrimSpace(button.CommandText)
			if commandText == "" {
				continue
			}
			resolved, ok := control.ResolveFeishuTextCommand(control.CatalogContext{Backend: pageCatalogBackend(pageBackend, button.CatalogBackend)}, commandText)
			if !ok {
				continue
			}
			if label == "" {
				label = commandText
			}
			if shouldUseLocalPageAction(resolved.Action) {
				payload = actionPayloadPageLocalAction(string(resolved.Action.Kind), control.FeishuActionArgumentText(resolved.Action.Text))
			} else {
				payload = actionPayloadPageAction(string(resolved.Action.Kind), control.FeishuActionArgumentText(resolved.Action.Text))
				if enriched, ok := resolveCatalogActionForPage(control.Action{
					Kind:             resolved.Action.Kind,
					Text:             resolved.Action.Text,
					CommandID:        xutil.FirstNonEmpty(strings.TrimSpace(button.CommandID), strings.TrimSpace(resolved.Action.CommandID)),
					CatalogFamilyID:  xutil.FirstNonEmpty(strings.TrimSpace(button.CatalogFamilyID), strings.TrimSpace(resolved.FamilyID)),
					CatalogVariantID: xutil.FirstNonEmpty(strings.TrimSpace(button.CatalogVariantID), strings.TrimSpace(resolved.VariantID)),
					CatalogBackend:   pageCatalogBackend(pageBackend, button.CatalogBackend),
				}, pageCatalogBackend(pageBackend, button.CatalogBackend)); ok {
					payload = actionPayloadWithCatalog(payload, enriched.FamilyID, enriched.VariantID, string(enriched.Backend))
				}
			}
		case control.CommandCatalogButtonOpenURL:
			openURL := strings.TrimSpace(button.OpenURL)
			if openURL == "" {
				continue
			}
			if label == "" {
				label = openURL
			}
			buttonType := defaultType
			if style := strings.TrimSpace(button.Style); style != "" {
				buttonType = style
			}
			actions = append(actions, cardOpenURLButtonElement(label, buttonType, button.OpenURL, button.Disabled, ""))
			continue
		case control.CommandCatalogButtonCallbackAction:
			if len(button.CallbackValue) == 0 {
				continue
			}
			payload = cloneActionPayload(button.CallbackValue)
			if !isLocalPagePayload(payload) {
				if resolved, ok := resolveCatalogActionForPage(control.Action{
					CommandID:        strings.TrimSpace(button.CommandID),
					CatalogFamilyID:  strings.TrimSpace(button.CatalogFamilyID),
					CatalogVariantID: strings.TrimSpace(button.CatalogVariantID),
					CatalogBackend:   pageCatalogBackend(pageBackend, button.CatalogBackend),
				}, pageCatalogBackend(pageBackend, button.CatalogBackend)); ok {
					payload = actionPayloadWithCatalog(payload, resolved.FamilyID, resolved.VariantID, string(resolved.Backend))
				}
			}
		default:
			continue
		}
		if label == "" {
			continue
		}
		buttonType := defaultType
		if style := strings.TrimSpace(button.Style); style != "" {
			buttonType = style
		}
		actions = append(actions, cardCallbackButtonElement(label, buttonType, stampActionValue(payload, daemonLifecycleID), button.Disabled, ""))
	}
	return actions
}

func isLocalPagePayload(payload map[string]any) bool {
	switch strings.TrimSpace(fmt.Sprint(payload[frontstagecontract.CardActionPayloadKeyKind])) {
	case cardActionKindPageLocalAction, cardActionKindPageLocalSubmit:
		return true
	default:
		return false
	}
}

func pageCompactButtonElements(buttons []control.CommandCatalogButton, pageBackend controlBackend, daemonLifecycleID string) []map[string]any {
	elements := make([]map[string]any, 0, len(buttons))
	for _, button := range buttons {
		actions := pageButtons([]control.CommandCatalogButton{button}, pageCatalogBackend(pageBackend, button.CatalogBackend), daemonLifecycleID)
		if len(actions) == 0 {
			continue
		}
		if group := cardkit.ButtonGroupElement(actions); len(group) != 0 {
			elements = append(elements, group)
		}
	}
	return elements
}

func resolveCatalogActionForPage(action control.Action, backend controlBackend) (control.ResolvedCommand, bool) {
	return control.ResolveFeishuActionCatalog(control.CatalogContext{Backend: backend}, action)
}

func shouldUseLocalPageAction(action control.Action) bool {
	binding, ok := control.ResolveFeishuCommandBindingFromAction(action)
	if !ok {
		return false
	}
	switch binding.Kind {
	case control.FeishuCommandBindingDaemonCommand:
		return false
	case control.FeishuCommandBindingConfigFlow:
		_, ok := control.FeishuUIIntentFromAction(action)
		return ok
	case control.FeishuCommandBindingInlinePage,
		control.FeishuCommandBindingWorkspaceSession,
		control.FeishuCommandBindingTerminalPage,
		control.FeishuCommandBindingOwnerEntry:
		return true
	default:
		return false
	}
}

func pageCatalogBackend(pageBackend, itemBackend controlBackend) controlBackend {
	if itemBackend != "" {
		return itemBackend
	}
	return pageBackend
}

func commandCatalogFormName(form control.CommandCatalogForm) string {
	formName := strings.TrimSpace(form.CommandID)
	if formName == "" {
		formName = "command_form"
	} else {
		formName = "command_form_" + formName
	}
	if fieldName := strings.TrimSpace(form.Field.Name); fieldName != "" {
		formName += "_" + fieldName
	}
	return formName
}

func commandCatalogSubmitButtonName(formName string) string {
	formName = strings.TrimSpace(formName)
	if formName == "" {
		return "submit"
	}
	return "submit_" + formName
}

func commandCatalogFormFieldElement(field control.CommandCatalogFormField) map[string]any {
	name := strings.TrimSpace(field.Name)
	element := map[string]any{
		"tag":  "input",
		"name": name,
	}
	switch field.Kind {
	case control.CommandCatalogFormFieldSelectStatic:
		element["tag"] = "select_static"
		if placeholder := strings.TrimSpace(field.Placeholder); placeholder != "" {
			element["placeholder"] = cardkit.PlainText(placeholder)
		}
		if options := commandCatalogSelectStaticOptions(field.Options); len(options) != 0 {
			element["options"] = options
		}
		if value := strings.TrimSpace(field.DefaultValue); value != "" {
			element["initial_option"] = value
		}
	default:
		if label := strings.TrimSpace(field.Label); label != "" {
			element["label"] = map[string]any{
				"tag":     "plain_text",
				"content": label,
			}
			element["label_position"] = "left"
		}
		if placeholder := strings.TrimSpace(field.Placeholder); placeholder != "" {
			element["placeholder"] = map[string]any{
				"tag":     "plain_text",
				"content": placeholder,
			}
		}
		if value := strings.TrimSpace(field.DefaultValue); value != "" {
			element["default_value"] = value
		}
	}
	return element
}

func commandCatalogSelectStaticOptions(options []control.CommandCatalogFormFieldOption) []map[string]any {
	result := make([]map[string]any, 0, len(options))
	for _, option := range options {
		value := strings.TrimSpace(option.Value)
		label := strings.TrimSpace(option.Label)
		if value == "" || label == "" {
			continue
		}
		result = append(result, map[string]any{
			"text":  cardkit.PlainText(label),
			"value": value,
		})
	}
	return result
}

func cloneNormalizedCardSections(sections []control.FeishuCardTextSection) []control.FeishuCardTextSection {
	if len(sections) == 0 {
		return nil
	}
	return append([]control.FeishuCardTextSection(nil), sections...)
}

func commandCatalogEntryFallbackSections(entry control.CommandCatalogEntry) []control.FeishuCardTextSection {
	lines := make([]string, 0, 4)
	if title := strings.TrimSpace(entry.Title); title != "" {
		lines = append(lines, title)
	}
	if commands := formatCommandPlainText(entry.Commands); commands != "" {
		lines = append(lines, "命令："+commands)
	}
	if desc := strings.TrimSpace(entry.Description); desc != "" {
		lines = append(lines, desc)
	}
	if examples := formatExamplesPlainText(entry.Examples); examples != "" {
		lines = append(lines, "例如："+examples)
	}
	section := control.FeishuCardTextSection{Lines: lines}
	if normalized := section.Normalized(); normalized.Label != "" || len(normalized.Lines) != 0 {
		return []control.FeishuCardTextSection{normalized}
	}
	return nil
}

func splitCommandCatalogPlainTextLines(text string) []string {
	lines := []string{}
	for _, line := range strings.Split(text, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	return lines
}

func formatCommandPlainText(commands []string) string {
	parts := make([]string, 0, len(commands))
	for _, command := range commands {
		command = strings.TrimSpace(command)
		if command == "" {
			continue
		}
		parts = append(parts, command)
	}
	return strings.Join(parts, " / ")
}

func formatExamplesPlainText(examples []string) string {
	parts := make([]string, 0, len(examples))
	for _, example := range examples {
		example = strings.TrimSpace(example)
		if example == "" {
			continue
		}
		parts = append(parts, example)
	}
	return strings.Join(parts, "，")
}

func commandCatalogBreadcrumbMarkdown(view control.FeishuPageView, opts PageRenderOptions) string {
	if menuHome := menuHomeHeaderMarkdown(view, opts); menuHome != "" {
		return menuHome
	}
	parts := make([]string, 0, len(view.Breadcrumbs))
	for _, item := range view.Breadcrumbs {
		label := strings.TrimSpace(item.Label)
		if label == "" {
			continue
		}
		parts = append(parts, label)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " / ")
}

func menuHomeHeaderMarkdown(view control.FeishuPageView, opts PageRenderOptions) string {
	if strings.TrimSpace(view.CommandID) != control.FeishuCommandMenu {
		return ""
	}
	if len(view.Breadcrumbs) != 1 {
		return ""
	}
	if strings.TrimSpace(view.Breadcrumbs[0].Label) != commandCatalogRootLabel {
		return ""
	}
	version := strings.TrimSpace(opts.MenuHomeVersion)
	if version == "" {
		version = menuHomeVersionFallback
	}
	return strings.Join([]string{
		fmt.Sprintf("%s · %s", menuHomeProductName, version),
		fmt.Sprintf("GitHub: [%s](%s)", menuHomeGitHubLabel, menuHomeGitHubURL),
		fmt.Sprintf("使用说明：[查看文档](%s)", menuHomeUsageGuideURL),
	}, "\n")
}

func cloneActionPayload(payload map[string]any) map[string]any {
	if len(payload) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(payload))
	for key, value := range payload {
		cloned[key] = value
	}
	return cloned
}
