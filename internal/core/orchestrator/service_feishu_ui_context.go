package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) buildFeishuUISurfaceContext(surface *state.SurfaceConsoleRecord) control.FeishuUISurfaceContext {
	if surface == nil {
		return control.FeishuUISurfaceContext{
			InlineReplaceFreshness:         "daemon_lifecycle",
			InlineReplaceRequiresFreshness: true,
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
		Gate:                           snapshotGateSummary(surface),
		InlineReplaceFreshness:         "daemon_lifecycle",
		InlineReplaceRequiresFreshness: true,
		CallbackPayloadOwner:           control.FeishuUICallbackPayloadOwnerAdapter,
	}
	if surface.ActiveRequestCapture != nil {
		context.RouteMutationBlocked = true
		context.RouteMutationBlockedBy = "request_capture"
		return context
	}
	if activePendingRequest(surface) != nil {
		context.RouteMutationBlocked = true
		context.RouteMutationBlockedBy = "pending_request"
	}
	return context
}

func (s *Service) buildFeishuSelectionContext(surface *state.SurfaceConsoleRecord, prompt control.SelectionPrompt) *control.FeishuUISelectionContext {
	return &control.FeishuUISelectionContext{
		DTOOwner:     control.FeishuUIDTOwnerTransition,
		Surface:      s.buildFeishuUISurfaceContext(surface),
		PromptKind:   prompt.Kind,
		Layout:       strings.TrimSpace(prompt.Layout),
		Title:        strings.TrimSpace(prompt.Title),
		ContextTitle: strings.TrimSpace(prompt.ContextTitle),
		ContextText:  strings.TrimSpace(prompt.ContextText),
		ContextKey:   strings.TrimSpace(prompt.ContextKey),
	}
}

func (s *Service) buildFeishuCommandContext(surface *state.SurfaceConsoleRecord, view string, menuStage commandMenuStage, catalog control.CommandCatalog) *control.FeishuUICommandContext {
	return &control.FeishuUICommandContext{
		DTOOwner:    control.FeishuUIDTOwnerTransition,
		Surface:     s.buildFeishuUISurfaceContext(surface),
		MenuStage:   string(menuStage),
		MenuView:    strings.TrimSpace(view),
		Title:       strings.TrimSpace(catalog.Title),
		Summary:     strings.TrimSpace(catalog.Summary),
		Breadcrumbs: append([]control.CommandCatalogBreadcrumb(nil), catalog.Breadcrumbs...),
	}
}

func (s *Service) buildFeishuRequestContext(surface *state.SurfaceConsoleRecord, prompt control.RequestPrompt) *control.FeishuUIRequestContext {
	return &control.FeishuUIRequestContext{
		DTOOwner:    control.FeishuUIDTOwnerTransition,
		Surface:     s.buildFeishuUISurfaceContext(surface),
		RequestID:   strings.TrimSpace(prompt.RequestID),
		RequestType: strings.TrimSpace(prompt.RequestType),
		ThreadID:    strings.TrimSpace(prompt.ThreadID),
		ThreadTitle: strings.TrimSpace(prompt.ThreadTitle),
		Title:       strings.TrimSpace(prompt.Title),
	}
}

func (s *Service) selectionPromptEvent(surface *state.SurfaceConsoleRecord, prompt control.SelectionPrompt) control.UIEvent {
	return control.UIEvent{
		Kind:                   control.UIEventSelectionPrompt,
		SurfaceSessionID:       surface.SurfaceSessionID,
		SelectionPrompt:        &prompt,
		FeishuSelectionContext: s.buildFeishuSelectionContext(surface, prompt),
	}
}

func (s *Service) requestPromptEvent(surface *state.SurfaceConsoleRecord, prompt control.RequestPrompt) control.UIEvent {
	return control.UIEvent{
		Kind:                 control.UIEventRequestPrompt,
		SurfaceSessionID:     surface.SurfaceSessionID,
		RequestPrompt:        &prompt,
		FeishuRequestContext: s.buildFeishuRequestContext(surface, prompt),
	}
}
