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
	switch blockedBy := s.surfaceRouteMutationBlock(surface); blockedBy {
	case surfaceRouteMutationBlockRequestCapture:
		context.RouteMutationBlocked = true
		context.RouteMutationBlockedBy = "request_capture"
		return context
	case surfaceRouteMutationBlockTargetPicker:
		context.RouteMutationBlocked = true
		context.RouteMutationBlockedBy = "target_picker"
		return context
	case surfaceRouteMutationBlockPathPicker:
		context.RouteMutationBlocked = true
		context.RouteMutationBlockedBy = "path_picker"
		return context
	case surfaceRouteMutationBlockPendingRequest:
		context.RouteMutationBlocked = true
		context.RouteMutationBlockedBy = "pending_request"
		return context
	case surfaceRouteMutationBlockReviewRunning:
		context.RouteMutationBlocked = true
		context.RouteMutationBlockedBy = "review_running"
	}
	return context
}

func (s *Service) buildFeishuSelectionContextFromView(surface *state.SurfaceConsoleRecord, view control.FeishuSelectionView) *control.FeishuUISelectionContext {
	semantics := control.DeriveFeishuSelectionSemantics(view)
	context := &control.FeishuUISelectionContext{
		DTOOwner:         control.FeishuUIDTOwnerSelection,
		Surface:          s.buildFeishuUISurfaceContext(surface),
		PromptKind:       semantics.PromptKind,
		CatalogFamilyID:  strings.TrimSpace(view.CatalogFamilyID),
		CatalogVariantID: strings.TrimSpace(view.CatalogVariantID),
		CatalogBackend:   view.CatalogBackend,
		ViewMode:         strings.TrimSpace(semantics.ViewMode),
		Layout:           strings.TrimSpace(semantics.Layout),
		Title:            strings.TrimSpace(semantics.Title),
		ContextTitle:     strings.TrimSpace(semantics.ContextTitle),
		ContextText:      strings.TrimSpace(semantics.ContextText),
		ContextKey:       strings.TrimSpace(semantics.ContextKey),
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
		DTOOwner:                 control.FeishuUIDTOwnerTargetPicker,
		Surface:                  s.buildFeishuUISurfaceContext(surface),
		PickerID:                 strings.TrimSpace(view.PickerID),
		Source:                   view.Source,
		CatalogFamilyID:          strings.TrimSpace(view.CatalogFamilyID),
		CatalogVariantID:         strings.TrimSpace(view.CatalogVariantID),
		CatalogBackend:           view.CatalogBackend,
		Title:                    strings.TrimSpace(view.Title),
		Page:                     view.Page,
		WorkspaceSelectionLocked: view.WorkspaceSelectionLocked,
		LockedWorkspaceKey:       strings.TrimSpace(view.LockedWorkspaceKey),
		AllowNewThread:           view.AllowNewThread,
		SelectedWorkspaceKey:     strings.TrimSpace(view.SelectedWorkspaceKey),
		SelectedSessionValue:     strings.TrimSpace(view.SelectedSessionValue),
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

func (s *Service) selectionViewEvent(surface *state.SurfaceConsoleRecord, view control.FeishuSelectionView) eventcontract.Event {
	return surfaceEventFromPayload(
		surface,
		eventcontract.SelectionPayload{
			View:    view,
			Context: s.buildFeishuSelectionContextFromView(surface, view),
		},
		eventcontract.EventMeta{
			InlineReplaceMode: eventcontract.InlineReplaceCurrentCard,
		},
	)
}

func (s *Service) requestViewEvent(surface *state.SurfaceConsoleRecord, view control.FeishuRequestView) eventcontract.Event {
	view = control.NormalizeFeishuRequestView(view)
	return surfaceEventFromPayload(
		surface,
		eventcontract.RequestPayload{
			View:    view,
			Context: s.buildFeishuRequestContextFromView(surface, view),
		},
		eventcontract.EventMeta{},
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
		eventcontract.EventMeta{
			InlineReplaceMode: inlineReplaceMode(inline),
		},
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
		eventcontract.EventMeta{
			InlineReplaceMode: inlineReplaceMode(inline),
		},
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
		eventcontract.EventMeta{
			InlineReplaceMode:    inlineReplaceMode(inline),
			SourceMessageID:      strings.TrimSpace(sourceMessageID),
			SourceMessagePreview: "",
		},
	)
}
