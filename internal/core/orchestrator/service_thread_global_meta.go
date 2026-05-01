package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) threadSelectionMetaText(surface *state.SurfaceConsoleRecord, view *mergedThreadView, status string) string {
	if view == nil {
		return ""
	}
	if s.surfaceIsVSCode(surface) && strings.TrimSpace(surface.AttachedInstanceID) != "" {
		return s.vscodeThreadSelectionMetaText(surface, view, status)
	}
	status = strings.TrimSpace(status)
	if surface != nil && surface.SelectedThreadID == view.ThreadID && s.surfaceOwnsThread(surface, view.ThreadID) {
		if status != "" {
			return status
		}
		return "已接管"
	}
	age := humanizeRelativeTime(s.now(), threadLastUsedAt(view))
	if strings.Contains(status, "VS Code 占用中") {
		if age != "" {
			return age + " · VS Code 占用中"
		}
		return "VS Code 占用中"
	}
	if status != "" && (strings.Contains(status, "其他飞书会话接管") || strings.Contains(status, "不可接管") || strings.Contains(status, "不存在") || strings.Contains(status, "切换工作区")) {
		return status
	}
	if age != "" {
		return age
	}
	return firstNonEmpty(status, "时间未知")
}

func (s *Service) vscodeThreadSelectionMetaText(surface *state.SurfaceConsoleRecord, view *mergedThreadView, status string) string {
	status = strings.TrimSpace(status)
	age := humanizeRelativeTime(s.now(), threadLastUsedAt(view))
	isCurrent := surface != nil && surface.SelectedThreadID == view.ThreadID && s.surfaceOwnsThread(surface, view.ThreadID)
	if isCurrent {
		parts := []string{firstNonEmpty(status, "已接管")}
		if age != "" {
			parts = append(parts, age)
		}
		return strings.Join(parts, " · ")
	}
	if status != "" && (strings.Contains(status, "其他飞书会话接管") || strings.Contains(status, "不可接管") || strings.Contains(status, "不存在")) {
		return status
	}
	parts := make([]string, 0, 2)
	if view != nil && view.Inst != nil && strings.TrimSpace(view.Inst.ObservedFocusedThreadID) == view.ThreadID {
		parts = append(parts, "VS Code 当前焦点")
	}
	if age != "" {
		parts = append(parts, age)
	}
	if len(parts) != 0 {
		return strings.Join(parts, " · ")
	}
	if status != "" {
		return status
	}
	return "时间未知"
}
