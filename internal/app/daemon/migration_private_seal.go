package daemon

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/ingressowner"
)

type personalSourceSealEvidence struct {
	Neutral ingressowner.DrainProof   `json:"neutral"`
	Workers []personalSourceStopProof `json:"workers"`
}

func personalSnapshotSemanticHash(source PersonalMigrationSource) (string, error) {
	source.SHA256 = ""
	source.Source.SHA256 = ""
	source.Source.CapturedAt = time.Time{}
	raw, err := json.Marshal(source)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// sealPersonalSource persists immutable bytes only after independently checking
// the neutral registry and all actual source receipts. Every reader must call
// verifyPersonalSourceSeal again; a DB row alone is not a migration proof.
func (a *App) sealPersonalSource(ctx context.Context, router *ingressowner.Router, binding ingressowner.OwnerScopeBinding, proof ingressowner.DrainProof, limits workstore.PersonalExportLimits) (workstore.PersonalMigrationSeal, error) {
	var zero workstore.PersonalMigrationSeal
	r := a.teamRuntime
	if r == nil || router == nil {
		return zero, fmt.Errorf("source runtime and neutral router required")
	}
	if err := router.VerifyBoundOwnerMigrationDrain(ctx, binding, proof.MigrationID, proof); err != nil {
		return zero, err
	}
	var previous workstore.PersonalMigrationSeal
	err := r.store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		previous, e = tx.GetPersonalMigrationSeal(binding.ScopeID, proof.MigrationID)
		return e
	})
	if err == nil {
		if err = a.verifyPersonalSourceSeal(ctx, router, binding, previous, limits); err != nil {
			return zero, err
		}
		return previous, nil
	}
	if !errors.Is(err, workstore.ErrNotFound) {
		return zero, err
	}
	if err := r.waitPersonalSourceActivities(ctx, binding.ScopeID); err != nil {
		return zero, err
	}
	source, err := a.capturePersonalMigrationSource(ctx, r.store, binding.ScopeID, limits)
	if err != nil {
		return zero, err
	}
	semantic, err := personalSnapshotSemanticHash(source)
	if err != nil {
		return zero, err
	}
	manifest, err := json.Marshal(source)
	if err != nil {
		return zero, err
	}
	sum := sha256.Sum256(manifest)
	hash := hex.EncodeToString(sum[:])
	var seal workstore.PersonalMigrationSeal
	err = r.store.Within(ctx, func(tx *workstore.Tx) error {
		proofs, e := collectPersonalSourceStopProof(tx, binding.ScopeID)
		if e != nil {
			return e
		}
		current, e := tx.CapturePersonalExport(binding.ScopeID, limits)
		if e != nil {
			return e
		}
		candidate := source
		candidate.Source = current
		currentHash, e := personalSnapshotSemanticHash(candidate)
		if e != nil {
			return e
		}
		if currentHash != semantic {
			return workstore.ErrConflict
		}
		fence, e := tx.GetPersonalMigrationFence(binding.ScopeID)
		if e != nil {
			return e
		}
		if fence.MigrationID != proof.MigrationID {
			return workstore.ErrConflict
		}
		evidence, e := json.Marshal(personalSourceSealEvidence{Neutral: proof, Workers: proofs})
		if e != nil {
			return e
		}
		seal = workstore.PersonalMigrationSeal{ScopeID: binding.ScopeID, MigrationID: proof.MigrationID, FenceRevision: fence.Revision, SourceHash: hash, SemanticHash: semantic, ProofHash: proof.Digest, Manifest: manifest, Evidence: evidence, CreatedAt: time.Now().UTC()}
		return tx.StorePersonalMigrationSeal(seal)
	})
	if err != nil {
		return zero, err
	}
	if err = a.verifyPersonalSourceSeal(ctx, router, binding, seal, limits); err != nil {
		return zero, err
	}
	return seal, nil
}

func (a *App) verifyPersonalSourceSeal(ctx context.Context, router *ingressowner.Router, binding ingressowner.OwnerScopeBinding, seal workstore.PersonalMigrationSeal, limits workstore.PersonalExportLimits) error {
	if a.teamRuntime == nil || router == nil || seal.ScopeID != binding.ScopeID {
		return workstore.ErrConflict
	}
	var evidence personalSourceSealEvidence
	if err := json.Unmarshal(seal.Evidence, &evidence); err != nil {
		return err
	}
	if evidence.Neutral.Digest != seal.ProofHash || evidence.Neutral.MigrationID != seal.MigrationID {
		return workstore.ErrConflict
	}
	if err := router.VerifyBoundOwnerMigrationDrain(ctx, binding, seal.MigrationID, evidence.Neutral); err != nil {
		return err
	}
	if err := a.verifyPersonalSourceSealLocal(ctx, seal, limits); err != nil {
		return err
	}
	return router.VerifyBoundOwnerMigrationDrain(ctx, binding, seal.MigrationID, evidence.Neutral)
}

// The neutral router invokes this only while holding its owner lock, after it
// has already checked the corresponding bound drain and coverage proof.
func (a *App) verifyPersonalSourceSealLocal(ctx context.Context, seal workstore.PersonalMigrationSeal, limits workstore.PersonalExportLimits) error {
	if a.teamRuntime == nil {
		return workstore.ErrConflict
	}
	var saved workstore.PersonalMigrationSeal
	if err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		saved, e = tx.GetPersonalMigrationSeal(seal.ScopeID, seal.MigrationID)
		if e != nil {
			return e
		}
		if saved.SourceHash != seal.SourceHash || saved.SemanticHash != seal.SemanticHash || saved.ProofHash != seal.ProofHash || saved.FenceRevision != seal.FenceRevision || !bytes.Equal(saved.Manifest, seal.Manifest) || !bytes.Equal(saved.Evidence, seal.Evidence) {
			return workstore.ErrConflict
		}
		_, e = collectPersonalSourceStopProof(tx, seal.ScopeID)
		return e
	}); err != nil {
		return err
	}
	var isolatedEvidence personalSourceSealEvidence
	if json.Unmarshal(saved.Evidence, &isolatedEvidence) != nil {
		return workstore.ErrConflict
	}
	for _, worker := range isolatedEvidence.Workers {
		if worker.Isolated != nil {
			if err := a.teamRuntime.verifyPersonalExecutionIsolation(ctx, *worker.Isolated); err != nil {
				return err
			}
		}
	}
	sum := sha256.Sum256(saved.Manifest)
	if hex.EncodeToString(sum[:]) != saved.SourceHash {
		return workstore.ErrConflict
	}
	var frozen PersonalMigrationSource
	if err := json.Unmarshal(saved.Manifest, &frozen); err != nil {
		return err
	}
	if err := frozen.Verify(); err != nil {
		return err
	}
	current, err := a.capturePersonalMigrationSource(ctx, a.teamRuntime.store, seal.ScopeID, limits)
	if err != nil {
		return err
	}
	hash, err := personalSnapshotSemanticHash(current)
	if err != nil {
		return err
	}
	if hash != saved.SemanticHash {
		return workstore.ErrConflict
	}
	return nil
}
