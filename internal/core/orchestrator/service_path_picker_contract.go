package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func clearPathPickerStatus(record *activePathPickerRecord) {
	if record == nil {
		return
	}
	record.StatusTitle = ""
	record.StatusText = ""
	record.StatusSections = nil
	record.StatusFooter = ""
}

func setPathPickerStatus(record *activePathPickerRecord, title, text string, sections []control.FeishuCardTextSection, footer string) {
	if record == nil {
		return
	}
	record.StatusTitle = strings.TrimSpace(title)
	record.StatusText = strings.TrimSpace(text)
	record.StatusSections = cloneFeishuCardSections(sections)
	record.StatusFooter = strings.TrimSpace(footer)
}

func pathPickerBodySections(rootPath, currentPath, selectedPath string) []control.FeishuCardTextSection {
	sections := make([]control.FeishuCardTextSection, 0, 3)
	if section, ok := targetPickerSummarySection("允许范围", rootPath, ""); ok {
		sections = append(sections, section)
	}
	if section, ok := targetPickerSummarySection("当前目录", currentPath, ""); ok {
		sections = append(sections, section)
	}
	if section, ok := targetPickerSummarySection("当前选择", selectedPath, ""); ok {
		sections = append(sections, section)
	}
	return cloneFeishuCardSections(sections)
}

func pathPickerStatusNoticeSections(title, text string, sections []control.FeishuCardTextSection, footer string) []control.FeishuCardTextSection {
	result := make([]control.FeishuCardTextSection, 0, len(sections)+2)
	if text = strings.TrimSpace(text); text != "" {
		label := strings.TrimSpace(title)
		if label == "" {
			label = "说明"
		}
		result = append(result, control.FeishuCardTextSection{
			Label: label,
			Lines: []string{text},
		})
	}
	result = append(result, cloneFeishuCardSections(sections)...)
	if footer = strings.TrimSpace(footer); footer != "" {
		result = append(result, control.FeishuCardTextSection{
			Label: "下一步",
			Lines: []string{footer},
		})
	}
	return cloneFeishuCardSections(result)
}

func (s *Service) pathPickerNotice(surface *state.SurfaceConsoleRecord, record *activePathPickerRecord, code, title, text string, inline bool) []control.UIEvent {
	if surface == nil || record == nil {
		return notice(surface, code, text)
	}
	setPathPickerStatus(record, title, text, nil, "")
	view, err := s.buildPathPickerView(surface, record)
	if err != nil {
		return notice(surface, code, text)
	}
	return []control.UIEvent{s.pathPickerViewEvent(surface, view, inline)}
}

func (s *Service) pathPickerInlineNotice(surface *state.SurfaceConsoleRecord, record *activePathPickerRecord, code, title, text string) []control.UIEvent {
	return s.pathPickerNotice(surface, record, code, title, text, true)
}

func (s *Service) finishPathPickerWithStatus(
	surface *state.SurfaceConsoleRecord,
	record *activePathPickerRecord,
	title, text string,
	sections []control.FeishuCardTextSection,
	footer string,
	inline bool,
	appendEvents []control.UIEvent,
) []control.UIEvent {
	if record == nil {
		return append([]control.UIEvent(nil), appendEvents...)
	}
	setPathPickerStatus(record, title, text, sections, footer)
	view, err := s.buildPathPickerView(surface, record)
	if err != nil {
		s.clearSurfacePathPicker(surface)
		if len(appendEvents) != 0 {
			return append(notice(surface, "path_picker_unavailable", err.Error()), appendEvents...)
		}
		return notice(surface, "path_picker_unavailable", err.Error())
	}
	view.Terminal = true
	view.Sealed = true
	event := s.pathPickerViewEvent(surface, view, inline)
	s.clearSurfacePathPicker(surface)
	return append([]control.UIEvent{event}, appendEvents...)
}

func pathPickerFilteredFollowupEvents(events []control.UIEvent) []control.UIEvent {
	if len(events) == 0 {
		return nil
	}
	filtered := make([]control.UIEvent, 0, len(events))
	for _, event := range events {
		if event.Kind == control.UIEventNotice {
			continue
		}
		filtered = append(filtered, event)
	}
	return filtered
}

func pathPickerFirstNoticeText(events []control.UIEvent) string {
	for _, event := range events {
		if event.Notice == nil {
			continue
		}
		if text := strings.TrimSpace(event.Notice.Text); text != "" {
			return text
		}
	}
	return ""
}
