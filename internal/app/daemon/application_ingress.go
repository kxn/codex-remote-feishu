package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/ingressmapper"
	"github.com/kxn/codex-remote-feishu/internal/app/ingressowner"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
)

// applicationIngress is installed before gateways start. Historical private
// owners remain drained until their individual migration is activated.
type applicationIngress struct {
	host          *App
	router        *ingressowner.Router
	resolver      ingressowner.Resolver
	private, team ingressowner.Admitter
	stageWake     chan struct{}
	stager        privateIngressStager
}

func (g *applicationIngress) generation(ctx context.Context, gateway, app string) (int64, error) {
	if g == nil || g.host == nil || g.host.teamRuntime == nil {
		return 0, fmt.Errorf("gateway authority unavailable")
	}
	binding, err := (teamDirectory{app: g.host, store: g.host.teamRuntime.store}).ResolveGateway(ctx, gateway, app)
	if err != nil {
		return 0, err
	}
	return binding.BotGeneration, nil
}
func (g *applicationIngress) deliver(ctx context.Context, event ingressowner.Event) (ingressowner.Record, error) {
	if g == nil || g.router == nil || g.resolver == nil || g.private == nil || g.team == nil {
		return ingressowner.Record{}, fmt.Errorf("application ingress unavailable")
	}
	record, _, err := g.router.Admit(ctx, event, g.resolver)
	if err != nil {
		return record, err
	}
	if record.State != ingressowner.Pending || record.Owner == nil {
		return record, nil
	}
	if record.DeliveryTarget == nil && record.Owner.Application == ingressowner.Team {
		migrationID, _, lookupErr := g.router.CommittedMigration(ctx, *record.Owner)
		if lookupErr == nil {
			if record.Event.Kind != ingressowner.Message {
				return record, nil
			}
			if err = g.router.MapPendingDelivery(ctx, record.Event.Key, migrationID); err != nil {
				return record, err
			}
			record, err = g.router.Get(ctx, record.Event.Key)
			if err != nil {
				return record, err
			}
		} else if !errors.Is(lookupErr, sql.ErrNoRows) && !errors.Is(lookupErr, ingressowner.ErrOwnerDraining) {
			return record, lookupErr
		}
	}
	owner := *record.Owner
	if record.DeliveryTarget != nil {
		owner = *record.DeliveryTarget
	}
	var admit ingressowner.Admitter
	switch owner.Application {
	case ingressowner.Private:
		admit = g.private
	case ingressowner.Team:
		admit = g.team
	default:
		return record, ingressowner.ErrConflict
	}
	record, err = g.router.Deliver(ctx, event.Key, admit)
	if errors.Is(err, ingressowner.ErrOwnerDraining) {
		return record, nil
	}
	if errors.Is(err, privatechat.ErrDeferred) {
		g.wakeStaging()
		return record, nil
	}
	return record, err
}
func (g *applicationIngress) hook() *feishu.RawIngressHook {
	return &feishu.RawIngressHook{
		Authorize: func(ctx context.Context, raw platformingress.RawIngressEnvelope) (bool, error) {
			if err := raw.Validate(); err != nil {
				return false, err
			}
			if raw.SenderTenant != "" && raw.SenderTenant != raw.Tenant {
				return false, nil
			}
			_, err := g.generation(ctx, raw.GatewayRef, raw.AppID)
			return err == nil, err
		},
		Receive: func(ctx context.Context, raw platformingress.RawIngressEnvelope) error {
			gen, err := g.generation(ctx, raw.GatewayRef, raw.AppID)
			if err != nil {
				return err
			}
			e, err := ingressmapper.Message(raw, gen)
			if err != nil {
				return err
			}
			_, err = g.deliver(ctx, e)
			return err
		},
		ReceiveInteractionResponse: func(ctx context.Context, raw platformingress.HumanInteractionEnvelope) (feishu.InteractionResponse, error) {
			gen, err := g.generation(ctx, raw.GatewayRef, raw.AppID)
			if err != nil {
				return feishu.InteractionResponse{}, err
			}
			e, err := ingressmapper.Card(raw, gen)
			if err != nil {
				return feishu.InteractionResponse{}, err
			}
			record, err := g.deliver(ctx, e)
			if err != nil {
				return feishu.InteractionResponse{}, err
			}
			if record.State == ingressowner.Unresolved && e.Kind == ingressowner.Card && e.CardKind == "private_view" {
				// A same-card response belongs to this HTTP callback. Background
				// admission would commit navigation after its response was gone.
				readCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
				readErr := g.recoverUnknownPrivateCard(readCtx, e)
				cancel()
				if readErr != nil {
					return feishu.InteractionResponse{RawBody: json.RawMessage(`{"toast":{"type":"info","content":"暂时无法核实这张卡片，菜单未改变。请稍后再点，或发送 /menu 打开菜单。"}}`)}, nil
				}
				record, err = g.deliver(ctx, e)
				if err != nil {
					return feishu.InteractionResponse{}, err
				}
			}
			if record.Receipt != nil && record.Receipt.Replacement != nil {
				current, err := g.router.CallbackResponseCurrent(ctx, record)
				if err != nil {
					return feishu.InteractionResponse{}, err
				}
				if !current {
					return feishu.InteractionResponse{RawBody: json.RawMessage(`{"toast":{"type":"info","content":"菜单已更新，请使用当前卡片。"}}`)}, nil
				}
			}
			if len(record.Response) > 0 {
				return feishu.InteractionResponse{RawBody: record.Response}, nil
			}
			if record.State == ingressowner.Expired {
				return feishu.InteractionResponse{RawBody: json.RawMessage(`{"toast":{"type":"info","content":"这张卡片已过期，请重新打开。"}}`)}, nil
			}
			return feishu.InteractionResponse{}, nil
		},
		ReceiveMenu: func(ctx context.Context, raw feishu.MenuIngressEvent) error {
			gen, err := g.generation(ctx, raw.GatewayRef, raw.AppID)
			if err != nil {
				return err
			}
			e, err := ingressmapper.Menu(raw, gen)
			if err != nil {
				return err
			}
			_, err = g.deliver(ctx, e)
			return err
		},
		ReceivePlatformEvent: func(ctx context.Context, raw feishu.PlatformIngressEvent) error {
			gen, err := g.generation(ctx, raw.GatewayRef, raw.AppID)
			if err != nil {
				return err
			}
			e, err := ingressmapper.Observation(raw, gen)
			if err != nil {
				return err
			}
			_, err = g.deliver(ctx, e)
			return err
		},
	}
}
