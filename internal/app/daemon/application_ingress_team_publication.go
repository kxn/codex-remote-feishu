package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

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
			if projection.UIViewID != "" {
				return ingressowner.ErrUnresolved
			}
			mapping, e := tx.GetPlatformMessageMapping(projection.Tenant, projection.ChatID, receipt.MessageID)
			if e != nil || mapping.OutboxID != outbox.ID || mapping.OperationID != op.ID || mapping.ScopeID != op.ScopeID || mapping.RoleID != projection.RoleID || mapping.BotGeneration != projection.BotGeneration {
				return ingressowner.ErrConflict
			}
			m = ingressowner.PlatformMessage{Tenant: projection.Tenant, AppID: projection.AppID, GatewayID: projection.GatewayRef, BindingGeneration: projection.BotGeneration, ChatID: projection.ChatID, MessageID: receipt.MessageID, Version: 1, Lifecycle: "active"}
		case "team_view":
			view, e := tx.GetUIView(proof.ID)
			if e != nil {
				return e
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
					first = err
				}
				after = id
			}
		}
	}
	return first
}
