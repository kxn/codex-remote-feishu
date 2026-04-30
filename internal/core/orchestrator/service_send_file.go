package orchestrator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const sendFilePathPickerConsumerKind = "send_file"
const sendFileLargeThresholdBytes int64 = 100 * 1024 * 1024

type sendFilePathPickerConsumer struct{}

func (sendFilePathPickerConsumer) PathPickerOwnsConfirmLifecycle() bool { return true }

func (sendFilePathPickerConsumer) PathPickerConfirmed(_ *Service, surface *state.SurfaceConsoleRecord, result control.PathPickerResult) []eventcontract.Event {
	if surface == nil {
		return nil
	}
	selectedPath := strings.TrimSpace(result.SelectedPath)
	if selectedPath == "" {
		return notice(surface, "send_file_invalid", "未选中文件，请重新选择。")
	}
	return []eventcontract.Event{{
		Kind:             eventcontract.KindDaemonCommand,
		GatewayID:        surface.GatewayID,
		SurfaceSessionID: surface.SurfaceSessionID,
		DaemonCommand: &control.DaemonCommand{
			Kind:             control.DaemonCommandSendIMFile,
			GatewayID:        surface.GatewayID,
			SurfaceSessionID: surface.SurfaceSessionID,
			PickerID:         strings.TrimSpace(result.PickerID),
			LocalPath:        selectedPath,
		},
	}}
}

func (sendFilePathPickerConsumer) PathPickerCancelled(s *Service, surface *state.SurfaceConsoleRecord, result control.PathPickerResult) []eventcontract.Event {
	return s.sendFileCancelledTerminalEvents(surface, result)
}

func (s *Service) openSendFilePicker(surface *state.SurfaceConsoleRecord) []eventcontract.Event {
	return s.openSendFilePickerWithInline(surface, "", false)
}

func sendFileReplacementSummarySections(lines ...string) []control.FeishuCardTextSection {
	bodyLines := make([]string, 0, len(lines))
	for _, line := range lines {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			bodyLines = append(bodyLines, trimmed)
		}
	}
	if len(bodyLines) == 0 {
		return nil
	}
	return []control.FeishuCardTextSection{{Lines: bodyLines}}
}

func sendFileInlineTerminalEvent(surface *state.SurfaceConsoleRecord, messageID, title, theme string, lines ...string) eventcontract.Event {
	_ = messageID
	view := control.NormalizeFeishuPageView(control.FeishuPageView{
		Title:           strings.TrimSpace(title),
		ThemeKey:        strings.TrimSpace(theme),
		Interactive:     false,
		SummarySections: sendFileReplacementSummarySections(lines...),
	})
	return surfaceEventFromPayload(
		surface,
		eventcontract.PagePayload{View: view},
		eventcontract.EventMeta{
			InlineReplaceMode: eventcontract.InlineReplaceCurrentCard,
		},
	)
}

func sendFileUnavailableEvents(surface *state.SurfaceConsoleRecord, code, sourceMessageID, text string, inline bool) []eventcontract.Event {
	if surface == nil {
		return nil
	}
	text = strings.TrimSpace(text)
	if inline && strings.TrimSpace(sourceMessageID) != "" {
		return []eventcontract.Event{sendFileInlineTerminalEvent(surface, sourceMessageID, "当前不能发送文件", "error", text)}
	}
	return notice(surface, code, text)
}

func (s *Service) sendFileCancelledTerminalEvents(surface *state.SurfaceConsoleRecord, result control.PathPickerResult) []eventcontract.Event {
	if surface == nil {
		return nil
	}
	if strings.TrimSpace(result.MessageID) == "" {
		return notice(surface, "send_file_cancelled", "已取消发送文件。")
	}
	statusSections := []control.FeishuCardTextSection{{Lines: []string{"本次文件发送已取消。"}}}
	if selected := strings.TrimSpace(filepath.Base(result.SelectedPath)); selected != "" {
		statusSections = append([]control.FeishuCardTextSection{{
			Label: "文件",
			Lines: []string{selected},
		}}, statusSections...)
	}
	view := control.NormalizeFeishuPathPickerView(control.FeishuPathPickerView{
		PickerID:       strings.TrimSpace(result.PickerID),
		MessageID:      strings.TrimSpace(result.MessageID),
		Mode:           control.PathPickerModeFile,
		Title:          "发送文件",
		SelectedPath:   strings.TrimSpace(result.SelectedPath),
		Phase:          frontstagecontract.PhaseCancelled,
		StatusTitle:    "已取消发送文件",
		StatusSections: statusSections,
	})
	return []eventcontract.Event{s.pathPickerViewEvent(surface, view, false)}
}

