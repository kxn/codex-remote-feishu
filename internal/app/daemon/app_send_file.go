package daemon

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

const sendIMFileCommandTimeout = 2 * time.Minute

type preparedSendIMFile struct {
	sender   feishu.IMFileSender
	request  feishu.IMFileSendRequest
	fileName string
	fileSize int64
}

func (a *App) handleSendIMFileCommand(command control.DaemonCommand) []eventcontract.Event {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.shuttingDown {
		return nil
	}
	return a.handleSendIMFileCommandLocked(command)
}

func (a *App) handleSendIMFileCommandLocked(command control.DaemonCommand) []eventcontract.Event {
	prepared, failEvents, ok := a.prepareSendIMFileLocked(command)
	if !ok {
		return failEvents
	}
	if strings.TrimSpace(command.PickerID) != "" {
		startEvents, started := a.service.HandleSendFileStarted(
			command.SurfaceSessionID,
			command.PickerID,
			command.LocalPath,
			prepared.fileSize,
		)
		if !started {
			return startEvents
		}
		a.startSendIMFileBackground(command, prepared)
		return startEvents
	}
	return a.runPreparedSendIMFileLocked(command, prepared)
}

func (a *App) prepareSendIMFileLocked(command control.DaemonCommand) (preparedSendIMFile, []eventcontract.Event, bool) {
	path := strings.TrimSpace(command.LocalPath)
	if path == "" {
		return preparedSendIMFile{}, a.sendFilePreflightFailureLocked(command, "send_file_invalid", "文件路径无效，请重新选择后再试。"), false
	}
	fileSize, err := orchestrator.ValidateSendFilePath(path)
	if err != nil {
		code := "send_file_invalid"
		switch {
		case strings.Contains(err.Error(), "不存在"):
			code = "send_file_not_found"
		case strings.Contains(err.Error(), "可访问"):
			code = "send_file_access_failed"
		}
		return preparedSendIMFile{}, a.sendFilePreflightFailureLocked(command, code, err.Error()), false
	}
	resolved, toolErr := a.resolveToolSurfaceContextLocked(command.SurfaceSessionID)
	if toolErr != nil {
		return preparedSendIMFile{}, a.sendFilePreflightFailureLocked(command, "send_file_unavailable", sendFileToolErrorText(toolErr)), false
	}
	sender, ok := a.gateway.(feishu.IMFileSender)
	if !ok {
		return preparedSendIMFile{}, a.sendFilePreflightFailureLocked(command, "send_file_unavailable", "当前运行环境暂不支持发送飞书文件消息。"), false
	}
	return preparedSendIMFile{
		sender: sender,
		request: feishu.IMFileSendRequest{
			GatewayID:        resolved.GatewayID,
			SurfaceSessionID: resolved.SurfaceSessionID,
			ChatID:           resolved.ChatID,
			ActorUserID:      resolved.ActorUserID,
			Path:             path,
		},
		fileName: filepath.Base(path),
		fileSize: fileSize,
	}, nil, true
}

func (a *App) runPreparedSendIMFileLocked(command control.DaemonCommand, prepared preparedSendIMFile) []eventcontract.Event {
	a.mu.Unlock()
	result, err := a.sendPreparedIMFile(prepared)
	a.mu.Lock()
	if err != nil {
		_ = a.observeFeishuPermissionError(prepared.request.GatewayID, err)
		return sendFileFailureEvents(command.SurfaceSessionID, err)
	}
	fileName := strings.TrimSpace(result.FileName)
	if fileName == "" {
		fileName = strings.TrimSpace(prepared.fileName)
	}
	return []eventcontract.Event{{
		Kind:             eventcontract.KindNotice,
		SurfaceSessionID: command.SurfaceSessionID,
		Notice: &control.Notice{
			Code:  "send_file_sent",
			Title: "文件已发送",
			Text:  fmt.Sprintf("已把 `%s` 发送到当前聊天。", fileName),
		},
	}}
}

func (a *App) startSendIMFileBackground(command control.DaemonCommand, prepared preparedSendIMFile) {
	go func() {
		result, err := a.sendPreparedIMFile(prepared)
		a.mu.Lock()
		defer a.mu.Unlock()
		if a.shuttingDown {
			return
		}
		if err != nil {
			_ = a.observeFeishuPermissionError(prepared.request.GatewayID, err)
			a.handleUIEventsLocked(context.Background(), sendFileFailureEvents(command.SurfaceSessionID, err))
			return
		}
		fileName := strings.TrimSpace(result.FileName)
		if fileName == "" {
			fileName = strings.TrimSpace(prepared.fileName)
		}
		a.debugf("send file background success: surface=%s file=%s size=%d", command.SurfaceSessionID, fileName, prepared.fileSize)
	}()
}

func (a *App) sendPreparedIMFile(prepared preparedSendIMFile) (feishu.IMFileSendResult, error) {
	sendCtx, cancel := context.WithTimeout(context.Background(), sendIMFileCommandTimeout)
	defer cancel()
	result, err := prepared.sender.SendIMFile(sendCtx, prepared.request)
	if err != nil {
		return feishu.IMFileSendResult{}, err
	}
	return result, nil
}

func (a *App) sendFilePreflightFailureLocked(command control.DaemonCommand, code, text string) []eventcontract.Event {
	if strings.TrimSpace(command.PickerID) != "" {
		if events := a.service.HandleSendFilePreflightFailure(command.SurfaceSessionID, command.PickerID, text); len(events) != 0 {
			return events
		}
	}
	return sendFileNotice(command.SurfaceSessionID, code, text)
}

func sendFileFailureEvents(surfaceID string, err error) []eventcontract.Event {
	var sendErr *feishu.IMFileSendError
	if errors.As(err, &sendErr) {
		switch sendErr.Code {
		case feishu.IMFileSendErrorUploadFailed:
			return sendFileNotice(surfaceID, "send_file_upload_failed", "文件上传失败，请稍后重试。")
		case feishu.IMFileSendErrorSendFailed, feishu.IMFileSendErrorMissingReceiveTarget, feishu.IMFileSendErrorGatewayNotRunning:
			return sendFileNotice(surfaceID, "send_file_failed", "文件发送失败，请稍后重试。")
		}
	}
	return sendFileNotice(surfaceID, "send_file_failed", "文件发送失败，请稍后重试。")
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
		return "当前处于 vscode 模式，暂不支持从飞书选择文件发送。请先 `/mode codex`。"
	default:
		if msg := strings.TrimSpace(err.Message); msg != "" {
			return msg
		}
		return "当前无法发送文件，请稍后再试。"
	}
}

func sendFileNotice(surfaceID, code, text string) []eventcontract.Event {
	title := "发送文件失败"
	if code == "send_file_sent" {
		title = "文件已发送"
	}
	return []eventcontract.Event{{
		Kind:             eventcontract.KindNotice,
		SurfaceSessionID: surfaceID,
		Notice: &control.Notice{
			Code:  code,
			Title: title,
			Text:  text,
		},
	}}
}
