package control

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

const modelPresetCommandFieldName = "command_args_model_preset"

func BuildFeishuCommandConfigPageView(view FeishuCatalogConfigView) FeishuPageView {
	flow, ok := ResolveFeishuConfigFlowDefinitionFromView(view)
	if !ok || flow.PageBuilder == nil {
		return FeishuPageView{}
	}
	return flow.PageBuilder(view)
}

func modePageViewFromCommandConfigView(view FeishuCatalogConfigView) FeishuPageView {
	def, _ := FeishuCommandDefinitionByID(FeishuCommandMode)
	bodySections := BuildFeishuCommandConfigBodySections(def, view)
	noticeSections := BuildFeishuCommandConfigNoticeSections(def, view)
	if view.Sealed {
		return sealedCommandPageViewForDefinition(def, view, bodySections, noticeSections)
	}
	return commandConfigPageView(def, view, bodySections, noticeSections, []CommandCatalogSection{{
		Title: "立即切换",
		Entries: []CommandCatalogEntry{{
			Buttons: fixedChoiceButtonsFromOptions(def.Options, strings.TrimSpace(view.CurrentValue), "codex"),
		}},
	}})
}

func codexProviderPageViewFromCommandConfigView(view FeishuCatalogConfigView) FeishuPageView {
	def, _ := FeishuCommandDefinitionByID(FeishuCommandCodexProvider)
	bodySections := BuildFeishuCommandConfigBodySections(def, view)
	noticeSections := BuildFeishuCommandConfigNoticeSections(def, view)
	if view.Sealed {
		return sealedCommandPageViewForDefinition(def, view, bodySections, noticeSections)
	}
	defaultValue := strings.TrimSpace(view.FormDefaultValue)
	if !commandCatalogFormOptionExists(view.FormOptions, defaultValue) {
		defaultValue = strings.TrimSpace(view.CurrentValue)
	}
	if !commandCatalogFormOptionExists(view.FormOptions, defaultValue) {
		defaultValue = ""
	}
	return commandConfigPageView(def, view, bodySections, noticeSections, []CommandCatalogSection{{
		Title: "立即切换",
		Entries: []CommandCatalogEntry{{
			Form: &CommandCatalogForm{
				CommandID:   FeishuCommandCodexProvider,
				CommandText: "/codexprovider",
				SubmitLabel: "切换",
				Field: CommandCatalogFormField{
					Name:         "command_args",
					Kind:         CommandCatalogFormFieldSelectStatic,
					Placeholder:  "选择 Codex Provider",
					DefaultValue: defaultValue,
					Options:      append([]CommandCatalogFormFieldOption(nil), view.FormOptions...),
				},
			},
		}},
	}})
}

func claudeProfilePageViewFromCommandConfigView(view FeishuCatalogConfigView) FeishuPageView {
	def, _ := FeishuCommandDefinitionByID(FeishuCommandClaudeProfile)
	bodySections := BuildFeishuCommandConfigBodySections(def, view)
	noticeSections := BuildFeishuCommandConfigNoticeSections(def, view)
	if view.Sealed {
		return sealedCommandPageViewForDefinition(def, view, bodySections, noticeSections)
	}
	defaultValue := strings.TrimSpace(view.FormDefaultValue)
	if !commandCatalogFormOptionExists(view.FormOptions, defaultValue) {
		defaultValue = strings.TrimSpace(view.CurrentValue)
	}
	if !commandCatalogFormOptionExists(view.FormOptions, defaultValue) {
		defaultValue = ""
	}
	return commandConfigPageView(def, view, bodySections, noticeSections, []CommandCatalogSection{{
		Title: "立即切换",
		Entries: []CommandCatalogEntry{{
			Form: &CommandCatalogForm{
				CommandID:   FeishuCommandClaudeProfile,
				CommandText: "/claudeprofile",
				SubmitLabel: "切换",
				Field: CommandCatalogFormField{
					Name:         "command_args",
					Kind:         CommandCatalogFormFieldSelectStatic,
					Placeholder:  "选择 Claude 配置",
					DefaultValue: defaultValue,
					Options:      append([]CommandCatalogFormFieldOption(nil), view.FormOptions...),
				},
			},
		}},
	}})
}

