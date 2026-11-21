package workstore

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"strconv"
)

type AdoptBootstrapInputRequest struct {
	OperationID     string
	SourceMessageID string
	TargetContextID string
}

// This private projection reads the source frozen by the create use case. It
// intentionally never treats model-written InitialRequest as a human message.
type bootstrapSourceRequest struct {
	Tenant                    string `json:"tenant"`
	ChatID                    string `json:"chat_id"`
	PrincipalID               string `json:"principal_id"`
	Key                       string `json:"key"`
	LeaderRoleID              string `json:"leader_role_id"`
	ExpectedBindingGeneration int64  `json:"expected_binding_generation"`
	SourceMessageID           string `json:"source_message_id"`
}

func (t *Tx) AdoptBootstrapInput(input AdoptBootstrapInputRequest) (derived work.Message, created bool, err error) {
	err = t.atomic(func() error {
		operation, e := t.GetOperation(input.OperationID)
		if e != nil {
			return e
		}
		if operation.Kind != "project.provision" {
			return fmt.Errorf("bootstrap adoption requires a project provisioning operation")
		}
		project, e := t.GetProject(operation.TargetID)
		if e != nil {
			return e
		}
		if operation.ScopeID != project.ScopeID {
			return ErrScopeDenied
		}
		var existing, target string
		e = t.tx.QueryRowContext(t.ctx, "SELECT derived_message_id,target_context_id FROM bootstrap_adoptions WHERE operation_id=? AND source_message_id=?", input.OperationID, input.SourceMessageID).Scan(&existing, &target)
		if e == nil {
			if target != input.TargetContextID {
				return ErrIdempotencyConflict
			}
			derived, e = t.GetMessage(existing)
			return e
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		body, e := t.GetContent(project.ScopeID, operation.RequestRef)
		if e != nil {
			return e
		}
		if body.SHA256 != operation.RequestHash {
			return fmt.Errorf("provisioning request digest mismatch")
		}
		var request bootstrapSourceRequest
		if e = json.Unmarshal(body.Data, &request); e != nil {
			return e
		}
		if request.SourceMessageID == "" || request.SourceMessageID != input.SourceMessageID || request.LeaderRoleID != project.LeaderRoleID || operation.Actor != (work.Actor{Kind: work.ActorPrincipal, ID: request.PrincipalID}) {
			return fmt.Errorf("bootstrap source was not explicitly adopted by project creation")
		}
		source, e := t.GetMessage(input.SourceMessageID)
		if e != nil {
			return e
		}
		scope, e := t.GetScope(source.ScopeID)
		if e != nil {
			return e
		}
		if scope.Kind != work.ScopeBootstrap || scope.Tenant != request.Tenant || scope.ChatID != request.ChatID || scope.PrincipalID != request.PrincipalID || scope.RoleID != request.LeaderRoleID || scope.BootstrapOperationID != request.Key || scope.BindingGeneration != request.ExpectedBindingGeneration {
			return fmt.Errorf("source message is outside the exact bootstrap establishment")
		}
		if source.Source != (work.Actor{Kind: work.ActorPrincipal, ID: request.PrincipalID}) {
			return fmt.Errorf("bootstrap input is not the original human message")
		}
		if source.PlatformKey != nil {
			if source.PlatformKey.Tenant != request.Tenant || source.PlatformKey.ChatID != request.ChatID {
				return fmt.Errorf("bootstrap platform source mismatch")
			}
		} else {
			form, e := t.GetOperation(scope.BootstrapOperationID)
			if e != nil {
				return e
			}
			original, e := t.GetContent(scope.ID, source.BodyRef)
			if e != nil {
				return e
			}
			if form.Kind != "bootstrap.form" || form.ScopeID != scope.ID || form.Actor != source.Source || form.TargetID != scope.ID || form.State != work.OperationConfirmed || form.RequestRef != source.BodyRef || form.RequestHash != original.SHA256 {
				return fmt.Errorf("non-platform bootstrap source lacks a verified human form receipt")
			}
		}
		channel, e := t.GetProjectChannelBinding(request.Tenant, request.ChatID)
		if e != nil {
			return e
		}
		if !channel.Active || channel.ProjectID != project.ID || channel.BindingGeneration != request.ExpectedBindingGeneration+1 || operation.TargetGeneration != channel.BindingGeneration {
			return fmt.Errorf("project channel no longer matches establishment")
		}
		if input.TargetContextID != "" {
			context, e := t.GetContext(input.TargetContextID)
			if e != nil {
				return e
			}
			if context.ScopeID != project.ScopeID || context.RoleID != project.LeaderRoleID || context.TaskID != "" {
				return ErrScopeDenied
			}
		}
		original, e := t.GetContent(scope.ID, source.BodyRef)
		if e != nil {
			return e
		}
		key, _ := json.Marshal([]string{operation.ID, source.ID})
		sum := sha256.Sum256(key)
		id := "bootstrap-adopted:" + hex.EncodeToString(sum[:])
		content := work.NewContent(id+":body", project.ScopeID, original.MediaType, original.Data, original.CreatedAt)
		if e = t.PutContent(content); e != nil {
			return e
		}
		derived = work.Message{ID: id, ScopeID: project.ScopeID, ConversationID: source.ConversationID, Source: source.Source, BodyRef: content.ID, DerivedFrom: &work.SourceRef{Kind: "message", ID: source.ID, Version: strconv.FormatInt(source.Revision, 10), EvidenceRef: source.BodyRef}, Revision: 1, CreatedAt: source.CreatedAt}

		for index, attachment := range source.Attachments {
			originalAttachment, e := t.GetContent(source.ScopeID, attachment.ContentRef)
			if e != nil {
				return e
			}
			copy := work.NewContent(id+":attachment:"+strconv.Itoa(index), project.ScopeID, originalAttachment.MediaType, originalAttachment.Data, originalAttachment.CreatedAt)
			if e = t.PutContent(copy); e != nil {
				return e
			}
			attachment.ContentRef = copy.ID
			derived.Attachments = append(derived.Attachments, attachment)
		}
		queued := work.Inbox{ID: id + ":inbox", ScopeID: project.ScopeID, RecipientRoleID: project.LeaderRoleID, RecipientContextID: input.TargetContextID, EventID: "message:" + id, MessageID: id, State: work.InboxPending, AvailableAt: original.CreatedAt, Revision: 1}
		if _, _, e = t.AcceptMessage(derived, []work.Inbox{queued}); e != nil {
			return e
		}
		_, e = t.tx.ExecContext(t.ctx, "INSERT INTO bootstrap_adoptions(operation_id,source_message_id,derived_message_id,target_context_id) VALUES(?,?,?,?)", operation.ID, source.ID, derived.ID, input.TargetContextID)
		if e == nil {
			created = true
		}
		return e
	})
	return
}
