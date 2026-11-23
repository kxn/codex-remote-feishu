package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/ingressowner"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type personalTargetClaim struct {
	ScopeID, MigrationID string
	Source               ingressowner.OwnerScopeBinding
	Target               ingressowner.Owner
}
type personalTargetClaimRef struct{ ScopeID, MigrationID string }

func personalTargetClaimID(scope, id string) string {
	raw, _ := json.Marshal([]string{scope, id})
	return "personal-migration-target:" + migrationBytesHash(raw)
}

// ClaimTargetBinding first closes neutral intake and the source writer fence,
// then persists a random target owner and registers its verified P2P binding.
func (c *personalMigrationCoordinator) ClaimTargetBinding(ctx context.Context, binding ingressowner.OwnerScopeBinding, migrationID string, now time.Time) (ingressowner.PrivateBinding, error) {
	var zero ingressowner.PrivateBinding
	if c == nil || c.app == nil || c.app.teamRuntime == nil || c.router == nil || migrationID == "" || binding.ScopeID == "" || now.IsZero() {
		return zero, ingressowner.ErrInvalid
	}
	source, err := c.router.LookupImportedOwner(ctx, binding.Tenant, binding.AppID, binding.GatewayID, binding.BotGeneration, binding.ChatID, binding.OpenID)
	if err != nil {
		return zero, err
	}
	if source != binding.Owner {
		return zero, ingressowner.ErrConflict
	}
	verified, witness, err := (teamMigrationOwnerVerifier{store: c.app.teamRuntime.store}).historicalBinding(ctx, binding.ScopeID, binding.AppID, binding.SourceEventKey.EventID, binding.BotGeneration)
	if err != nil {
		return zero, err
	}
	verified.Owner = binding.Owner
	if verified != binding {
		return zero, ingressowner.ErrConflict
	}
	if err = (teamMigrationOwnerVerifier{store: c.app.teamRuntime.store}).VerifyOwnerScopeBinding(ctx, binding, nil, witness); err != nil {
		return zero, err
	}
	var claim personalTargetClaim
	err = c.app.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		_, e := tx.GetContent(binding.ScopeID, personalTargetClaimID(binding.ScopeID, migrationID))
		return e
	})
	if errors.Is(err, workstore.ErrNotFound) {
		if _, err = c.router.BeginBoundOwnerMigration(ctx, binding, migrationID); err != nil {
			return zero, err
		}
		if _, _, err = c.app.teamRuntime.beginPersonalSourceFence(ctx, binding.ScopeID, migrationID, now); err != nil {
			return zero, err
		}
	} else if err != nil {
		return zero, err
	}
	err = c.app.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		fence, e := tx.GetPersonalMigrationFence(binding.ScopeID)
		if e != nil {
			return e
		}
		if fence.MigrationID != migrationID {
			return workstore.ErrConflict
		}
		content, e := tx.GetContent(binding.ScopeID, personalTargetClaimID(binding.ScopeID, migrationID))
		if e == nil {
			if json.Unmarshal(content.Data, &claim) != nil || claim.ScopeID != binding.ScopeID || claim.MigrationID != migrationID || claim.Source != binding {
				return workstore.ErrConflict
			}
			return nil
		}
		if !errors.Is(e, workstore.ErrNotFound) {
			return e
		}
		var nonce [24]byte
		if _, e = rand.Read(nonce[:]); e != nil {
			return e
		}
		claim = personalTargetClaim{ScopeID: binding.ScopeID, MigrationID: migrationID, Source: binding, Target: ingressowner.Owner{Application: ingressowner.Private, ID: hex.EncodeToString(nonce[:]), Epoch: binding.Owner.Epoch + 1}}
		raw, e := json.Marshal(claim)
		if e != nil {
			return e
		}
		return tx.PutContent(work.NewContent(personalTargetClaimID(binding.ScopeID, migrationID), binding.ScopeID, "application/json", raw, now))
	})
	if err != nil {
		return zero, err
	}
	target := ingressowner.PrivateBinding{Tenant: binding.Tenant, AppID: binding.AppID, GatewayID: binding.GatewayID, BindingGeneration: binding.BotGeneration, Operator: ingressowner.Operator{IDType: "open_id", ID: binding.OpenID}, ChatID: binding.ChatID, Owner: claim.Target}
	proof, err := json.Marshal(personalTargetClaimRef{binding.ScopeID, migrationID})
	if err != nil {
		return zero, err
	}
	if err = c.router.RecordPrivateBinding(ctx, target, proof); err != nil {
		return zero, err
	}
	return target, nil
}

// VerifyPrivateBinding derives authority from the durable fence, target claim,
// real SDK witness and neutral historical owner; proof is only a lookup key.
func (c *personalMigrationCoordinator) VerifyPrivateBinding(ctx context.Context, b ingressowner.PrivateBinding, proof []byte) error {
	if c == nil || c.app == nil || c.app.teamRuntime == nil || c.router == nil {
		return ingressowner.ErrInvalid
	}
	var ref personalTargetClaimRef
	if json.Unmarshal(proof, &ref) != nil || ref.ScopeID == "" || ref.MigrationID == "" {
		return ingressowner.ErrInvalid
	}
	var claim personalTargetClaim
	err := c.app.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		fence, e := tx.GetPersonalMigrationFence(ref.ScopeID)
		if e != nil {
			return e
		}
		if fence.MigrationID != ref.MigrationID {
			return workstore.ErrConflict
		}
		content, e := tx.GetContent(ref.ScopeID, personalTargetClaimID(ref.ScopeID, ref.MigrationID))
		if e != nil {
			return e
		}
		if json.Unmarshal(content.Data, &claim) != nil || claim.ScopeID != ref.ScopeID || claim.MigrationID != ref.MigrationID {
			return workstore.ErrConflict
		}
		return nil
	})
	if err != nil {
		return err
	}
	s := claim.Source
	if claim.Target.Application != ingressowner.Private || claim.Target.ID == "" || claim.Target.Epoch != s.Owner.Epoch+1 || b.Owner != claim.Target || b.Tenant != s.Tenant || b.AppID != s.AppID || b.GatewayID != s.GatewayID || b.BindingGeneration != s.BotGeneration || b.ChatID != s.ChatID || b.Operator.IDType != "open_id" || b.Operator.ID != s.OpenID {
		return ingressowner.ErrConflict
	}
	owner, err := c.router.LookupImportedOwner(ctx, s.Tenant, s.AppID, s.GatewayID, s.BotGeneration, s.ChatID, s.OpenID)
	if err != nil {
		return err
	}
	if owner != s.Owner {
		return ingressowner.ErrConflict
	}
	actual, witness, err := (teamMigrationOwnerVerifier{store: c.app.teamRuntime.store}).historicalBinding(ctx, s.ScopeID, s.AppID, s.SourceEventKey.EventID, s.BotGeneration)
	if err != nil {
		return err
	}
	actual.Owner = s.Owner
	if actual != s {
		return ingressowner.ErrConflict
	}
	if err = (teamMigrationOwnerVerifier{store: c.app.teamRuntime.store}).VerifyOwnerScopeBinding(ctx, s, nil, witness); err != nil {
		return err
	}
	return nil
}

var _ ingressowner.BindingVerifier = (*personalMigrationCoordinator)(nil)