func (s *Service) openSendFilePickerWithInline(surface *state.SurfaceConsoleRecord, sourceMessageID string, inline bool) []eventcontract.Event {
	if surface == nil {
		return nil
	}
	if s.normalizeSurfaceProductMode(surface) != state.ProductModeNormal {
		return sendFileUnavailableEvents(surface, "send_file_normal_only", sourceMessageID, "当前处于 vscode 模式，暂不支持从飞书选择文件发送。请先 `/mode codex`。", inline)
	}
	if strings.TrimSpace(surface.AttachedInstanceID) == "" {
		return sendFileUnavailableEvents(surface, "send_file_requires_workspace", sourceMessageID, "当前还没有接管工作区。请先 `/list` 选择工作区，然后再发送文件。", inline)
	}
	workspaceKey := s.surfaceCurrentWorkspaceKey(surface)
	if workspaceKey == "" {
		return sendFileUnavailableEvents(surface, "send_file_requires_workspace", sourceMessageID, "当前还没有可用的工作区路径，请重新 `/list` 选择工作区后再试。", inline)
	}
	if inst := s.root.Instances[surface.AttachedInstanceID]; inst != nil {
		if root := strings.TrimSpace(inst.WorkspaceRoot); root != "" {
			workspaceKey = root
		}
	}
	return s.openPathPickerWithInline(surface, surface.ActorUserID, control.PathPickerRequest{
		Mode:            control.PathPickerModeFile,
		Title:           "选择要发送的文件",
		RootPath:        workspaceKey,
		InitialPath:     filepath.Clean(workspaceKey),
		SourceMessageID: strings.TrimSpace(sourceMessageID),
		ConfirmLabel:    "发送到当前聊天",
		CancelLabel:     "取消",
		ConsumerKind:    sendFilePathPickerConsumerKind,
	}, inline)
}

func (s *Service) HandleSendFilePreflightFailure(surfaceID, pickerID, text string) []eventcontract.Event {
	surface := s.root.Surfaces[strings.TrimSpace(surfaceID)]
	if surface == nil {
		return []eventcontract.Event{{
			Kind:             eventcontract.KindNotice,
			SurfaceSessionID: strings.TrimSpace(surfaceID),
			Notice: &control.Notice{
				Code:  "send_file_failed",
				Title: "发送文件失败",
				Text:  strings.TrimSpace(text),
			},
		}}
	}
	record, blocked := s.requireActivePathPicker(surface, pickerID, "")
	if blocked != nil {
		return blocked
	}
	if strings.TrimSpace(record.ConsumerKind) != sendFilePathPickerConsumerKind {
		return notice(surface, "send_file_failed", strings.TrimSpace(text))
	}
	setPathPickerStatus(record, "发送文件失败", text, nil, "")
	view, err := s.buildPathPickerView(surface, record)
	if err != nil {
		return notice(surface, "send_file_failed", strings.TrimSpace(text))
	}
	return []eventcontract.Event{s.pathPickerViewEvent(surface, view, false)}
}

func (s *Service) HandleSendFileStarted(surfaceID, pickerID, selectedPath string, sizeBytes int64) ([]eventcontract.Event, bool) {
	surface := s.root.Surfaces[strings.TrimSpace(surfaceID)]
	if surface == nil {
		return nil, false
	}
	record, blocked := s.requireActivePathPicker(surface, pickerID, "")
	if blocked != nil {
		return blocked, false
	}
	if strings.TrimSpace(record.ConsumerKind) != sendFilePathPickerConsumerKind {
		return notice(surface, "send_file_failed", "这张发送文件卡片已失效，请重新发起。"), false
	}
	view := control.NormalizeFeishuPathPickerView(control.FeishuPathPickerView{
		PickerID:       strings.TrimSpace(record.PickerID),
		MessageID:      strings.TrimSpace(record.MessageID),
		Mode:           control.PathPickerModeFile,
		Title:          strings.TrimSpace(firstNonEmpty(record.Title, "发送文件")),
		SelectedPath:   strings.TrimSpace(selectedPath),
		Phase:          frontstagecontract.PhaseSucceeded,
		StatusTitle:    "已开始发送，可继续其他操作",
		StatusSections: sendFileStartedStatusSections(selectedPath, sizeBytes),
	})
	s.clearSurfacePathPicker(surface)
	return []eventcontract.Event{s.pathPickerViewEvent(surface, view, false)}, true
}

func sendFileStartedStatusSections(selectedPath string, sizeBytes int64) []control.FeishuCardTextSection {
	name := strings.TrimSpace(filepath.Base(strings.TrimSpace(selectedPath)))
	if name == "" {
		name = strings.TrimSpace(selectedPath)
	}
	sections := []control.FeishuCardTextSection{
		{Label: "文件", Lines: []string{name}},
		{Label: "大小", Lines: []string{formatSendFileSize(sizeBytes)}},
		{Label: "结果", Lines: []string{"后台已开始发送；成功后会直接出现在当前聊天里。"}},
	}
	if sizeBytes > sendFileLargeThresholdBytes {
		sections = append(sections, control.FeishuCardTextSection{Label: "提示", Lines: []string{"文件较大，请耐心等待"}})
	}
	return sections
}

func formatSendFileSize(sizeBytes int64) string {
	if sizeBytes < 0 {
		return "未知"
	}
	const unit = 1024
	if sizeBytes < unit {
		return fmt.Sprintf("%d B", sizeBytes)
	}
	div, exp := int64(unit), 0
	for n := sizeBytes / unit; n >= unit && exp < 5; n /= unit {
		div *= unit
		exp++
	}
	value := float64(sizeBytes) / float64(div)
	units := []string{"KB", "MB", "GB", "TB", "PB", "EB"}
	if exp >= len(units) {
		return fmt.Sprintf("%d B", sizeBytes)
	}
	return fmt.Sprintf("%.1f %s", value, units[exp])
}

func ValidateSendFilePath(path string) (int64, error) {
	info, err := os.Stat(strings.TrimSpace(path))
	switch {
	case os.IsNotExist(err):
		return 0, fmt.Errorf("所选文件已不存在，请重新选择。")
	case err != nil:
		return 0, fmt.Errorf("读取文件失败，请确认这个文件当前可访问。")
	case info.IsDir():
		return 0, fmt.Errorf("当前只能发送文件，不能发送目录。")
	default:
		return info.Size(), nil
	}
}
