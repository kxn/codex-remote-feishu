package orchestrator

import (
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const (
	defaultPlanProposalTTL       = 30 * time.Minute
	planProposalPayloadKind      = "plan_proposal"
	planProposalActionExecute    = "execute"
	planProposalActionExecuteNew = "execute_new"
	planProposalActionCancel     = "cancel"
)

func newPlanProposalRecord(proposalID, instanceID, threadID, turnID, threadCWD, planText string, createdAt time.Time, ttl time.Duration) *activePlanProposalRecord {
	createdAt = createdAt.UTC()
	return &activePlanProposalRecord{
		ProposalID: strings.TrimSpace(proposalID),
		InstanceID: strings.TrimSpace(instanceID),
		ThreadID:   strings.TrimSpace(threadID),
		TurnID:     strings.TrimSpace(turnID),
		ThreadCWD:  strings.TrimSpace(threadCWD),
		PlanText:   strings.TrimSpace(planText),
		CreatedAt:  createdAt,
		ExpiresAt:  createdAt.Add(ttl),
	}
}

func planProposalButton(label, proposalID, optionID, style string) control.CommandCatalogButton {
	return control.CommandCatalogButton{
		Label: label,
		Kind:  control.CommandCatalogButtonCallbackAction,
		CallbackValue: map[string]any{
			"kind":      planProposalPayloadKind,
			"picker_id": strings.TrimSpace(proposalID),
			"option_id": strings.TrimSpace(optionID),
		},
		Style: strings.TrimSpace(style),
	}
}

func splitPlanProposalLines(text string) []string {
	lines := make([]string, 0, 8)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

func planProposalNoticeSections(text string, theme string) []control.FeishuCardTextSection {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	label := "说明"
	if strings.TrimSpace(theme) == "error" {
		label = "错误"
	}
	return []control.FeishuCardTextSection{{
		Label: label,
		Lines: []string{text},
	}}
}

func planProposalMessageID(flow *activeOwnerCardFlowRecord, inlineMessageID string) string {
	if messageID := strings.TrimSpace(inlineMessageID); messageID != "" {
		return messageID
	}
	if flow == nil {
		return ""
	}
	return strings.TrimSpace(flow.MessageID)
}

func planProposalTrackingKey(flow *activeOwnerCardFlowRecord) string {
	if flow == nil {
		return ""
	}
	if strings.TrimSpace(flow.MessageID) != "" {
		return ""
	}
	return strings.TrimSpace(flow.FlowID)
}

func buildPlanProposalPageView(flow *activeOwnerCardFlowRecord, proposal *activePlanProposalRecord, inlineMessageID, statusText, theme string, buttons []control.CommandCatalogButton, sealed bool) control.FeishuPageView {
	interactive := len(buttons) != 0 && !sealed
	bodySections := []control.FeishuCardTextSection(nil)
	if proposal != nil {
		bodySections = append(bodySections, control.FeishuCardTextSection{
			Label: "提案内容",
			Lines: splitPlanProposalLines(proposal.PlanText),
		}.Normalized())
	}
	view := control.FeishuPageView{
		CommandID:                     control.FeishuCommandPlan,
		Title:                         "提案计划",
		MessageID:                     planProposalMessageID(flow, inlineMessageID),
		TrackingKey:                   planProposalTrackingKey(flow),
		ThemeKey:                      firstNonEmpty(strings.TrimSpace(theme), "plan"),
		Patchable:                     true,
		BodySections:                  bodySections,
		NoticeSections:                planProposalNoticeSections(statusText, theme),
		Interactive:                   interactive,
		Sealed:                        sealed,
		DisplayStyle:                  control.CommandCatalogDisplayCompactButtons,
		SuppressDefaultRelatedButtons: true,
	}
	if interactive {
		view.Sections = []control.CommandCatalogSection{{
			Title: "下一步",
			Entries: []control.CommandCatalogEntry{{
				Buttons: append([]control.CommandCatalogButton(nil), buttons...),
			}},
		}}
	}
	return control.FeishuPageViewFromCommandPageView(view)
}

func planProposalEvent(surface *state.SurfaceConsoleRecord, flow *activeOwnerCardFlowRecord, proposal *activePlanProposalRecord, inlineMessageID, statusText, theme string, buttons []control.CommandCatalogButton, sealed bool, inlineReplace bool) eventcontract.Event {
	view := buildPlanProposalPageView(flow, proposal, inlineMessageID, statusText, theme, buttons, sealed)
	return eventcontract.Event{
		Kind:                     eventcontract.EventFeishuPageView,
		GatewayID:                surface.GatewayID,
		SurfaceSessionID:         surface.SurfaceSessionID,
		InlineReplaceCurrentCard: inlineReplace,
		PageView:                 &view,
	}
}

func (s *Service) clearPlanProposalRuntime(surface *state.SurfaceConsoleRecord) {
	if surface == nil {
		return
	}
	s.clearSurfacePlanProposal(surface)
}

func (s *Service) requireActivePlanProposal(surface *state.SurfaceConsoleRecord, proposalID, actorUserID string) (*activeOwnerCardFlowRecord, *activePlanProposalRecord, []eventcontract.Event) {
	flow, blocked := s.requireActiveOwnerCardFlow(
		surface,
		ownerCardFlowKindPlanProposal,
		proposalID,
		actorUserID,
		"这张提案计划卡片已失效，请等待新的提案计划。",
		"这张提案计划卡片只允许发起者本人操作。",
	)
	if blocked != nil {
		return nil, nil, blocked
	}
	record := s.activePlanProposal(surface)
	if record == nil || strings.TrimSpace(record.ProposalID) != strings.TrimSpace(proposalID) {
		s.clearPlanProposalRuntime(surface)
		return nil, nil, notice(surface, "plan_proposal_expired", "这张提案计划卡片已失效，请等待新的提案计划。")
	}
	if !record.ExpiresAt.IsZero() && !record.ExpiresAt.After(s.now()) {
		s.clearPlanProposalRuntime(surface)
		return nil, nil, notice(surface, "plan_proposal_expired", "这张提案计划卡片已过期，请等待新的提案计划。")
	}
	if surface == nil || strings.TrimSpace(surface.AttachedInstanceID) != strings.TrimSpace(record.InstanceID) {
		s.clearPlanProposalRuntime(surface)
		return nil, nil, notice(surface, "plan_proposal_expired", "当前工作目标已变化，这张提案计划卡片已失效。")
	}
	return flow, record, nil
}

func (s *Service) sealPlanProposal(surface *state.SurfaceConsoleRecord, inlineMessageID, text, theme string, inlineReplace bool) []eventcontract.Event {
	if surface == nil {
		return nil
	}
	flow := s.activeOwnerCardFlow(surface)
	record := s.activePlanProposal(surface)
	if flow == nil || flow.Kind != ownerCardFlowKindPlanProposal || record == nil {
		s.clearPlanProposalRuntime(surface)
		return nil
	}
	event := planProposalEvent(surface, flow, record, inlineMessageID, text, theme, nil, true, inlineReplace)
	s.clearPlanProposalRuntime(surface)
	return []eventcontract.Event{event}
}

func (s *Service) maybeSealPlanProposalForInput(surface *state.SurfaceConsoleRecord) []eventcontract.Event {
	record := s.activePlanProposal(surface)
	if record == nil {
		return nil
	}
	return s.sealPlanProposal(surface, "", "检测到新的输入，当前提案计划已失效。", "info", false)
}

func (s *Service) maybeSealPlanProposalForRouteChange(surface *state.SurfaceConsoleRecord, reason string) []eventcontract.Event {
	record := s.activePlanProposal(surface)
	if record == nil {
		return nil
	}
	return s.sealPlanProposal(surface, "", firstNonEmpty(strings.TrimSpace(reason), "当前工作目标已变化，当前提案计划已失效。"), "info", false)
}

func (s *Service) maybeSealPlanProposalForTurnStart(instanceID, threadID, turnID string) []eventcontract.Event {
	if strings.TrimSpace(threadID) == "" || strings.TrimSpace(turnID) == "" {
		return nil
	}
	surface := s.threadClaimSurface(threadID)
	record := s.activePlanProposal(surface)
	if surface == nil || record == nil {
		return nil
	}
	if strings.TrimSpace(record.InstanceID) != strings.TrimSpace(instanceID) || strings.TrimSpace(record.ThreadID) != strings.TrimSpace(threadID) {
		return nil
	}
	if strings.TrimSpace(record.TurnID) == strings.TrimSpace(turnID) {
		return nil
	}
	return s.sealPlanProposal(surface, "", "当前会话已开始新的执行，之前的提案计划已失效。", "info", false)
}

func (s *Service) storePendingPlanProposal(instanceID, threadID, turnID, itemID, itemKind, text string) []eventcontract.Event {
	key := turnRenderKey(instanceID, threadID, turnID)
	s.progress.pendingPlanProposal[key] = &completedTextItem{
		InstanceID: instanceID,
		ThreadID:   threadID,
		TurnID:     turnID,
		ItemID:     itemID,
		ItemKind:   itemKind,
		Text:       strings.TrimSpace(text),
	}
	return nil
}

func (s *Service) takePendingPlanProposal(instanceID, threadID, turnID string) *completedTextItem {
	key := turnRenderKey(instanceID, threadID, turnID)
	pending := s.progress.pendingPlanProposal[key]
	delete(s.progress.pendingPlanProposal, key)
	return pending
}

func (s *Service) shouldSuppressPlanProposal(surface *state.SurfaceConsoleRecord, instanceID, threadID string) bool {
	if surface == nil || surface.Abandoning {
		return true
	}
	if strings.TrimSpace(surface.AttachedInstanceID) != strings.TrimSpace(instanceID) {
		return true
	}
	if surface.ActiveRequestCapture != nil || activePendingRequest(surface) != nil {
		return true
	}
	if countPendingDrafts(surface) != 0 || len(surface.QueuedQueueItemIDs) != 0 || strings.TrimSpace(surface.ActiveQueueItemID) != "" {
		return true
	}
	if strings.TrimSpace(surface.SelectedThreadID) != "" && strings.TrimSpace(surface.SelectedThreadID) != strings.TrimSpace(threadID) {
		return true
	}
	switch surface.RouteMode {
	case state.RouteModeUnbound, state.RouteModeNewThreadReady:
		return true
	default:
		return false
	}
}

func (s *Service) maybePresentCompletedPlanProposal(instanceID, threadID, turnID string) []eventcontract.Event {
	pending := s.takePendingPlanProposal(instanceID, threadID, turnID)
	if pending == nil || strings.TrimSpace(pending.Text) == "" {
		return nil
	}
	surface := s.turnSurface(instanceID, threadID, turnID)
	if s.shouldSuppressPlanProposal(surface, instanceID, threadID) {
		return nil
	}
	inst := s.root.Instances[instanceID]
	if inst == nil {
		return nil
	}
	thread := inst.Threads[threadID]
	threadCWD := strings.TrimSpace(firstNonEmpty(threadCWDFromRecord(thread), inst.WorkspaceRoot))
	now := s.now()
	events := s.maybeSealPlanProposalForRouteChange(surface, "新的提案计划已生成，上一张提案计划卡片已失效。")
	flow := newOwnerCardFlowRecord(ownerCardFlowKindPlanProposal, s.pickers.nextPlanProposalToken(), firstNonEmpty(surface.ActorUserID), now, defaultPlanProposalTTL, ownerCardFlowPhaseResolved)
	record := newPlanProposalRecord(flow.FlowID, instanceID, threadID, turnID, threadCWD, pending.Text, now, defaultPlanProposalTTL)
	s.setActiveOwnerCardFlow(surface, flow)
	s.setActivePlanProposal(surface, record)
	buttons := []control.CommandCatalogButton{
		planProposalButton("直接执行", flow.FlowID, planProposalActionExecute, "primary"),
		planProposalButton("清空上下文并执行", flow.FlowID, planProposalActionExecuteNew, ""),
		planProposalButton("取消", flow.FlowID, planProposalActionCancel, ""),
	}
	events = append(events, planProposalEvent(surface, flow, record, "", "这是一轮执行结束后整理出的提案计划。你可以直接继续执行，或在新上下文里按计划开工。", "plan", buttons, false, false))
	return events
}

func planProposalDirectExecutePrompt() string {
	return "Implement the plan."
}

func planProposalFreshContextPrompt(planText string) string {
	return strings.TrimSpace("A previous turn produced the following plan. Start from a fresh context, reread the necessary files, and implement it.\n\nPlan:\n" + strings.TrimSpace(planText))
}

func planProposalActionPreview(optionID string) string {
	switch strings.TrimSpace(optionID) {
	case planProposalActionExecute:
		return "直接执行提案计划"
	case planProposalActionExecuteNew:
		return "清空上下文并执行提案计划"
	default:
		return "提案计划处理"
	}
}

func (s *Service) handlePlanProposalDecision(surface *state.SurfaceConsoleRecord, action control.Action) []eventcontract.Event {
	_, proposal, blocked := s.requireActivePlanProposal(surface, action.PickerID, action.ActorUserID)
	if blocked != nil {
		return blocked
	}
	switch strings.TrimSpace(action.OptionID) {
	case planProposalActionExecute:
		cwd := strings.TrimSpace(proposal.ThreadCWD)
		if cwd == "" {
			inst := s.root.Instances[surface.AttachedInstanceID]
			if inst != nil {
				cwd = strings.TrimSpace(inst.WorkspaceRoot)
			}
		}
		if cwd == "" {
			return s.sealPlanProposal(surface, action.MessageID, "当前无法确定执行目录，请先重新选择工作区或会话。", "error", commandCardOwnsInlineResult(action))
		}
		surface.PlanMode = state.PlanModeSettingOff
		events := s.sealPlanProposal(surface, action.MessageID, "已关闭 Plan mode，并开始按这份提案继续执行。", "success", commandCardOwnsInlineResult(action))
		return append(events, s.enqueueQueueItem(
			surface,
			action.MessageID,
			planProposalActionPreview(action.OptionID),
			nil,
			[]agentproto.Input{{Type: agentproto.InputText, Text: planProposalDirectExecutePrompt()}},
			proposal.ThreadID,
			cwd,
			surface.RouteMode,
			surface.PromptOverride,
			false,
		)...)
	case planProposalActionExecuteNew:
		cwd := strings.TrimSpace(proposal.ThreadCWD)
		if cwd == "" {
			inst := s.root.Instances[surface.AttachedInstanceID]
			if inst != nil {
				cwd = strings.TrimSpace(inst.WorkspaceRoot)
			}
		}
		if cwd == "" {
			return s.sealPlanProposal(surface, action.MessageID, "当前无法确定新上下文的工作目录，请先重新选择工作区或会话。", "error", commandCardOwnsInlineResult(action))
		}
		surface.PlanMode = state.PlanModeSettingOff
		events := s.sealPlanProposal(surface, action.MessageID, "已关闭 Plan mode，并开始在新上下文里按提案执行。", "success", commandCardOwnsInlineResult(action))
		return append(events, s.enqueueQueueItem(
			surface,
			action.MessageID,
			planProposalActionPreview(action.OptionID),
			nil,
			[]agentproto.Input{{Type: agentproto.InputText, Text: planProposalFreshContextPrompt(proposal.PlanText)}},
			"",
			cwd,
			state.RouteModeNewThreadReady,
			surface.PromptOverride,
			false,
		)...)
	case planProposalActionCancel:
		return s.sealPlanProposal(surface, action.MessageID, "已取消这张提案计划卡片。", "info", commandCardOwnsInlineResult(action))
	default:
		return s.sealPlanProposal(surface, action.MessageID, "当前提案计划动作无效，请等待新的提案计划。", "error", commandCardOwnsInlineResult(action))
	}
}
