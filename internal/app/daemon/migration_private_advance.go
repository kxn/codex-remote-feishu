package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	gatewaypkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/gateway"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/ingressowner"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// personalMigrationCoordinator holds already assembled trusted source and
// target ports. Gateway coverage and the target binding must be registered by
// the host before Advance; it never starts a gateway or enables the target.
type personalMigrationCoordinator struct {
	app       *App
	router    *ingressowner.Router
	journal   *privatechat.Journal
	processor *privatechat.Processor
	assetRoot string
	limits    workstore.PersonalExportLimits
	mu        sync.Mutex
	verified  map[ingressowner.Owner]personalMigrationTarget
}
type personalMigrationTarget struct {
	binding ingressowner.OwnerScopeBinding
	target  ingressowner.Owner
	request privatechat.MigrationImportRequest
}
type personalMigrationProgress struct {
	Stage          string
	Source         workstore.PersonalMigrationSeal
	Receipt        privatechat.MigrationImportReceipt
	RemainingHolds int
}

func (c *personalMigrationCoordinator) PrepareHistoricalBindings(ctx context.Context) ([]ingressowner.OwnerScopeBinding, error) {
	if c == nil || c.app == nil {
		return nil, fmt.Errorf("migration coordinator unavailable")
	}
	if c.app.teamRuntime == nil || c.app.teamRuntime.store == nil {
		return nil, privatechat.ErrJournalState
	}
	var scopes []work.WorkScope
	if err := c.app.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error { var e error; scopes, e = tx.ListScopes(); return e }); err != nil {
		return nil, err
	}
	for _, scope := range scopes {
		if scope.Kind != work.ScopePersonal {
			continue
		}
		var fence workstore.PersonalMigrationFence
		err := c.app.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error { var e error; fence, e = tx.GetPersonalMigrationFence(scope.ID); return e })
		if err != nil && !errors.Is(err, workstore.ErrNotFound) {
			return nil, err
		}
		id := fence.MigrationID
		if id == "" {
			raw, _ := json.Marshal([]string{scope.ID, scope.RoleID})
			id = "personal-migration:" + migrationBytesHash(raw)
		}
		if _, _, err = c.app.teamRuntime.beginPersonalSourceFence(ctx, scope.ID, id, time.Now().UTC()); err != nil {
			return nil, err
		}
	}
	bindings, err := c.app.prepareHistoricalBindings(ctx, c.router)
	if err != nil {
		return nil, err
	}
	for _, binding := range bindings {
		migrationID, _, e := c.MigrationIDs(ctx, binding)
		if e != nil {
			return nil, e
		}
		if _, _, e = c.app.teamRuntime.beginPersonalSourceFence(ctx, binding.ScopeID, migrationID, time.Now().UTC()); e != nil {
			return nil, e
		}
	}
	return bindings, nil
}

// MigrationIDs preserves an existing fence identity on restart. New IDs are
// deterministic from the registry-minted source owner and personal scope.
func (c *personalMigrationCoordinator) MigrationIDs(ctx context.Context, binding ingressowner.OwnerScopeBinding) (string, string, error) {
	if c == nil || c.app == nil || c.app.teamRuntime == nil || binding.ScopeID == "" || binding.Owner.ID == "" {
		return "", "", privatechat.ErrJournalState
	}
	var fence workstore.PersonalMigrationFence
	err := c.app.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		fence, e = tx.GetPersonalMigrationFence(binding.ScopeID)
		return e
	})
	if err != nil && !errors.Is(err, workstore.ErrNotFound) {
		return "", "", err
	}
	raw, e := json.Marshal(struct {
		Scope string
		Owner ingressowner.Owner
	}{binding.ScopeID, binding.Owner})
	if e != nil {
		return "", "", e
	}
	migrationID := fence.MigrationID
	if migrationID == "" {
		migrationID = "personal-migration:" + migrationBytesHash(raw)
	}
	return migrationID, "private-import:" + migrationBytesHash(append(raw, []byte(migrationID)...)), nil
}

// AdvanceOwner derives the private target owner and journal owner from the
// durable source claim, so callers never choose a target identity.
func (c *personalMigrationCoordinator) AdvanceOwner(ctx context.Context, binding ingressowner.OwnerScopeBinding, migrationID, importID string, now time.Time) (personalMigrationProgress, error) {
	target, err := c.ClaimTargetBinding(ctx, binding, migrationID, now)
	if err != nil {
		return personalMigrationProgress{}, err
	}
	owner := privatechat.Owner{GatewayID: target.GatewayID, SurfaceID: gatewaypkg.SurfaceIDForInbound(target.GatewayID, "", "p2p", target.Operator.ID), ChatID: target.ChatID, UserID: target.Operator.ID, TenantID: target.Tenant, AppID: target.AppID}
	return c.Advance(ctx, binding, target.Owner, privatechat.MigrationImportRequest{ImportID: importID, SourceHandle: migrationID, Owner: owner, Generation: target.Owner.Epoch}, now)
}

