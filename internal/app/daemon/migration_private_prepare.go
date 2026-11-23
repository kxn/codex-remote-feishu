package daemon

import (
	"context"
	"errors"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/ingressowner"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// prepareHistoricalBindings runs before accepting SDK traffic. Every personal
// scope without an unambiguous historical SDK identity receives a durable
// per-bot guard, leaving unrelated bots and group ingress available.
func (a *App) prepareHistoricalBindings(ctx context.Context, router *ingressowner.Router) ([]ingressowner.OwnerScopeBinding, error) {
	if a.teamRuntime == nil || a.teamRuntime.store == nil || router == nil {
		return nil, fmt.Errorf("migration source and router required")
	}
	store := a.teamRuntime.store
	verifier := teamMigrationOwnerVerifier{store: store}
	router.SetOwnerScopeBindingVerifier(verifier)
	router.SetLegacyScopeVerifier(verifier)
	var scopes []work.WorkScope
	if err := store.Within(ctx, func(tx *workstore.Tx) error { var e error; scopes, e = tx.ListScopes(); return e }); err != nil {
		return nil, err
	}
	type candidate struct {
		binding ingressowner.OwnerScopeBinding
		proof   []byte
	}
	var selected []candidate
	for _, scope := range scopes {
		if scope.Kind != work.ScopePersonal {
			continue
		}
		var bot work.RoleBotBinding
		if err := store.Within(ctx, func(tx *workstore.Tx) error { var e error; bot, e = tx.GetRoleBotBinding(scope.RoleID); return e }); err != nil {
			if errors.Is(err, workstore.ErrNotFound) {
				if e := router.RecordLegacyScopeBarrier(ctx, ingressowner.LegacyScopeBarrier{ScopeID: scope.ID, RoleID: scope.RoleID, Reason: "missing_bot_binding"}); e != nil {
					return nil, e
				}
				continue
			}
			return nil, err
		}
		barrier := ingressowner.LegacyScopeBarrier{ScopeID: scope.ID, RoleID: scope.RoleID, AppID: bot.AppID, GatewayID: bot.GatewayRef, BotGeneration: bot.IdentityGeneration}
		var choice *candidate
		ambiguous := false
		var after int64
		for {
			var observations []work.IngressObservation
			var next int64
			if err := store.Within(ctx, func(tx *workstore.Tx) error {
				var e error
				observations, next, e = tx.PersonalMigrationWitnessCandidates(scope.ID, after, 200)
				return e
			}); err != nil {
				return nil, err
			}
			for _, o := range observations {
				if o.Envelope.ChatType != "p2p" || o.Envelope.SenderType != "user" || o.Gateway.AppID != bot.AppID || o.Gateway.BotGeneration != bot.IdentityGeneration || o.Gateway.GatewayRef != bot.GatewayRef {
					continue
				}
				b, p, err := verifier.historicalBinding(ctx, scope.ID, o.Envelope.AppID, o.Envelope.EventID, o.Gateway.BotGeneration)
				if err != nil {
					if errors.Is(err, workstore.ErrScopeDenied) {
						continue
					}
					return nil, err
				}
				if choice != nil && (choice.binding.Tenant != b.Tenant || choice.binding.ChatID != b.ChatID || choice.binding.OpenID != b.OpenID) {
					ambiguous = true
					continue
				}
				if choice == nil {
					choice = &candidate{b, p}
				}
			}
			if len(observations) < 200 {
				break
			}
			if next <= after {
				return nil, workstore.ErrConflict
			}
			after = next
		}
		if choice == nil || ambiguous {
			barrier.Reason = "missing_sdk_identity"
			if ambiguous {
				barrier.Reason = "ambiguous_sdk_identity"
			}
			if e := router.RecordLegacyScopeBarrier(ctx, barrier); e != nil {
				return nil, e
			}
			continue
		}
		selected = append(selected, *choice)
	}
	result := make([]ingressowner.OwnerScopeBinding, 0, len(selected))
	for _, c := range selected {
		b, err := router.ImportOwnerScopeBinding(ctx, c.binding, c.proof)
		if err != nil {
			if errors.Is(err, ingressowner.ErrConflict) {
				barrier := ingressowner.LegacyScopeBarrier{ScopeID: c.binding.ScopeID, RoleID: c.binding.RoleID, AppID: c.binding.AppID, GatewayID: c.binding.GatewayID, BotGeneration: c.binding.BotGeneration, Reason: "source_owner_conflict"}
				if e := router.RecordLegacyScopeBarrier(ctx, barrier); e != nil {
					return nil, e
				}
				continue
			}
			return nil, err
		}
		if err = router.ResolveLegacyScopeBarrier(ctx, b); err != nil {
			return nil, err
		}
		result = append(result, b)
	}
	return result, nil
}
