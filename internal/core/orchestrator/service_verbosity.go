package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type surfaceVisibilityClass string

const (
	surfaceVisibilityAlwaysVisible surfaceVisibilityClass = "always_visible"
	surfaceVisibilityProgressText  surfaceVisibilityClass = "progress_text"
	surfaceVisibilityPlan          surfaceVisibilityClass = "plan"
	surfaceVisibilityProcessDetail surfaceVisibilityClass = "process_detail"
	surfaceVisibilityUINavigation  surfaceVisibilityClass = "ui_navigation"
)

func (s *Service) filterEventsForSurfaceVisibility(events []control.UIEvent) []control.UIEvent {
	if len(events) == 0 {
		return nil
	}
	filtered := make([]control.UIEvent, 0, len(events))
	for _, event := range events {
		if s.allowSurfaceVisibleEvent(event) {
			filtered = append(filtered, event)
		}
	}
	return filtered
}

func (s *Service) allowSurfaceVisibleEvent(event control.UIEvent) bool {
	if event.Command != nil || event.DaemonCommand != nil {
		return true
	}
	if strings.TrimSpace(event.SurfaceSessionID) == "" {
		return true
	}
	surface := s.root.Surfaces[event.SurfaceSessionID]
	verbosity := state.SurfaceVerbosityNormal
	if surface != nil {
		verbosity = state.NormalizeSurfaceVerbosity(surface.Verbosity)
	}
	switch verbosity {
	case state.SurfaceVerbosityQuiet:
		switch classifySurfaceVisibleEvent(event) {
		case surfaceVisibilityProgressText, surfaceVisibilityPlan, surfaceVisibilityProcessDetail:
			return false
		default:
			return true
		}
	case state.SurfaceVerbosityNormal:
		return classifySurfaceVisibleEvent(event) != surfaceVisibilityProcessDetail
	case state.SurfaceVerbosityVerbose:
		return true
	default:
		return true
	}
}

func classifySurfaceVisibleEvent(event control.UIEvent) surfaceVisibilityClass {
	switch event.Kind {
	case control.UIEventPlanUpdated:
		return surfaceVisibilityPlan
	case control.UIEventExecCommandProgress:
		return surfaceVisibilityProgressText
	case control.UIEventBlockCommitted:
		if event.Block != nil && event.Block.Final {
			return surfaceVisibilityAlwaysVisible
		}
		return surfaceVisibilityProgressText
	case control.UIEventFeishuDirectRequestPrompt:
		return surfaceVisibilityAlwaysVisible
	case control.UIEventImageOutput:
		return surfaceVisibilityAlwaysVisible
	case control.UIEventNotice:
		if event.Notice != nil && noticeIsAlwaysVisible(*event.Notice) {
			return surfaceVisibilityAlwaysVisible
		}
		return surfaceVisibilityUINavigation
	case control.UIEventSnapshot,
		control.UIEventFeishuDirectSelectionPrompt,
		control.UIEventFeishuDirectCommandCatalog,
		control.UIEventFeishuPathPicker,
		control.UIEventFeishuTargetPicker,
		control.UIEventPendingInput,
		control.UIEventThreadSelectionChange:
		return surfaceVisibilityUINavigation
	default:
		return surfaceVisibilityUINavigation
	}
}

func noticeIsAlwaysVisible(notice control.Notice) bool {
	theme := strings.ToLower(strings.TrimSpace(notice.ThemeKey))
	code := strings.ToLower(strings.TrimSpace(notice.Code))
	title := strings.TrimSpace(notice.Title)
	text := strings.TrimSpace(notice.Text)
	switch {
	case theme == "error" || strings.Contains(theme, "error") || strings.Contains(theme, "fail"):
		return true
	case strings.Contains(code, "error"), strings.Contains(code, "failed"), strings.Contains(code, "rejected"), strings.Contains(code, "offline"), strings.Contains(code, "expired"), strings.Contains(code, "invalid"):
		return true
	case strings.Contains(title, "错误"), strings.Contains(title, "失败"), strings.Contains(title, "无法"), strings.Contains(title, "拒绝"), strings.Contains(title, "离线"), strings.Contains(title, "过期"), strings.Contains(title, "失效"):
		return true
	case strings.Contains(text, "链路错误"), strings.Contains(text, "创建失败"), strings.Contains(text, "连接失败"):
		return true
	default:
		return false
	}
}
