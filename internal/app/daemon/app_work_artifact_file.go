package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// sendTeamSavedArtifactAsset can only export an asset inside the exact saved
// version of this reader. There is no caller-selected content ID or OS path.
func (a *App) sendTeamSavedArtifactAsset(ctx context.Context, interaction workstore.HumanInteraction, view workstore.UIView) error {
	r := a.teamRuntime
	var asset workservice.SavedDocumentAsset
	var operation work.Operation
	perform := false
	err := r.store.Within(ctx, func(tx *workstore.Tx) error {
		if err := tx.ValidateViewInteraction(view, interaction); err != nil {
			return err
		}
		scope, err := tx.GetScope(view.ScopeID)
		if err != nil {
			return err
		}
		if scope.Kind == work.ScopeProject {
			if err = tx.RequireProjectPrincipalAccess(scope.ID, interaction.PrincipalID, time.Now()); err != nil {
				return err
			}
		}
		body, err := tx.GetContent(view.ScopeID, view.ID+":artifact-cursor")
		if err != nil {
			return err
		}
		var cursor artifactCursor
		if err = json.Unmarshal(body.Data, &cursor); err != nil {
			return err
		}
		if cursor.Index < 0 || cursor.Index >= len(cursor.References) {
			return fmt.Errorf("artifact selection invalid")
		}
		version, err := tx.GetResourceVersion(view.ScopeID, cursor.References[cursor.Index].VersionID)
		if err != nil {
			return err
		}
		content, err := tx.GetContent(view.ScopeID, version.ContentRef)
		if err != nil {
			return err
		}
		if content.SHA256 != strings.TrimPrefix(version.ContentHash, "sha256:") {
			return fmt.Errorf("saved version integrity mismatch")
		}
		var saved struct {
			Assets []workservice.SavedDocumentAsset `json:"assets"`
		}
		if err = json.Unmarshal(content.Data, &saved); err != nil {
			return err
		}
		index, err := businessPageInt(interaction.Envelope.Values["asset"])
		if err != nil {
			return err
		}
		if index < 0 || index >= len(saved.Assets) {
			return fmt.Errorf("attachment selection invalid")
		}
		asset = saved.Assets[index]
		sum := sha256.Sum256(asset.Content)
		if hex.EncodeToString(sum[:]) != asset.SHA256 || len(asset.Content) == 0 || len(asset.Content) > 30_000_000 {
			return fmt.Errorf("saved attachment bytes or digest invalid")
		}
		id := workui.ID("artifact.file", version.ID, asset.SHA256, view.ChatID, interaction.PrincipalID, fmt.Sprint(view.Binding.IdentityGeneration))
		if existing, e := tx.GetOperation(id); e == nil {
			operation = existing
			return nil
		} else if e != workstore.ErrNotFound {
			return e
		}
		raw, _ := json.Marshal(map[string]any{"version_id": version.ID, "asset": index, "asset_sha256": asset.SHA256, "view_id": view.ID, "interaction_id": interaction.ID, "binding": view.Binding, "chat_id": view.ChatID, "reply_to_message_id": interaction.Envelope.MessageID})
		intent := work.NewContent(id+":request", view.ScopeID, "application/json", raw, time.Now().UTC())
		if err = tx.PutContent(intent); err != nil {
			return err
		}
		operation = work.Operation{ID: id, ScopeID: view.ScopeID, Actor: work.Actor{Kind: work.ActorPrincipal, ID: interaction.PrincipalID}, Kind: "feishu.file.publish", TargetID: view.ChatID, TargetGeneration: view.Binding.IdentityGeneration, IdempotencyKey: id, RequestRef: intent.ID, RequestHash: intent.SHA256, State: work.OperationSending, Steps: []work.OperationStep{{ID: "upload", State: work.OperationSending}}, Revision: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
		_, _, err = tx.PutOperation(operation, 0)
		perform = err == nil
		return err
	})
	if err != nil {
		return err
	}
	if !perform {
		if operation.State != work.OperationConfirmed {
			return fmt.Errorf("这次附件发送的结果尚待核对，保留原记录，未再次上传或发送")
		}
		return r.store.Within(ctx, func(tx *workstore.Tx) error {
			return tx.FinishHumanInteraction(interaction.ID, interaction.Revision, operation.ID)
		})
	}
	gateway, err := (teamDirectory{app: a, store: r.store}).gateway(ctx, view.Binding)
	if err != nil {
		return a.recordArtifactFileFailure(ctx, operation.ID, err)
	}
	name := asset.FileName
	if name == "" || name == "图片" || name == "文档附件" {
		name = "文档附件"
		if strings.HasPrefix(asset.MediaType, "image/") {
			name = "图片"
			switch asset.MediaType {
			case "image/png":
				name += ".png"
			case "image/jpeg":
				name += ".jpg"
			case "image/gif":
				name += ".gif"
			case "image/webp":
				name += ".webp"
			}
		}
	}
	key, err := gateway.ProjectChatAPI().UploadResultFile(ctx, feishu.UploadResultFileRequest{FileName: name, Content: asset.Content, FileType: "stream"})
	if err != nil {
		return a.recordArtifactFileFailure(ctx, operation.ID, err)
	}
	err = r.store.Within(ctx, func(tx *workstore.Tx) error {
		current, e := tx.GetOperation(operation.ID)
		if e != nil {
			return e
		}
		expected := current.Revision
		current.Revision++
		current.UpdatedAt = time.Now().UTC()
		current.Steps = []work.OperationStep{{ID: "upload", State: work.OperationConfirmed, Receipt: key}, {ID: "publish", State: work.OperationPrepared}}
		_, _, e = tx.PutOperation(current, expected)
		return e
	})
	if err != nil {
		return err
	}
	// Recheck authorization before sending. Upload success is retained even when
	// membership or bot identity changed while the upload was in flight.
	err = r.store.Within(ctx, func(tx *workstore.Tx) error {
		binding, e := tx.GetRoleBotBinding(view.Binding.RoleID)
		if e != nil {
			return e
		}
		if binding != view.Binding {
			return workstore.ErrConflict
		}
		scope, e := tx.GetScope(view.ScopeID)
		if e != nil {
			return e
		}
		if scope.Kind == work.ScopeProject {
			if e = tx.RequireProjectPrincipalAccess(view.ScopeID, interaction.PrincipalID, time.Now()); e != nil {
				return e
			}
		}
		current, e := tx.GetOperation(operation.ID)
		if e != nil {
			return e
		}
		expected := current.Revision
		current.Revision++
		current.Steps[1].State = work.OperationSending
		current.UpdatedAt = time.Now().UTC()
		_, _, e = tx.PutOperation(current, expected)
		return e
	})
	if err != nil {
		return a.recordArtifactFileFailure(ctx, operation.ID, err)
	}
	receipt, err := gateway.ProjectChatAPI().PublishResultFile(ctx, feishu.PublishResultFileRequest{ChatID: view.ChatID, ReplyToMessageID: interaction.Envelope.MessageID, FileKey: key, UUID: operation.ID})
	if err != nil {
		return a.recordArtifactFileFailure(ctx, operation.ID, err)
	}
	receiptCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return r.store.Within(receiptCtx, func(tx *workstore.Tx) error {
		current, e := tx.GetOperation(operation.ID)
		if e != nil {
			return e
		}
		expected := current.Revision
		current.Revision++
		current.State = work.OperationConfirmed
		current.UpdatedAt = time.Now().UTC()
		raw, _ := json.Marshal(receipt)
		current.ExternalReceipt = string(raw)
		current.Steps[1].State = work.OperationConfirmed
		current.Steps[1].Receipt = string(raw)
		if _, _, e = tx.PutOperation(current, expected); e != nil {
			return e
		}
		return tx.FinishHumanInteraction(interaction.ID, interaction.Revision, current.ID)
	})
}
func (a *App) recordArtifactFileFailure(ctx context.Context, id string, cause error) error {
	receiptCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	err := a.teamRuntime.store.Within(receiptCtx, func(tx *workstore.Tx) error {
		op, err := tx.GetOperation(id)
		if err != nil {
			return err
		}
		expected := op.Revision
		op.Revision++
		op.State = work.OperationUnknown
		op.LastError = cause.Error()
		op.UpdatedAt = time.Now().UTC()
		for index := range op.Steps {
			if op.Steps[index].State == work.OperationSending {
				op.Steps[index].State = work.OperationUnknown
			}
		}
		_, _, err = tx.PutOperation(op, expected)
		return err
	})
	if err != nil {
		return fmt.Errorf("%v; failed to save uncertain attachment receipt: %w", cause, err)
	}
	return fmt.Errorf("附件上传或发送结果尚待核对，未重复操作：%w", cause)
}
