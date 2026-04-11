package orchestrator

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type commandMenuStage string

const (
	commandMenuStageDetached      commandMenuStage = "detached"
	commandMenuStageNormalWorking commandMenuStage = "normal_working"
	commandMenuStageVSCodeWorking commandMenuStage = "vscode_working"
)

func (s *Service) buildCommandMenuCatalog(surface *state.SurfaceConsoleRecord, raw string) control.CommandCatalog {
	stage := s.commandMenuStage(surface)
	switch parseCommandMenuView(raw) {
	case control.FeishuCommandGroupCurrentWork:
		return s.buildCommandMenuGroupCatalog(surface, stage, control.FeishuCommandGroupCurrentWork)
	case control.FeishuCommandGroupSendSettings:
		return s.buildCommandMenuGroupCatalog(surface, stage, control.FeishuCommandGroupSendSettings)
	case control.FeishuCommandGroupSwitchTarget:
		return s.buildCommandMenuGroupCatalog(surface, stage, control.FeishuCommandGroupSwitchTarget)
	case control.FeishuCommandGroupMaintenance:
		return s.buildCommandMenuGroupCatalog(surface, stage, control.FeishuCommandGroupMaintenance)
	default:
		return s.buildCommandMenuHomeCatalog(surface, stage)
	}
}

func parseCommandMenuView(raw string) string {
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) < 2 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(fields[1]))
}

func (s *Service) commandMenuStage(surface *state.SurfaceConsoleRecord) commandMenuStage {
	if surface == nil || strings.TrimSpace(surface.AttachedInstanceID) == "" {
		return commandMenuStageDetached
	}
	if s.normalizeSurfaceProductMode(surface) == state.ProductModeVSCode {
		return commandMenuStageVSCodeWorking
	}
	return commandMenuStageNormalWorking
}

func (s *Service) buildCommandMenuHomeCatalog(surface *state.SurfaceConsoleRecord, stage commandMenuStage) control.CommandCatalog {
	sections := []control.CommandCatalogSection{
		{
			Title:   "全部分组",
			Entries: s.commandMenuGroupEntries(),
		},
		{
			Title:   "常用操作",
			Entries: s.commandMenuHomeEntries(stage),
		},
	}
	return control.CommandCatalog{
		Title:        "命令菜单",
		Interactive:  true,
		DisplayStyle: control.CommandCatalogDisplayCompactButtons,
		Sections:     sections,
	}
}

func (s *Service) buildCommandMenuGroupCatalog(surface *state.SurfaceConsoleRecord, stage commandMenuStage, groupID string) control.CommandCatalog {
	group, ok := control.FeishuCommandGroupByID(groupID)
	if !ok {
		return s.buildCommandMenuHomeCatalog(surface, stage)
	}
	entries := make([]control.CommandCatalogEntry, 0, 6)
	for _, def := range control.FeishuCommandDefinitionsForGroup(groupID) {
		if !def.ShowInMenu {
			continue
		}
		if def.ID == control.FeishuCommandFollow && stage != commandMenuStageVSCodeWorking {
			continue
		}
		entries = append(entries, commandEntryForDefinition(def))
	}
	return control.CommandCatalog{
		Title:        "命令菜单",
		Interactive:  true,
		DisplayStyle: control.CommandCatalogDisplayCompactButtons,
		Breadcrumbs: []control.CommandCatalogBreadcrumb{
			{Label: "菜单首页"},
			{Label: group.Title},
		},
		Sections: []control.CommandCatalogSection{{
			Title:   group.Title,
			Entries: entries,
		}},
		RelatedButtons: []control.CommandCatalogButton{{
			Label:       "返回上一层",
			Kind:        control.CommandCatalogButtonRunCommand,
			CommandText: menuCommandText(""),
		}},
	}
}

