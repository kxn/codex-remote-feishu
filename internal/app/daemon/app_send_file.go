package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

const sendIMFileCommandTimeout = 2 * time.Minute

func (a *App) handleSendIMFileCommand(command control.DaemonCommand) []control.UIEvent {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.shuttingDown {
		return nil
	}
	return a.handleSendIMFileCommandLocked(command)
}

func (a *App) handleSendIMFileCommandLocked(command control.DaemonCommand) []control.UIEvent {
	path := strings.TrimSpace(command.LocalPath)
	if path == "" {
		return sendFileNotice(command.SurfaceSessionID, "send_file_invalid", "文件路径无效，请重新选择后再试。")
	}
	info, err := os.Stat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return sendFileNotice(command.SurfaceSessionID, "send_file_not_found", "所选文件已不存在，请重新选择。")
	case err != nil:
		return sendFileNotice(command.SurfaceSessionID, "send_file_access_failed", "读取文件失败，请确认这个文件当前可访问。")
	case info.IsDir():
		return sendFileNotice(command.SurfaceSessionID, "send_file_invalid", "当前只能发送文件，不能发送目录。")
	}

	resolved, toolErr := a.resolveToolSurfaceContextLocked(command.SurfaceSessionID)
	if toolErr != nil {
		return sendFileNotice(command.SurfaceSessionID, "send_file_unavailable", sendFileToolErrorText(toolErr))
	}

	sender, ok := a.gateway.(feishu.IMFileSender)
	if !ok {
		return sendFileNotice(command.SurfaceSessionID, "send_file_unavailable", "当前运行环境暂不支持发送飞书文件消息。")
	}

	sendCtx, cancel := context.WithTimeout(context.Background(), sendIMFileCommandTimeout)
	defer cancel()

	// Do not hold the app lock across Feishu upload/send IO.
	request := feishu.IMFileSendRequest{
		GatewayID:        resolved.GatewayID,
		SurfaceSessionID: resolved.SurfaceSessionID,
		ChatID:           resolved.ChatID,
		ActorUserID:      resolved.ActorUserID,
		Path:             path,
	}
	a.mu.Unlock()
	result, err := sender.SendIMFile(sendCtx, request)
	a.mu.Lock()
	if err != nil {
		_ = a.observeFeishuPermissionError(resolved.GatewayID, err)
		var sendErr *feishu.IMFileSendError
		if errors.As(err, &sendErr) {
			switch sendErr.Code {
			case feishu.IMFileSendErrorUploadFailed:
				return sendFileNotice(command.SurfaceSessionID, "send_file_upload_failed", "文件上传失败，请稍后重试。")
			case feishu.IMFileSendErrorSendFailed, feishu.IMFileSendErrorMissingReceiveTarget, feishu.IMFileSendErrorGatewayNotRunning:
				return sendFileNotice(command.SurfaceSessionID, "send_file_failed", "文件发送失败，请稍后重试。")
			}
		}
		return sendFileNotice(command.SurfaceSessionID, "send_file_failed", "文件发送失败，请稍后重试。")
	}
	fileName := strings.TrimSpace(result.FileName)
	if fileName == "" {
		fileName = filepath.Base(path)
	}
	return []control.UIEvent{{
		Kind:             control.UIEventNotice,
		SurfaceSessionID: command.SurfaceSessionID,
		Notice: &control.Notice{
			Code:  "send_file_sent",
			Title: "文件已发送",
			Text:  fmt.Sprintf("已把 `%s` 发送到当前聊天。", fileName),
		},
	}}
}

func sendFileToolErrorText(err *toolError) string {
	if err == nil {
		return "当前无法发送文件，请稍后再试。"
	}
	switch err.Code {
	case "surface_not_found":
		return "当前聊天对应的会话不存在，请重新进入后再试。"
	case "surface_not_attached":
		return "当前还没有接管工作区。请先 `/list` 选择工作区，然后再发送文件。"
	case "surface_mode_unsupported":
		return "当前处于 vscode 模式，暂不支持从飞书选择文件发送。请先 `/mode normal`。"
	default:
		if msg := strings.TrimSpace(err.Message); msg != "" {
			return msg
		}
		return "当前无法发送文件，请稍后再试。"
	}
}

func sendFileNotice(surfaceID, code, text string) []control.UIEvent {
	title := "发送文件失败"
	if code == "send_file_sent" {
		title = "文件已发送"
	}
	return []control.UIEvent{{
		Kind:             control.UIEventNotice,
		SurfaceSessionID: surfaceID,
		Notice: &control.Notice{
			Code:  code,
			Title: title,
			Text:  text,
		},
	}}
}
