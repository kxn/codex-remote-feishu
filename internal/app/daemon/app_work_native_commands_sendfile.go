package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/worksettings"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type teamFileIntent struct {
	Source             workstore.PrivateActionSource `json:"source"`
	Binding            work.RoleBotBinding           `json:"binding"`
	ChatID             string                        `json:"chat_id"`
	ReplyToMessageID   string                        `json:"reply_to_message_id"`
	Path               string                        `json:"path"`
	Name               string                        `json:"name"`
	ContentID          string                        `json:"content_id"`
	NavigationRevision int64                         `json:"navigation_revision"`
	PreferenceScopeID  string                        `json:"preference_scope_id"`
	PreferenceRevision int64                         `json:"preference_revision"`
}

func (a *App) teamSendFileRoots(tx *workstore.Tx, source teamHumanSource) (roots []string, base, target string, revision int64, err error) {
	target, err = a.teamPickerTarget(tx, source)
	if err != nil {
		return
	}
	preference, e := tx.GetExecutionPreference(target)
	if e != nil && e != workstore.ErrNotFound {
		err = e
		return
	}
	if e == nil {
		revision = preference.Revision
		for _, id := range preference.SelectedLocationIDs {
			location, e := tx.GetLocation(id)
			if e != nil {
				err = e
				return
			}
			roots = append(roots, location.Cwd)
			if id == preference.DefaultLocationID {
				base = location.Cwd
			}
		}
	}
	context, _, e := worksettings.FrozenContext(tx, source.Authority)
	if e != nil {
		err = e
		return
	}
	workers, e := tx.ListWorkers(source.ScopeID)
	if e != nil {
		err = e
		return
	}
	for _, worker := range workers {
		if worker.ContextID != context.ID || worker.State == work.WorkerRetired || worker.LocationID == "" {
			continue
		}
		location, e := tx.GetLocation(worker.LocationID)
		if e != nil {
			err = e
			return
		}
		if location.IsolatedContextID == context.ID {
			roots = append(roots, location.Cwd)
			if base == "" {
				base = location.Cwd
			}
		}
	}
	return
}
func teamAuthorizedFilePath(path, base string, roots []string) (string, error) {
	if !filepath.IsAbs(path) {
		if base == "" {
			return "", fmt.Errorf("先明确选择执行目录，或使用本工作已生成成果的绝对路径")
		}
		path = filepath.Join(base, path)
	}
	actual, err := filepath.EvalSymlinks(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	for _, root := range roots {
		root, e := filepath.EvalSymlinks(root)
		if e != nil {
			continue
		}
		relative, e := filepath.Rel(root, actual)
		if e == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative) {
			return actual, nil
		}
	}
	return "", fmt.Errorf("该文件不属于你为当前工作明确选择的目录或它的独立工作区")
}
func (a *App) handleTeamSendFile(ctx context.Context, source teamHumanSource, action control.Action) (string, error) {
	fields := strings.Fields(action.Text)
	if len(fields) < 2 {
		return "", fmt.Errorf("用法：/sendfile 文件路径。文件必须来自当前已选目录或本工作区")
	}
	requested := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(action.Text), fields[0]))
	requested = strings.Trim(requested, "\"")
	id := teamID("file-publish", source.Key)
	var intent teamFileIntent
	var existing *work.Operation
	err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		if e := a.validateTeamPickerSource(tx, source); e != nil {
			return e
		}
		if op, e := tx.GetOperation(id); e == nil {
			body, e := tx.GetContent(source.ScopeID, op.RequestRef)
			if e != nil {
				return e
			}
			if e = json.Unmarshal(body.Data, &intent); e != nil {
				return e
			}
			if intent.Source != source.Authority {
				return workstore.ErrIdempotencyConflict
			}
			existing = &op
			return nil
		} else if e != workstore.ErrNotFound {
			return e
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if existing != nil {
		if existing.State == work.OperationConfirmed {
			return a.queueTeamPicker(ctx, source, "file.result", id, 1, workui.Panel{Title: "文件已发送", Paragraphs: []string{"文件已发送：" + intent.Name}})
		}
		return "", fmt.Errorf("这次文件上传或发送的结果仍待确认。请先查看聊天中是否已经收到文件，再决定是否重新发送")
	}
	var roots []string
	var base, target string
	var preferenceRevision int64
	err = a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		roots, base, target, preferenceRevision, e = a.teamSendFileRoots(tx, source)
		return e
	})
	if err != nil {
		return "", err
	}
	path, err := teamAuthorizedFilePath(requested, base, roots)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 30_000_000 {
		return "", fmt.Errorf("只能发送非空且不超过30 MB的普通文件")
	}
	bytes, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if len(bytes) > 30_000_000 {
		return "", fmt.Errorf("文件在读取时已超过30 MB")
	}
	content := work.NewContent(id+":file", source.ScopeID, http.DetectContentType(bytes), bytes, time.Now().UTC())
	// Source retains command/callback authority; a private file is a new chat
	// message, not a reply to that source (which may belong to an old topic).
	intent = teamFileIntent{Source: source.Authority, Binding: work.RoleBotBinding{RoleID: source.RoleID, GatewayRef: source.Gateway.GatewayRef, AppID: source.Gateway.AppID, IdentityGeneration: source.Gateway.BotGeneration}, ChatID: source.ChatID, Path: path, Name: filepath.Base(path), ContentID: content.ID, NavigationRevision: source.NavigationRevision, PreferenceScopeID: target, PreferenceRevision: preferenceRevision}
	err = a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		current, e := a.teamPickerTarget(tx, source)
		if e != nil {
			return e
		}
		if current != target {
			return workstore.ErrConflict
		}
		preference, e := tx.GetExecutionPreference(target)
		if e == workstore.ErrNotFound {
			preference.Revision = 0
		} else if e != nil {
			return e
		}
		if preference.Revision != preferenceRevision {
			return workstore.ErrConflict
		}
		if e = tx.PutContent(content); e != nil {
			return e
		}
		raw, e := json.Marshal(intent)
		if e != nil {
			return e
		}
		body := work.NewContent(id+":request", source.ScopeID, "application/json", raw, time.Now().UTC())
		if e = tx.PutContent(body); e != nil {
			return e
		}
		_, _, e = tx.PutOperation(work.Operation{ID: id, ScopeID: source.ScopeID, Actor: work.Actor{Kind: work.ActorPrincipal, ID: source.PrincipalID}, Kind: "feishu.file.publish", TargetID: source.ChatID, TargetGeneration: source.Gateway.BotGeneration, IdempotencyKey: source.Key, RequestRef: body.ID, RequestHash: body.SHA256, State: work.OperationSending, Revision: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}, 0)
		return e
	})
	if err != nil {
		return "", err
	}
	gateway, err := (teamDirectory{app: a, store: a.teamRuntime.store}).gateway(ctx, intent.Binding)
	if err != nil {
		return "", a.finishTeamFileFailure(ctx, id, err)
	}
	fileKey, err := gateway.ProjectChatAPI().UploadResultFile(ctx, feishu.UploadResultFileRequest{FileName: intent.Name, FileType: "stream", Content: content.Data})
	if err != nil {
		return "", a.finishTeamFileFailure(ctx, id, err)
	}
	err = a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		op, e := tx.GetOperation(id)
		if e != nil {
			return e
		}
		old := op.Revision
		op.Revision++
		op.Steps = append(op.Steps, work.OperationStep{ID: "saved", State: work.OperationConfirmed, Receipt: fileKey}, work.OperationStep{ID: "send", State: work.OperationSending})
		_, _, e = tx.PutOperation(op, old)
		return e
	})
	if err != nil {
		return "", err
	}
	receipt, err := gateway.ProjectChatAPI().PublishResultFile(ctx, feishu.PublishResultFileRequest{ChatID: intent.ChatID, ReplyToMessageID: intent.ReplyToMessageID, FileKey: fileKey, UUID: strings.TrimPrefix(id, "file-publish_")})
	if err != nil {
		return "", a.finishTeamFileFailure(ctx, id, err)
	}
	err = a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		op, e := tx.GetOperation(id)
		if e != nil {
			return e
		}
		old := op.Revision
		op.Revision++
		op.State = work.OperationConfirmed
		op.ExternalReceipt = stringMustJSON(receipt)
		op.Steps[1].State = work.OperationConfirmed
		op.Steps[1].Receipt = receipt.MessageID
		_, _, e = tx.PutOperation(op, old)
		return e
	})
	if err != nil {
		return "", err
	}
	return a.queueTeamPicker(ctx, source, "file.result", id, 1, workui.Panel{Title: "文件已发送", Paragraphs: []string{intent.Name, "文件已发送，可以在这条消息附近查看附件。"}})
}
func stringMustJSON(value any) string { raw, _ := json.Marshal(value); return string(raw) }
func (a *App) finishTeamFileFailure(ctx context.Context, id string, cause error) error {
	save, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	err := a.teamRuntime.store.Within(save, func(tx *workstore.Tx) error {
		op, e := tx.GetOperation(id)
		if e != nil {
			return e
		}
		old := op.Revision
		op.Revision++
		op.State = work.OperationUnknown
		op.LastError = cause.Error()
		_, _, e = tx.PutOperation(op, old)
		return e
	})
	if err != nil {
		return fmt.Errorf("%v; 持久失败记录：%w", cause, err)
	}
	return cause
}
