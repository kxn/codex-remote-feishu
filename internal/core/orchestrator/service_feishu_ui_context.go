package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) buildFeishuUIOwnerCardFlowContext(flow *activeOwnerCardFlowRecord) *control.FeishuUIOwnerCardFlowContext {
	if flow == nil {
		return nil
	}
	return &control.FeishuUIOwnerCardFlowContext{
		FlowID:    strings.TrimSpace(flow.FlowID),
		Kind:      strings.TrimSpace(string(flow.Kind)),
		Revision:  flow.Revision,
		Phase:     strings.TrimSpace(string(flow.Phase)),
		MessageID: strings.TrimSpace(flow.MessageID),
	}
}

func (s *Service) buildFeishuUISurfaceContext(surface *state.SurfaceConsoleRecord) control.FeishuUISurfaceContext {
	if surface == nil {
		return control.FeishuUISurfaceContext{
			InlineReplaceFreshness:         control.FeishuUIInlineReplaceFreshnessDaemonLifecycle,
			InlineReplaceRequiresFreshness: true,
			InlineReplaceViewSession:       control.FeishuUIInlineReplaceViewSessionSurfaceState,
			InlineReplaceRequiresViewState: false,
			CallbackPayloadOwner:           control.FeishuUICallbackPayloadOwnerAdapter,
		}
	}
	context := control.FeishuUISurfaceContext{
		SurfaceSessionID:               surface.SurfaceSessionID,
		GatewayID:                      surface.GatewayID,
		ProductMode:                    string(s.normalizeSurfaceProductMode(surface)),
		AttachedInstanceID:             strings.TrimSpace(surface.AttachedInstanceID),
		CurrentWorkspaceKey:            s.surfaceCurrentWorkspaceKey(surface),
		RouteMode:                      string(surface.RouteMode),
		SelectedThreadID:               strings.TrimSpace(surface.SelectedThreadID),
		Gate:                           s.snapshotGateSummary(surface),
		InlineReplaceFreshness:         control.FeishuUIInlineReplaceFreshnessDaemonLifecycle,
		InlineReplaceRequiresFreshness: true,
		InlineReplaceViewSession:       control.FeishuUIInlineReplaceViewSessionSurfaceState,
		InlineReplaceRequiresViewState: false,
		CallbackPayloadOwner:           control.FeishuUICallbackPayloadOwnerAdapter,
		ActiveOwnerCard:                s.buildFeishuUIOwnerCardFlowContext(s.activeOwnerCardFlow(surface)),
	}
	if surface.ActiveRequestCapture != nil {
		context.RouteMutationBlocked = true
		context.RouteMutationBlockedBy = "request_capture"
		return context
	}
	if s.targetPickerHasBlockingProcessing(surface) {
		context.RouteMutationBlocked = true
		context.RouteMutationBlockedBy = "target_picker"
		return context
	}
	if s.activePathPicker(surface) != nil {
		context.RouteMutationBlocked = true
		context.RouteMutationBlockedBy = "path_picker"
		return context
	}
	if activePendingRequest(surface) != nil {
		context.RouteMutationBlocked = true
		context.RouteMutationBlockedBy = "pending_request"
	}
	return context
}

func (s *Service) buildFeishuSelectionContextFromPromptView(surface *state.SurfaceConsoleRecord, prompt control.FeishuDirectSelectionPrompt) *control.FeishuUISelectionContext {
	return &control.FeishuUISelectionContext{
		DTOOwner:     control.FeishuUIDTOwnerSelection,
		Surface:      s.buildFeishuUISurfaceContext(surface),
		PromptKind:   prompt.Kind,
		Layout:       strings.TrimSpace(prompt.Layout),
		Title:        strings.TrimSpace(prompt.Title),
		ContextTitle: strings.TrimSpace(prompt.ContextTitle),
		ContextText:  strings.TrimSpace(prompt.ContextText),
		ContextKey:   strings.TrimSpace(prompt.ContextKey),
	}
}

