package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/ingressmapper"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/ingressowner"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type teamMigrationOwnerVerifier struct {
	store *workstore.Store
}
type teamMigrationSourceWitness struct {
	IngressID string `json:"ingress_id"`
	Revision  int64  `json:"revision"`
	FactHash  string `json:"fact_hash"`
}

func (v teamMigrationOwnerVerifier) VerifyLegacyScopeBarrier(ctx context.Context, b ingressowner.LegacyScopeBarrier) error {
	if v.store == nil || (b.Reason != "missing_sdk_identity" && b.Reason != "ambiguous_sdk_identity" && b.Reason != "source_owner_conflict" && b.Reason != "missing_bot_binding") {
		return workstore.ErrScopeDenied
	}
	return v.store.Within(ctx, func(tx *workstore.Tx) error {
		scope, err := tx.GetScope(b.ScopeID)
		if err != nil {
			return err
		}
		if scope.Kind != work.ScopePersonal || scope.RoleID != b.RoleID {
			return workstore.ErrScopeDenied
		}
		bot, err := tx.GetRoleBotBinding(scope.RoleID)
		if errors.Is(err, workstore.ErrNotFound) {
			if b.Reason != "missing_bot_binding" || b.AppID != "" || b.GatewayID != "" || b.BotGeneration != 0 {
				return workstore.ErrScopeDenied
			}
			return nil
		}
		if err != nil {
			return err
		}
		if b.AppID != bot.AppID || b.GatewayID != bot.GatewayRef || b.BotGeneration != bot.IdentityGeneration {
			return workstore.ErrScopeDenied
		}
		return nil
	})
}

// historicalBinding builds a witness from the source store, never from a
// caller-supplied owner. The neutral registry assigns the opaque owner ID.
func (v teamMigrationOwnerVerifier) historicalBinding(ctx context.Context, scopeID, appID, eventID string, botGeneration int64) (ingressowner.OwnerScopeBinding, []byte, error) {
	var binding ingressowner.OwnerScopeBinding
	var witness teamMigrationSourceWitness
	if v.store == nil {
		return binding, nil, fmt.Errorf("trusted team store required")
	}
	err := v.store.Within(ctx, func(tx *workstore.Tx) error {
		scope, e := tx.GetScope(scopeID)
		if e != nil {
			return e
		}
		if scope.Kind != work.ScopePersonal {
			return workstore.ErrScopeDenied
		}
		record, observation, e := tx.PersonalMigrationIngressObservation(scopeID, appID, eventID, botGeneration)
		if e != nil {
			return e
		}
		event, e := ingressmapper.Message(observation.Envelope, botGeneration)
		if e != nil {
			return e
		}
		hash, e := ingressowner.EventFactHash(event)
		if e != nil {
			return e
		}
		binding = ingressowner.OwnerScopeBinding{SourceEventKey: event.Key, SourceFactHash: hash, ScopeID: scope.ID, PrincipalID: scope.PrincipalID, RoleID: scope.RoleID, GatewayID: observation.Gateway.GatewayRef, AppID: appID, BotGeneration: botGeneration, Tenant: event.Key.Tenant, ChatID: event.ChatID, OpenID: observation.Envelope.SenderIDs.OpenID}
		witness = teamMigrationSourceWitness{IngressID: record.ID, Revision: record.Revision, FactHash: hash}
		return nil
	})
	if err != nil {
		return ingressowner.OwnerScopeBinding{}, nil, err
	}
	proof, err := json.Marshal(witness)
	if err != nil {
		return ingressowner.OwnerScopeBinding{}, nil, err
	}
	if err = v.VerifyOwnerScopeBinding(ctx, binding, nil, proof); err != nil {
		return ingressowner.OwnerScopeBinding{}, nil, err
	}
	return binding, proof, nil
}

func (v teamMigrationOwnerVerifier) VerifyOwnerScopeBinding(ctx context.Context, b ingressowner.OwnerScopeBinding, neutral *ingressowner.Record, proof []byte) error {
	if v.store == nil {
		return fmt.Errorf("trusted team store required")
	}
	var witness teamMigrationSourceWitness
	if err := json.Unmarshal(proof, &witness); err != nil || witness.IngressID == "" || witness.Revision < 1 || witness.FactHash == "" {
		return fmt.Errorf("invalid team migration witness")
	}
	return v.store.Within(ctx, func(tx *workstore.Tx) error {
		scope, err := tx.GetScope(b.ScopeID)
		if err != nil {
			return err
		}
		if scope.Kind != work.ScopePersonal || scope.PrincipalID != b.PrincipalID || scope.RoleID != b.RoleID {
			return workstore.ErrScopeDenied
		}
		bot, err := tx.GetRoleBotBinding(scope.RoleID)
		if err != nil {
			return err
		}
		if bot.GatewayRef != b.GatewayID || bot.AppID != b.AppID || bot.IdentityGeneration != b.BotGeneration {
			return workstore.ErrConflict
		}
		raw, observation, err := tx.PersonalMigrationIngressObservation(b.ScopeID, b.AppID, b.SourceEventKey.EventID, b.BotGeneration)
		if err != nil {
			return err
		}
		if raw.ID != witness.IngressID || raw.Revision < witness.Revision {
			return workstore.ErrConflict
		}
		if observation.Gateway.GatewayRef != b.GatewayID || observation.Gateway.AppID != b.AppID || observation.Gateway.RoleID != b.RoleID || observation.Gateway.BotGeneration != b.BotGeneration || observation.Envelope.Tenant != b.Tenant || observation.Envelope.ChatID != b.ChatID || observation.Envelope.SenderIDs.OpenID != b.OpenID || observation.Envelope.EventID != b.SourceEventKey.EventID || b.SourceEventKey.Tenant != b.Tenant || b.SourceEventKey.AppID != b.AppID {
			return workstore.ErrScopeDenied
		}
		matches, err := tx.PersonalMigrationOpenIDMatches(scope.ID, b.Tenant, b.AppID, b.OpenID, b.BotGeneration)
		if err != nil {
			return err
		}
		if !matches {
			return workstore.ErrScopeDenied
		}
		event, err := ingressmapper.Message(observation.Envelope, b.BotGeneration)
		if err != nil {
			return err
		}
		hash, err := ingressowner.EventFactHash(event)
		if err != nil {
			return err
		}
		if hash != b.SourceFactHash || hash != witness.FactHash {
			return workstore.ErrConflict
		}
		if neutral != nil {
			actual, err := ingressowner.EventFactHash(neutral.Event)
			if err != nil {
				return err
			}
			if actual != hash {
				return workstore.ErrConflict
			}
		}
		return nil
	})
}
