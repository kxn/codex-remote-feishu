package orchestrator

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type commandMenuStage string

const (
	commandMenuStageDetached      commandMenuStage = commandMenuStage(control.FeishuCommandMenuStageDetached)
	commandMenuStageNormalWorking commandMenuStage = commandMenuStage(control.FeishuCommandMenuStageNormalWorking)
	commandMenuStageVSCodeWorking commandMenuStage = commandMenuStage(control.FeishuCommandMenuStageVSCodeWorking)
)

func (s *Service) buildCommandMenuCatalog(surface *state.SurfaceConsoleRecord, raw string) control.FeishuDirectCommandCatalog {
	return s.commandCatalogFromView(surface, s.buildCommandMenuView(surface, raw))
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

func (s *Service) buildCommandMenuHomeCatalog(surface *state.SurfaceConsoleRecord) control.FeishuDirectCommandCatalog {
	sections := []control.CommandCatalogSection{{
		Title:   "全部分组",
		Entries: s.commandMenuGroupEntries(),
	}}
	return control.FeishuDirectCommandCatalog{
		Title:        "命令菜单",
		Interactive:  true,
		DisplayStyle: control.CommandCatalogDisplayCompactButtons,
		Sections:     sections,
	}
}

func (s *Service) buildCommandHelpCatalog(surface *state.SurfaceConsoleRecord) control.FeishuDirectCommandCatalog {
	return control.BuildFeishuCommandCatalogForDisplay(
		"Slash 命令帮助",
		"以下是当前主展示的 canonical slash command。历史 alias 仍可兼容，但不再作为新的主展示入口。",
		false,
		string(s.normalizeSurfaceProductMode(surface)),
		"",
	)
}

func (s *Service) buildCommandMenuGroupCatalog(surface *state.SurfaceConsoleRecord, stage commandMenuStage, groupID string) control.FeishuDirectCommandCatalog {
	group, ok := control.FeishuCommandGroupByID(groupID)
	if !ok {
		return s.buildCommandMenuHomeCatalog(surface)
	}
	entries := make([]control.CommandCatalogEntry, 0, 6)
	productMode := string(s.normalizeSurfaceProductMode(surface))
	for _, def := range control.FeishuCommandDefinitionsForGroup(groupID) {
		def, ok := control.FeishuCommandDefinitionForDisplay(def, productMode, true, string(stage))
		if !ok {
			continue
		}
		entries = append(entries, commandEntryForDefinition(def))
	}
	return control.FeishuDirectCommandCatalog{
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

func (s *Service) buildModeCatalog(surface *state.SurfaceConsoleRecord) control.FeishuDirectCommandCatalog {
	return s.commandCatalogFromView(surface, s.buildModeCommandView(surface))
}

func (s *Service) buildAutoContinueCatalog(surface *state.SurfaceConsoleRecord) control.FeishuDirectCommandCatalog {
	return s.commandCatalogFromView(surface, s.buildAutoContinueCommandView(surface))
}

func (s *Service) buildReasoningCatalog(surface *state.SurfaceConsoleRecord) control.FeishuDirectCommandCatalog {
	return s.commandCatalogFromView(surface, s.buildReasoningCommandView(surface))
}

func (s *Service) buildAccessCatalog(surface *state.SurfaceConsoleRecord) control.FeishuDirectCommandCatalog {
	return s.commandCatalogFromView(surface, s.buildAccessCommandView(surface))
}

func (s *Service) buildModelCatalog(surface *state.SurfaceConsoleRecord) control.FeishuDirectCommandCatalog {
	return s.commandCatalogFromView(surface, s.buildModelCommandView(surface))
}

func (s *Service) buildAttachmentRequiredCatalog(surface *state.SurfaceConsoleRecord, def control.FeishuCommandDefinition) control.FeishuDirectCommandCatalog {
	return control.FeishuDirectCommandCatalog{
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
		clearSurfaceCommandCapture(surface)
		return []control.UIEvent{s.commandViewEvent(surface, s.buildModelCommandView(surface))}
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
		return []control.UIEvent{s.commandViewEvent(surface, s.buildModelCommandView(surface))}
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