func (s *Service) buildFeishuSelectionContextFromView(surface *state.SurfaceConsoleRecord, view control.FeishuSelectionView) *control.FeishuUISelectionContext {
	context := &control.FeishuUISelectionContext{
		DTOOwner:   control.FeishuUIDTOwnerSelection,
		Surface:    s.buildFeishuUISurfaceContext(surface),
		PromptKind: view.PromptKind,
	}
	if view.Prompt != nil {
		return s.buildFeishuSelectionContextFromPromptView(surface, *view.Prompt)
	}
	if view.Instance != nil {
		context.Layout = "vscode_instance_list"
		context.Title = "在线 VS Code 实例"
		if view.Instance.Current != nil {
			context.ContextTitle = "当前实例"
			context.ContextText = strings.TrimSpace(view.Instance.Current.ContextText)
		}
		return context
	}
	if view.Workspace != nil {
		context.Layout = "grouped_attach_workspace"
		context.Title = "工作区列表"
		context.ViewMode = "paged"
		if view.Workspace.Current != nil {
			context.ContextTitle = "当前工作区"
			context.ContextText = workspaceSelectionContextText(view.Workspace.Current.WorkspaceLabel, view.Workspace.Current.AgeText)
			context.ContextKey = strings.TrimSpace(view.Workspace.Current.WorkspaceKey)
		}
		return context
	}
	if view.Thread == nil {
		return context
	}
	context.ViewMode = string(view.Thread.Mode)
	switch view.Thread.Mode {
	case control.FeishuThreadSelectionNormalGlobalRecent:
		context.Layout = "workspace_grouped_useall"
		context.Title = "全部会话"
	case control.FeishuThreadSelectionNormalGlobalAll:
		context.Layout = "workspace_grouped_useall"
		context.Title = "全部会话"
	case control.FeishuThreadSelectionNormalScopedRecent:
		context.Title = "最近会话"
	case control.FeishuThreadSelectionNormalScopedAll:
		context.Title = "当前工作区全部会话"
	case control.FeishuThreadSelectionNormalWorkspaceView:
		context.Layout = "workspace_grouped_useall"
		if view.Thread.Workspace != nil {
			context.Title = strings.TrimSpace(view.Thread.Workspace.WorkspaceLabel) + " 全部会话"
			context.ContextKey = strings.TrimSpace(view.Thread.Workspace.WorkspaceKey)
		}
	case control.FeishuThreadSelectionVSCodeRecent:
		context.Layout = "vscode_instance_threads"
		context.Title = "最近会话"
	case control.FeishuThreadSelectionVSCodeAll, control.FeishuThreadSelectionVSCodeScopedAll:
		context.Layout = "vscode_instance_threads"
		context.Title = "当前实例全部会话"
	}
	if view.Thread.CurrentWorkspace != nil {
		context.ContextTitle = "当前工作区"
		context.ContextKey = strings.TrimSpace(view.Thread.CurrentWorkspace.WorkspaceKey)
		line := strings.TrimSpace(view.Thread.CurrentWorkspace.WorkspaceLabel)
		if age := strings.TrimSpace(view.Thread.CurrentWorkspace.AgeText); age != "" {
			line += " · " + age
		}
		context.ContextText = strings.Join([]string{line, "同工作区内切换请直接用 /use"}, "\n")
	}
	if view.Thread.CurrentInstance != nil {
		context.ContextTitle = "当前实例"
		context.ContextText = strings.TrimSpace(view.Thread.CurrentInstance.Label)
		if status := strings.TrimSpace(view.Thread.CurrentInstance.Status); status != "" {
			context.ContextText += " · " + status
		}
	}
	return context
}

func (s *Service) buildFeishuRequestContextFromView(surface *state.SurfaceConsoleRecord, prompt control.FeishuRequestView) *control.FeishuUIRequestContext {
	return &control.FeishuUIRequestContext{
		DTOOwner:    control.FeishuUIDTOwnerRequest,
		Surface:     s.buildFeishuUISurfaceContext(surface),
		RequestID:   strings.TrimSpace(prompt.RequestID),
		RequestType: strings.TrimSpace(prompt.RequestType),
		ThreadID:    strings.TrimSpace(prompt.ThreadID),
		ThreadTitle: strings.TrimSpace(prompt.ThreadTitle),
		Title:       strings.TrimSpace(prompt.Title),
	}
}

