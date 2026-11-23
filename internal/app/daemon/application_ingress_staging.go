package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	gatewaypkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/gateway"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/ingressmapper"
	"github.com/kxn/codex-remote-feishu/internal/app/ingressowner"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
)

type privateIngressStager interface {
	StagePrivateMessage(context.Context, platformingress.RawIngressEnvelope, string) (control.Action, bool, error)
}

func (g *applicationIngress) wakeStaging() {
	if g != nil && g.stageWake != nil {
		select {
		case g.stageWake <- struct{}{}:
		default:
		}
	}
}

// RunStaging recovers pending neutral inputs on startup and stages media/quotes
// outside the private app lock. A failed read is retried; no user input is sent.
func (g *applicationIngress) RunStaging(ctx context.Context) error {
	if g == nil || g.router == nil || g.stager == nil || g.stageWake == nil {
		return fmt.Errorf("durable ingress staging unavailable")
	}
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	g.wakeStaging()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		case <-g.stageWake:
		}
		var after int64
		for {
			records, err := g.router.Outstanding(ctx, after, 128)
			if err != nil {
				return err
			}
			if len(records) == 0 {
				break
			}
			for _, record := range records {
				after = record.Sequence
				// Card navigation must be admitted while its synchronous callback
				// can return the stored response. Leave unresolved clicks for a
				// platform retry or the user's next click, never consume them here.
				if record.Event.Kind == ingressowner.Card {
					continue
				}
				if record.State != ingressowner.Pending || record.Owner == nil || record.Event.Kind != ingressowner.Message {
					continue
				}
				owner := *record.Owner
				if record.DeliveryTarget != nil {
					owner = *record.DeliveryTarget
				}
				if owner.Application == ingressowner.Team && record.DeliveryTarget == nil {
					if _, _, e := g.router.CommittedMigration(ctx, owner); e == nil {
						_, _ = g.deliver(ctx, record.Event)
						updated, e := g.router.Get(ctx, record.Event.Key)
						if e == nil {
							record = updated
							if record.DeliveryTarget != nil {
								owner = *record.DeliveryTarget
							}
						}
					}
				}
				if owner.Application != ingressowner.Private {
					continue
				}
				_ = g.stagePrivate(ctx, record)
			}
			if len(records) < 128 {
				break
			}
		}
	}
}
func (g *applicationIngress) stagePrivate(ctx context.Context, record ingressowner.Record) error {
	owner := *record.Owner
	if record.DeliveryTarget != nil {
		owner = *record.DeliveryTarget
	}
	if _, err := g.router.StagedPrivateAction(ctx, record, owner); err == nil {
		_, err = g.deliver(ctx, record.Event)
		return err
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	raw, err := ingressmapper.RestoreMessage(record.Event)
	if err != nil {
		return err
	}
	_, _, parseErr := feishu.DecodePrivateMessage(ctx, raw)
	if parseErr == nil {
		_, err = g.deliver(ctx, record.Event)
		return err
	}
	if !errors.Is(parseErr, gatewaypkg.ErrInboundStaging) {
		return parseErr
	}
	action, ok, err := g.stager.StagePrivateMessage(ctx, raw, g.router.RouterID())
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("staged input unsupported")
	}
	binding, err := g.router.PrivateBindingForOwner(ctx, record.Event.Key.Tenant, record.Event.Key.AppID, record.Event.Source.GatewayID, record.Event.Source.BindingGeneration, record.Event.ChatID, owner)
	if err != nil {
		return err
	}
	surface := gatewaypkg.SurfaceIDForInbound(binding.GatewayID, "", "p2p", binding.Operator.ID)
	if action.GatewayID != binding.GatewayID || action.SurfaceSessionID != surface || action.ActorUserID != binding.Operator.ID || action.ChatID != binding.ChatID || action.MessageID != record.Event.MessageID {
		return ingressowner.ErrConflict
	}
	switch action.Kind {
	case control.ActionTextMessage, control.ActionImageMessage, control.ActionFileMessage:
	default:
		return ingressowner.ErrConflict
	}
	body, err := json.Marshal(action)
	if err != nil {
		return err
	}
	if err = g.router.StoreStagedPrivateAction(ctx, record.Event.Key, owner, body); err != nil {
		return err
	}
	_, err = g.deliver(ctx, record.Event)
	return err
}
