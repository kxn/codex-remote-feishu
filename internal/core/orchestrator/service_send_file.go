package orchestrator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const sendFilePathPickerConsumerKind = "send_file"
const sendFileLargeThresholdBytes int64 = 100 * 1024 * 1024

type sendFilePathPickerConsumer struct{}

func (sendFilePathPickerConsumer) PathPickerOwnsConfirmLifecycle() bool { return true }

func (sendFilePathPickerConsumer) PathPickerConfirmed(_ *Service, surface *state.SurfaceConsoleRecord, result control.PathPickerResult) []control.UIEvent {
	if surface == nil {
		return nil
	}
	selectedPath := strings.TrimSpace(result.SelectedPath)
	if selectedPath == "" {
		return notice(surface, "send_file_invalid", "未选中文件，请重新选择。")
	}
	return []control.UIEvent{{
		Kind:             control.UIEventDaemonCommand,
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

func (sendFilePathPickerConsumer) PathPickerCancelled(_ *Service, surface *state.SurfaceConsoleRecord, _ control.PathPickerResult) []control.UIEvent {
	return notice(surface, "send_file_cancelled", "已取消发送文件。")
}

func (s *Service) openSendFilePicker(surface *state.SurfaceConsoleRecord) []control.UIEvent {
	return s.openSendFilePickerWithInline(surface, false)
}

func (s *Service) openSendFilePickerWithInline(surface *state.SurfaceConsoleRecord, inline bool) []control.UIEvent {
	if surface == nil {
		return nil
	}
	if s.normalizeSurfaceProductMode(surface) != state.ProductModeNormal {
		return notice(surface, "send_file_normal_only", "当前处于 vscode 模式，暂不支持从飞书选择文件发送。请先 `/mode normal`。")
	}
	if strings.TrimSpace(surface.AttachedInstanceID) == "" {
		return notice(surface, "send_file_requires_workspace", "当前还没有接管工作区。请先 `/list` 选择工作区，然后再发送文件。")
	}
	workspaceKey := s.surfaceCurrentWorkspaceKey(surface)
	if workspaceKey == "" {
		return notice(surface, "send_file_requires_workspace", "当前还没有可用的工作区路径，请重新 `/list` 选择工作区后再试。")
	}
	if inst := s.root.Instances[surface.AttachedInstanceID]; inst != nil {
		if root := strings.TrimSpace(inst.WorkspaceRoot); root != "" {
			workspaceKey = root
		}
	}
	return s.openPathPickerWithInline(surface, surface.ActorUserID, control.PathPickerRequest{
		Mode:         control.PathPickerModeFile,
		Title:        "选择要发送的文件",
		RootPath:     workspaceKey,
		InitialPath:  filepath.Clean(workspaceKey),
		ConfirmLabel: "发送到当前聊天",
		CancelLabel:  "取消",
		ConsumerKind: sendFilePathPickerConsumerKind,
	}, inline)
}

func (s *Service) HandleSendFilePreflightFailure(surfaceID, pickerID, text string) []control.UIEvent {
	surface := s.root.Surfaces[strings.TrimSpace(surfaceID)]
	if surface == nil {
		return []control.UIEvent{{
			Kind:             control.UIEventNotice,
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
	record.Hint = strings.TrimSpace(text)
	view, err := s.buildPathPickerView(record)
	if err != nil {
		return notice(surface, "send_file_failed", strings.TrimSpace(text))
	}
	return []control.UIEvent{s.pathPickerViewEvent(surface, view, false)}
}

func (s *Service) HandleSendFileStarted(surfaceID, pickerID, selectedPath string, sizeBytes int64) ([]control.UIEvent, bool) {
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
	view := control.FeishuPathPickerView{
		PickerID:     strings.TrimSpace(record.PickerID),
		MessageID:    strings.TrimSpace(record.MessageID),
		Mode:         control.PathPickerModeFile,
		Title:        strings.TrimSpace(firstNonEmpty(record.Title, "发送文件")),
		SelectedPath: strings.TrimSpace(selectedPath),
		Terminal:     true,
		StatusTitle:  "已开始发送，可继续其他操作",
		StatusText:   sendFileStartedStatusText(selectedPath, sizeBytes),
	}
	s.clearSurfacePathPicker(surface)
	return []control.UIEvent{s.pathPickerViewEvent(surface, view, false)}, true
}

func sendFileStartedStatusText(selectedPath string, sizeBytes int64) string {
	name := strings.TrimSpace(filepath.Base(strings.TrimSpace(selectedPath)))
	if name == "" {
		name = strings.TrimSpace(selectedPath)
	}
	parts := []string{
		"**文件**\n`" + name + "`",
		"**大小**\n`" + formatSendFileSize(sizeBytes) + "`",
		"**结果**\n后台已开始发送；成功后会直接出现在当前聊天里。",
	}
	if sizeBytes > sendFileLargeThresholdBytes {
		parts = append(parts, "**提示**\n文件较大，请耐心等待")
	}
	return strings.Join(parts, "\n\n")
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