// Advance may be repeated after a crash. It derives every proof from the two
// stores and router; hold resolution remains blocked for uncertain outcomes.
func (c *personalMigrationCoordinator) Advance(ctx context.Context, binding ingressowner.OwnerScopeBinding, target ingressowner.Owner, req privatechat.MigrationImportRequest, now time.Time) (personalMigrationProgress, error) {
	var progress personalMigrationProgress
	if c == nil || c.app == nil || c.app.teamRuntime == nil || c.router == nil || c.journal == nil || req.SourceHandle == "" || req.ImportID == "" || binding.ScopeID == "" || target.Application != ingressowner.Private {
		return progress, privatechat.ErrJournalState
	}
	targetBinding, err := c.router.PrivateBindingForOwner(ctx, binding.Tenant, binding.AppID, binding.GatewayID, binding.BotGeneration, binding.ChatID, target)
	if err != nil {
		return progress, err
	}
	expected := privatechat.Owner{GatewayID: targetBinding.GatewayID, SurfaceID: gatewaypkg.SurfaceIDForInbound(targetBinding.GatewayID, "", "p2p", targetBinding.Operator.ID), ChatID: targetBinding.ChatID, UserID: targetBinding.Operator.ID, TenantID: targetBinding.Tenant, AppID: targetBinding.AppID}
	if req.Owner != expected || req.Generation != target.Epoch {
		return progress, privatechat.ErrJournalConflict
	}
	c.mu.Lock()
	if c.verified == nil {
		c.verified = make(map[ingressowner.Owner]personalMigrationTarget)
	}
	old, exists := c.verified[binding.Owner]
	current := personalMigrationTarget{binding, target, req}
	if exists && old != current {
		c.mu.Unlock()
		return progress, privatechat.ErrJournalConflict
	}
	c.verified[binding.Owner] = current
	c.mu.Unlock()
	c.router.SetMigrationVerifier(c)
	c.router.SetTargetReadyVerifier(c)
	var seal workstore.PersonalMigrationSeal
	err = c.app.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		seal, e = tx.GetPersonalMigrationSeal(binding.ScopeID, req.SourceHandle)
		return e
	})
	if errors.Is(err, workstore.ErrNotFound) {
		progress.Stage = "draining"
		drained, e := c.app.teamRuntime.advanceBoundPersonalSourceDrain(ctx, c.router, binding, req.SourceHandle, now)
		if e != nil {
			return progress, e
		}
		if e = c.recordLegacyCodexEvidence(ctx, binding.ScopeID, req.Owner, now); e != nil {
			return progress, e
		}
		seal, e = c.app.sealPersonalSource(ctx, c.router, binding, drained.Neutral, c.limits)
		if e != nil {
			return progress, e
		}
	} else if err != nil {
		return progress, err
	}
	progress.Source = seal
	progress.Stage = "sealed"
	adapter := teamSealedImportVerifier{app: c.app, router: c.router, binding: binding, target: target, admitter: privateApplicationAdmitter{router: c.router}, assetRoot: c.assetRoot, limits: c.limits}
	importer, err := privatechat.NewMigrationImporter(c.journal, adapter, c.assetRoot)
	if err != nil {
		return progress, err
	}
	receipt, err := c.journal.MigrationReceipt(ctx, req.Owner, req.Generation, req.ImportID)
	if errors.Is(err, privatechat.ErrJournalState) {
		receipt, err = importer.Import(ctx, req)
	}
	if err != nil {
		return progress, err
	}
	if receipt.SourceHash != seal.SourceHash || receipt.SourceScope != binding.ScopeID {
		return progress, privatechat.ErrJournalConflict
	}
	progress.Receipt = receipt
	progress.Stage = "imported"
	var evidence personalSourceSealEvidence
	if err = json.Unmarshal(seal.Evidence, &evidence); err != nil {
		return progress, err
	}
	proof := ingressowner.ImportProof{Drain: evidence.Neutral, SourceSnapshotHash: seal.SourceHash, Target: target, TransactionID: req.ImportID}
	if id, activatedTarget, e := c.router.CommittedMigration(ctx, binding.Owner); e == nil {
		if id != req.SourceHandle || activatedTarget != target {
			return progress, privatechat.ErrJournalConflict
		}
		if e = c.router.VerifyCommittedOwnerMigration(ctx, proof); e != nil {
			return progress, e
		}
		progress.Stage = "activated"
		return progress, nil
	} else if !errors.Is(e, ingressowner.ErrOwnerDraining) && !errors.Is(e, sql.ErrNoRows) {
		return progress, e
	}
	if err = c.router.CommitOwnerMigration(ctx, proof); err != nil {
		return progress, err
	}
	progress.Stage = "transferred"
	for {
		checkpoint, e := c.journal.LoadCheckpoint(ctx, req.Owner, req.Generation)
		if e != nil {
			return progress, e
		}
		progress.RemainingHolds = len(checkpoint.Core.Holds)
		if len(checkpoint.Core.Holds) == 0 || (len(checkpoint.Core.Holds) == 1 && checkpoint.Core.Holds[0].Kind == "headless_launch") {
			if !checkpoint.Core.MigrationRecoveryReleased {
				return progress, privatechat.ErrRecoveryHeld
			}
			hasPlan, e := c.journal.HasMigrationResumePlan(ctx, req)
			if e != nil {
				return progress, e
			}
			if len(checkpoint.Core.Holds) == 0 && hasPlan {
				if c.processor == nil {
					return progress, privatechat.ErrDeferred
				}
				if _, e = c.processor.PrepareMigrationResume(ctx, req.Owner, req.Generation); e != nil {
					return progress, e
				}
			}
			ready, e := c.journal.MigrationReadyReceipt(ctx, req)
			if e != nil {
				return progress, e
			}
			readyProof, e := json.Marshal(ready)
			if e != nil {
				return progress, e
			}
			if e = c.router.ActivateMigrationTarget(ctx, proof, readyProof); e != nil {
				return progress, e
			}
			progress.Stage = "activated"
			return progress, nil
		}
		resolved := false
		for _, hold := range checkpoint.Core.Holds {
			raw, e := json.Marshal(hold)
			if e != nil {
				return progress, e
			}
			e = importer.ResolveMigrationHold(ctx, req, migrationBytesHash(raw))
			if errors.Is(e, privatechat.ErrRecoveryHeld) {
				continue
			}
			if e != nil {
				return progress, e
			}
			resolved = true
			break
		}
		if !resolved {
			return progress, privatechat.ErrRecoveryHeld
		}
	}
}