func (s *Service) commandMenuHomeEntries(stage commandMenuStage) []control.CommandCatalogEntry {
	commandIDs := []string{control.FeishuCommandList, control.FeishuCommandUse, control.FeishuCommandStatus}
	switch stage {
	case commandMenuStageNormalWorking:
		commandIDs = []string{
			control.FeishuCommandStop,
			control.FeishuCommandNew,
			control.FeishuCommandReasoning,
			control.FeishuCommandModel,
			control.FeishuCommandAccess,
		}
	case commandMenuStageVSCodeWorking:
		commandIDs = []string{
			control.FeishuCommandStop,
			control.FeishuCommandReasoning,
			control.FeishuCommandModel,
			control.FeishuCommandAccess,
			control.FeishuCommandFollow,
		}
	}
	entries := make([]control.CommandCatalogEntry, 0, len(commandIDs))
	for _, commandID := range commandIDs {
		def, ok := control.FeishuCommandDefinitionByID(commandID)
		if !ok {
			continue
		}
		entries = append(entries, commandEntryForDefinition(def))
	}
	return entries
}

func (s *Service) commandMenuGroupEntries() []control.CommandCatalogEntry {
	entries := make([]control.CommandCatalogEntry, 0, len(control.FeishuCommandGroups()))
	for _, group := range control.FeishuCommandGroups() {
		entries = append(entries, control.CommandCatalogEntry{
			Title:       group.Title,
			Description: group.Description,
			Buttons: []control.CommandCatalogButton{{
				Label:       submenuButtonLabel(group.Title),
				Kind:        control.CommandCatalogButtonRunCommand,
				CommandText: menuCommandText(group.ID),
			}},
		})
	}
	return entries
}

func commandEntryForDefinition(def control.FeishuCommandDefinition) control.CommandCatalogEntry {
	return control.CommandCatalogEntry{
		Title:       strings.TrimSpace(def.Title),
		Commands:    []string{def.CanonicalSlash},
		Description: strings.TrimSpace(def.Description),
		Examples:    append([]string(nil), def.Examples...),
		Buttons: []control.CommandCatalogButton{{
			Label:       commandMenuButtonLabel(def),
			Kind:        control.CommandCatalogButtonRunCommand,
			CommandText: def.CanonicalSlash,
		}},
	}
}

func commandMenuButtonLabel(def control.FeishuCommandDefinition) string {
	title := strings.TrimSpace(def.Title)
	command := strings.TrimSpace(def.CanonicalSlash)
	switch {
	case title == "":
		return command
	case command == "":
		return title
	default:
		return title + " " + command
	}
}

func submenuButtonLabel(label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return "打开子菜单"
	}
	return label + " ›"
}

func menuCommandText(view string) string {
	if strings.TrimSpace(view) == "" {
		return "/menu"
	}
	return "/menu " + strings.TrimSpace(view)
}

func commandBackButtons(groupID string) []control.CommandCatalogButton {
	if group, ok := control.FeishuCommandGroupByID(groupID); ok {
		return []control.CommandCatalogButton{{
			Label:       "返回" + group.Title,
			Kind:        control.CommandCatalogButtonRunCommand,
			CommandText: menuCommandText(groupID),
		}}
	}
	return nil
}

func (s *Service) buildModeCatalog(surface *state.SurfaceConsoleRecord) control.CommandCatalog {
	current := s.normalizeSurfaceProductMode(surface)
	sections := []control.CommandCatalogSection{{
		Title: "立即切换",
		Entries: []control.CommandCatalogEntry{{
			Buttons: []control.CommandCatalogButton{
				choiceCommandButton("normal", "/mode normal", current == state.ProductModeNormal, "primary"),
				choiceCommandButton("vscode", "/mode vscode", current == state.ProductModeVSCode, ""),
			},
		}},
	}}
	if form := commandCatalogForm(control.FeishuCommandMode, ""); form != nil {
		sections = append(sections, control.CommandCatalogSection{
			Title: "手动输入",
			Entries: []control.CommandCatalogEntry{{
				Form: form,
			}},
		})
	}
	return control.CommandCatalog{
		Title:          "切换模式",
		Summary:        fmt.Sprintf("当前模式：`%s`。", current),
		Interactive:    true,
		DisplayStyle:   control.CommandCatalogDisplayCompactButtons,
		Breadcrumbs:    commandBreadcrumbs(control.FeishuCommandGroupMaintenance, "切换模式"),
		Sections:       sections,
		RelatedButtons: commandBackButtons(control.FeishuCommandGroupMaintenance),
	}
}

