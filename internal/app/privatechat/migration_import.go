package privatechat

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/renderer"
)

const migrationImportSchema = `
CREATE TABLE IF NOT EXISTS private_migration_imports(import_id TEXT PRIMARY KEY,source_scope TEXT NOT NULL,source_hash TEXT NOT NULL,proof_hash TEXT NOT NULL,tenant TEXT NOT NULL,app_id TEXT NOT NULL,gateway TEXT NOT NULL,surface TEXT NOT NULL,chat TEXT NOT NULL,user_id TEXT NOT NULL,generation INTEGER NOT NULL,receipt BLOB NOT NULL,UNIQUE(tenant,app_id,gateway,surface,chat,user_id,generation));
CREATE TABLE IF NOT EXISTS private_migration_records(import_id TEXT NOT NULL REFERENCES private_migration_imports(import_id),source_table TEXT NOT NULL,source_key TEXT NOT NULL,source_scope TEXT NOT NULL,source_order INTEGER NOT NULL,source_revision INTEGER NOT NULL,payload BLOB NOT NULL,payload_hash TEXT NOT NULL,data BLOB,data_hash TEXT NOT NULL,PRIMARY KEY(import_id,source_table,source_key));
CREATE TABLE IF NOT EXISTS private_migration_assets(import_id TEXT NOT NULL REFERENCES private_migration_imports(import_id),content_id TEXT NOT NULL,source_message_id TEXT NOT NULL,path TEXT NOT NULL,media_type TEXT NOT NULL,size INTEGER NOT NULL,sha256 TEXT NOT NULL,PRIMARY KEY(import_id,content_id,source_message_id));
CREATE TABLE IF NOT EXISTS private_migration_resolutions(import_id TEXT NOT NULL REFERENCES private_migration_imports(import_id),hold_hash TEXT NOT NULL,hold BLOB NOT NULL,evidence BLOB NOT NULL,revision INTEGER NOT NULL,PRIMARY KEY(import_id,hold_hash));
CREATE TABLE IF NOT EXISTS private_migration_resume(import_id TEXT PRIMARY KEY REFERENCES private_migration_imports(import_id),plan BLOB NOT NULL,plan_hash TEXT NOT NULL,effect_id TEXT NOT NULL DEFAULT '');
CREATE TABLE IF NOT EXISTS private_migration_input_holds(import_id TEXT NOT NULL REFERENCES private_migration_imports(import_id),inbox_id TEXT NOT NULL,message_id TEXT NOT NULL,reason TEXT NOT NULL,payload_hash TEXT NOT NULL,PRIMARY KEY(import_id,inbox_id));
CREATE TABLE IF NOT EXISTS private_migration_recovered_inputs(import_id TEXT NOT NULL REFERENCES private_migration_imports(import_id),inbox_id TEXT NOT NULL,payload_hash TEXT NOT NULL,ingress_seq INTEGER NOT NULL REFERENCES ingress(seq),revision INTEGER NOT NULL,PRIMARY KEY(import_id,inbox_id));
CREATE TABLE IF NOT EXISTS private_migration_recovery_session_proofs(import_id TEXT NOT NULL,inbox_id TEXT NOT NULL,proof BLOB NOT NULL,PRIMARY KEY(import_id,inbox_id),FOREIGN KEY(import_id,inbox_id) REFERENCES private_migration_recovered_inputs(import_id,inbox_id));
CREATE TABLE IF NOT EXISTS private_migration_suspended_queue(import_id TEXT PRIMARY KEY REFERENCES private_migration_imports(import_id),item_count INTEGER NOT NULL,payload BLOB NOT NULL,payload_hash TEXT NOT NULL);`

type MigrationImportRequest struct {
	ImportID, SourceHandle string
	Owner                  Owner
	Generation             int64
}

// MigrationSourceVerifier is installed by the trusted source host. It must
// independently load and validate the sealed source, neutral intake/coverage
// proof and canonical SDK input; caller-provided JSON is not an authority.
type MigrationSourceVerifier interface {
	LoadVerifiedMigration(context.Context, MigrationImportRequest) (MigrationImportBundle, error)
}
type MigrationImportBundle struct {
	SourceScope, SourceHash, ProofHash string
	Checkpoint                         orchestrator.PrivateCheckpoint
	Events                             []MigrationImportedEvent
	Records                            []MigrationImportedRecord
	Assets                             []MigrationImportedAsset
	Resume                             *MigrationResumeIntent
	InputHolds                         []MigrationInputHold
	SuspendQueue                       bool
}
type MigrationInputHold struct{ InboxID, MessageID, Reason, PayloadHash string }
type MigrationImportedEvent struct {
	Ingress                            Ingress
	SourceScope, SourceKey, SourceHash string
	Applied                            bool
}
type MigrationImportedRecord struct {
	Table, Key, ScopeID string
	Order, Revision     int64
	Payload             json.RawMessage
	PayloadHash         string
	Data                []byte
	DataHash            string
}
type MigrationImportedAsset struct {
	ContentID, SourceMessageID, Path, MediaType, SHA256 string
	Size                                                int64
}

