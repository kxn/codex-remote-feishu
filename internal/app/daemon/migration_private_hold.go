package daemon

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/ingressowner"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// WithVerifiedMigrationHold authorizes only the source-wide barrier here.
// Each remaining source-operation hold requires its own reconciliation proof.
func (v teamSealedImportVerifier) WithVerifiedMigrationHold(ctx context.Context, req privatechat.MigrationImportRequest, receipt privatechat.MigrationImportReceipt, hold orchestrator.PrivateRecoveryHold, commit func(json.RawMessage) error) error {
	if (hold.Kind != "migration_source_unverified" && hold.Kind != "source_native_commands") || req.SourceHandle == "" || receipt.ImportID != req.ImportID || receipt.Owner != req.Owner || receipt.Generation != req.Generation || receipt.SourceScope != v.binding.ScopeID {
		return privatechat.ErrRecoveryHeld
	}
	var seal workstore.PersonalMigrationSeal
	if err := v.app.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		seal, e = tx.GetPersonalMigrationSeal(v.binding.ScopeID, req.SourceHandle)
		return e
	}); err != nil {
		return err
	}
	if seal.SourceHash != receipt.SourceHash || migrationBytesHash(seal.Evidence) != receipt.ProofHash {
		return privatechat.ErrJournalConflict
	}
	var source PersonalMigrationSource
	var frozen struct {
		SourceHash string `json:"source_hash"`
		Reason     string `json:"reason"`
	}
	var evidence personalSourceSealEvidence
	if json.Unmarshal(seal.Manifest, &source) != nil || json.Unmarshal(seal.Evidence, &evidence) != nil {
		return privatechat.ErrJournalConflict
	}
	if hold.Kind == "migration_source_unverified" && (json.Unmarshal(hold.Frozen, &frozen) != nil || frozen.SourceHash != source.SHA256 || frozen.Reason != "drain_and_final_export_proof_required") {
		return privatechat.ErrJournalConflict
	}
	if hold.Kind == "source_native_commands" {
		var command workstore.NativeCommand
		if json.Unmarshal(hold.Frozen, &command) != nil || command.CommandID == "" || command.Binding.ScopeID != seal.ScopeID {
			return privatechat.ErrJournalConflict
		}
		found := 0
		for _, row := range source.Source.Records {
			if row.Table == "native_commands" && row.Key == command.CommandID && bytes.Equal(row.Payload, hold.Frozen) {
				found++
			}
		}
		stopped := 0
		for _, worker := range evidence.Workers {
			if worker.WorkerID == command.Binding.WorkerSessionID {
				stopped++
			}
		}
		if found != 1 || stopped != 1 {
			return privatechat.ErrRecoveryHeld
		}
		if err := v.app.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
			op, err := tx.GetOperation(command.CommandID)
			if err != nil {
				return err
			}
			if op.ScopeID != seal.ScopeID || (op.State != work.OperationConfirmed && op.State != work.OperationTerminal) {
				return privatechat.ErrRecoveryHeld
			}
			return nil
		}); err != nil {
			return err
		}
	}
	proof := ingressowner.ImportProof{Drain: evidence.Neutral, SourceSnapshotHash: seal.SourceHash, Target: v.target, TransactionID: req.ImportID}
	return v.router.WithCommittedOwnerMigration(ctx, proof, func() error {
		if err := v.app.verifyPersonalSourceSealLocal(ctx, seal, v.limits); err != nil {
			return err
		}
		raw, err := json.Marshal(proof)
		if err != nil {
			return err
		}
		return commit(raw)
	})
}
