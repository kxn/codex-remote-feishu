package projector

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func ProjectNoticeContent(notice control.Notice) (string, []map[string]any) {
	if elements := projectNoticeElements(notice); len(elements) != 0 {
		return "", elements
	}
	return projectNoticeBody(notice), nil
}

func projectNoticeElements(notice control.Notice) []map[string]any {
	if len(notice.Sections) == 0 {
		return nil
	}
	sections := make([]control.FeishuCardTextSection, 0, len(notice.Sections))
	for _, section := range notice.Sections {
		normalized := section.Normalized()
		if normalized.Label == "" && len(normalized.Lines) == 0 {
			continue
		}
		sections = append(sections, normalized)
	}
	if len(sections) == 0 {
		return nil
	}
	elements := make([]map[string]any, 0, len(sections)*2)
	return appendCardTextSections(elements, sections)
}

func projectNoticeBody(notice control.Notice) string {
	if len(notice.Sections) != 0 {
		return ""
	}
	if strings.HasPrefix(strings.TrimSpace(notice.Title), "链路错误") {
		return renderSystemInlineTags(notice.Text)
	}
	switch notice.Code {
	case "debug_error", "surface_override_usage", "surface_access_usage", "message_recall_too_late":
		return renderSystemInlineTags(notice.Text)
	default:
		return notice.Text
	}
}