func (s *Service) buildAutoContinueCatalog(surface *state.SurfaceConsoleRecord) control.CommandCatalog {
	enabled := surface != nil && surface.AutoContinue.Enabled
	statusText := "关闭"
	if enabled {
		statusText = "开启"
	}
	sections := []control.CommandCatalogSection{{
		Title: "立即切换",
		Entries: []control.CommandCatalogEntry{{
			Buttons: []control.CommandCatalogButton{
				choiceCommandButton("on", "/autowhip on", enabled, "primary"),
				choiceCommandButton("off", "/autowhip off", !enabled, ""),
			},
		}},
	}}
	if form := commandCatalogForm(control.FeishuCommandAutoContinue, ""); form != nil {
		sections = append(sections, control.CommandCatalogSection{
			Title: "手动输入",
			Entries: []control.CommandCatalogEntry{{
				Form: form,
			}},
		})
	}
	return control.CommandCatalog{
		Title:          "autowhip",
		Summary:        fmt.Sprintf("当前：`%s`。", statusText),
		Interactive:    true,
		DisplayStyle:   control.CommandCatalogDisplayCompactButtons,
		Breadcrumbs:    commandBreadcrumbs(control.FeishuCommandGroupMaintenance, "autowhip"),
		Sections:       sections,
		RelatedButtons: commandBackButtons(control.FeishuCommandGroupMaintenance),
	}
}

func (s *Service) buildReasoningCatalog(surface *state.SurfaceConsoleRecord) control.CommandCatalog {
	def, _ := control.FeishuCommandDefinitionByID(control.FeishuCommandReasoning)
	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil {
		return s.buildAttachmentRequiredCatalog(surface, def)
	}
	summary := s.resolveNextPromptSummary(inst, surface, "", "", state.ModelConfigRecord{})
	sections := []control.CommandCatalogSection{{
		Title: "立即应用",
		Entries: []control.CommandCatalogEntry{{
			Buttons: choiceButtonsFromOptions(def.Options, summary.OverrideReasoningEffort, ""),
		}},
	}}
	if form := commandCatalogForm(control.FeishuCommandReasoning, ""); form != nil {
		sections = append(sections, control.CommandCatalogSection{
			Title: "手动输入",
			Entries: []control.CommandCatalogEntry{{
				Form: form,
			}},
		})
	}
	return control.CommandCatalog{
		Title:          def.Title,
		Summary:        reasoningCatalogSummary(summary),
		Interactive:    true,
		DisplayStyle:   control.CommandCatalogDisplayCompactButtons,
		Breadcrumbs:    commandBreadcrumbs(def.GroupID, def.Title),
		Sections:       sections,
		RelatedButtons: commandBackButtons(def.GroupID),
	}
}

func (s *Service) buildAccessCatalog(surface *state.SurfaceConsoleRecord) control.CommandCatalog {
	def, _ := control.FeishuCommandDefinitionByID(control.FeishuCommandAccess)
	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil {
		return s.buildAttachmentRequiredCatalog(surface, def)
	}
	summary := s.resolveNextPromptSummary(inst, surface, "", "", state.ModelConfigRecord{})
	sections := []control.CommandCatalogSection{{
		Title: "立即应用",
		Entries: []control.CommandCatalogEntry{{
			Buttons: choiceButtonsFromOptions(def.Options, summary.OverrideAccessMode, ""),
		}},
	}}
	if form := commandCatalogForm(control.FeishuCommandAccess, ""); form != nil {
		sections = append(sections, control.CommandCatalogSection{
			Title: "手动输入",
			Entries: []control.CommandCatalogEntry{{
				Form: form,
			}},
		})
	}
	return control.CommandCatalog{
		Title:          def.Title,
		Summary:        accessCatalogSummary(summary),
		Interactive:    true,
		DisplayStyle:   control.CommandCatalogDisplayCompactButtons,
		Breadcrumbs:    commandBreadcrumbs(def.GroupID, def.Title),
		Sections:       sections,
		RelatedButtons: commandBackButtons(def.GroupID),
	}
}

