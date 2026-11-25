package daemon

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/ingressowner"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
)

type privatePublicationBinding struct {
	router  *ingressowner.Router
	journal *privatechat.Journal
	// bindingFor resolves only an already verified and persisted P2P binding.
	bindingFor func(context.Context, privatechat.Owner, int64) (ingressowner.PrivateBinding, error)
}
type privatePublicationProof struct {
	Owner              privatechat.Owner `json:"owner"`
	Generation         int64             `json:"generation"`
	EffectID           string            `json:"effect_id"`
	Ordinal            int               `json:"ordinal"`
	CallbackSettlement bool              `json:"callback_settlement,omitempty"`
}

func (p *privatePublicationBinding) CheckPrivateModelView(ctx context.Context, o privatechat.Owner, generation int64, message, token, sourceEventID string) (bool, bool, error) {
	b, err := p.binding(ctx, o, generation)
	if err != nil {
		return false, false, err
	}
	return p.router.CurrentPrivateView(ctx, b.Owner, b.Tenant, b.AppID, b.ChatID, message, token, sourceEventID)
}

func (p *privatePublicationBinding) binding(ctx context.Context, o privatechat.Owner, generation int64) (ingressowner.PrivateBinding, error) {
	if p == nil || p.router == nil || p.journal == nil || p.bindingFor == nil {
		return ingressowner.PrivateBinding{}, fmt.Errorf("private publication ownership unavailable")
	}
	b, err := p.bindingFor(ctx, o, generation)
	if err != nil {
		return b, err
	}
	if b.Owner.Application != ingressowner.Private || b.Owner.Epoch != generation || b.Tenant != o.TenantID || b.AppID != o.AppID || b.GatewayID != o.GatewayID || b.ChatID != o.ChatID || b.Operator.ID != o.UserID || b.BindingGeneration <= 0 {
		return b, ingressowner.ErrConflict
	}
	if err = p.router.VerifyPrivateBindingRecord(ctx, b); err != nil {
		return b, err
	}
	return b, nil
}
func (p *privatePublicationBinding) PrepareOperation(ctx context.Context, o privatechat.Owner, generation int64, effectID string, ordinal int, raw json.RawMessage) (json.RawMessage, error) {
	b, err := p.binding(ctx, o, generation)
	if err != nil {
		return nil, err
	}
	op, err := feishu.ThawPrivateOperation(raw)
	if err != nil {
		return nil, err
	}
	if op.GatewayID != o.GatewayID || op.SurfaceSessionID != o.SurfaceID || op.ChatID != o.ChatID {
		return nil, ingressowner.ErrConflict
	}
	if op.Kind != feishu.OperationSendCard && op.Kind != feishu.OperationUpdateCard {
		return raw, nil
	}
	view, err := p.router.IssuePrivateView(ctx, b.Owner, effectID, ordinal, raw)
	if err != nil {
		return nil, err
	}
	stamped, _, err := feishu.StampPrivateView(raw, view.Token)
	return stamped, err
}

func (p *privatePublicationBinding) ValidateOperation(ctx context.Context, o privatechat.Owner, generation int64, effectID string, ordinal int, raw json.RawMessage) error {
	b, err := p.binding(ctx, o, generation)
	if err != nil {
		return err
	}
	op, err := feishu.ThawPrivateOperation(raw)
	if err != nil || (op.Kind != feishu.OperationSendCard && op.Kind != feishu.OperationUpdateCard) {
		return err
	}
	unstamped, token, err := feishu.UnstampPrivateView(raw)
	if err != nil {
		return err
	}
	view, err := p.router.PrivateView(ctx, b.Owner, effectID, ordinal)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(unstamped)
	if view.RenderHash != hex.EncodeToString(digest[:]) || (token != "" && view.Token != token) {
		return ingressowner.ErrConflict
	}
	return nil
}

func (p *privatePublicationBinding) ConfirmOperation(ctx context.Context, o privatechat.Owner, generation int64, effectID string, ordinal int) error {
	return p.confirmOperation(ctx, o, generation, effectID, ordinal, false)
}

