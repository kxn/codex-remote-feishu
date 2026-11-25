package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/ingressowner"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
)

func (g *applicationIngress) confirmCallbackPublication(ctx context.Context, o privatechat.Owner, generation int64, id string, ordinal int) error {
	binding, ok := g.host.applications.publisher.ownership.(*privatePublicationBinding)
	if !ok {
		return ingressowner.ErrConflict
	}
	return binding.confirmOperation(ctx, o, generation, id, ordinal, true)
}

// settlePrivateCardCallback waits only for an already-issued publication;
// the callback never authorizes sending or supplies a platform receipt.
func (g *applicationIngress) settlePrivateCardCallback(ctx context.Context, event ingressowner.Event) (bool, error) {
	issued, err := g.router.PrivateViewByToken(ctx, event.CardRef)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	o, generation, err := g.host.applications.journal.CallbackPublicationOwner(ctx, issued.EffectID)
	if err != nil {
		return false, err
	}
	if generation != issued.Owner.Epoch || o.TenantID != event.Key.Tenant || o.AppID != event.Key.AppID || o.GatewayID != event.Source.GatewayID || (event.ChatID != "" && o.ChatID != event.ChatID) {
		return false, ingressowner.ErrConflict
	}
	b, err := g.router.PrivateBindingForOwner(ctx, event.Key.Tenant, event.Key.AppID, event.Source.GatewayID, event.Source.BindingGeneration, o.ChatID, issued.Owner)
	if err != nil {
		return false, err
	}
	if event.Operator != b.Operator {
		return false, ingressowner.ErrConflict
	}
	if o.UserID != b.Operator.ID {
		return false, ingressowner.ErrConflict
	}
	known, current, err := g.router.CurrentPrivateView(ctx, issued.Owner, b.Tenant, b.AppID, b.ChatID, event.MessageID, event.CardRef, "")
	if err != nil {
		return false, err
	}
	if known && current {
		return false, nil
	}
	for {
		eligible, e := g.host.applications.journal.CallbackPublicationEligible(ctx, o, b.Owner.Epoch, issued.EffectID, issued.Ordinal, event.MessageID)
		if e != nil {
			return false, e
		}
		if !eligible {
			return false, nil
		}
		pub, e := g.host.applications.journal.LoadPublication(ctx, o, b.Owner.Epoch, issued.EffectID)
		if e != nil {
			return false, e
		}
		if issued.Ordinal < 0 || issued.Ordinal >= len(pub.Operations) {
			return false, ingressowner.ErrConflict
		}
		item := pub.Operations[issued.Ordinal]
		if item.Ordinal != issued.Ordinal {
			return false, ingressowner.ErrConflict
		}
		op, e := feishu.ThawPrivateOperation(item.Body)
		if e != nil {
			return false, e
		}
		if (op.Kind != feishu.OperationSendCard && op.Kind != feishu.OperationUpdateCard) || op.GatewayID != b.GatewayID || op.SurfaceSessionID != o.SurfaceID || op.ChatID != b.ChatID || (op.Kind == feishu.OperationUpdateCard && op.MessageID != event.MessageID) || (op.MessageID != "" && op.MessageID != event.MessageID) {
			return false, ingressowner.ErrConflict
		}

		switch item.State {
		case "prepared":
			return false, nil
		case "confirmed":
			var receipt feishu.PrivateOperationReceipt
			if json.Unmarshal(item.Receipt, &receipt) != nil || receipt.MessageID != event.MessageID {
				return false, ingressowner.ErrConflict
			}
			e = g.confirmCallbackPublication(ctx, o, b.Owner.Epoch, issued.EffectID, issued.Ordinal)
			if e != nil {
				_, current, lookupErr := g.router.CurrentPrivateView(ctx, issued.Owner, b.Tenant, b.AppID, b.ChatID, event.MessageID, event.CardRef, "")
				if lookupErr == nil && current {
					return false, nil
				}
			}
			return true, e
		case "unknown":
			e = g.recoverUnknownPrivateCard(ctx, event)
			if e != nil {
				_, current, lookupErr := g.router.CurrentPrivateView(ctx, issued.Owner, b.Tenant, b.AppID, b.ChatID, event.MessageID, event.CardRef, "")
				if lookupErr == nil && current {
					return false, nil
				}
			}
			return true, e
		case "sending":
			select {
			case <-ctx.Done():
				return true, ctx.Err()
			case <-time.After(20 * time.Millisecond):
			}
		default:
			return false, ingressowner.ErrConflict
		}
	}
}
