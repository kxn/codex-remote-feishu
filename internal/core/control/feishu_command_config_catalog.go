package control

import "strings"

var commonFeishuModelValues = []string{
	"gpt-5.4",
	"gpt-5.4-mini",
	"gpt-5.3-codex",
	"gpt-5.2",
	"gpt-5.2-codex",
}

const modelPresetCommandFieldName = "command_args_model_preset"

func BuildFeishuCommandConfigCatalog(view FeishuCommandConfigView) FeishuDirectCommandCatalog {
	switch strings.TrimSpace(view.CommandID) {
	case FeishuCommandMode:
		return modeCatalogFromCommandConfigView(view)
	case FeishuCommandAutoContinue:
		return autoContinueCatalogFromCommandConfigView(view)
	case FeishuCommandReasoning:
		return reasoningCatalogFromCommandConfigView(view)
	case FeishuCommandAccess:
		return accessCatalogFromCommandConfigView(view)
	case FeishuCommandModel:
		return modelCatalogFromCommandConfigView(view)
	case FeishuCommandVerbose:
		return verboseCatalogFromCommandConfigView(view)
	default:
		return FeishuDirectCommandCatalog{}
	}
}

func modeCatalogFromCommandConfigView(view FeishuCommandConfigView) FeishuDirectCommandCatalog {
	def, _ := FeishuCommandDefinitionByID(FeishuCommandMode)
	summarySections := BuildFeishuCommandConfigSummarySections(def, view)
	if view.Sealed {
		return sealedCommandCatalogForDefinition(def, summarySections)
	}
	return commandConfigCatalog(def, summarySections, []CommandCatalogSection{{
		Title: "立即切换",
		Entries: []CommandCatalogEntry{{
			Buttons: fixedChoiceButtonsFromOptions(def.Options, strings.TrimSpace(view.CurrentValue), "normal"),
		}},
	}})
}

func autoContinueCatalogFromCommandConfigView(view FeishuCommandConfigView) FeishuDirectCommandCatalog {
	def, _ := FeishuCommandDefinitionByID(FeishuCommandAutoContinue)
	summarySections := BuildFeishuCommandConfigSummarySections(def, view)
	if view.Sealed {
		return sealedCommandCatalogForDefinition(def, summarySections)
	}
	return commandConfigCatalog(def, summarySections, []CommandCatalogSection{{
		Title: "立即切换",
		Entries: []CommandCatalogEntry{{
			Buttons: fixedChoiceButtonsFromOptions(def.Options, strings.TrimSpace(view.CurrentValue), "on"),
		}},
	}})
}

func reasoningCatalogFromCommandConfigView(view FeishuCommandConfigView) FeishuDirectCommandCatalog {
	def, _ := FeishuCommandDefinitionByID(FeishuCommandReasoning)
	summarySections := BuildFeishuCommandConfigSummarySections(def, view)
	if view.RequiresAttachment {
		return BuildFeishuAttachmentRequiredCatalog(def, view)
	}
	if view.Sealed {
		return sealedCommandCatalogForDefinition(def, summarySections)
	}
	return commandConfigCatalog(def, summarySections, []CommandCatalogSection{{
		Title: "立即应用",
		Entries: []CommandCatalogEntry{{
			Buttons: choiceButtonsFromOptions(def.Options, strings.TrimSpace(view.OverrideValue), ""),
		}},
	}})
}

func accessCatalogFromCommandConfigView(view FeishuCommandConfigView) FeishuDirectCommandCatalog {
	def, _ := FeishuCommandDefinitionByID(FeishuCommandAccess)
	summarySections := BuildFeishuCommandConfigSummarySections(def, view)
	if view.RequiresAttachment {
		return BuildFeishuAttachmentRequiredCatalog(def, view)
	}
	if view.Sealed {
		return sealedCommandCatalogForDefinition(def, summarySections)
	}
	return commandConfigCatalog(def, summarySections, []CommandCatalogSection{{
		Title: "立即应用",
		Entries: []CommandCatalogEntry{{
			Buttons: choiceButtonsFromOptions(def.Options, strings.TrimSpace(view.OverrideValue), ""),
		}},
	}})
}

func modelCatalogFromCommandConfigView(view FeishuCommandConfigView) FeishuDirectCommandCatalog {
	def, _ := FeishuCommandDefinitionByID(FeishuCommandModel)
	summarySections := BuildFeishuCommandConfigSummarySections(def, view)
	if view.RequiresAttachment {
		return BuildFeishuAttachmentRequiredCatalog(def, view)
	}
	if view.Sealed {
		return sealedCommandCatalogForDefinition(def, summarySections)
	}
	sections := []CommandCatalogSection{{
		Title: "常见模型",
		Entries: []CommandCatalogEntry{{
			Form: modelPresetForm(view),
		}},
	}}
	manualEntry := CommandCatalogEntry{
		Form: FeishuCommandFormWithDefault(FeishuCommandModel, strings.TrimSpace(view.FormDefaultValue)),
	}
	if strings.TrimSpace(view.OverrideValue) != "" || strings.TrimSpace(view.OverrideExtraValue) != "" {
		manualEntry.Buttons = append(manualEntry.Buttons, choiceCommandButton("清除覆盖", "/model clear", false, ""))
	}
	sections = append(sections, CommandCatalogSection{
		Title:   "手动输入",
		Entries: []CommandCatalogEntry{manualEntry},
	})
	return commandConfigCatalog(def, summarySections, sections)
}

