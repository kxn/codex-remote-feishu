package orchestrator

import (
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) ApplyFeishuUIIntent(action control.Action, intent control.FeishuUIIntent) []control.UIEvent {
	surface := s.ensureSurface(action)
	return s.applyFeishuUIIntent(surface, intent)
}

func (s *Service) applyFeishuUIIntent(surface *state.SurfaceConsoleRecord, intent control.FeishuUIIntent) []control.UIEvent {
	switch intent.Kind {
	case control.FeishuUIIntentShowCommandMenu:
		return []control.UIEvent{s.commandViewEvent(surface, s.buildCommandMenuView(surface, intent.RawText))}
	case control.FeishuUIIntentShowModeCatalog:
		return []control.UIEvent{s.commandViewEvent(surface, s.buildModeCommandView(surface))}
	case control.FeishuUIIntentShowAutoContinueCatalog:
		return []control.UIEvent{s.commandViewEvent(surface, s.buildAutoContinueCommandView(surface))}
	case control.FeishuUIIntentShowReasoningCatalog:
		return []control.UIEvent{s.commandViewEvent(surface, s.buildReasoningCommandView(surface))}
	case control.FeishuUIIntentShowAccessCatalog:
		return []control.UIEvent{s.commandViewEvent(surface, s.buildAccessCommandView(surface))}
	case control.FeishuUIIntentShowModelCatalog:
		return []control.UIEvent{s.commandViewEvent(surface, s.buildModelCommandView(surface))}
	case control.FeishuUIIntentShowRecentWorkspaces:
		return s.presentWorkspaceSelection(surface)
	case control.FeishuUIIntentShowAllWorkspaces:
		return s.presentAllWorkspaceSelection(surface)
	case control.FeishuUIIntentShowThreads:
		return s.presentThreadSelection(surface, false)
	case control.FeishuUIIntentShowAllThreads:
		return s.presentThreadSelection(surface, true)
	case control.FeishuUIIntentShowScopedThreads:
		return s.presentScopedThreadSelection(surface)
	case control.FeishuUIIntentShowWorkspaceThreads:
		return s.presentWorkspaceThreadSelection(surface, intent.WorkspaceKey)
	case control.FeishuUIIntentShowAllThreadWorkspaces:
		return s.presentAllThreadWorkspaces(surface)
	case control.FeishuUIIntentShowRecentThreadWorkspaces:
		return s.presentThreadSelection(surface, true)
	default:
		return nil
	}
}
