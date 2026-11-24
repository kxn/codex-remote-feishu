package daemon

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/ingressowner"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// teamPublicationBinding re-reads the actual confirmed team publication;
// callback-supplied view identifiers are only lookup keys, never receipts.
type teamPublicationBinding struct {
	router *ingressowner.Router
	store  *workstore.Store
}
type teamPublicationProof struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

func missingPublicationReference(err error) error {
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, workstore.ErrNotFound) {
		return fmt.Errorf("confirmed publication references missing source: %w", ingressowner.ErrConflict)
	}
	return err
}

func (p *teamPublicationBinding) confirmed(ctx context.Context, proof teamPublicationProof) (ingressowner.PlatformMessage, error) {
	var m ingressowner.PlatformMessage
	if p == nil || p.router == nil || p.store == nil || proof.ID == "" {
		return m, ingressowner.ErrInvalid
	}
	err := p.store.Within(ctx, func(tx *workstore.Tx) error {
		switch proof.Kind {
		case "team_message":
			outbox, e := tx.GetOutbox(proof.ID)
			if e != nil {
				return e
			}
			scope, e := tx.GetScope(outbox.ScopeID)
			if e != nil {
				return e
			}
			if scope.Kind != work.ScopeProject && scope.Kind != work.ScopeBootstrap {
				return ingressowner.ErrUnresolved
			}
			op, e := tx.GetOperation(outbox.OperationID)
			if e != nil {
				return e
			}
			body, e := tx.GetContent(op.ScopeID, op.RequestRef)
			if e != nil {
				return e
			}
			var projection workservice.MessageProjection
			var receipt feishu.ProjectPublishReceipt
			if outbox.State != work.OutboxSent || op.State != work.OperationConfirmed || outbox.ScopeID != op.ScopeID || json.Unmarshal(body.Data, &projection) != nil || projection.Tenant == "" || projection.ChatID != outbox.Target || outbox.ExternalMessageID == "" || json.Unmarshal([]byte(op.ExternalReceipt), &receipt) != nil || receipt.MessageID != outbox.ExternalMessageID || receipt.ChatID != projection.ChatID {
				return ingressowner.ErrConflict
			}
			original := projection
			for _, step := range op.Steps {
				if step.ID != "send_projection" {
					continue
				}
				frozen, e := tx.GetContent(op.ScopeID, step.Receipt)
				if e != nil {
					return e
				}
				if step.State != work.OperationConfirmed || json.Unmarshal(frozen.Data, &projection) != nil || projection.RoleID != original.RoleID || projection.BotGeneration != original.BotGeneration || projection.AppID != original.AppID || projection.GatewayRef != original.GatewayRef || projection.Tenant != original.Tenant || projection.ChatID != original.ChatID || projection.ChannelGeneration != original.ChannelGeneration || projection.TaskID != original.TaskID || projection.Body != original.Body {
					return ingressowner.ErrConflict
				}
				break
			}
			if projection.UIViewID != "" {
				return ingressowner.ErrUnresolved
			}
			if op.Actor != (work.Actor{Kind: work.ActorRole, ID: projection.RoleID}) || op.TargetGeneration != projection.BotGeneration {
				return ingressowner.ErrConflict
			}
			mapping, e := tx.GetPlatformMessageMapping(projection.Tenant, projection.ChatID, receipt.MessageID)
			if e != nil || mapping.ScopeID != op.ScopeID || mapping.RoleID != projection.RoleID || mapping.BotGeneration != projection.BotGeneration {
				return ingressowner.ErrConflict
			}
			if mapping.OutboxID != outbox.ID || mapping.OperationID != op.ID {
				// A confirmed PATCH retains the immutable original message mapping.
				// Validate that original receipt and identity instead of treating a
				// different operation as automatically superseded.
				if projection.ReplaceMessageID != receipt.MessageID || mapping.OutboxID == "" || mapping.OperationID == "" {
					return ingressowner.ErrConflict
				}
				originalOut, e := tx.GetOutbox(mapping.OutboxID)
				if e != nil {
					return missingPublicationReference(e)
				}
				originalOp, e := tx.GetOperation(mapping.OperationID)
				if e != nil {
					return missingPublicationReference(e)
				}
				origin, e := tx.GetContent(originalOp.ScopeID, originalOp.RequestRef)
				if e != nil {
					return missingPublicationReference(e)
				}
				var originProjection workservice.MessageProjection
				var originReceipt feishu.ProjectPublishReceipt
				if originalOut.State != work.OutboxSent || originalOut.OperationID != originalOp.ID || originalOut.ScopeID != op.ScopeID || originalOut.Target != projection.ChatID || originalOut.ExternalMessageID != receipt.MessageID || originalOp.State != work.OperationConfirmed || originalOp.ScopeID != op.ScopeID || originalOp.Actor != (work.Actor{Kind: work.ActorRole, ID: projection.RoleID}) || originalOp.TargetGeneration != projection.BotGeneration || json.Unmarshal([]byte(originalOp.ExternalReceipt), &originReceipt) != nil || originReceipt.MessageID != receipt.MessageID || originReceipt.ChatID != receipt.ChatID || json.Unmarshal(origin.Data, &originProjection) != nil || originProjection.Tenant != projection.Tenant || originProjection.ChatID != projection.ChatID || originProjection.RoleID != projection.RoleID || originProjection.GatewayRef != projection.GatewayRef || originProjection.AppID != projection.AppID || originProjection.BotGeneration != projection.BotGeneration || originProjection.ChannelGeneration != projection.ChannelGeneration {
					return ingressowner.ErrConflict
				}
			}
			m = ingressowner.PlatformMessage{Tenant: projection.Tenant, AppID: projection.AppID, GatewayID: projection.GatewayRef, BindingGeneration: projection.BotGeneration, ChatID: projection.ChatID, MessageID: receipt.MessageID, Version: 1, Lifecycle: "active"}
		case "team_view":
			view, e := tx.GetUIView(proof.ID)
			if e != nil {
				return e
			}
			scope, e := tx.GetScope(view.ScopeID)
			if e != nil {
				return e
			}
			if scope.Kind != work.ScopeProject && scope.Kind != work.ScopeBootstrap {
				return ingressowner.ErrUnresolved
			}
			op, e := tx.GetOperation(view.OperationID)
			if e != nil {
				return e
			}
			var receipt feishu.ProjectPublishReceipt
			if view.Invalidated || view.ExternalMessageID == "" || op.State != work.OperationConfirmed || op.ScopeID != view.ScopeID || json.Unmarshal([]byte(op.ExternalReceipt), &receipt) != nil || receipt.MessageID != view.ExternalMessageID || receipt.ChatID != view.ChatID {
				return ingressowner.ErrConflict
			}
			m = ingressowner.PlatformMessage{Tenant: view.Tenant, AppID: view.Binding.AppID, GatewayID: view.Binding.GatewayRef, BindingGeneration: view.Binding.IdentityGeneration, ChatID: view.ChatID, MessageID: view.ExternalMessageID, Version: 1, Lifecycle: "active", CardKind: "team_view", CardRef: view.ID}
		case "team_setup_ingress":
			notice, e := tx.GetSelectionNotice(proof.ID)
			if e != nil {
				return e
			}
			source, e := tx.GetIngress(notice.IngressID)
			if e != nil {
				return missingPublicationReference(e)
			}
			if source.Envelope.ChatType != "group" || source.Envelope.Tenant != notice.Tenant || source.Envelope.ChatID != notice.ChatID || source.Gateway != notice.Gateway {
				return ingressowner.ErrConflict
			}
			if notice.State != work.OperationConfirmed || notice.ExternalMessageID == "" {
				return ingressowner.ErrConflict
			}
			m = ingressowner.PlatformMessage{Tenant: notice.Tenant, AppID: notice.Gateway.AppID, GatewayID: notice.Gateway.GatewayRef, BindingGeneration: notice.Gateway.BotGeneration, ChatID: notice.ChatID, MessageID: notice.ExternalMessageID, Version: 1, Lifecycle: "active", CardKind: "team_setup_ingress", CardRef: notice.IngressID}
		default:
			return ingressowner.ErrInvalid
		}
		return nil
	})
	if err != nil {
		return m, err
	}
	m.Owner, err = p.router.TeamOwnerForChat(ctx, m.Tenant, m.AppID, m.GatewayID, m.BindingGeneration, m.ChatID)
	return m, err
}
func (p *teamPublicationBinding) VerifyPlatformReceipt(ctx context.Context, m ingressowner.PlatformMessage, raw []byte) error {
	var proof teamPublicationProof
	if json.Unmarshal(raw, &proof) != nil {
		return ingressowner.ErrInvalid
	}
	actual, err := p.confirmed(ctx, proof)
	if err != nil {
		return err
	}
	actual.Version = m.Version
	if actual != m {
		return ingressowner.ErrConflict
	}
	return nil
}
func (p *teamPublicationBinding) confirm(ctx context.Context, proof teamPublicationProof) error {
	m, err := p.confirmed(ctx, proof)
	if err != nil {
		return err
	}
	prior, err := p.router.PublishedMessage(ctx, m.Tenant, m.AppID, m.ChatID, m.MessageID)
	expected := int64(0)
	if err == nil {
		if prior == m {
			return nil
		}
		expected = prior.Version
		m.Version = prior.Version + 1
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	raw, err := json.Marshal(proof)
	if err != nil {
		return err
	}
	return p.router.RecordPublished(ctx, m, raw, expected)
}

// ReconcileConfirmed repairs a crash after the team receipt committed and
// before the independent neutral message projection committed. Missing group
// owner witnesses stay pending and are retried on a later pass.
func (p *teamPublicationBinding) ReconcileConfirmed(ctx context.Context) error {
	var first error
	for _, kind := range []string{"team_view", "team_setup_ingress", "team_message"} {
		after := ""
		for {
			var ids []string
			err := p.store.Within(ctx, func(tx *workstore.Tx) error {
				var e error
				ids, e = tx.ListConfirmedPresentationRefs(kind, after, 100)
				return e
			})
			if err != nil {
				return err
			}
			if len(ids) == 0 {
				break
			}
			for _, id := range ids {
				if err := ctx.Err(); err != nil {
					return err
				}
				if err := p.confirm(ctx, teamPublicationProof{Kind: kind, ID: id}); err != nil && !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, ingressowner.ErrUnresolved) && first == nil {
					objectHash := sha256.Sum256([]byte(id))
					first = fmt.Errorf("%s object %x: %w", kind, objectHash[:6], err)
				}
				after = id
			}
		}
	}
	return first
}