func (s *Service) buildModelCatalog(surface *state.SurfaceConsoleRecord) control.CommandCatalog {
	def, _ := control.FeishuCommandDefinitionByID(control.FeishuCommandModel)
	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil {
		return s.buildAttachmentRequiredCatalog(surface, def)
	}
	summary := s.resolveNextPromptSummary(inst, surface, "", "", state.ModelConfigRecord{})
	presetButtons := []control.CommandCatalogButton{
		choiceCommandButton("gpt-5.4", "/model gpt-5.4", summary.OverrideModel == "gpt-5.4", ""),
		choiceCommandButton("gpt-5.4-mini", "/model gpt-5.4-mini", summary.OverrideModel == "gpt-5.4-mini", ""),
	}
	manualEntry := control.CommandCatalogEntry{
		Form: commandCatalogForm(control.FeishuCommandModel, ""),
	}
	if strings.TrimSpace(summary.OverrideModel) != "" || strings.TrimSpace(summary.OverrideReasoningEffort) != "" {
		manualEntry.Buttons = append(manualEntry.Buttons, choiceCommandButton("清除覆盖", "/model clear", false, ""))
	}
	return control.CommandCatalog{
		Title:        def.Title,
		Summary:      modelCatalogSummary(summary),
		Interactive:  true,
		DisplayStyle: control.CommandCatalogDisplayCompactButtons,
		Breadcrumbs:  commandBreadcrumbs(def.GroupID, def.Title),
		Sections: []control.CommandCatalogSection{
			{
				Title: "常见模型",
				Entries: []control.CommandCatalogEntry{{
					Buttons: presetButtons,
				}},
			},
			{
				Title:   "手动输入",
				Entries: []control.CommandCatalogEntry{manualEntry},
			},
		},
		RelatedButtons: commandBackButtons(def.GroupID),
	}
}

func (s *Service) buildAttachmentRequiredCatalog(surface *state.SurfaceConsoleRecord, def control.FeishuCommandDefinition) control.CommandCatalog {
	return control.CommandCatalog{
		Title:        def.Title,
		Summary:      "还没接管目标。先开始或继续工作，再回来调整这个参数。",
		Interactive:  true,
		DisplayStyle: control.CommandCatalogDisplayCompactButtons,
		Breadcrumbs:  commandBreadcrumbs(def.GroupID, def.Title),
		Sections: []control.CommandCatalogSection{{
			Title: "开始 / 继续工作",
			Entries: []control.CommandCatalogEntry{
				recoveryEntry(control.FeishuCommandList),
				recoveryEntry(control.FeishuCommandUse),
				recoveryEntry(control.FeishuCommandStatus),
			},
		}},
		RelatedButtons: commandBackButtons(def.GroupID),
	}
}

func recoveryEntry(commandID string) control.CommandCatalogEntry {
	def, ok := control.FeishuCommandDefinitionByID(commandID)
	if !ok {
		return control.CommandCatalogEntry{}
	}
	return control.CommandCatalogEntry{
		Title:       def.Title,
		Commands:    []string{def.CanonicalSlash},
		Description: def.Description,
		Buttons: []control.CommandCatalogButton{{
			Label:       commandMenuButtonLabel(def),
			Kind:        control.CommandCatalogButtonRunCommand,
			CommandText: def.CanonicalSlash,
		}},
	}
}

func commandCatalogForm(commandID, defaultValue string) *control.CommandCatalogForm {
	form, ok := control.FeishuCommandForm(commandID)
	if !ok || form == nil {
		return nil
	}
	cloned := *form
	cloned.Field = form.Field
	cloned.Field.DefaultValue = strings.TrimSpace(defaultValue)
	return &cloned
}

func commandBreadcrumbs(groupID, title string) []control.CommandCatalogBreadcrumb {
	breadcrumbs := []control.CommandCatalogBreadcrumb{{Label: "菜单首页"}}
	if group, ok := control.FeishuCommandGroupByID(groupID); ok {
		breadcrumbs = append(breadcrumbs, control.CommandCatalogBreadcrumb{Label: group.Title})
	}
	if strings.TrimSpace(title) != "" {
		breadcrumbs = append(breadcrumbs, control.CommandCatalogBreadcrumb{Label: title})
	}
	return breadcrumbs
}

func choiceCommandButton(label, commandText string, disabled bool, style string) control.CommandCatalogButton {
	return control.CommandCatalogButton{
		Label:       label,
		Kind:        control.CommandCatalogButtonRunCommand,
		CommandText: commandText,
		Style:       style,
		Disabled:    disabled,
	}
}