func autoWhipPageViewFromCommandConfigView(view FeishuCatalogConfigView) FeishuPageView {
	def, _ := FeishuCommandDefinitionByID(FeishuCommandAutoWhip)
	bodySections := BuildFeishuCommandConfigBodySections(def, view)
	noticeSections := BuildFeishuCommandConfigNoticeSections(def, view)
	if view.Sealed {
		return sealedCommandPageViewForDefinition(def, view, bodySections, noticeSections)
	}
	return commandConfigPageView(def, view, bodySections, noticeSections, []CommandCatalogSection{{
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
		return sealedCommandPageViewForDefinition(def, view, bodySections, noticeSections)
	}
	return commandConfigPageView(def, view, bodySections, noticeSections, []CommandCatalogSection{{
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
		return sealedCommandPageViewForDefinition(def, view, bodySections, noticeSections)
	}
	return commandConfigPageView(def, view, bodySections, noticeSections, []CommandCatalogSection{{
		Title: "立即应用",
		Entries: []CommandCatalogEntry{{
			Buttons: choiceButtonsFromOptions(reasoningOptionsForConfigView(view), strings.TrimSpace(view.OverrideValue), "clear"),
		}},
	}})
}

func reasoningOptionsForConfigView(view FeishuCatalogConfigView) []FeishuCommandOption {
	if len(view.FormOptions) != 0 {
		return reasoningOptionsFromFormOptions(view.FormOptions)
	}
	backend := agentproto.NormalizeBackend(view.CatalogBackend)
	return ReasoningOptionsForBackend(backend)
}

func reasoningOptionsFromFormOptions(options []CommandCatalogFormFieldOption) []FeishuCommandOption {
	out := make([]FeishuCommandOption, 0, len(options))
	for _, option := range options {
		value := strings.ToLower(strings.TrimSpace(option.Value))
		if value == "" {
			continue
		}
		label := strings.TrimSpace(option.Label)
		if label == "" {
			label = value
		}
		description := "把后续飞书消息切到 " + value + " 推理，直到 clear 或接管清理。"
		if value == "clear" {
			description = "清除飞书临时推理强度覆盖，后续使用当前模型默认。"
		}
		out = append(out, FeishuCommandOption{
			Value:       value,
			Label:       label,
			Description: description,
			CommandText: "/reasoning " + value,
			MenuKey:     "reasoning_" + value,
		})
	}
	return out
}

func accessPageViewFromCommandConfigView(view FeishuCatalogConfigView) FeishuPageView {
	def, _ := FeishuCommandDefinitionByID(FeishuCommandAccess)
	bodySections := BuildFeishuCommandConfigBodySections(def, view)
	noticeSections := BuildFeishuCommandConfigNoticeSections(def, view)
	if view.RequiresAttachment {
		return BuildFeishuAttachmentRequiredPageView(def, view)
	}
	if view.Sealed {
		return sealedCommandPageViewForDefinition(def, view, bodySections, noticeSections)
	}
	return commandConfigPageView(def, view, bodySections, noticeSections, []CommandCatalogSection{{
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
		return sealedCommandPageViewForDefinition(def, view, bodySections, noticeSections)
	}
	return commandConfigPageView(def, view, bodySections, noticeSections, []CommandCatalogSection{{
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
		return sealedCommandPageViewForDefinition(def, view, bodySections, noticeSections)
	}
	sections := []CommandCatalogSection{}
	if presetForm := modelPresetForm(view); presetForm != nil {
		sections = append(sections, CommandCatalogSection{
			Title: "可用模型",
			Entries: []CommandCatalogEntry{{
				Form: presetForm,
			}},
		})
	}
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
	return commandConfigPageView(def, view, bodySections, noticeSections, sections)
}

func verbosePageViewFromCommandConfigView(view FeishuCatalogConfigView) FeishuPageView {
	def, _ := FeishuCommandDefinitionByID(FeishuCommandVerbose)
	bodySections := BuildFeishuCommandConfigBodySections(def, view)
	noticeSections := BuildFeishuCommandConfigNoticeSections(def, view)
	if view.Sealed {
		return sealedCommandPageViewForDefinition(def, view, bodySections, noticeSections)
	}
	return commandConfigPageView(def, view, bodySections, noticeSections, []CommandCatalogSection{{
		Title: "立即切换",
		Entries: []CommandCatalogEntry{{
			Buttons: fixedChoiceButtonsFromOptions(def.Options, strings.TrimSpace(view.CurrentValue), "normal"),
		}},
	}})
}

func commandConfigPageView(def FeishuCommandDefinition, view FeishuCatalogConfigView, bodySections, noticeSections []FeishuCardTextSection, sections []CommandCatalogSection) FeishuPageView {
	familyID := strings.TrimSpace(view.CatalogFamilyID)
	if familyID == "" {
		familyID = strings.TrimSpace(def.ID)
	}
	variantID := strings.TrimSpace(view.CatalogVariantID)
	if variantID == "" {
		variantID = defaultFeishuCommandDisplayVariantID(def.ID)
	}
	sections = stampCommandSectionsCatalogProvenance(sections, familyID, variantID, view.CatalogBackend)
	return NormalizeFeishuPageView(FeishuPageView{
		CommandID:       strings.TrimSpace(def.ID),
		CatalogBackend:  view.CatalogBackend,
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

func sealedCommandPageViewForDefinition(def FeishuCommandDefinition, view FeishuCatalogConfigView, bodySections, noticeSections []FeishuCardTextSection) FeishuPageView {
	return NormalizeFeishuPageView(FeishuPageView{
		CommandID:       strings.TrimSpace(def.ID),
		CatalogBackend:  view.CatalogBackend,
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
	options := append([]CommandCatalogFormFieldOption(nil), view.FormOptions...)
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
			Label:        "从当前实例返回的列表里选择模型。",
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

func commandCatalogOptionLabel(options []CommandCatalogFormFieldOption, value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return strings.TrimSpace(fallback)
	}
	for _, option := range options {
		if strings.TrimSpace(option.Value) != value {
			continue
		}
		label := strings.TrimSpace(option.Label)
		if label != "" {
			return label
		}
	}
	return strings.TrimSpace(fallback)
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
