package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
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

func (s *Service) pathPickerNotice(surface *state.SurfaceConsoleRecord, record *activePathPickerRecord, code, title, text string, inline bool) []eventcontract.Event {
	if surface == nil || record == nil {
		return notice(surface, code, text)
	}
	setPathPickerStatus(record, title, text, nil, "")
	view, err := s.buildPathPickerView(surface, record)
	if err != nil {
		return notice(surface, code, text)
	}
	return []eventcontract.Event{s.pathPickerViewEvent(surface, view, inline)}
}

func (s *Service) pathPickerInlineNotice(surface *state.SurfaceConsoleRecord, record *activePathPickerRecord, code, title, text string) []eventcontract.Event {
	return s.pathPickerNotice(surface, record, code, title, text, true)
}

func (s *Service) finishPathPickerWithStatus(
	surface *state.SurfaceConsoleRecord,
	record *activePathPickerRecord,
	phase frontstagecontract.Phase,
	title, text string,
	sections []control.FeishuCardTextSection,
	footer string,
	inline bool,
	appendEvents []eventcontract.Event,
) []eventcontract.Event {
	if record == nil {
		return append([]eventcontract.Event(nil), appendEvents...)
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
	view.Phase = phase
	view = control.NormalizeFeishuPathPickerView(view)
	event := s.pathPickerViewEvent(surface, view, inline)
	s.clearSurfacePathPicker(surface)
	return append([]eventcontract.Event{event}, appendEvents...)
}
