package feishu

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func commandCatalogBody(catalog control.FeishuDirectCommandCatalog) string {
	if len(catalog.SummarySections) != 0 {
		return ""
	}
	if !catalog.LegacySummaryMarkdown {
		return ""
	}
	return renderSystemInlineTags(strings.TrimSpace(catalog.Summary))
}

func commandCatalogElements(catalog control.FeishuDirectCommandCatalog, daemonLifecycleID string) []map[string]any {
	elements := make([]map[string]any, 0, len(catalog.Sections)*3+len(catalog.SummarySections)*2+2)
	if breadcrumb := commandCatalogBreadcrumbMarkdown(catalog.Breadcrumbs); breadcrumb != "" {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": breadcrumb,
		})
	}
	if len(catalog.SummarySections) != 0 {
		elements = appendCardTextSections(elements, catalog.SummarySections)
	} else if !catalog.LegacySummaryMarkdown {
		elements = appendCardTextSections(elements, commandCatalogSummaryFallbackSections(catalog.Summary))
	}
	for _, section := range catalog.Sections {
		title := strings.TrimSpace(section.Title)
		if title != "" {
			elements = append(elements, map[string]any{
				"tag":     "markdown",
				"content": "**" + title + "**",
			})
		}
		for _, entry := range section.Entries {
			renderedCompactButtons := false
			if catalog.DisplayStyle == control.CommandCatalogDisplayCompactButtons && catalog.Interactive && len(entry.Buttons) > 0 {
				elements = append(elements, commandCatalogCompactButtonElements(entry.Buttons, daemonLifecycleID)...)
				renderedCompactButtons = true
				if entry.Form == nil {
					continue
				}
			}
			if entry.LegacyMarkdown {
				if markdown := commandCatalogEntryMarkdown(entry); markdown != "" {
					elements = append(elements, map[string]any{
						"tag":     "markdown",
						"content": markdown,
					})
				}
			} else {
				elements = appendCardTextSections(elements, commandCatalogEntryFallbackSections(entry))
			}
			if catalog.Interactive && len(entry.Buttons) > 0 && !renderedCompactButtons {
				if group := cardButtonGroupElement(commandCatalogButtons(entry.Buttons, daemonLifecycleID)); len(group) != 0 {
					elements = append(elements, group)
				}
			}
			if catalog.Interactive && entry.Form != nil {
				elements = append(elements, commandCatalogFormElement(*entry.Form, daemonLifecycleID))
			}
		}
	}
	if len(catalog.RelatedButtons) > 0 {
		if group := cardButtonGroupElement(commandCatalogButtons(catalog.RelatedButtons, daemonLifecycleID)); len(group) != 0 {
			elements = append(elements, group)
		}
	}
	return elements
}

func commandCatalogFormElement(form control.CommandCatalogForm, daemonLifecycleID string) map[string]any {
	field := form.Field
	input := map[string]any{
		"tag":  "input",
		"name": strings.TrimSpace(field.Name),
	}
	if label := strings.TrimSpace(field.Label); label != "" {
		input["label"] = map[string]any{
			"tag":     "plain_text",
			"content": label,
		}
		input["label_position"] = "left"
	}
	if placeholder := strings.TrimSpace(field.Placeholder); placeholder != "" {
		input["placeholder"] = map[string]any{
			"tag":     "plain_text",
			"content": placeholder,
		}
	}
	if value := strings.TrimSpace(field.DefaultValue); value != "" {
		input["default_value"] = value
	}
	submitValue := stampActionValue(map[string]any{
		cardActionPayloadKeyKind:          cardActionKindSubmitCommandForm,
		cardActionPayloadKeyCommandID:     strings.TrimSpace(form.CommandID),
		cardActionPayloadKeyCommandLegacy: strings.TrimSpace(form.CommandText),
		cardActionPayloadKeyFieldName:     strings.TrimSpace(field.Name),
	}, daemonLifecycleID)
	formName := strings.TrimSpace(form.CommandID)
	if formName == "" {
		formName = "command_form"
	} else {
		formName = "command_form_" + formName
	}
	return map[string]any{
		"tag":  "form",
		"name": formName,
		"elements": []map[string]any{
			input,
			cardFormSubmitButtonElement(firstNonEmpty(strings.TrimSpace(form.SubmitLabel), "执行"), submitValue),
		},
	}
}

