package orchestrator

import (
	"fmt"
	"strings"
	"time"

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
			Title:   "现在最常用",
			Entries: s.commandMenuHomeEntries(stage),
		},
		{
			Title:   "浏览分组",
			Entries: s.commandMenuGroupEntries(),
		},
	}
	return control.CommandCatalog{
		Title:       "命令菜单",
		Summary:     s.commandMenuHomeSummary(surface, stage),
		Interactive: true,
		Sections:    sections,
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
		Title:       "命令菜单",
		Summary:     fmt.Sprintf("当前阶段：%s。%s", s.commandMenuStageLabel(stage), group.Description),
		Interactive: true,
		Breadcrumbs: []control.CommandCatalogBreadcrumb{
			{Label: "菜单首页"},
			{Label: group.Title},
		},
		Sections: []control.CommandCatalogSection{{
			Title:   group.Title,
			Entries: entries,
		}},
		RelatedButtons: []control.CommandCatalogButton{{
			Label:       "返回首页",
			Kind:        control.CommandCatalogButtonRunCommand,
			CommandText: "/menu",
		}},
	}
}

func (s *Service) commandMenuHomeSummary(surface *state.SurfaceConsoleRecord, stage commandMenuStage) string {
	switch stage {
	case commandMenuStageDetached:
		if s.normalizeSurfaceProductMode(surface) == state.ProductModeVSCode {
			return "当前还没接管任何实例或会话。先用 `/list`、`/use`、`/status` 让路由重新就绪。"
		}
		return "当前还没接管任何工作区或会话。先用 `/list`、`/use`、`/status` 开始或继续工作。"
	case commandMenuStageVSCodeWorking:
		return "当前处于 vscode 工作态。首页优先展示 `/stop` 和发送设置，`/follow` 仅作为相关动作保留。"
	default:
		return "当前处于 normal 工作态。首页优先展示 `/stop`、`/new`，以及常用发送设置。"
	}
}

func (s *Service) commandMenuStageLabel(stage commandMenuStage) string {
	switch stage {
	case commandMenuStageDetached:
		return "未接管"
	case commandMenuStageVSCodeWorking:
		return "vscode 工作中"
	default:
		return "normal 工作中"
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
				Label:       "打开",
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
			Label:       menuActionButtonLabel(def),
			Kind:        control.CommandCatalogButtonRunCommand,
			CommandText: def.CanonicalSlash,
		}},
	}
}

func menuActionButtonLabel(def control.FeishuCommandDefinition) string {
	switch def.ArgumentKind {
	case control.FeishuCommandArgumentChoice, control.FeishuCommandArgumentText:
		return "打开"
	default:
		return strings.TrimSpace(def.Title)
	}
}

func menuCommandText(view string) string {
	if strings.TrimSpace(view) == "" {
		return "/menu"
	}
	return "/menu " + strings.TrimSpace(view)
}

func commandBackButtons(groupID string) []control.CommandCatalogButton {
	buttons := []control.CommandCatalogButton{
		{
			Label:       "返回首页",
			Kind:        control.CommandCatalogButtonRunCommand,
			CommandText: "/menu",
		},
	}
	if group, ok := control.FeishuCommandGroupByID(groupID); ok {
		buttons = append([]control.CommandCatalogButton{{
			Label:       "返回" + group.Title,
			Kind:        control.CommandCatalogButtonRunCommand,
			CommandText: menuCommandText(groupID),
		}}, buttons...)
	}
	return buttons
}