func verboseCatalogFromCommandConfigView(view FeishuCommandConfigView) FeishuDirectCommandCatalog {
	def, _ := FeishuCommandDefinitionByID(FeishuCommandVerbose)
	summarySections := BuildFeishuCommandConfigSummarySections(def, view)
	if view.Sealed {
		return sealedCommandCatalogForDefinition(def, summarySections)
	}
	return commandConfigCatalog(def, summarySections, []CommandCatalogSection{{
		Title: "立即切换",
		Entries: []CommandCatalogEntry{{
			Buttons: fixedChoiceButtonsFromOptions(def.Options, strings.TrimSpace(view.CurrentValue), "normal"),
		}},
	}})
}

func commandConfigCatalog(def FeishuCommandDefinition, summarySections []FeishuCardTextSection, sections []CommandCatalogSection) FeishuDirectCommandCatalog {
	return FeishuDirectCommandCatalog{
		Title:           def.Title,
		SummarySections: append([]FeishuCardTextSection(nil), summarySections...),
		Interactive:     true,
		DisplayStyle:    CommandCatalogDisplayCompactButtons,
		Breadcrumbs:     FeishuCommandBreadcrumbs(def.GroupID, def.Title),
		Sections:        sections,
		RelatedButtons:  FeishuCommandBackButtons(def.GroupID),
	}
}

func sealedCommandCatalogForDefinition(def FeishuCommandDefinition, summarySections []FeishuCardTextSection) FeishuDirectCommandCatalog {
	return FeishuDirectCommandCatalog{
		Title:           def.Title,
		SummarySections: append([]FeishuCardTextSection(nil), summarySections...),
		Interactive:     false,
		DisplayStyle:    CommandCatalogDisplayCompactButtons,
		Breadcrumbs:     FeishuCommandBreadcrumbs(def.GroupID, def.Title),
	}
}

func modelPresetForm(view FeishuCommandConfigView) *CommandCatalogForm {
	options := make([]CommandCatalogFormFieldOption, 0, len(commonFeishuModelValues))
	for _, model := range commonFeishuModelValues {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		options = append(options, CommandCatalogFormFieldOption{
			Label: model,
			Value: model,
		})
	}
	if len(options) == 0 {
		return nil
	}
	defaultValue := strings.TrimSpace(view.OverrideValue)
	if !commandCatalogFormOptionExists(options, defaultValue) {
		defaultValue = ""
	}
	return &CommandCatalogForm{
		CommandID:   FeishuCommandModel,
		CommandText: "/model",
		SubmitLabel: "应用",
		Field: CommandCatalogFormField{
			Name:         modelPresetCommandFieldName,
			Kind:         CommandCatalogFormFieldSelectStatic,
			Label:        "从下拉里选择常见模型。",
			Placeholder:  "选择模型",
			DefaultValue: defaultValue,
			Options:      options,
		},
	}
}

func commandCatalogFormOptionExists(options []CommandCatalogFormFieldOption, value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for _, option := range options {
		if strings.TrimSpace(option.Value) == value {
			return true
		}
	}
	return false
}

func choiceCommandButton(label, commandText string, disabled bool, style string) CommandCatalogButton {
	return CommandCatalogButton{
		Label:       label,
		Kind:        CommandCatalogButtonRunCommand,
		CommandText: commandText,
		Style:       style,
		Disabled:    disabled,
	}
}

func choiceButtonsFromOptions(options []FeishuCommandOption, currentOverride, primaryValue string) []CommandCatalogButton {
	buttons := make([]CommandCatalogButton, 0, len(options))
	currentOverride = strings.TrimSpace(currentOverride)
	for _, option := range options {
		value := strings.TrimSpace(option.Value)
		if value == "" {
			continue
		}
		style := ""
		if value == primaryValue {
			style = "primary"
		}
		disabled := false
		switch value {
		case "clear":
			disabled = currentOverride == ""
		default:
			disabled = currentOverride != "" && currentOverride == value
		}
		label := strings.TrimSpace(option.Label)
		if disabled && value != "clear" {
			label += "（当前）"
			style = "primary"
		}
		buttons = append(buttons, CommandCatalogButton{
			Label:       label,
			Kind:        CommandCatalogButtonRunCommand,
			CommandText: strings.TrimSpace(option.CommandText),
			Style:       style,
			Disabled:    disabled,
		})
	}
	return buttons
}

func fixedChoiceButtonsFromOptions(options []FeishuCommandOption, currentValue, primaryValue string) []CommandCatalogButton {
	buttons := make([]CommandCatalogButton, 0, len(options))
	currentValue = strings.TrimSpace(currentValue)
	for _, option := range options {
		value := strings.TrimSpace(option.Value)
		if value == "" {
			continue
		}
		style := ""
		if value == primaryValue {
			style = "primary"
		}
		buttons = append(buttons, CommandCatalogButton{
			Label:       strings.TrimSpace(option.Label),
			Kind:        CommandCatalogButtonRunCommand,
			CommandText: strings.TrimSpace(option.CommandText),
			Style:       style,
			Disabled:    currentValue != "" && currentValue == value,
		})
	}
	return buttons
}
