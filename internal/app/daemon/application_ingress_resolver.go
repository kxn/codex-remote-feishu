package daemon

import (
	"context"
	"database/sql"
	"errors"

	"github.com/kxn/codex-remote-feishu/internal/app/ingressowner"
)

type applicationOwnerResolver struct {
	router *ingressowner.Router
}

func (r applicationOwnerResolver) Resolve(ctx context.Context, e ingressowner.Event, message *ingressowner.PlatformMessage) (ingressowner.Owner, error) {
	if r.router == nil {
		return ingressowner.Owner{}, ingressowner.ErrInvalid
	}
	if message != nil {
		if message.Owner.Application == ingressowner.Team {
			return message.Owner, nil
		}
		b, err := r.router.PrivateBindingForOwner(ctx, e.Key.Tenant, e.Key.AppID, e.Source.GatewayID, e.Source.BindingGeneration, message.ChatID, message.Owner)
		if err != nil {
			return ingressowner.Owner{}, err
		}
		if e.Kind != ingressowner.Recall && (e.Operator != b.Operator) {
			return ingressowner.Owner{}, ingressowner.ErrConflict
		}
		return b.Owner, nil
	}
	if e.Kind == ingressowner.Message && e.ChatType == "group" {
		return r.router.EnsureObservedTeamOwner(ctx, e)
	}
	if e.Kind == ingressowner.GroupMember {
		return r.router.TeamOwnerForChat(ctx, e.Key.Tenant, e.Key.AppID, e.Source.GatewayID, e.Source.BindingGeneration, e.ChatID)
	}
	if e.Kind == ingressowner.Message && e.ChatType == "p2p" {
		if e.Operator.IDType == "open_id" {
			owner, err := r.router.LookupImportedOwner(ctx, e.Key.Tenant, e.Key.AppID, e.Source.GatewayID, e.Source.BindingGeneration, e.ChatID, e.Operator.ID)
			if err == nil {
				return owner, nil
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return ingressowner.Owner{}, err
			}
		}
		binding, err := r.router.EnsureObservedPrivateBinding(ctx, e)
		if err != nil {
			return ingressowner.Owner{}, err
		}
		return binding.Owner, nil
	}
	if e.Kind == ingressowner.Menu {
		binding, err := r.router.PrivateBindingForEvent(ctx, e)
		if err != nil {
			return ingressowner.Owner{}, err
		}
		if err = r.router.AssertPrivateEventOwner(ctx, e, binding); err != nil {
			return ingressowner.Owner{}, err
		}
		if binding.Operator.IDType == "open_id" {
			source, lookupErr := r.router.LookupImportedOwner(ctx, e.Key.Tenant, e.Key.AppID, e.Source.GatewayID, e.Source.BindingGeneration, binding.ChatID, binding.Operator.ID)
			if lookupErr == nil {
				_, target, e2 := r.router.CommittedMigration(ctx, source)
				if e2 != nil || target != binding.Owner {
					return ingressowner.Owner{}, ingressowner.ErrUnresolved
				}
			} else if !errors.Is(lookupErr, sql.ErrNoRows) {
				return ingressowner.Owner{}, lookupErr
			}
		}
		return binding.Owner, nil
	}
	return ingressowner.Owner{}, ingressowner.ErrUnresolved
}
