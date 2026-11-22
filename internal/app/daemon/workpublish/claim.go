package workpublish

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (p *Publisher) claim(ctx context.Context, id string, recovery bool) (publication, error) {
	var job publication
	err := p.store.Within(ctx, func(tx *workstore.Tx) error {
		item, err := tx.GetOutbox(id)
		if err != nil {
			return err
		}
		op, err := tx.GetOperation(item.OperationID)
		if err != nil {
			return err
		}
		abort := func(reason string) error {
			oldOp, oldItem := op.Revision, item.Revision
			op.State, op.LastError, op.Revision, op.UpdatedAt = work.OperationTerminal, reason, op.Revision+1, p.now()
			item.State, item.Revision = work.OutboxTerminal, item.Revision+1
			if _, _, e := tx.PutOperation(op, oldOp); e != nil {
				return e
			}
			if e := tx.PutOutbox(item, oldItem); e != nil {
				return e
			}
			job.TerminalReason = reason
			return nil
		}
		if op.Kind != "feishu.publish" || op.ScopeID != item.ScopeID || op.RequestRef != item.PayloadRef {
			return fmt.Errorf("outbox publication binding mismatch")
		}
		for _, step := range op.Steps {
			if step.ID == "topic_reanchored" {
				return fmt.Errorf("original send remains unknown; subsequent updates use the human-selected topic")
			}
		}
		if recovery {
			if item.State != work.OutboxUnknown && item.State != work.OutboxSending {
				return workstore.ErrConflict
			}
			if op.State != work.OperationUnknown && op.State != work.OperationSending {
				return workstore.ErrConflict
			}
			var started time.Time
			for _, step := range op.Steps {
				if step.ID == "first_send" {
					started, _ = time.Parse(time.RFC3339Nano, step.Receipt)
				}
			}
			if started.IsZero() || p.now().Before(started) || p.now().Sub(started) > 55*time.Minute {
				return fmt.Errorf("uncertain send is outside the safe deduplication recovery window")
			}
			// The new send uses the same immutable UUID/body. Preserve first_send.
			if op.State == work.OperationUnknown {
				previous := op.Revision
				op.State = work.OperationRetryable
				op.Revision++
				if _, _, err = tx.PutOperation(op, previous); err != nil {
					return err
				}
			}
			if item.State == work.OutboxUnknown {
				previous := item.Revision
				item.State = work.OutboxRetryable
				item.Revision++
				if err = tx.PutOutbox(item, previous); err != nil {
					return err
				}
			}
		} else {
			if item.State != work.OutboxPending && item.State != work.OutboxRetryable {
				return workstore.ErrConflict
			}
			if op.State != work.OperationPrepared && op.State != work.OperationRetryable {
				return workstore.ErrConflict
			}
		}
		body, err := tx.GetContent(item.ScopeID, item.PayloadRef)
		if err != nil {
			return err
		}
		if err = json.Unmarshal(body.Data, &job.Projection); err != nil {
			return err
		}
		if err = p.resolvePrivateDisplay(tx, op, item, &job, recovery); err != nil {
			return err
		}
		if job.TerminalReason != "" {
			return abort(job.TerminalReason)
		}
		if recovery && job.Projection.ReplaceMessageID != "" {
			return fmt.Errorf("uncertain card update requires observation; replay could overwrite a newer view")
		}
		if job.Projection.UIViewID != "" {
			view, e := tx.GetUIView(job.Projection.UIViewID)
			if e != nil {
				return e
			}
			if job.Projection.ReplaceMessageID != "" {
				old, e := tx.GetUIView(view.ReplacesViewID)
				if e != nil {
					return e
				}
				if old.ExternalMessageID != job.Projection.ReplaceMessageID || old.ScopeID != view.ScopeID || old.SurfaceKey != view.SurfaceKey || old.Binding != view.Binding || old.ChatID != view.ChatID || old.Tenant != view.Tenant || old.PrincipalID != view.PrincipalID {
					return fmt.Errorf("card replacement lost its original publication binding")
				}
			}
			if view.Invalidated && !recovery {
				return abort("presentation superseded before publication")
			}
		}
		if job.Projection.ReplaceMessageID != "" && job.Projection.UIViewID == "" && job.Projection.PrivateDisplay == nil {
			return fmt.Errorf("card update requires a durable presentation owner")
		}
		binding, err := tx.GetRoleBotBinding(job.Projection.RoleID)
		if err != nil {
			return err
		}
		if binding.IdentityGeneration != job.Projection.BotGeneration || binding.AppID != job.Projection.AppID || binding.GatewayRef != job.Projection.GatewayRef {
			return abort("publication bot identity changed; delivery needs an explicit decision under the new identity")
		}
		scope, err := tx.GetScope(item.ScopeID)
		if err != nil {
			return err
		}
		if scope.Kind == work.ScopeProject {
			channel, e := tx.GetProjectChannelBinding(job.Projection.Tenant, job.Projection.ChatID)
			if e != nil {
				return e
			}
			if !channel.Active || channel.ProjectID != scope.ProjectID || channel.BindingGeneration != job.Projection.ChannelGeneration {
				return abort("publication channel binding changed; original destination was not replaced")
			}
		} else if scope.Kind == work.ScopeBootstrap && (scope.Tenant != job.Projection.Tenant || scope.ChatID != job.Projection.ChatID) {
			return fmt.Errorf("bootstrap publication escaped chat")
		}
		if err = workservice.ValidatePublication(tx, op, job.Projection); err != nil {
			if errors.Is(err, workservice.ErrPublicationDenied) {
				return abort(err.Error())
			}
			return err
		}
		if obsolete, e := p.progressObsolete(tx, scope, op, job.Projection); e != nil {
			return e
		} else if obsolete {
			return abort("progress superseded by newer state or its attempt's final result")
		}
		if err = p.resolveGroupProgress(tx, scope, op, &job, recovery); err != nil {
			return err
		}
		if job.TerminalReason != "" {
			return abort(job.TerminalReason)
		}
		if err = p.resolveTopic(tx, &op, &job); err != nil {
			return err
		}
		if recovery && job.Projection.ReplaceMessageID != "" {
			return fmt.Errorf("uncertain card update requires observation before recovery")
		}
		job.ReplyInThread, err = publicationRepliesInThread(tx, scope, job.Projection)
		if err != nil {
			return err
		}

		root := job.Projection.RootMessageID
		if root == "" {
			root = job.Projection.ReplyToMessageID
		}
		if root != "" {
			recalled, e := tx.IsPlatformMessageRecalled(job.Projection.Tenant, job.Projection.ChatID, root)
			if e == nil && !recalled && job.Projection.TaskID != "" {
				recalled, e = tx.TaskTopicUnavailable(work.TaskConversation{ScopeID: item.ScopeID, TaskID: job.Projection.TaskID, Tenant: job.Projection.Tenant, ChatID: job.Projection.ChatID, RootMessageID: root, ChannelGeneration: job.Projection.ChannelGeneration})
			}
			if e != nil {
				return e
			}
			if recalled {
				if recovery {
					return fmt.Errorf("topic root was recalled; original send remains unknown")
				}
				return abort("topic root was recalled; delivery requires an explicit replacement topic")
			}
		}
		if !recovery {
			if err = p.reserveBackground(tx, scope, op, job.Projection); err != nil {
				return err
			}
		}
		found := false
		for _, step := range op.Steps {
			if step.ID == "first_send" {
				found = true
			}
		}
		if !found {
			op.Steps = append(op.Steps, work.OperationStep{ID: "first_send", State: work.OperationSending, Receipt: p.now().UTC().Format(time.RFC3339Nano)})
		}
		previousOp, previousItem := op.Revision, item.Revision
		op.State, op.Revision, op.UpdatedAt = work.OperationSending, op.Revision+1, p.now()
		item.State, item.Revision = work.OutboxSending, item.Revision+1
		if _, _, err = tx.PutOperation(op, previousOp); err != nil {
			return err
		}
		if err = tx.PutOutbox(item, previousItem); err != nil {
			return err
		}
		job.Operation, job.Outbox, job.Binding = op, item, binding
		return nil
	})
	return job, err
}
