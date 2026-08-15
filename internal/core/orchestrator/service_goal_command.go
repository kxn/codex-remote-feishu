package orchestrator

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type goalUserCommand struct {
	SurfaceID string
	ThreadID  string
	Action    string
	Budget    *int64
}

func (s *Service) handleGoalCommand(surface *state.SurfaceConsoleRecord, action control.Action) []eventcontract.Event {
	if !s.surfaceIsHeadless(surface) || s.surfaceBackend(surface) != agentproto.BackendCodex {
		return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
			StatusKind: "error",
			StatusText: "Goal 只支持 Codex headless 模式，请先 `/mode codex`。",
		})
	}
	threadID := strings.TrimSpace(surface.SelectedThreadID)
	if threadID == "" {
		return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
			StatusKind: "error",
			StatusText: "当前没有精确选中的会话，无法操作 Goal。请先 `/list` 或 `/use` 选中一个 Codex 会话。",
		})
	}
	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil || !inst.Online {
		return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
			StatusKind: "error",
			StatusText: "当前实例不可用，暂时无法操作 Goal。",
		})
	}
	if thread := inst.Threads[threadID]; thread != nil && (threadIsReview(thread) || thread.TrafficClass == agentproto.TrafficClassInternalHelper) {
		return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
			StatusKind: "error",
			StatusText: "当前会话是审阅/辅助会话，不支持 Goal。",
		})
	}

	parts := strings.Fields(strings.TrimSpace(action.Text))
	sub := ""
	if len(parts) > 1 {
		sub = strings.ToLower(parts[1])
	}
	switch sub {
	case "new", "edit":
		objective, budget := parseGoalObjectiveAndBudget(parts)
		if objective == "" {
			return []eventcontract.Event{s.pageEvent(surface, s.goalCommandFormPage(surface, sub))}
		}
		return s.issueGoalCommand(surface, threadID, "set", goalSetCommand(threadID, objective, budget, "user_control"))
	case "pause":
		return s.issueGoalCommand(surface, threadID, "pause", goalStatusCommand(threadID, "paused", "user_control"))
	case "resume":
		return s.issueGoalCommand(surface, threadID, "resume", goalStatusCommand(threadID, "active", "user_control"))
	case "clear":
		confirm := false
		for _, part := range parts[2:] {
			if strings.TrimSpace(part) == "--confirm" {
				confirm = true
			}
		}
		if !confirm {
			return []eventcontract.Event{s.pageEvent(surface, s.goalClearConfirmPage(surface))}
		}
		return s.issueGoalCommand(surface, threadID, "clear", goalClearCommand(threadID, "user_control"))
	default:
		return s.issueGoalCommand(surface, threadID, "get", goalGetCommand(threadID, "user_control"))
	}
}

func parseGoalObjectiveAndBudget(parts []string) (string, *int64) {
	var budget *int64
	objectiveParts := make([]string, 0, len(parts))
	for index := 2; index < len(parts); index++ {
		part := strings.TrimSpace(parts[index])
		if part == "--budget" && index+1 < len(parts) {
			if value, err := strconv.ParseInt(strings.TrimSpace(parts[index+1]), 10, 64); err == nil {
				budget = &value
			}
			index++
			continue
		}
		if strings.HasPrefix(part, "--budget=") {
			if value, err := strconv.ParseInt(strings.TrimPrefix(part, "--budget="), 10, 64); err == nil {
				budget = &value
			}
			continue
		}
		objectiveParts = append(objectiveParts, part)
	}
	return strings.Join(objectiveParts, " "), budget
}

func goalSetCommand(threadID, objective string, budget *int64, purpose string) *agentproto.Command {
	command := &agentproto.Command{
		Kind:   agentproto.CommandThreadGoalSet,
		Target: agentproto.Target{ThreadID: threadID},
		Goal: agentproto.GoalCommand{
			Objective: objective,
			Purpose:   purpose,
		},
	}
	if budget != nil {
		command.Goal.TokenBudget = budget
	}
	return command
}

func goalStatusCommand(threadID, status, purpose string) *agentproto.Command {
	return &agentproto.Command{
		Kind:   agentproto.CommandThreadGoalSet,
		Target: agentproto.Target{ThreadID: threadID},
		Goal: agentproto.GoalCommand{
			Status:  status,
			Purpose: purpose,
		},
	}
}

