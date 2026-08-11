package projector

import (
	"path/filepath"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu/cardkit"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	frontstagecontract "github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

func PathPickerElements(view control.FeishuPathPickerView, daemonLifecycleID string) []map[string]any {
	view = control.NormalizeFeishuPathPickerView(view)
	if view.Terminal || view.Sealed {
		return sealedPathPickerElements(view)
	}
	switch view.Mode {
	case control.PathPickerModeFile:
		return fileModePathPickerElements(view, daemonLifecycleID)
	case control.PathPickerModeDirectory:
		return directoryModePathPickerElements(view, daemonLifecycleID)
	default:
		return directoryModePathPickerElements(view, daemonLifecycleID)
	}
}

func sealedPathPickerElements(view control.FeishuPathPickerView) []map[string]any {
	elements := make([]map[string]any, 0, 6)
	if title := strings.TrimSpace(view.StatusTitle); title != "" {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": "**" + title + "**",
		})
	}
	bodySections := pathPickerBodySectionsForView(view)
	if len(bodySections) != 0 {
		elements = cardkit.AppendTextSections(elements, bodySections)
	}
	if noticeSections := pathPickerNoticeSectionsForView(view); len(noticeSections) != 0 {
		if len(bodySections) != 0 {
			elements = append(elements, cardDividerElement())
		}
		elements = cardkit.AppendTextSections(elements, noticeSections)
	}
	return elements
}

func fileModePathPickerElements(view control.FeishuPathPickerView, daemonLifecycleID string) []map[string]any {
	return paginatedFileModePathPickerElements(view, daemonLifecycleID)
}

func directoryModePathPickerElements(view control.FeishuPathPickerView, daemonLifecycleID string) []map[string]any {
	if strings.TrimSpace(view.StageLabel) != "" || strings.TrimSpace(view.Question) != "" {
		return ownerSubpageDirectoryModePathPickerElements(view, daemonLifecycleID)
	}
	return paginatedDirectoryModePathPickerElements(view, daemonLifecycleID)
}

func ownerSubpageDirectoryModePathPickerElements(view control.FeishuPathPickerView, daemonLifecycleID string) []map[string]any {
	return paginatedOwnerSubpageDirectoryModePathPickerElements(view, daemonLifecycleID)
}

func pathPickerBodySectionsForView(view control.FeishuPathPickerView) []control.FeishuCardTextSection {
	if len(view.BodySections) != 0 {
		return view.BodySections
	}
	sections := make([]control.FeishuCardTextSection, 0, 3)
	if root := strings.TrimSpace(view.RootPath); root != "" {
		sections = append(sections, control.FeishuCardTextSection{
			Label: "允许范围",
			Lines: []string{root},
		})
	}
	if current := strings.TrimSpace(view.CurrentPath); current != "" {
		sections = append(sections, control.FeishuCardTextSection{
			Label: "当前目录",
			Lines: []string{current},
		})
	}
	if selected := strings.TrimSpace(view.SelectedPath); selected != "" {
		sections = append(sections, control.FeishuCardTextSection{
			Label: "当前选择",
			Lines: []string{selected},
		})
	}
	return sections
}

func pathPickerNoticeSectionsForView(view control.FeishuPathPickerView) []control.FeishuCardTextSection {
	if len(view.NoticeSections) != 0 {
		return view.NoticeSections
	}
	sections := make([]control.FeishuCardTextSection, 0, len(view.StatusSections)+2)
	if text := strings.TrimSpace(view.StatusText); text != "" {
		label := strings.TrimSpace(view.StatusTitle)
		if label == "" {
			label = "说明"
		}
		sections = append(sections, control.FeishuCardTextSection{
			Label: label,
			Lines: []string{text},
		})
	}
	sections = append(sections, view.StatusSections...)
	if footer := strings.TrimSpace(view.StatusFooter); footer != "" {
		sections = append(sections, control.FeishuCardTextSection{
			Label: "下一步",
			Lines: []string{footer},
		})
	}
	return sections
}

func currentDirectoryPathPickerOption(currentPath string) map[string]any {
	label := pathPickerCurrentDirectoryLabel(currentPath)
	return map[string]any{
		"text":  cardkit.PlainText(label),
		"value": ".",
	}
}

func pathPickerCurrentDirectoryLabel(currentPath string) string {
	currentPath = strings.TrimSpace(currentPath)
	name := strings.TrimSpace(filepath.Base(currentPath))
	if name == "" || name == "." {
		name = currentPath
	}
	if name == "" {
		name = "当前目录"
	}
	return name + "（当前目录）"
}

func pathPickerSelectStaticOptions(view control.FeishuPathPickerView, kind control.PathPickerEntryKind) ([]map[string]any, string) {
	options := make([]map[string]any, 0, len(view.Entries))
	initialOption := ""
	for _, entry := range view.Entries {
		if entry.Disabled || entry.Kind != kind {
			continue
		}
		value := strings.TrimSpace(entry.Name)
		if value == "" {
			continue
		}
		options = append(options, map[string]any{
			"text":  cardkit.PlainText(pathPickerSelectStaticLabel(entry)),
			"value": value,
		})
		if entry.Selected {
			initialOption = value
		}
	}
	return options, initialOption
}

func pathPickerSelectStaticLabel(entry control.FeishuPathPickerEntry) string {
	label := strings.TrimSpace(xutil.FirstNonEmpty(entry.Label, entry.Name))
	if entry.Kind == control.PathPickerEntryDirectory {
		return label + "/"
	}
	return label
}

func pathPickerSelectStaticElement(name, placeholder string, payload map[string]any, options []map[string]any, initialOption string) map[string]any {
	return selectStaticElement(name, placeholder, payload, options, initialOption)
}

func pathPickerFieldActionPayload(kind, pickerID, fieldName string) map[string]any {
	payload := actionPayloadPathPicker(kind, pickerID, "")
	if strings.TrimSpace(fieldName) != "" {
		payload[frontstagecontract.CardActionPayloadKeyFieldName] = strings.TrimSpace(fieldName)
	}
	return payload
}
