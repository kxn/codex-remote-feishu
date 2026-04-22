package projector

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func PageBody(view control.FeishuPageView) string {
	return ""
}

func PageElements(view control.FeishuPageView, daemonLifecycleID string) []map[string]any {
	view = control.NormalizeFeishuPageView(view)
	elements := make([]map[string]any, 0, len(view.Sections)*3+len(view.SummarySections)*2+len(view.NoticeSections)*2+3)
	if breadcrumb := commandCatalogBreadcrumbMarkdown(view.Breadcrumbs); breadcrumb != "" {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": breadcrumb,
		})
	}
	bodySections := control.BuildFeishuPageBodySections(view)
	if len(bodySections) != 0 {
		elements = appendCardTextSections(elements, bodySections)
	} else {
		elements = appendCardTextSections(elements, cloneNormalizedCardSections(view.SummarySections))
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
				elements = append(elements, pageCompactButtonElements(entry.Buttons, daemonLifecycleID)...)
				renderedCompactButtons = true
				if entry.Form == nil {
					continue
				}
			}
			elements = appendCardTextSections(elements, commandCatalogEntryFallbackSections(entry))
			if view.Interactive && len(entry.Buttons) > 0 && !renderedCompactButtons {
				if group := cardButtonGroupElement(pageButtons(entry.Buttons, daemonLifecycleID)); len(group) != 0 {
					elements = append(elements, group)
				}
			}
			if view.Interactive && entry.Form != nil {
				if formElement, ok := pageFormElement(*entry.Form, daemonLifecycleID); ok {
					elements = append(elements, formElement)
				}
			}
		}
		hasBusinessContent = true
	}
	if noticeSections := control.BuildFeishuPageNoticeSections(view); len(noticeSections) != 0 {
		if hasBusinessContent {
			elements = append(elements, cardDividerElement())
		}
		elements = appendCardTextSections(elements, noticeSections)
	}
	if len(view.RelatedButtons) > 0 {
		elements = appendCardFooterButtonGroup(elements, pageButtons(view.RelatedButtons, daemonLifecycleID))
	}
	return elements
}

func pageFormElement(form control.CommandCatalogForm, daemonLifecycleID string) (map[string]any, bool) {
	actionKind, ok := control.ActionKindForFeishuCommandID(strings.TrimSpace(form.CommandID))
	if !ok {
		return nil, false
	}
	field := form.Field
	formName := commandCatalogFormName(form)
	submitValue := stampActionValue(
		actionPayloadPageSubmit(
			string(actionKind),
			control.FeishuActionArgumentText(form.CommandText),
			strings.TrimSpace(field.Name),
		),
		daemonLifecycleID,
	)
	submitButton := cardFormSubmitButtonElement(firstNonEmpty(strings.TrimSpace(form.SubmitLabel), "执行"), submitValue)
	if len(submitButton) != 0 {
		submitButton["name"] = commandCatalogSubmitButtonName(formName)
	}
	return map[string]any{
		"tag":  "form",
		"name": formName,
		"elements": []map[string]any{
			commandCatalogFormFieldElement(field),
			submitButton,
		},
	}, true
}

func pageButtons(buttons []control.CommandCatalogButton, daemonLifecycleID string) []map[string]any {
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
			action, ok := control.ParseFeishuTextAction(commandText)
			if !ok {
				continue
			}
			if label == "" {
				label = commandText
			}
			payload = actionPayloadPageAction(string(action.Kind), control.FeishuActionArgumentText(action.Text))
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

func pageCompactButtonElements(buttons []control.CommandCatalogButton, daemonLifecycleID string) []map[string]any {
	elements := make([]map[string]any, 0, len(buttons))
	for _, button := range buttons {
		actions := pageButtons([]control.CommandCatalogButton{button}, daemonLifecycleID)
		if len(actions) == 0 {
			continue
		}
		if group := cardButtonGroupElement(actions); len(group) != 0 {
			elements = append(elements, group)
		}
	}
	return elements
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
			element["placeholder"] = cardPlainText(placeholder)
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
			"text":  cardPlainText(label),
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

func commandCatalogBreadcrumbMarkdown(items []control.CommandCatalogBreadcrumb) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
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