func goalClearCommand(threadID, purpose string) *agentproto.Command {
	return &agentproto.Command{
		Kind:   agentproto.CommandThreadGoalClear,
		Target: agentproto.Target{ThreadID: threadID},
		Goal: agentproto.GoalCommand{
			Purpose: purpose,
		},
	}
}

func goalGetCommand(threadID, purpose string) *agentproto.Command {
	return &agentproto.Command{
		Kind:   agentproto.CommandThreadGoalGet,
		Target: agentproto.Target{ThreadID: threadID},
		Goal: agentproto.GoalCommand{
			Purpose: purpose,
		},
	}
}

func (s *Service) issueGoalCommand(surface *state.SurfaceConsoleRecord, threadID, actionName string, command *agentproto.Command) []eventcontract.Event {
	commandID := s.nextAgentCommandID()
	command.CommandID = commandID
	if s.goalUserCommands == nil {
		s.goalUserCommands = map[string]goalUserCommand{}
	}
	s.goalUserCommands[commandID] = goalUserCommand{
		SurfaceID: surface.SurfaceSessionID,
		ThreadID:  threadID,
		Action:    actionName,
	}
	events := []eventcontract.Event{{
		Kind:             eventcontract.KindAgentCommand,
		SurfaceSessionID: surface.SurfaceSessionID,
		Command:          command,
	}}
	if actionName != "get" {
		events = append(events, s.pageEvent(surface, goalLoadingPage(surface)))
	}
	return events
}

func (s *Service) applyGoalUserCommandResult(instanceID string, event agentproto.Event) []eventcontract.Event {
	if s.goalUserCommands == nil {
		return nil
	}
	userCommand, ok := s.goalUserCommands[event.CommandID]
	if !ok {
		return nil
	}
	delete(s.goalUserCommands, event.CommandID)
	surface := s.root.Surfaces[userCommand.SurfaceID]
	if surface == nil {
		return nil
	}
	if event.ErrorMessage != "" {
		return []eventcontract.Event{s.pageEvent(surface, goalErrorPage(surface, "Goal 操作失败", event.ErrorMessage))}
	}
	switch userCommand.Action {
	case "clear":
		return []eventcontract.Event{s.pageEvent(surface, goalErrorPage(surface, "Goal 已清除", ""))}
	default:
		return []eventcontract.Event{s.pageEvent(surface, goalStatusPage(surface, event.ThreadGoal))}
	}
}

func goalLoadingPage(surface *state.SurfaceConsoleRecord) control.FeishuPageView {
	return control.FeishuPageView{
		PageID:      control.FeishuCommandGoal,
		CommandID:   control.FeishuCommandGoal,
		Title:       "Goal",
		StatusText:  "正在处理…",
		Interactive: true,
	}
}

func (s *Service) goalCommandFormPage(surface *state.SurfaceConsoleRecord, sub string) control.FeishuPageView {
	title := "创建 Goal"
	submitLabel := "创建"
	commandText := "/goal new"
	if sub == "edit" {
		title = "编辑 Goal"
		submitLabel = "更新"
		commandText = "/goal edit"
	}
	defaultText := ""
	if sub == "edit" {
		if goal := s.surfaceGoalForPage(surface); goal != nil && !goal.Cleared {
			defaultText = strings.TrimSpace(goal.Objective)
			if goal.TokenBudget != nil {
				defaultText += fmt.Sprintf(" --budget %d", *goal.TokenBudget)
			}
		}
	}
	return control.NormalizeFeishuPageView(control.FeishuPageView{
		PageID:      control.FeishuCommandGoal,
		CommandID:   control.FeishuCommandGoal,
		Title:       title,
		StatusText:  "输入目标描述，可选 `--budget <tokens>` 限制预算。",
		Interactive: true,
		ThemeKey:    "primary",
		Sections: []control.CommandCatalogSection{{
			Title: "目标",
			Entries: []control.CommandCatalogEntry{{
				Form: &control.CommandCatalogForm{
					CommandID:   control.FeishuCommandGoal,
					CommandText: commandText,
					SubmitLabel: submitLabel,
					Field: control.CommandCatalogFormField{
						Name:         "command_args",
						Kind:         control.CommandCatalogFormFieldText,
						Label:        "目标描述",
						Placeholder:  "例如：完成登录流程重构",
						DefaultValue: defaultText,
					},
				},
			}},
		}},
		RelatedButtons: []control.CommandCatalogButton{
			goalCommandButton("取消", "/goal"),
		},
	})
}