func (s *Service) buildModeCatalog(surface *state.SurfaceConsoleRecord) control.CommandCatalog {
	current := s.normalizeSurfaceProductMode(surface)
	return control.CommandCatalog{
		Title:       "切换模式",
		Summary:     fmt.Sprintf("当前模式：`%s`。切换前会校验当前是否还有运行中的 turn、派发中的请求或排队消息。", current),
		Interactive: true,
		Breadcrumbs: commandBreadcrumbs(control.FeishuCommandGroupMaintenance, "切换模式"),
		Sections: []control.CommandCatalogSection{{
			Title: "可选模式",
			Entries: []control.CommandCatalogEntry{{
				Title:       "normal / vscode",
				Description: "点击后立即尝试切换。",
				Buttons: []control.CommandCatalogButton{
					choiceCommandButton("normal", "/mode normal", current == state.ProductModeNormal, "primary"),
					choiceCommandButton("vscode", "/mode vscode", current == state.ProductModeVSCode, ""),
				},
			}},
		}},
		RelatedButtons: commandBackButtons(control.FeishuCommandGroupMaintenance),
	}
}

func (s *Service) buildAutoContinueCatalog(surface *state.SurfaceConsoleRecord) control.CommandCatalog {
	enabled := surface != nil && surface.AutoContinue.Enabled
	statusText := "关闭"
	if enabled {
		statusText = "开启"
	}
	return control.CommandCatalog{
		Title:       "自动续跑",
		Summary:     fmt.Sprintf("当前 auto-continue：`%s`。这个开关只作用于当前飞书会话，daemon 重启后会恢复为关闭。", statusText),
		Interactive: true,
		Breadcrumbs: commandBreadcrumbs(control.FeishuCommandGroupMaintenance, "自动续跑"),
		Sections: []control.CommandCatalogSection{{
			Title: "可选状态",
			Entries: []control.CommandCatalogEntry{{
				Title:       "on / off",
				Description: "点击后立即切换当前飞书会话的 auto-continue。",
				Buttons: []control.CommandCatalogButton{
					choiceCommandButton("on", "/autocontinue on", enabled, "primary"),
					choiceCommandButton("off", "/autocontinue off", !enabled, ""),
				},
			}},
		}},
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
	return control.CommandCatalog{
		Title:       def.Title,
		Summary:     reasoningCatalogSummary(summary),
		Interactive: true,
		Breadcrumbs: commandBreadcrumbs(def.GroupID, def.Title),
		Sections: []control.CommandCatalogSection{{
			Title: "可选参数",
			Entries: []control.CommandCatalogEntry{{
				Title:       "点击即应用",
				Description: "只作用于后续从飞书发出的消息；`clear` 用于清除飞书侧 override。",
				Buttons:     choiceButtonsFromOptions(def.Options, summary.OverrideReasoningEffort, ""),
			}},
		}},
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
	return control.CommandCatalog{
		Title:       def.Title,
		Summary:     accessCatalogSummary(summary),
		Interactive: true,
		Breadcrumbs: commandBreadcrumbs(def.GroupID, def.Title),
		Sections: []control.CommandCatalogSection{{
			Title: "可选参数",
			Entries: []control.CommandCatalogEntry{{
				Title:       "点击即应用",
				Description: "`clear` 会恢复飞书默认执行权限。",
				Buttons:     choiceButtonsFromOptions(def.Options, summary.OverrideAccessMode, ""),
			}},
		}},
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
	manualButtons := []control.CommandCatalogButton{{
		Label:     "开始输入",
		Kind:      control.CommandCatalogButtonStartCommandCapture,
		CommandID: control.FeishuCommandModel,
		Style:     "primary",
	}}
	if strings.TrimSpace(summary.OverrideModel) != "" || strings.TrimSpace(summary.OverrideReasoningEffort) != "" {
		manualButtons = append(manualButtons, choiceCommandButton("clear", "/model clear", false, ""))
	}
	return control.CommandCatalog{
		Title:       def.Title,
		Summary:     modelCatalogSummary(summary),
		Interactive: true,
		Breadcrumbs: commandBreadcrumbs(def.GroupID, def.Title),
		Sections: []control.CommandCatalogSection{
			{
				Title: "常见模型",
				Entries: []control.CommandCatalogEntry{{
					Title:       "直达示例",
					Description: "如果你已经知道完整命令或完整 menu key，可以直接发送；这里保留两个常见直达示例。",
					Examples:    []string{"/model gpt-5.4", "/model gpt-5.4-mini"},
					Buttons:     presetButtons,
				}},
			},
			{
				Title: "手动输入",
				Entries: []control.CommandCatalogEntry{{
					Title:       "capture/apply fallback",
					Description: "点击“开始输入”后，下一条普通文本会先被捕获为模型名，再给你一张确认卡片，不会直接生效。",
					Buttons:     manualButtons,
				}},
			},
		},
		RelatedButtons: commandBackButtons(def.GroupID),
	}
}

func (s *Service) buildModelCaptureWaitingCatalog(surface *state.SurfaceConsoleRecord) control.CommandCatalog {
	def, _ := control.FeishuCommandDefinitionByID(control.FeishuCommandModel)
	summary := s.notAttachedText(surface)
	if inst := s.root.Instances[surface.AttachedInstanceID]; inst != nil {
		summary = modelCatalogSummary(s.resolveNextPromptSummary(inst, surface, "", "", state.ModelConfigRecord{}))
	}
	return control.CommandCatalog{
		Title:       def.Title,
		Summary:     summary + "\n\n接下来一条普通文本会先被捕获为模型名；收到后会再给你一张确认卡片。",
		Interactive: true,
		Breadcrumbs: commandBreadcrumbs(def.GroupID, def.Title),
		Sections: []control.CommandCatalogSection{{
			Title: "等待输入",
			Entries: []control.CommandCatalogEntry{{
				Title:       "发送一条普通文本",
				Description: "例如直接发送 `gpt-5.4` 或 `gpt-5.4 high`。如果你改主意了，可以直接取消或发送其他 slash command 退出这次捕获。",
				Buttons: []control.CommandCatalogButton{{
					Label:     "取消",
					Kind:      control.CommandCatalogButtonCancelCommandCapture,
					CommandID: control.FeishuCommandModel,
				}},
			}},
		}},
		RelatedButtons: commandBackButtons(def.GroupID),
	}
}

func (s *Service) buildModelApplyCatalog(surface *state.SurfaceConsoleRecord, captured string) control.CommandCatalog {
	def, _ := control.FeishuCommandDefinitionByID(control.FeishuCommandModel)
	captured = strings.TrimSpace(captured)
	summary := fmt.Sprintf("已捕获模型输入：`%s`。点击 Apply 后才会更新当前飞书临时模型覆盖。", captured)
	if inst := s.root.Instances[surface.AttachedInstanceID]; inst != nil {
		summary = modelCatalogSummary(s.resolveNextPromptSummary(inst, surface, "", "", state.ModelConfigRecord{})) + "\n\n" + summary
	}
	return control.CommandCatalog{
		Title:       def.Title,
		Summary:     summary,
		Interactive: true,
		Breadcrumbs: commandBreadcrumbs(def.GroupID, def.Title),
		Sections: []control.CommandCatalogSection{{
			Title: "待应用内容",
			Entries: []control.CommandCatalogEntry{{
				Title:       "确认后生效",
				Commands:    []string{"/model " + captured},
				Description: "如果你希望重新输入，先点“重新输入”；如果不想应用，点“取消”。",
				Buttons: []control.CommandCatalogButton{
					choiceCommandButton("Apply", "/model "+captured, false, "primary"),
					{
						Label:     "重新输入",
						Kind:      control.CommandCatalogButtonStartCommandCapture,
						CommandID: control.FeishuCommandModel,
					},
					{
						Label:     "取消",
						Kind:      control.CommandCatalogButtonCancelCommandCapture,
						CommandID: control.FeishuCommandModel,
					},
				},
			}},
		}},
		RelatedButtons: commandBackButtons(def.GroupID),
	}
}

func (s *Service) buildAttachmentRequiredCatalog(surface *state.SurfaceConsoleRecord, def control.FeishuCommandDefinition) control.CommandCatalog {
	return control.CommandCatalog{
		Title:       def.Title,
		Summary:     s.notAttachedText(surface) + "\n\n这类发送前覆盖会在接管目标后才有意义。先让路由进入可工作状态，再回来调整参数。",
		Interactive: true,
		Breadcrumbs: commandBreadcrumbs(def.GroupID, def.Title),
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
			Label:       def.Title,
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
		buttons = append(buttons, control.CommandCatalogButton{
			Label:       option.Label,
			Kind:        control.CommandCatalogButtonRunCommand,
			CommandText: option.CommandText,
			Style:       style,
			Disabled:    disabled,
		})
	}
	return buttons
}

func reasoningCatalogSummary(summary control.PromptRouteSummary) string {
	lines := []string{
		fmt.Sprintf("当前 effective 推理强度：`%s`。", displayPromptValue(summary.EffectiveReasoningEffort, "未设置")),
		fmt.Sprintf("当前飞书 override：`%s`。", displayPromptValue(summary.OverrideReasoningEffort, "无")),
	}
	if strings.TrimSpace(summary.BaseReasoningEffort) != "" {
		lines = append(lines, fmt.Sprintf("底层默认：`%s`（来源：%s）。", summary.BaseReasoningEffort, displayPromptValue(summary.BaseReasoningEffortSource, "unknown")))
	}
	return strings.Join(lines, "\n")
}

func accessCatalogSummary(summary control.PromptRouteSummary) string {
	lines := []string{
		fmt.Sprintf("当前 effective 执行权限：`%s`。", displayPromptValue(summary.EffectiveAccessMode, "未设置")),
		fmt.Sprintf("当前飞书 override：`%s`。", displayPromptValue(summary.OverrideAccessMode, "无")),
	}
	if strings.TrimSpace(summary.EffectiveAccessModeSource) != "" {
		lines = append(lines, fmt.Sprintf("当前 effective 来源：%s。", summary.EffectiveAccessModeSource))
	}
	return strings.Join(lines, "\n")
}

func modelCatalogSummary(summary control.PromptRouteSummary) string {
	lines := []string{
		fmt.Sprintf("当前 effective 模型：`%s`。", displayPromptValue(summary.EffectiveModel, "未设置")),
		fmt.Sprintf("当前飞书 override 模型：`%s`。", displayPromptValue(summary.OverrideModel, "无")),
	}
	if strings.TrimSpace(summary.OverrideReasoningEffort) != "" {
		lines = append(lines, fmt.Sprintf("当前飞书 override 推理强度：`%s`。`/model clear` 会一并清除这里的附带推理覆盖。", summary.OverrideReasoningEffort))
	}
	if strings.TrimSpace(summary.BaseModel) != "" {
		lines = append(lines, fmt.Sprintf("底层默认模型：`%s`（来源：%s）。", summary.BaseModel, displayPromptValue(summary.BaseModelSource, "unknown")))
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
		return notice(surface, "request_pending", "当前有待确认请求。请先点击卡片上的“允许一次”、“拒绝”或“告诉 Codex 怎么改”。")
	}
	switch action.CommandID {
	case control.FeishuCommandModel:
		if s.root.Instances[surface.AttachedInstanceID] == nil {
			def, _ := control.FeishuCommandDefinitionByID(control.FeishuCommandModel)
			return []control.UIEvent{commandCatalogEvent(surface, s.buildAttachmentRequiredCatalog(surface, def))}
		}
		surface.ActiveCommandCapture = &state.CommandCaptureRecord{
			CommandID: control.FeishuCommandModel,
			CreatedAt: s.now(),
			ExpiresAt: s.now().Add(10 * time.Minute),
		}
		return []control.UIEvent{commandCatalogEvent(surface, s.buildModelCaptureWaitingCatalog(surface))}
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
		return notice(surface, "command_capture_expired", "上一条命令输入已过期，请重新点击卡片按钮后再发送文本。")
	}
	clearSurfaceCommandCapture(surface)
	switch capture.CommandID {
	case control.FeishuCommandModel:
		return []control.UIEvent{commandCatalogEvent(surface, s.buildModelApplyCatalog(surface, text))}
	default:
		return notice(surface, "command_capture_unsupported", "当前命令输入已失效，请重新打开命令卡片。")
	}
}