func (s *Service) buildFeishuPageContextFromView(surface *state.SurfaceConsoleRecord, view control.FeishuPageView) *control.FeishuUIPageContext {
	return &control.FeishuUIPageContext{
		DTOOwner:  control.FeishuUIDTOwnerPage,
		Surface:   s.buildFeishuUISurfaceContext(surface),
		PageID:    strings.TrimSpace(view.PageID),
		CommandID: strings.TrimSpace(view.CommandID),
		Title:     strings.TrimSpace(view.Title),
	}
}

func (s *Service) buildFeishuPathPickerContextFromView(surface *state.SurfaceConsoleRecord, view control.FeishuPathPickerView) *control.FeishuUIPathPickerContext {
	return &control.FeishuUIPathPickerContext{
		DTOOwner:     control.FeishuUIDTOwnerPathPicker,
		Surface:      s.buildFeishuUISurfaceContext(surface),
		PickerID:     strings.TrimSpace(view.PickerID),
		Mode:         view.Mode,
		Title:        strings.TrimSpace(view.Title),
		RootPath:     strings.TrimSpace(view.RootPath),
		CurrentPath:  strings.TrimSpace(view.CurrentPath),
		SelectedPath: strings.TrimSpace(view.SelectedPath),
	}
}

func (s *Service) buildFeishuTargetPickerContextFromView(surface *state.SurfaceConsoleRecord, view control.FeishuTargetPickerView) *control.FeishuUITargetPickerContext {
	return &control.FeishuUITargetPickerContext{
		DTOOwner:             control.FeishuUIDTOwnerTargetPicker,
		Surface:              s.buildFeishuUISurfaceContext(surface),
		PickerID:             strings.TrimSpace(view.PickerID),
		Source:               view.Source,
		Title:                strings.TrimSpace(view.Title),
		Page:                 view.Page,
		SelectedMode:         view.SelectedMode,
		SelectedSource:       view.SelectedSource,
		SelectedWorkspaceKey: strings.TrimSpace(view.SelectedWorkspaceKey),
		SelectedSessionValue: strings.TrimSpace(view.SelectedSessionValue),
	}
}

func (s *Service) buildFeishuThreadHistoryContextFromView(surface *state.SurfaceConsoleRecord, view control.FeishuThreadHistoryView) *control.FeishuUIThreadHistoryContext {
	flow := s.activeOwnerCardFlow(surface)
	if flow != nil && (flow.Kind != ownerCardFlowKindThreadHistory || strings.TrimSpace(flow.FlowID) != strings.TrimSpace(view.PickerID)) {
		flow = nil
	}
	return &control.FeishuUIThreadHistoryContext{
		DTOOwner:       control.FeishuUIDTOwnerThreadHistory,
		Surface:        s.buildFeishuUISurfaceContext(surface),
		OwnerCard:      s.buildFeishuUIOwnerCardFlowContext(flow),
		PickerID:       strings.TrimSpace(view.PickerID),
		ThreadID:       strings.TrimSpace(view.ThreadID),
		Mode:           view.Mode,
		Page:           view.Page,
		SelectedTurnID: strings.TrimSpace(view.SelectedTurnID),
		Loading:        view.Loading,
	}
}

func (s *Service) feishuDirectSelectionPromptEvent(surface *state.SurfaceConsoleRecord, prompt control.FeishuDirectSelectionPrompt) eventcontract.Event {
	return s.feishuDirectSelectionPromptEventWithInline(surface, prompt, false)
}

func (s *Service) feishuDirectSelectionPromptEventWithInline(surface *state.SurfaceConsoleRecord, prompt control.FeishuDirectSelectionPrompt, inline bool) eventcontract.Event {
	promptView := prompt
	view := control.FeishuSelectionView{
		PromptKind: prompt.Kind,
		Prompt:     &promptView,
	}
	return surfaceEventFromPayload(
		surface,
		eventcontract.SelectionPayload{
			View:    view,
			Context: s.buildFeishuSelectionContextFromView(surface, view),
		},
		navigationDeliverySemantics(),
		inline,
		"",
		"",
	)
}