func goalErrorPage(surface *state.SurfaceConsoleRecord, title, text string) control.FeishuPageView {
	page := goalStatusPage(surface, nil)
	page.Title = title
	if text != "" {
		page.StatusText = text
	}
	page.StatusKind = "error"
	return page
}

func goalStatusPage(surface *state.SurfaceConsoleRecord, goal *agentproto.ThreadGoalUpdate) control.FeishuPageView {
	page := control.FeishuPageView{
		PageID:      control.FeishuCommandGoal,
		CommandID:   control.FeishuCommandGoal,
		Title:       "Goal",
		Interactive: true,
		ThemeKey:    "primary",
	}
	if goal == nil || goal.Cleared {
		page.BodySections = []control.FeishuCardTextSection{{
			Lines: []string{"当前会话没有 Goal。发送 `/goal new <目标>` 创建，例如：`/goal new 完成登录流程重构`。"},
		}}
		page.RelatedButtons = []control.CommandCatalogButton{
			goalCommandButton("创建 Goal", "/goal new"),
		}
		return page
	}
	status := strings.TrimSpace(goal.Status)
	if status == "" {
		status = "unknown"
	}
	page.StatusText = fmt.Sprintf("状态：%s", status)
	page.BodySections = []control.FeishuCardTextSection{
		{Label: "目标", Lines: []string{goal.Objective}},
	}
	if goal.TokenBudget != nil {
		page.BodySections = append(page.BodySections, control.FeishuCardTextSection{
			Label: "用量",
			Lines: []string{fmt.Sprintf("预算 %d tokens / 已用 %d tokens，耗时 %d 秒", *goal.TokenBudget, goal.TokensUsed, goal.TimeUsedSeconds)},
		})
	} else {
		page.BodySections = append(page.BodySections, control.FeishuCardTextSection{
			Label: "用量",
			Lines: []string{fmt.Sprintf("已用 %d tokens，耗时 %d 秒", goal.TokensUsed, goal.TimeUsedSeconds)},
		})
	}
	buttons := []control.CommandCatalogButton{
		goalCommandButton("刷新", "/goal"),
		goalCommandButton("编辑", "/goal edit"),
	}
	switch status {
	case "active":
		buttons = append(buttons, goalCommandButton("暂停", "/goal pause"))
	case "paused":
		buttons = append(buttons, goalCommandButton("恢复", "/goal resume"))
	}
	buttons = append(buttons, goalCommandButton("清除", "/goal clear"))
	page.RelatedButtons = buttons
	return page
}

func (s *Service) goalClearConfirmPage(surface *state.SurfaceConsoleRecord) control.FeishuPageView {
	page := goalStatusPage(surface, s.surfaceGoalForPage(surface))
	page.Title = "清除 Goal"
	page.StatusText = "确定要清除当前会话的 Goal 吗？清除后进度与 usage 会一并清空。"
	page.RelatedButtons = []control.CommandCatalogButton{
		goalCommandButton("确认清除", "/goal clear --confirm"),
		goalCommandButton("取消", "/goal"),
	}
	return page
}

func (s *Service) surfaceGoalForPage(surface *state.SurfaceConsoleRecord) *agentproto.ThreadGoalUpdate {
	if surface == nil {
		return nil
	}
	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil || strings.TrimSpace(surface.SelectedThreadID) == "" {
		return nil
	}
	thread := inst.Threads[strings.TrimSpace(surface.SelectedThreadID)]
	if thread == nil {
		return nil
	}
	return agentproto.CloneThreadGoalUpdate(thread.ThreadGoal)
}

func goalCommandButton(label, commandText string) control.CommandCatalogButton {
	return control.CommandCatalogButton{
		Label:       label,
		Kind:        control.CommandCatalogButtonCallbackAction,
		CommandText: commandText,
		CommandID:   control.FeishuCommandGoal,
	}
}
