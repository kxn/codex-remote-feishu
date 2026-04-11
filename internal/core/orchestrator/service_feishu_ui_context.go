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

func (s *Service) buildFeishuSelectionContextFromView(surface *state.SurfaceConsoleRecord, view control.FeishuSelectionView) *control.FeishuUISelectionContext {
	context := &control.FeishuUISelectionContext{
		DTOOwner:   control.FeishuUIDTOwnerSelection,
		Surface:    s.buildFeishuUISurfaceContext(surface),
		PromptKind: view.PromptKind,
	}
	if view.Workspace != nil {
		context.Layout = "grouped_attach_workspace"
		context.Title = "工作区列表"
		context.ViewMode = "recent"
		if view.Workspace.Expanded {
			context.Title = "全部工作区"
			context.ViewMode = "all"
		}
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

func (s *Service) buildFeishuCommandContextFromView(surface *state.SurfaceConsoleRecord, view control.FeishuCommandView, catalog control.CommandCatalog) *control.FeishuUICommandContext {
	context := &control.FeishuUICommandContext{
		DTOOwner:    control.FeishuUIDTOwnerCommand,
		Surface:     s.buildFeishuUISurfaceContext(surface),
		Title:       strings.TrimSpace(catalog.Title),
		Summary:     strings.TrimSpace(catalog.Summary),
		Breadcrumbs: append([]control.CommandCatalogBreadcrumb(nil), catalog.Breadcrumbs...),
	}
	switch {
	case view.Menu != nil:
		context.ViewKind = "menu"
		context.MenuStage = strings.TrimSpace(view.Menu.Stage)
		context.MenuView = strings.TrimSpace(view.Menu.GroupID)
	case view.Config != nil:
		commandID := strings.TrimSpace(view.Config.CommandID)
		context.ViewKind = "config"
		context.MenuView = commandID
		context.CommandID = commandID
		context.NeedsTarget = view.Config.RequiresAttachment
	}
	return context
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

func (s *Service) selectionViewEvent(surface *state.SurfaceConsoleRecord, view control.FeishuSelectionView) control.UIEvent {
	return control.UIEvent{
		Kind:                   control.UIEventSelectionPrompt,
		SurfaceSessionID:       surface.SurfaceSessionID,
		FeishuSelectionView:    &view,
		FeishuSelectionContext: s.buildFeishuSelectionContextFromView(surface, view),
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