func (p *privatePublicationBinding) confirmOperation(ctx context.Context, o privatechat.Owner, generation int64, effectID string, ordinal int, callbackSettlement bool) error {
	b, err := p.binding(ctx, o, generation)
	if err != nil {
		return err
	}
	publication, err := p.journal.LoadPublication(ctx, o, generation, effectID)
	if err != nil {
		return err
	}
	if ordinal < 0 || ordinal >= len(publication.Operations) {
		return ingressowner.ErrConflict
	}
	stored := publication.Operations[ordinal]
	if stored.Ordinal != ordinal || stored.State != "confirmed" {
		return ingressowner.ErrConflict
	}
	var receipt feishu.PrivateOperationReceipt
	if err = json.Unmarshal(stored.Receipt, &receipt); err != nil {
		return err
	}
	switch receipt.Kind {
	case feishu.OperationSendCard, feishu.OperationUpdateCard, feishu.OperationSendText, feishu.OperationSendImage:
	default:
		return nil
	}
	if receipt.MessageID == "" {
		return ingressowner.ErrConflict
	}
	cardRef := ""
	if receipt.Kind == feishu.OperationSendCard || receipt.Kind == feishu.OperationUpdateCard {
		_, cardRef, err = feishu.UnstampPrivateView(stored.Body)
		if err != nil {
			return err
		}
	}
	message := ingressowner.PlatformMessage{Tenant: o.TenantID, AppID: o.AppID, GatewayID: o.GatewayID, BindingGeneration: b.BindingGeneration, ChatID: o.ChatID, MessageID: receipt.MessageID, Owner: b.Owner, Version: 1, Lifecycle: "active"}
	if cardRef != "" {
		message.CardKind = "private_view"
		message.CardRef = cardRef
	}
	old, err := p.router.PublishedMessage(ctx, o.TenantID, o.AppID, o.ChatID, receipt.MessageID)
	expected := int64(0)
	if err == nil {
		if old.Owner != message.Owner || old.GatewayID != message.GatewayID || old.BindingGeneration != message.BindingGeneration {
			return ingressowner.ErrConflict
		}
		if old.CardRef == message.CardRef && old.CardKind == message.CardKind {
			message.Version = old.Version
		} else {
			expected = old.Version
			message.Version = old.Version + 1
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	proof, _ := json.Marshal(privatePublicationProof{Owner: o, Generation: generation, EffectID: effectID, Ordinal: ordinal, CallbackSettlement: callbackSettlement})
	return p.router.RecordPublished(ctx, message, proof, expected)
}

// VerifyPlatformReceipt accepts only an operation already confirmed by the
// private journal from an actual gateway result; callback parameters cannot
// manufacture a publication binding.
func (p *privatePublicationBinding) VerifyPlatformReceipt(ctx context.Context, m ingressowner.PlatformMessage, proof []byte) error {
	var ref privatePublicationProof
	if json.Unmarshal(proof, &ref) != nil {
		return ingressowner.ErrInvalid
	}
	if ref.CallbackSettlement {
		eligible, err := p.journal.CallbackPublicationEligible(ctx, ref.Owner, ref.Generation, ref.EffectID, ref.Ordinal, m.MessageID)
		if err != nil {
			return err
		}
		if !eligible {
			return ingressowner.ErrConflict
		}
	}
	b, err := p.binding(ctx, ref.Owner, ref.Generation)
	if err != nil {
		return err
	}
	if m.Owner != b.Owner || m.Tenant != b.Tenant || m.AppID != b.AppID || m.GatewayID != b.GatewayID || m.BindingGeneration != b.BindingGeneration || m.ChatID != b.ChatID {
		return ingressowner.ErrConflict
	}
	pub, err := p.journal.LoadPublication(ctx, ref.Owner, ref.Generation, ref.EffectID)
	if err != nil {
		return err
	}
	if ref.Ordinal < 0 || ref.Ordinal >= len(pub.Operations) {
		return ingressowner.ErrConflict
	}
	item := pub.Operations[ref.Ordinal]
	if item.Ordinal != ref.Ordinal || item.State != "confirmed" {
		return ingressowner.ErrConflict
	}
	var receipt feishu.PrivateOperationReceipt
	if json.Unmarshal(item.Receipt, &receipt) != nil || receipt.Version != 1 || receipt.MessageID != m.MessageID || receipt.GatewayID != m.GatewayID || receipt.ChatID != m.ChatID || receipt.SurfaceSessionID != ref.Owner.SurfaceID {
		return ingressowner.ErrConflict
	}
	switch receipt.Kind {
	case feishu.OperationSendCard, feishu.OperationUpdateCard, feishu.OperationSendText, feishu.OperationSendImage:
	default:
		return ingressowner.ErrConflict
	}
	op, err := feishu.ThawPrivateOperation(item.Body)
	if err != nil || op.Kind != receipt.Kind || op.GatewayID != m.GatewayID || op.ChatID != m.ChatID || op.SurfaceSessionID != ref.Owner.SurfaceID {
		return ingressowner.ErrConflict
	}
	if receipt.Kind == feishu.OperationSendCard || receipt.Kind == feishu.OperationUpdateCard {
		original, token, e := feishu.UnstampPrivateView(item.Body)
		if e != nil {
			return e
		}
		view, e := p.router.PrivateView(ctx, b.Owner, ref.EffectID, ref.Ordinal)
		if e != nil {
			return e
		}
		sum := sha256.Sum256(original)
		if view.RenderHash != hex.EncodeToString(sum[:]) || token != "" && token != view.Token || m.CardRef != token || token != "" && m.CardKind != "private_view" {
			return ingressowner.ErrConflict
		}
	} else if m.CardRef != "" || m.CardKind != "" {
		return ingressowner.ErrConflict
	}
	return nil
}

// privatePublicationBindingFor reads the currently persisted Bot generation,
// then resolves an independently recorded private identity binding.
func (g *applicationIngress) privatePublicationBindingFor(ctx context.Context, o privatechat.Owner, generation int64) (ingressowner.PrivateBinding, error) {
	botGeneration, err := g.generation(ctx, o.GatewayID, o.AppID)
	if err != nil {
		return ingressowner.PrivateBinding{}, err
	}
	binding, err := g.router.PrivateBindingForOperator(ctx, o.TenantID, o.AppID, o.GatewayID, botGeneration, o.ChatID, o.UserID)
	if err != nil {
		return binding, err
	}
	if binding.Owner.Epoch != generation {
		return binding, ingressowner.ErrConflict
	}
	if binding.Operator.IDType == "open_id" {
		source, e := g.router.LookupImportedOwner(ctx, o.TenantID, o.AppID, o.GatewayID, botGeneration, o.ChatID, o.UserID)
		if e == nil {
			_, target, check := g.router.CommittedMigration(ctx, source)
			if check != nil {
				return binding, check
			}
			if target != binding.Owner {
				return binding, ingressowner.ErrConflict
			}
		} else if !errors.Is(e, sql.ErrNoRows) {
			return binding, e
		}
	}
	return binding, nil
}
