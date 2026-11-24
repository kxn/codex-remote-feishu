package privatechat

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

// MigrationHoldVerifier must bind the exact frozen hold to the sealed source,
// committed target receipt and committed neutral transfer. It executes commit
// only while its trusted transfer guard is held; commit performs local I/O only.
type MigrationHoldVerifier interface {
	WithVerifiedMigrationHold(context.Context, MigrationImportRequest, MigrationImportReceipt, orchestrator.PrivateRecoveryHold, func(json.RawMessage) error) error
}

// ResolveMigrationHold removes one exact, independently verified hold. It is
// deliberately unavailable once the processor has materialized the owner;
// migration assembly must finish reconciliation before activating that owner.
func (i *MigrationImporter) ResolveMigrationHold(ctx context.Context, req MigrationImportRequest, holdHash string) error {
	unlockReferences := i.journal.LockReferenceMutation()
	defer unlockReferences()
	verifier, ok := i.source.(MigrationHoldVerifier)
	if !ok || holdHash == "" {
		return ErrJournalState
	}
	receipt, err := i.journal.MigrationReceipt(ctx, req.Owner, req.Generation, req.ImportID)
	if err != nil {
		return err
	}
	cp, err := i.journal.LoadCheckpoint(ctx, req.Owner, req.Generation)
	if err != nil {
		return err
	}
	var hold orchestrator.PrivateRecoveryHold
	matches := 0
	for _, h := range cp.Core.Holds {
		raw, e := json.Marshal(h)
		if e != nil {
			return e
		}
		if migrationSHA(raw) == holdHash {
			hold = h
			matches++
		}
	}
	if matches != 1 {
		var savedHold, savedEvidence []byte
		if matches != 0 {
			return ErrJournalConflict
		}
		err = i.journal.db.QueryRowContext(ctx, `SELECT hold,evidence FROM private_migration_resolutions WHERE import_id=? AND hold_hash=?`, req.ImportID, holdHash).Scan(&savedHold, &savedEvidence)
		if err != nil {
			return err
		}
		if migrationSHA(savedHold) != holdHash || json.Unmarshal(savedHold, &hold) != nil {
			return ErrJournalConflict
		}
		return verifier.WithVerifiedMigrationHold(ctx, req, receipt, hold, func(evidence json.RawMessage) error {
			if string(evidence) != string(savedEvidence) {
				return ErrJournalConflict
			}
			return nil
		})
	}
	return verifier.WithVerifiedMigrationHold(ctx, req, receipt, hold, func(evidence json.RawMessage) error {
		if !json.Valid(evidence) || len(evidence) == 0 {
			return ErrJournalState
		}
		return i.commitMigrationResolution(ctx, req, cp, hold, holdHash, evidence)
	})
}