func personalSourceComponentsIsolated(s workstore.PersonalMigrationDrainStatus) bool {
	return s.UnsettledAttempts == 0 && s.UnsettledAdmissions == 0 && s.UnsettledOperations == 0 && s.UnsettledOutbox == 0 && s.UnsettledPrivateCommands == 0 && s.UnsettledMenuDestinations == 0 && s.UnsettledPreviews == 0 && s.UnsettledReactions == 0 && s.OccupiedCarriers == 0 && s.OccupiedResources == 0 && s.OccupiedControllers == 0
}

func (c *personalMigrationCoordinator) VerifyTargetReady(ctx context.Context, proof ingressowner.ImportProof, raw []byte) error {
	if err := c.VerifyImport(ctx, proof); err != nil {
		return err
	}
	c.mu.Lock()
	value, ok := c.verified[proof.Drain.Owner]
	c.mu.Unlock()
	if !ok {
		return privatechat.ErrJournalConflict
	}
	var claimed privatechat.MigrationReadyReceipt
	if err := json.Unmarshal(raw, &claimed); err != nil {
		return err
	}
	actual, err := c.journal.MigrationReadyReceipt(ctx, value.request)
	if err != nil {
		return err
	}
	if actual != claimed {
		return privatechat.ErrJournalConflict
	}
	return nil
}

// VerifyImport is invoked under the neutral owner lock. It reads only local
// durable source and target facts and never calls back into the router.
func (c *personalMigrationCoordinator) VerifyImport(ctx context.Context, proof ingressowner.ImportProof) error {
	c.mu.Lock()
	value, ok := c.verified[proof.Drain.Owner]
	c.mu.Unlock()
	if !ok || value.target != proof.Target || value.request.ImportID != proof.TransactionID || value.request.SourceHandle != proof.Drain.MigrationID {
		return privatechat.ErrJournalConflict
	}
	var seal workstore.PersonalMigrationSeal
	if err := c.app.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		seal, e = tx.GetPersonalMigrationSeal(value.binding.ScopeID, value.request.SourceHandle)
		return e
	}); err != nil {
		return err
	}
	if seal.SourceHash != proof.SourceSnapshotHash {
		return privatechat.ErrJournalConflict
	}
	var evidence personalSourceSealEvidence
	if json.Unmarshal(seal.Evidence, &evidence) != nil || evidence.Neutral != proof.Drain {
		return privatechat.ErrJournalConflict
	}
	if err := c.app.verifyPersonalSourceSealLocal(ctx, seal, c.limits); err != nil {
		return err
	}
	receipt, err := c.journal.MigrationReceipt(ctx, value.request.Owner, value.request.Generation, value.request.ImportID)
	if err != nil {
		return err
	}
	if receipt.SourceHash != seal.SourceHash || receipt.SourceScope != seal.ScopeID || receipt.ProofHash != migrationBytesHash(seal.Evidence) {
		return privatechat.ErrJournalConflict
	}
	return nil
}