func (s *Service) selectionViewEvent(surface *state.SurfaceConsoleRecord, view control.FeishuSelectionView) eventcontract.Event {
	return surfaceEventFromPayload(
		surface,
		eventcontract.SelectionPayload{
			View:    view,
			Context: s.buildFeishuSelectionContextFromView(surface, view),
		},
		navigationDeliverySemantics(),
		true,
		"",
		"",
	)
}

func (s *Service) requestViewEvent(surface *state.SurfaceConsoleRecord, view control.FeishuRequestView) eventcontract.Event {
	return surfaceEventFromPayload(
		surface,
		eventcontract.RequestPayload{
			View:    view,
			Context: s.buildFeishuRequestContextFromView(surface, view),
		},
		terminalDeliverySemantics(),
		false,
		"",
		"",
	)
}

func (s *Service) pathPickerViewEvent(surface *state.SurfaceConsoleRecord, view control.FeishuPathPickerView, inline bool) eventcontract.Event {
	if !inline {
		if messageID := s.pathPickerMessageID(surface, view.PickerID); messageID != "" {
			view.MessageID = messageID
		}
	}
	return surfaceEventFromPayload(
		surface,
		eventcontract.PathPickerPayload{
			View:    view,
			Context: s.buildFeishuPathPickerContextFromView(surface, view),
		},
		navigationDeliverySemantics(),
		inline,
		"",
		"",
	)
}

func (s *Service) pathPickerMessageID(surface *state.SurfaceConsoleRecord, pickerID string) string {
	record := s.activePathPicker(surface)
	if record != nil && strings.TrimSpace(record.PickerID) == strings.TrimSpace(pickerID) {
		if messageID := strings.TrimSpace(record.MessageID); messageID != "" {
			return messageID
		}
	}
	return s.pathPickerOwnerCardMessageID(surface, pickerID)
}

func (s *Service) targetPickerViewEvent(surface *state.SurfaceConsoleRecord, view control.FeishuTargetPickerView, inline bool) eventcontract.Event {
	if !inline {
		if flow := s.activeOwnerCardFlow(surface); flow != nil && flow.Kind == ownerCardFlowKindTargetPicker && strings.TrimSpace(flow.FlowID) == strings.TrimSpace(view.PickerID) {
			view.MessageID = strings.TrimSpace(flow.MessageID)
		}
	}
	return surfaceEventFromPayload(
		surface,
		eventcontract.TargetPickerPayload{
			View:    view,
			Context: s.buildFeishuTargetPickerContextFromView(surface, view),
		},
		navigationDeliverySemantics(),
		inline,
		"",
		"",
	)
}

func (s *Service) pathPickerOwnerCardMessageID(surface *state.SurfaceConsoleRecord, pickerID string) string {
	record := s.activePathPicker(surface)
	flow := s.activeOwnerCardFlow(surface)
	if record == nil || flow == nil {
		return ""
	}
	if strings.TrimSpace(record.PickerID) != strings.TrimSpace(pickerID) {
		return ""
	}
	if flow.Kind != ownerCardFlowKindTargetPicker {
		return ""
	}
	if strings.TrimSpace(record.OwnerFlowID) == "" || strings.TrimSpace(record.OwnerFlowID) != strings.TrimSpace(flow.FlowID) {
		return ""
	}
	return strings.TrimSpace(flow.MessageID)
}

func (s *Service) threadHistoryViewEvent(surface *state.SurfaceConsoleRecord, view control.FeishuThreadHistoryView, inline bool, sourceMessageID string) eventcontract.Event {
	return surfaceEventFromPayload(
		surface,
		eventcontract.ThreadHistoryPayload{
			View:    view,
			Context: s.buildFeishuThreadHistoryContextFromView(surface, view),
		},
		navigationDeliverySemantics(),
		inline,
		sourceMessageID,
		"",
	)
}