func choiceButtonsFromOptions(options []control.FeishuCommandOption, currentOverride, primaryValue string) []control.CommandCatalogButton {
	buttons := make([]control.CommandCatalogButton, 0, len(options))
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
		buttons = append(buttons, control.CommandCatalogButton{
			Label:       label,
			Kind:        control.CommandCatalogButtonRunCommand,
			CommandText: option.CommandText,
			Style:       style,
			Disabled:    disabled,
		})
	}
	return buttons
}

func reasoningCatalogSummary(summary control.PromptRouteSummary) string {
	return fmt.Sprintf(
		"当前：`%s`；飞书覆盖：`%s`。",
		displayPromptValue(summary.EffectiveReasoningEffort, "未设置"),
		displayPromptValue(summary.OverrideReasoningEffort, "无"),
	)
}

func accessCatalogSummary(summary control.PromptRouteSummary) string {
	return fmt.Sprintf(
		"当前：`%s`；飞书覆盖：`%s`。",
		displayPromptValue(summary.EffectiveAccessMode, "未设置"),
		displayPromptValue(summary.OverrideAccessMode, "无"),
	)
}

func modelCatalogSummary(summary control.PromptRouteSummary) string {
	lines := []string{
		fmt.Sprintf("当前模型：`%s`；飞书覆盖：`%s`。", displayPromptValue(summary.EffectiveModel, "未设置"), displayPromptValue(summary.OverrideModel, "无")),
	}
	if strings.TrimSpace(summary.OverrideReasoningEffort) != "" {
		lines = append(lines, fmt.Sprintf("附带推理覆盖：`%s`。", summary.OverrideReasoningEffort))
	}
	return strings.Join(lines, "\n")
}

func displayPromptValue(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func (s *Service) startCommandCapture(surface *state.SurfaceConsoleRecord, action control.Action) []control.UIEvent {
	if surface == nil {
		return nil
	}
	if surface.ActiveRequestCapture != nil {
		return notice(surface, "request_capture_waiting_text", "当前正在等待你发送一条文字处理意见，请先发送文本或重新处理确认卡片。")
	}
	if pending := activePendingRequest(surface); pending != nil {
		return notice(surface, "request_pending", pendingRequestNoticeText(activePendingRequest(surface)))
	}
	switch action.CommandID {
	case control.FeishuCommandModel:
		if s.root.Instances[surface.AttachedInstanceID] == nil {
			def, _ := control.FeishuCommandDefinitionByID(control.FeishuCommandModel)
			return []control.UIEvent{commandCatalogEvent(surface, s.buildAttachmentRequiredCatalog(surface, def))}
		}
		clearSurfaceCommandCapture(surface)
		return []control.UIEvent{commandCatalogEvent(surface, s.buildModelCatalog(surface))}
	default:
		return notice(surface, "command_capture_unsupported", "这个命令暂不支持 capture/apply 输入。")
	}
}

func (s *Service) cancelCommandCapture(surface *state.SurfaceConsoleRecord, action control.Action) []control.UIEvent {
	if surface == nil {
		return nil
	}
	clearSurfaceCommandCapture(surface)
	switch action.CommandID {
	case control.FeishuCommandModel:
		return []control.UIEvent{commandCatalogEvent(surface, s.buildModelCatalog(surface))}
	default:
		return nil
	}
}

func (s *Service) consumeCapturedCommandInput(surface *state.SurfaceConsoleRecord, text string) []control.UIEvent {
	if surface == nil {
		return nil
	}
	capture := surface.ActiveCommandCapture
	if commandCaptureExpired(s.now(), capture) {
		clearSurfaceCommandCapture(surface)
		return notice(surface, "command_capture_expired", "上一条命令输入已过期，请重新打开 `/model` 卡片后再提交。")
	}
	clearSurfaceCommandCapture(surface)
	switch capture.CommandID {
	case control.FeishuCommandModel:
		text = strings.TrimSpace(text)
		if text == "" {
			return notice(surface, "command_capture_empty", "没有收到可用输入，请重新打开模型卡片后提交。")
		}
		return s.handleModelCommand(surface, control.Action{
			Kind:             control.ActionModelCommand,
			GatewayID:        surface.GatewayID,
			SurfaceSessionID: surface.SurfaceSessionID,
			ChatID:           surface.ChatID,
			ActorUserID:      surface.ActorUserID,
			Text:             "/model " + text,
		})
	default:
		return notice(surface, "command_capture_unsupported", "当前命令输入已失效，请重新打开命令卡片。")
	}
}
