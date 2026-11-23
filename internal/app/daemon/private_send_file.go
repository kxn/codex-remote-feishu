package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

type privateFileReceipt struct {
	Version  int
	EffectID string
	Command  control.DaemonCommand
	Size     int64
	Snapshot string
	SHA256   string
	Error    string
	Unknown  bool
	Result   feishu.IMFileSendResult
}

// File preparation and the actual upload are separate committed effects. The
// original picker can finish immediately while the slow upload runs elsewhere.
func runPrivateSendFile(ctx context.Context, host *App, p *privatechat.Processor, j *privatechat.Journal, o privatechat.Owner, generation int64, id string) ([]privatechat.EffectRecord, error) {
	record, err := j.LoadEffect(ctx, o, generation, id)
	if err != nil {
		return nil, err
	}
	event, _, err := privatechat.DecodeStoredEffect(record.Effect.Body)
	if err != nil {
		return nil, err
	}
	cmd := event.DaemonCommand
	if cmd == nil || cmd.SurfaceSessionID != o.SurfaceID || event.SurfaceSessionID != o.SurfaceID || (cmd.Kind != control.DaemonCommandSendIMFile && cmd.Kind != control.DaemonCommandPrivateSendFile) {
		return nil, privatechat.ErrJournalConflict
	}
	reducer := privateFileReducer{host: host}
	if record.State == "confirmed" {
		return p.ApplyLocalHostReceipt(ctx, o, generation, id, reducer)
	}
	if record.State != "prepared" && record.State != "unknown" {
		return nil, privatechat.ErrJournalState
	}
	result := privateFileReceipt{Version: 1, EffectID: id, Command: *cmd}
	if cmd.Kind == control.DaemonCommandSendIMFile {
		// Validation is read-only; all picker changes occur after this receipt.
		result.Size, err = orchestrator.ValidateSendFilePath(cmd.LocalPath)
		if err != nil {
			result.Error = err.Error()
		}
		if _, ok := host.gateway.(feishu.IMFileSender); !ok {
			result.Error = "当前运行环境暂不支持发送飞书文件消息。"
		}
		if result.Error == "" {
			err = p.InspectLocalObservation(ctx, o, generation, id, func(app *privatechat.App, _ privatechat.EffectRecord) error {
				if e := (privateHostReducer{host}).check(app); e != nil {
					return e
				}
				resolved, e := host.resolveToolSurfaceContextLocked(o.SurfaceID)
				if e != nil {
					result.Error = sendFileToolErrorText(e)
					return nil
				}
				if resolved.GatewayID != o.GatewayID || resolved.ChatID != o.ChatID || resolved.ActorUserID != o.UserID {
					return privatechat.ErrJournalConflict
				}
				return nil
			})
			if err != nil {
				return nil, err
			}
		}
		if result.Error == "" {
			result.Snapshot, result.Size, result.SHA256, err = snapshotPrivateSendFile(ctx, host.headlessRuntime.Paths.StateDir, id, cmd.LocalPath)
			if err != nil {
				result.Error = err.Error()
			}
		}
	} else if record.State == "unknown" {
		result.Unknown = true
		result.Error = "上次文件发送中断，无法确认飞书是否已收到。请先查看聊天中的文件消息，再决定是否重新发送。"
	}
	if record.State == "prepared" {
		if err = j.MarkEffectSending(ctx, o, generation, id); err != nil {
			return nil, err
		}
	}
	if cmd.Kind == control.DaemonCommandPrivateSendFile && !result.Unknown {
		sender, ok := host.gateway.(feishu.IMFileSender)
		if !ok {
			result.Error = "当前运行环境暂不支持发送飞书文件消息。"
		} else {
			digest := sha256.Sum256([]byte(id))
			request := feishu.IMFileSendRequest{GatewayID: o.GatewayID, SurfaceSessionID: o.SurfaceID, ChatID: o.ChatID, ActorUserID: o.UserID, Path: cmd.LocalPath, IdempotencyKey: hex.EncodeToString(digest[:20])}
			if cmd.FileSHA256 != "" {
				request.Content, err = os.ReadFile(cmd.LocalPath)
				if err == nil {
					sum := sha256.Sum256(request.Content)
					if hex.EncodeToString(sum[:]) != cmd.FileSHA256 {
						err = fmt.Errorf("冻结的文件内容已更改，请重新选择文件。")
					}
				}
				if err != nil {
					result.Error = err.Error()
				} else {
					request.FileName = filepath.Base(cmd.LocalPath)
				}
			}
			sendCtx, cancel := context.WithTimeout(ctx, sendIMFileCommandTimeout)
			attempted := err == nil
			if attempted {
				result.Result, err = sender.SendIMFile(sendCtx, request)
			}
			cancel()
			if err != nil {
				result.Error = err.Error()
				result.Unknown = attempted
			}
			if err == nil && (result.Result.MessageID == "" || result.Result.GatewayID != o.GatewayID || result.Result.SurfaceSessionID != o.SurfaceID) {
				result.Unknown = true
				result.Error = "飞书文件发送响应缺少完整的消息标识，请检查聊天中的文件消息。"
			}
		}
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err = j.ConfirmLocalEffect(saveCtx, o, generation, id, raw, privateLocalVerifier{o, generation, record.Effect, raw}); err != nil {
		return nil, err
	}
	return p.ApplyLocalHostReceipt(ctx, o, generation, id, reducer)
}

type privateFileReducer struct{ host *App }

func (r privateFileReducer) ReduceHostReceipt(app *privatechat.App, effect privatechat.Effect, raw json.RawMessage) ([]eventcontract.Event, error) {
	if err := (privateHostReducer{r.host}).check(app); err != nil {
		return nil, err
	}
	event, _, err := privatechat.DecodeStoredEffect(effect.Body)
	if err != nil {
		return nil, err
	}
	var result privateFileReceipt
	if json.Unmarshal(raw, &result) != nil || result.Version != 1 || result.EffectID != effect.ID || event.DaemonCommand == nil {
		return nil, privatechat.ErrJournalConflict
	}
	cmd := *event.DaemonCommand
	expected, _ := json.Marshal(cmd)
	actual, _ := json.Marshal(result.Command)
	if string(expected) != string(actual) {
		return nil, privatechat.ErrJournalConflict
	}
	if result.Error != "" {
		if cmd.Kind == control.DaemonCommandSendIMFile {
			return r.host.sendFilePreflightFailureLocked(cmd, "send_file_invalid", result.Error), nil
		}
		code := "send_file_failed"
		if result.Unknown {
			code = "send_file_result_unknown"
		}
		return sendFileNotice(cmd.SurfaceSessionID, code, result.Error), nil
	}
	if cmd.Kind == control.DaemonCommandPrivateSendFile {
		if cmd.PickerID != "" {
			return nil, nil
		}
		return sendFileNotice(cmd.SurfaceSessionID, "send_file_sent", fmt.Sprintf("已把 `%s` 发送到当前聊天。", filepath.Base(cmd.LocalPath))), nil
	}
	var events []eventcontract.Event
	if cmd.PickerID != "" {
		var started bool
		events, started = app.Service().HandleSendFileStarted(cmd.SurfaceSessionID, cmd.PickerID, cmd.LocalPath, result.Size)
		if !started {
			return events, nil
		}
	}
	cmd.Kind = control.DaemonCommandPrivateSendFile
	if result.Snapshot == "" {
		return nil, privatechat.ErrJournalConflict
	}
	cmd.LocalPath = result.Snapshot
	cmd.FileSHA256 = result.SHA256
	events = append(events, eventcontract.Event{Kind: eventcontract.KindDaemonCommand, SurfaceSessionID: cmd.SurfaceSessionID, GatewayID: cmd.GatewayID, DaemonCommand: &cmd})
	return events, nil
}
