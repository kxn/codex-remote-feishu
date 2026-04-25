package control

import "strings"

var commonFeishuModelValues = []string{
	"gpt-5.5",
	"gpt-5.4",
	"gpt-5.4-mini",
	"gpt-5.3-codex",
	"gpt-5.2",
	"gpt-5.2-codex",
}

const modelPresetCommandFieldName = "command_args_model_preset"

func BuildFeishuCommandConfigPageView(view FeishuCatalogConfigView) FeishuPageView {
	switch strings.TrimSpace(view.CommandID) {
	case FeishuCommandMode:
		return modePageViewFromCommandConfigView(view)
	case FeishuCommandAutoWhip:
		return autoWhipPageViewFromCommandConfigView(view)
	case FeishuCommandAutoContinue:
		return autoContinuePageViewFromCommandConfigView(view)
	case FeishuCommandReasoning:
		return reasoningPageViewFromCommandConfigView(view)
	case FeishuCommandAccess:
		return accessPageViewFromCommandConfigView(view)
	case FeishuCommandPlan:
		return planPageViewFromCommandConfigView(view)
	case FeishuCommandModel:
		return modelPageViewFromCommandConfigView(view)
	case FeishuCommandVerbose:
		return verbosePageViewFromCommandConfigView(view)
	default:
		return FeishuPageView{}
	}
}

func modePageViewFromCommandConfigView(view FeishuCatalogConfigView) FeishuPageView {
	def, _ := FeishuCommandDefinitionByID(FeishuCommandMode)
	bodySections := BuildFeishuCommandConfigBodySections(def, view)
	noticeSections := BuildFeishuCommandConfigNoticeSections(def, view)
	if view.Sealed {
		return sealedCommandPageViewForDefinition(def, bodySections, noticeSections)
	}
	return commandConfigPageView(def, bodySections, noticeSections, []CommandCatalogSection{{
		Title: "立即切换",
		Entries: []CommandCatalogEntry{{
			Buttons: fixedChoiceButtonsFromOptions(def.Options, strings.TrimSpace(view.CurrentValue), "normal"),
		}},
	}})
}

func autoWhipPageViewFromCommandConfigView(view FeishuCatalogConfigView) FeishuPageView {
	def, _ := FeishuCommandDefinitionByID(FeishuCommandAutoWhip)
	bodySections := BuildFeishuCommandConfigBodySections(def, view)
	noticeSections := BuildFeishuCommandConfigNoticeSections(def, view)
	if view.Sealed {
		return sealedCommandPageViewForDefinition(def, bodySections, noticeSections)
	}
	return commandConfigPageView(def, bodySections, noticeSections, []CommandCatalogSection{{
		Title: "立即切换",
		Entries: []CommandCatalogEntry{{
			Buttons: fixedChoiceButtonsFromOptions(def.Options, strings.TrimSpace(view.CurrentValue), "on"),
		}},
	}})
}

func autoContinuePageViewFromCommandConfigView(view FeishuCatalogConfigView) FeishuPageView {
	def, _ := FeishuCommandDefinitionByID(FeishuCommandAutoContinue)
	bodySections := BuildFeishuCommandConfigBodySections(def, view)
	noticeSections := BuildFeishuCommandConfigNoticeSections(def, view)
	if view.Sealed {
		return sealedCommandPageViewForDefinition(def, bodySections, noticeSections)
	}
	return commandConfigPageView(def, bodySections, noticeSections, []CommandCatalogSection{{
		Title: "立即切换",
		Entries: []CommandCatalogEntry{{
			Buttons: fixedChoiceButtonsFromOptions(def.Options, strings.TrimSpace(view.CurrentValue), "on"),
		}},
	}})
}

func reasoningPageViewFromCommandConfigView(view FeishuCatalogConfigView) FeishuPageView {
	def, _ := FeishuCommandDefinitionByID(FeishuCommandReasoning)
	bodySections := BuildFeishuCommandConfigBodySections(def, view)
	noticeSections := BuildFeishuCommandConfigNoticeSections(def, view)
	if view.RequiresAttachment {
		return BuildFeishuAttachmentRequiredPageView(def, view)
	}
	if view.Sealed {
		return sealedCommandPageViewForDefinition(def, bodySections, noticeSections)
	}
	return commandConfigPageView(def, bodySections, noticeSections, []CommandCatalogSection{{
		Title: "立即应用",
		Entries: []CommandCatalogEntry{{
			Buttons: choiceButtonsFromOptions(def.Options, strings.TrimSpace(view.OverrideValue), ""),
		}},
	}})
}