func commandCatalogCompactButtonElements(buttons []control.CommandCatalogButton, daemonLifecycleID string) []map[string]any {
	elements := make([]map[string]any, 0, len(buttons))
	for _, button := range buttons {
		actions := commandCatalogButtonsWithDefault([]control.CommandCatalogButton{button}, daemonLifecycleID, "default")
		if len(actions) == 0 {
			continue
		}
		if group := cardButtonGroupElement(actions); len(group) != 0 {
			elements = append(elements, group)
		}
	}
	return elements
}

func commandCatalogEntryMarkdown(entry control.CommandCatalogEntry) string {
	parts := []string{}
	if title := strings.TrimSpace(entry.Title); title != "" {
		parts = append(parts, "**"+title+"**")
	}
	if commands := formatCommandTags(entry.Commands); commands != "" {
		parts = append(parts, commands)
	}
	if desc := strings.TrimSpace(entry.Description); desc != "" {
		parts = append(parts, desc)
	}
	line := strings.Join(parts, " ")
	if examples := formatCommandExamples(entry.Examples); examples != "" {
		if line == "" {
			return "例如：" + examples
		}
		return line + "\n例如：" + examples
	}
	return line
}

func commandCatalogSummaryFallbackSections(summary string) []control.FeishuCardTextSection {
	lines := splitCommandCatalogPlainTextLines(summary)
	if len(lines) == 0 {
		return nil
	}
	return []control.FeishuCardTextSection{{
		Lines: lines,
	}}
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

func formatCommandTags(commands []string) string {
	tags := make([]string, 0, len(commands))
	for _, command := range commands {
		command = strings.TrimSpace(command)
		if command == "" {
			continue
		}
		tags = append(tags, formatCommandTextTag(command))
	}
	return strings.Join(tags, " / ")
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

func formatCommandExamples(examples []string) string {
	tags := make([]string, 0, len(examples))
	for _, example := range examples {
		example = strings.TrimSpace(example)
		if example == "" {
			continue
		}
		tags = append(tags, formatCommandTextTag(example))
	}
	return strings.Join(tags, "，")
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

func commandCatalogButtons(buttons []control.CommandCatalogButton, daemonLifecycleID string) []map[string]any {
	return commandCatalogButtonsWithDefault(buttons, daemonLifecycleID, "")
}

func commandCatalogButtonsWithDefault(buttons []control.CommandCatalogButton, daemonLifecycleID, defaultTypeOverride string) []map[string]any {
	actions := make([]map[string]any, 0, len(buttons))
	defaultType := "default"
	if defaultTypeOverride != "" {
		defaultType = defaultTypeOverride
	} else if len(buttons) == 1 {
		defaultType = "primary"
	}
	for _, button := range buttons {
		label := strings.TrimSpace(button.Label)
		payload := map[string]any{}
		switch button.Kind {
		case "", control.CommandCatalogButtonRunCommand:
			commandText := strings.TrimSpace(button.CommandText)
			if commandText == "" {
				continue
			}
			if label == "" {
				label = commandText
			}
			payload = actionPayloadRunCommand(commandText)
		case control.CommandCatalogButtonOpenURL:
			openURL := strings.TrimSpace(button.OpenURL)
			if openURL == "" {
				continue
			}
			if label == "" {
				label = openURL
			}
		case control.CommandCatalogButtonCallbackAction:
			if len(button.CallbackValue) == 0 {
				continue
			}
			payload = cloneActionPayload(button.CallbackValue)
		case control.CommandCatalogButtonStartCommandCapture:
			commandID := strings.TrimSpace(button.CommandID)
			if commandID == "" {
				continue
			}
			payload = actionPayloadStartCommandCapture(commandID)
		case control.CommandCatalogButtonCancelCommandCapture:
			commandID := strings.TrimSpace(button.CommandID)
			if commandID == "" {
				continue
			}
			payload = actionPayloadCancelCommandCapture(commandID)
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
		if button.Kind == control.CommandCatalogButtonOpenURL {
			actions = append(actions, cardOpenURLButtonElement(label, buttonType, button.OpenURL, button.Disabled, ""))
			continue
		}
		actions = append(actions, cardCallbackButtonElement(label, buttonType, stampActionValue(payload, daemonLifecycleID), button.Disabled, ""))
	}
	return actions
}

func cloneActionPayload(value map[string]any) map[string]any {
	if len(value) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(value))
	for key, item := range value {
		cloned[key] = item
	}
	return cloned
}