func (i *MigrationImporter) commitMigrationResolution(ctx context.Context, req MigrationImportRequest, cp Checkpoint, hold orchestrator.PrivateRecoveryHold, hash string, evidence json.RawMessage) error {
	j := i.journal
	j.processorMu.Lock()
	defer j.processorMu.Unlock()
	if j.processor != nil {
		j.processor.mu.Lock()
		defer j.processor.mu.Unlock()
		if j.processor.poisoned {
			return ErrProcessorPoisoned
		}
		if _, loaded := j.processor.owners[req.Owner]; loaded {
			return ErrDeferred
		}
	}
	tx, err := j.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	o := req.Owner
	var revision, cursor int64
	var raw []byte
	err = tx.QueryRowContext(ctx, `SELECT revision,cursor,data FROM private_checkpoint WHERE tenant=? AND app_id=? AND gateway=? AND surface=? AND chat=? AND user_id=? AND generation=?`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, req.Generation).Scan(&revision, &cursor, &raw)
	if err != nil {
		return err
	}
	if revision != cp.Revision || cursor != cp.Cursor {
		return ErrJournalConflict
	}
	var stored orchestrator.PrivateCheckpoint
	if err = json.Unmarshal(raw, &stored); err != nil {
		return err
	}
	index := -1
	for n, h := range stored.Holds {
		b, e := json.Marshal(h)
		if e != nil {
			return e
		}
		if migrationSHA(b) == hash {
			if index >= 0 {
				return ErrJournalConflict
			}
			index = n
		}
	}
	if index < 0 {
		return ErrJournalConflict
	}
	var unsettled int
	err = tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM dispatch WHERE tenant=?1 AND app_id=?2 AND gateway=?3 AND surface=?4 AND chat=?5 AND user_id=?6 AND generation=?7 AND state IN ('prepared','sending','unknown','sent'))+(SELECT count(*) FROM private_effect WHERE tenant=?1 AND app_id=?2 AND gateway=?3 AND surface=?4 AND chat=?5 AND user_id=?6 AND generation=?7 AND state IN ('sending','unknown'))`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, req.Generation).Scan(&unsettled)
	if err != nil {
		return err
	}
	if unsettled != 0 {
		return ErrRecoveryHeld
	}
	stored.Holds = append(stored.Holds[:index], stored.Holds[index+1:]...)
	if len(stored.Holds) == 0 && stored.ActiveQueueItemID == "" && stored.AutoWhip.PendingReason == "" && stored.AutoContinue.Episode == nil {
		if err = verifyAppliedMigrationSources(ctx, tx, cp); err != nil {
			return err
		}
		stored.MigrationRecoveryReleased = true
	}
	validator := orchestrator.NewService(nil, orchestrator.Config{PrivateApplication: true}, nil)
	if err = validator.RestorePrivateCheckpoint(stored); err != nil {
		return err
	}
	raw, err = json.Marshal(stored)
	if err != nil {
		return err
	}
	base, err := json.Marshal(struct {
		Cursor     int64
		AppliedSeq int64
		NativeRef  *NativeEventRef
		Core       json.RawMessage
		Effects    []Effect
	}{cursor, 0, nil, raw, nil})
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE private_checkpoint SET revision=?,data=?,digest=? WHERE tenant=? AND app_id=? AND gateway=? AND surface=? AND chat=? AND user_id=? AND generation=? AND revision=?`, revision+1, raw, migrationSHA(base), o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, req.Generation, revision)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrJournalConflict
	}
	holdRaw, err := json.Marshal(hold)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO private_migration_resolutions(import_id,hold_hash,hold,evidence,revision) VALUES(?,?,?,?,?)`, req.ImportID, hash, holdRaw, []byte(evidence), revision+1)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func verifyAppliedMigrationSources(ctx context.Context, tx *sql.Tx, cp Checkpoint) error {
	required := map[string]bool{}
	for _, item := range cp.Core.QueueItems {
		if item == nil {
			return ErrJournalState
		}
		if item.SourceMessageID != "" {
			required[item.SourceMessageID] = true
		}
		for _, id := range item.SourceMessageIDs {
			if id != "" {
				required[id] = true
			}
		}
	}
	for _, image := range cp.Core.StagedImages {
		if image == nil {
			return ErrJournalState
		}
		if image.SourceMessageID != "" {
			required[image.SourceMessageID] = true
		}
	}
	for _, file := range cp.Core.StagedFiles {
		if file == nil {
			return ErrJournalState
		}
		if file.SourceMessageID != "" {
			required[file.SourceMessageID] = true
		}
	}
	if len(required) == 0 {
		return nil
	}
	o := cp.Owner
	rows, err := tx.QueryContext(ctx, `SELECT i.sources FROM ingress i JOIN private_applied a ON a.seq=i.seq WHERE i.tenant=? AND i.app_id=? AND i.gateway=? AND i.surface=? AND i.chat=? AND i.user_id=? AND i.generation=? AND i.seq<=?`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, cp.Generation, cp.Cursor)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return err
		}
		var ids []string
		if err = json.Unmarshal(raw, &ids); err != nil {
			return err
		}
		for _, id := range ids {
			delete(required, id)
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if len(required) > 0 {
		return errors.New("migration queued input lacks applied source ingress")
	}
	return nil
}