func accessPageViewFromCommandConfigView(view FeishuCatalogConfigView) FeishuPageView {
	def, _ := FeishuCommandDefinitionByID(FeishuCommandAccess)
	bodySections := BuildFeishuCommandConfigBodySections(def, view)
	noticeSections := BuildFeishuCommandConfigNoticeSections(def, view)
	if view.RequiresAttachment {
		return BuildFeishuAttachmentRequiredPageView(def, view)
	}
	if view.Sealed {
		return sealedCommandPageViewForDefinition(def, bodySections, noticeSections)
	}
	return commandConfigPageView(def, bodySections, noticeSections, []CommandCatalogSection{{
		Title: "立即应用",
		Entries: []CommandCatalogEntry{{
			Buttons: choiceButtonsFromOptions(def.Options, strings.TrimSpace(view.OverrideValue), ""),
		}},
	}})
}

func planPageViewFromCommandConfigView(view FeishuCatalogConfigView) FeishuPageView {
	def, _ := FeishuCommandDefinitionByID(FeishuCommandPlan)
	bodySections := BuildFeishuCommandConfigBodySections(def, view)
	noticeSections := BuildFeishuCommandConfigNoticeSections(def, view)
	if view.Sealed {
		return sealedCommandPageViewForDefinition(def, bodySections, noticeSections)
	}
	return commandConfigPageView(def, bodySections, noticeSections, []CommandCatalogSection{{
		Title: "立即切换",
		Entries: []CommandCatalogEntry{{
			Buttons: fixedChoiceButtonsFromOptions(def.Options, strings.TrimSpace(view.CurrentValue), "on"),
		}},
	}})
}

func modelPageViewFromCommandConfigView(view FeishuCatalogConfigView) FeishuPageView {
	def, _ := FeishuCommandDefinitionByID(FeishuCommandModel)
	bodySections := BuildFeishuCommandConfigBodySections(def, view)
	noticeSections := BuildFeishuCommandConfigNoticeSections(def, view)
	if view.RequiresAttachment {
		return BuildFeishuAttachmentRequiredPageView(def, view)
	}
	if view.Sealed {
		return sealedCommandPageViewForDefinition(def, bodySections, noticeSections)
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
	return commandConfigPageView(def, bodySections, noticeSections, sections)
}

func verbosePageViewFromCommandConfigView(view FeishuCatalogConfigView) FeishuPageView {
	def, _ := FeishuCommandDefinitionByID(FeishuCommandVerbose)
	bodySections := BuildFeishuCommandConfigBodySections(def, view)
	noticeSections := BuildFeishuCommandConfigNoticeSections(def, view)
	if view.Sealed {
		return sealedCommandPageViewForDefinition(def, bodySections, noticeSections)
	}
	return commandConfigPageView(def, bodySections, noticeSections, []CommandCatalogSection{{
		Title: "立即切换",
		Entries: []CommandCatalogEntry{{
			Buttons: fixedChoiceButtonsFromOptions(def.Options, strings.TrimSpace(view.CurrentValue), "normal"),
		}},
	}})
}

func commandConfigPageView(def FeishuCommandDefinition, bodySections, noticeSections []FeishuCardTextSection, sections []CommandCatalogSection) FeishuPageView {
	return NormalizeFeishuPageView(FeishuPageView{
		CommandID:       strings.TrimSpace(def.ID),
		Title:           def.Title,
		SummarySections: append([]FeishuCardTextSection(nil), bodySections...),
		BodySections:    append([]FeishuCardTextSection(nil), bodySections...),
		NoticeSections:  append([]FeishuCardTextSection(nil), noticeSections...),
		Interactive:     true,
		DisplayStyle:    CommandCatalogDisplayCompactButtons,
		Breadcrumbs:     FeishuCommandBreadcrumbs(def.GroupID, def.Title),
		Sections:        sections,
		RelatedButtons:  FeishuCommandBackButtons(def.GroupID),
	})
}

func sealedCommandPageViewForDefinition(def FeishuCommandDefinition, bodySections, noticeSections []FeishuCardTextSection) FeishuPageView {
	return NormalizeFeishuPageView(FeishuPageView{
		CommandID:       strings.TrimSpace(def.ID),
		Title:           def.Title,
		SummarySections: append([]FeishuCardTextSection(nil), bodySections...),
		BodySections:    append([]FeishuCardTextSection(nil), bodySections...),
		NoticeSections:  append([]FeishuCardTextSection(nil), noticeSections...),
		Interactive:     false,
		Sealed:          true,
		DisplayStyle:    CommandCatalogDisplayCompactButtons,
		Breadcrumbs:     FeishuCommandBreadcrumbs(def.GroupID, def.Title),
	})
}

func modelPresetForm(view FeishuCatalogConfigView) *CommandCatalogForm {
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
		Kind:        CommandCatalogButtonAction,
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
			Kind:        CommandCatalogButtonAction,
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
			Kind:        CommandCatalogButtonAction,
			CommandText: strings.TrimSpace(option.CommandText),
			Style:       style,
			Disabled:    currentValue != "" && currentValue == value,
		})
	}
	return buttons
}