// MigrationResumeIntent is an immutable source-derived selection, not an
// authorization to start a carrier. The source verifier supplies its facts.
type MigrationResumeIntent struct {
	EvidenceVersion, LegacyEvidenceID, LegacyEvidenceHash                                    string
	SourceHash, SourceFingerprint, SourceScope, WorkerID, LaunchContentID, LaunchContentHash string
	Backend, ProfileID, ConnectionIdentity, ThreadID, CWD, Namespace                         string
}
type MigrationImportReceipt struct {
	ImportID, SourceScope, SourceHash, ProofHash, BundleHash string
	Owner                                                    Owner
	Generation, Revision, Cursor                             int64
	Digest                                                   string
}

type MigrationImporter struct {
	journal   *Journal
	source    MigrationSourceVerifier
	assetRoot string
}

func NewMigrationImporter(j *Journal, source MigrationSourceVerifier, assetRoot string) (*MigrationImporter, error) {
	if j == nil || source == nil || !filepath.IsAbs(assetRoot) {
		return nil, ErrJournalState
	}
	root, err := filepath.EvalSymlinks(assetRoot)
	if err != nil {
		return nil, err
	}
	return &MigrationImporter{j, source, root}, nil
}

func (i *MigrationImporter) Import(ctx context.Context, req MigrationImportRequest) (MigrationImportReceipt, error) {
	var zero MigrationImportReceipt
	if req.ImportID == "" || req.SourceHandle == "" || !validOwner(req.Owner) || req.Generation < 1 {
		return zero, ErrJournalState
	}
	unlockReferences := i.journal.LockReferenceMutation()
	defer unlockReferences()
	bundle, err := i.source.LoadVerifiedMigration(ctx, req)
	if err != nil {
		return zero, err
	}
	if err = i.validateBundle(req, bundle); err != nil {
		return zero, err
	}
	bundleRaw, err := json.Marshal(bundle)
	if err != nil {
		return zero, err
	}
	bundleHash := migrationSHA(bundleRaw)
	j := i.journal
	j.processorMu.Lock()
	defer j.processorMu.Unlock()
	if j.processor != nil {
		j.processor.mu.Lock()
		defer j.processor.mu.Unlock()
		if j.processor.poisoned {
			return zero, ErrProcessorPoisoned
		}
	}
	tx, err := j.db.BeginTx(ctx, nil)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback()
	var old []byte
	err = tx.QueryRowContext(ctx, `SELECT receipt FROM private_migration_imports WHERE import_id=?`, req.ImportID).Scan(&old)
	if err == nil {
		var receipt MigrationImportReceipt
		if json.Unmarshal(old, &receipt) != nil {
			return zero, ErrJournalConflict
		}
		if receipt.Owner != req.Owner || receipt.Generation != req.Generation || receipt.SourceScope != bundle.SourceScope || receipt.SourceHash != bundle.SourceHash || receipt.ProofHash != bundle.ProofHash || receipt.BundleHash != bundleHash {
			return zero, ErrJournalConflict
		}
		return receipt, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return zero, err
	}
	if j.processor != nil {
		for owner := range j.processor.owners {
			if owner.SurfaceID == req.Owner.SurfaceID {
				return zero, ErrJournalConflict
			}
		}
	}
	var existing int
	if err = tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM private_checkpoint WHERE surface=?1)+(SELECT count(*) FROM ingress WHERE surface=?1)`, req.Owner.SurfaceID).Scan(&existing); err != nil {
		return zero, err
	}
	if existing != 0 {
		return zero, ErrJournalConflict
	}
	watermarks, err := migrationWatermarks(ctx, tx)
	if err != nil {
		return zero, err
	}
	checkpoint, err := remapMigrationCheckpoint(bundle.Checkpoint, watermarks)
	if err != nil {
		return zero, err
	}
	var suspendedRaw []byte
	if bundle.SuspendQueue {
		suspendedRaw, err = json.Marshal(checkpoint)
		if err != nil {
			return zero, err
		}
		checkpoint.QueuedQueueItemIDs = nil
		checkpoint.QueueItems = nil
		checkpoint.StagedImages = nil
		checkpoint.StagedFiles = nil
	}
	validator := orchestrator.NewService(nil, orchestrator.Config{PrivateApplication: true}, renderer.NewPlanner())
	if err = validator.RestorePrivateCheckpoint(checkpoint); err != nil {
		return zero, fmt.Errorf("migration checkpoint: %w", err)
	}
	var cursor int64
	for _, ev := range bundle.Events {
		sources, e := json.Marshal(ev.Ingress.Sources)
		if e != nil {
			return zero, e
		}
		res, e := tx.ExecContext(ctx, `INSERT INTO ingress(gateway,tenant,app_id,surface,chat,user_id,generation,event_id,lane,payload,sources) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, req.Owner.GatewayID, req.Owner.TenantID, req.Owner.AppID, req.Owner.SurfaceID, req.Owner.ChatID, req.Owner.UserID, req.Generation, ev.Ingress.EventID, ev.Ingress.Lane, []byte(ev.Ingress.Payload), sources)
		if e != nil {
			return zero, e
		}
		seq, e := res.LastInsertId()
		if e != nil {
			return zero, e
		}
		if ev.Applied {
			cursor = seq
			if _, e = tx.ExecContext(ctx, `INSERT INTO private_applied(seq,revision) VALUES(?,1)`, seq); e != nil {
				return zero, e
			}
		}
	}
	c := Checkpoint{Owner: req.Owner, Generation: req.Generation, Revision: 1, Cursor: cursor, Core: checkpoint}
	if err = verifyCheckpointSources(ctx, tx, c); err != nil {
		return zero, err
	}
	if bundle.SuspendQueue {
		var original orchestrator.PrivateCheckpoint
		if json.Unmarshal(suspendedRaw, &original) != nil {
			return zero, ErrJournalState
		}
		prior := c
		prior.Core = original
		if err = verifyCheckpointSources(ctx, tx, prior); err != nil {
			return zero, err
		}
	}
	raw, err := json.Marshal(checkpoint)
	if err != nil {
		return zero, err
	}
	base, err := json.Marshal(struct {
		Cursor     int64
		AppliedSeq int64
		NativeRef  *NativeEventRef
		Core       json.RawMessage
		Effects    []Effect
	}{cursor, 0, nil, raw, nil})
	if err != nil {
		return zero, err
	}
	digest := migrationSHA(base)
	if _, err = tx.ExecContext(ctx, `INSERT INTO private_checkpoint(tenant,app_id,gateway,surface,chat,user_id,generation,revision,cursor,data,digest) VALUES(?,?,?,?,?,?,?,1,?,?,?)`, req.Owner.TenantID, req.Owner.AppID, req.Owner.GatewayID, req.Owner.SurfaceID, req.Owner.ChatID, req.Owner.UserID, req.Generation, cursor, raw, digest); err != nil {
		return zero, err
	}
	receipt := MigrationImportReceipt{ImportID: req.ImportID, SourceScope: bundle.SourceScope, SourceHash: bundle.SourceHash, ProofHash: bundle.ProofHash, BundleHash: bundleHash, Owner: req.Owner, Generation: req.Generation, Revision: 1, Cursor: cursor, Digest: digest}
	receiptRaw, err := json.Marshal(receipt)
	if err != nil {
		return zero, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO private_migration_imports(import_id,source_scope,source_hash,proof_hash,tenant,app_id,gateway,surface,chat,user_id,generation,receipt) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, req.ImportID, bundle.SourceScope, bundle.SourceHash, bundle.ProofHash, req.Owner.TenantID, req.Owner.AppID, req.Owner.GatewayID, req.Owner.SurfaceID, req.Owner.ChatID, req.Owner.UserID, req.Generation, receiptRaw); err != nil {
		return zero, err
	}
	for _, r := range bundle.Records {
		if _, err = tx.ExecContext(ctx, `INSERT INTO private_migration_records(import_id,source_table,source_key,source_scope,source_order,source_revision,payload,payload_hash,data,data_hash) VALUES(?,?,?,?,?,?,?,?,?,?)`, req.ImportID, r.Table, r.Key, r.ScopeID, r.Order, r.Revision, []byte(r.Payload), r.PayloadHash, r.Data, r.DataHash); err != nil {
			return zero, err
		}
	}
	for _, a := range bundle.Assets {
		if _, err = tx.ExecContext(ctx, `INSERT INTO private_migration_assets(import_id,content_id,source_message_id,path,media_type,size,sha256) VALUES(?,?,?,?,?,?,?)`, req.ImportID, a.ContentID, a.SourceMessageID, a.Path, a.MediaType, a.Size, a.SHA256); err != nil {
			return zero, err
		}
	}
	if bundle.Resume != nil {
		plan, e := json.Marshal(bundle.Resume)
		if e != nil {
			return zero, e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO private_migration_resume(import_id,plan,plan_hash) VALUES(?,?,?)`, req.ImportID, plan, migrationSHA(plan)); e != nil {
			return zero, e
		}
	}
	for _, h := range bundle.InputHolds {
		if _, e := tx.ExecContext(ctx, `INSERT INTO private_migration_input_holds(import_id,inbox_id,message_id,reason,payload_hash) VALUES(?,?,?,?,?)`, req.ImportID, h.InboxID, h.MessageID, h.Reason, h.PayloadHash); e != nil {
			return zero, e
		}
	}
	if bundle.SuspendQueue {
		if _, err = tx.ExecContext(ctx, `INSERT INTO private_migration_suspended_queue(import_id,item_count,payload,payload_hash) VALUES(?,?,?,?)`, req.ImportID, len(bundle.Checkpoint.QueuedQueueItemIDs), suspendedRaw, migrationSHA(suspendedRaw)); err != nil {
			return zero, err
		}
	}
	if err = tx.Commit(); err != nil {
		return zero, err
	}
	if j.processor != nil {
		w, e := checkpoint.IDWatermarks()
		if e != nil {
			return zero, e
		}
		if e = j.processor.app.service.SeedPrivateIDWatermarks(w); e != nil {
			j.processor.poisoned = true
			return zero, e
		}
	}
	return receipt, nil
}
func migrationSHA(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
