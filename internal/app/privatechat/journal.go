package privatechat

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	_ "modernc.org/sqlite"
)

var ErrJournalConflict = errors.New("private journal conflict")
var ErrJournalState = errors.New("invalid private journal state")

// Ingress is an already authenticated private event. Payload and source IDs
// are immutable, including attachment references. Lane is "input" or "control".
type Ingress struct {
	Owner          Owner
	Generation     int64
	EventID, Lane  string
	OriginRouter   string
	OriginSequence int64
	Payload        json.RawMessage
	Sources        []string
}
type JournalEntry struct {
	Sequence int64
	Ingress  Ingress
}

type Journal struct {
	db            *sql.DB
	lock          *os.File
	processorMu   sync.Mutex
	referenceGate *sync.RWMutex
	processor     *Processor
}

// OpenJournal requires exclusive application-level ownership of path. The
// host must not open this database in a second live private application.
func OpenJournal(path string) (*Journal, error) {
	if strings.TrimSpace(path) == "" || path == ":memory:" || strings.HasPrefix(path, "file:") {
		return nil, errors.New("empty journal path")
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	// Resolve symlink aliases before taking the process lock.
	if resolved, resolveErr := filepath.EvalSymlinks(path); resolveErr == nil {
		path = resolved
	} else if os.IsNotExist(resolveErr) {
		parent, parentErr := filepath.EvalSymlinks(filepath.Dir(path))
		if parentErr != nil {
			return nil, parentErr
		}
		path = filepath.Join(parent, filepath.Base(path))
	} else {
		return nil, resolveErr
	}
	lock, err := lockJournal(path)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		unlockJournal(lock)
		return nil, err
	}
	if err = f.Chmod(0600); err != nil {
		f.Close()
		unlockJournal(lock)
		return nil, err
	}
	if err = f.Close(); err != nil {
		unlockJournal(lock)
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		unlockJournal(lock)
		return nil, err
	}
	db.SetMaxOpenConns(1)
	j := &Journal{db: db, lock: lock}
	var version int
	if err = db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version < 0 || version > 8 {
		j.Close()
		if err != nil {
			return nil, err
		}
		return nil, errors.New("unsupported private journal schema")
	}
	if version == 0 {
		var existing int
		if err = db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`).Scan(&existing); err != nil || existing != 0 {
			j.Close()
			if err != nil {
				return nil, err
			}
			return nil, errors.New("unversioned private journal is not empty")
		}
	}
	var mode string
	if err = db.QueryRow(`PRAGMA journal_mode=WAL`).Scan(&mode); err != nil || mode != "wal" {
		j.Close()
		return nil, errors.New("private journal requires WAL")
	}
	if _, err = db.Exec(`PRAGMA synchronous=FULL`); err != nil {
		j.Close()
		return nil, err
	}
	if version > 0 && version < 7 {
		if err = migrateDispatchSchema7(db); err != nil {
			j.Close()
			return nil, err
		}
		version = 7
	}
	var appliedTable, canceledColumn int
	if err = db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='private_applied'`).Scan(&appliedTable); err != nil {
		j.Close()
		return nil, err
	}
	if appliedTable != 0 {
		if err = db.QueryRow(`SELECT count(*) FROM pragma_table_info('private_applied') WHERE name='canceled_by'`).Scan(&canceledColumn); err != nil {
			j.Close()
			return nil, err
		}
	}
	appliedMigration := ""
	if appliedTable != 0 && canceledColumn == 0 {
		appliedMigration = `ALTER TABLE private_applied ADD COLUMN canceled_by INTEGER REFERENCES ingress(seq);`
	}
	var ingressTable, originColumn, discardColumn int
	if err = db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='ingress'`).Scan(&ingressTable); err != nil {
		j.Close()
		return nil, err
	}
	if ingressTable != 0 {
		if err = db.QueryRow(`SELECT count(*) FROM pragma_table_info('ingress') WHERE name='origin_router'`).Scan(&originColumn); err != nil {
			j.Close()
			return nil, err
		}
	}
	if appliedTable != 0 {
		if err = db.QueryRow(`SELECT count(*) FROM pragma_table_info('private_applied') WHERE name='route_discard'`).Scan(&discardColumn); err != nil {
			j.Close()
			return nil, err
		}
	}
	originMigration := ""
	if ingressTable != 0 && originColumn == 0 {
		originMigration += `ALTER TABLE ingress ADD COLUMN origin_router TEXT NOT NULL DEFAULT ''; ALTER TABLE ingress ADD COLUMN origin_seq INTEGER NOT NULL DEFAULT 0;`
	}
	if appliedTable != 0 && discardColumn == 0 {
		originMigration += `ALTER TABLE private_applied ADD COLUMN route_discard INTEGER NOT NULL DEFAULT 0;`
	}
	versionPragma := "PRAGMA user_version=8; COMMIT;"
	if _, err = db.Exec(`PRAGMA synchronous=FULL; PRAGMA foreign_keys=ON; BEGIN IMMEDIATE;
 CREATE TABLE IF NOT EXISTS ingress (seq INTEGER PRIMARY KEY AUTOINCREMENT, gateway TEXT NOT NULL, tenant TEXT NOT NULL, app_id TEXT NOT NULL, surface TEXT NOT NULL, chat TEXT NOT NULL, user_id TEXT NOT NULL, generation INTEGER NOT NULL, event_id TEXT NOT NULL, lane TEXT NOT NULL CHECK(lane IN ('input','control','inspect')), payload BLOB NOT NULL, sources BLOB NOT NULL, origin_router TEXT NOT NULL DEFAULT '', origin_seq INTEGER NOT NULL DEFAULT 0, UNIQUE(tenant,app_id,event_id));
 CREATE TABLE IF NOT EXISTS dispatch (id TEXT PRIMARY KEY, gateway TEXT NOT NULL, tenant TEXT NOT NULL, app_id TEXT NOT NULL, surface TEXT NOT NULL, chat TEXT NOT NULL, user_id TEXT NOT NULL, generation INTEGER NOT NULL, lane TEXT NOT NULL CHECK(lane IN ('input','control','inspect')), mode TEXT NOT NULL, backend TEXT NOT NULL, namespace TEXT NOT NULL, session TEXT NOT NULL, carrier TEXT NOT NULL, carrier_generation INTEGER NOT NULL, command_id TEXT NOT NULL UNIQUE, state TEXT NOT NULL CHECK(state IN ('prepared','sending','sent','resolved','accepted','unknown','failed')), evidence BLOB, request_id TEXT NOT NULL DEFAULT '', event_sequence INTEGER NOT NULL DEFAULT 0);
 CREATE TABLE IF NOT EXISTS dispatch_input (dispatch_id TEXT NOT NULL REFERENCES dispatch(id), seq INTEGER NOT NULL REFERENCES ingress(seq), PRIMARY KEY(dispatch_id,seq));
 CREATE TABLE IF NOT EXISTS private_checkpoint (tenant TEXT NOT NULL, app_id TEXT NOT NULL, gateway TEXT NOT NULL, surface TEXT NOT NULL, chat TEXT NOT NULL, user_id TEXT NOT NULL, generation INTEGER NOT NULL, revision INTEGER NOT NULL, cursor INTEGER NOT NULL, data BLOB NOT NULL, digest TEXT NOT NULL, PRIMARY KEY(tenant,app_id,gateway,surface,chat,user_id,generation));
 CREATE TABLE IF NOT EXISTS private_effect (id TEXT PRIMARY KEY, tenant TEXT NOT NULL, app_id TEXT NOT NULL, gateway TEXT NOT NULL, surface TEXT NOT NULL, chat TEXT NOT NULL, user_id TEXT NOT NULL, generation INTEGER NOT NULL, revision INTEGER NOT NULL, ordinal INTEGER NOT NULL, lane TEXT NOT NULL CHECK(lane IN ('interactive','background')), body BLOB NOT NULL, state TEXT NOT NULL CHECK(state IN ('prepared','sending','confirmed','unknown')), receipt BLOB, UNIQUE(tenant,app_id,gateway,surface,chat,user_id,generation,revision,ordinal));
 CREATE TABLE IF NOT EXISTS private_applied (seq INTEGER PRIMARY KEY REFERENCES ingress(seq), revision INTEGER NOT NULL, canceled_by INTEGER REFERENCES ingress(seq), route_discard INTEGER NOT NULL DEFAULT 0);
 CREATE TABLE IF NOT EXISTS private_callback_response (seq INTEGER PRIMARY KEY REFERENCES ingress(seq), revision INTEGER NOT NULL, body BLOB NOT NULL);
 CREATE TABLE IF NOT EXISTS private_effect_applied (effect_id TEXT PRIMARY KEY REFERENCES private_effect(id), revision INTEGER NOT NULL, receipt_digest TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS private_display_chain (effect_id TEXT PRIMARY KEY REFERENCES private_effect(id), chain_key TEXT NOT NULL);
 CREATE INDEX IF NOT EXISTS private_display_chain_key ON private_display_chain(chain_key);
 CREATE TABLE IF NOT EXISTS private_live_applied (incarnation TEXT NOT NULL, sequence INTEGER NOT NULL, payload_hash TEXT NOT NULL, revision INTEGER NOT NULL, PRIMARY KEY(incarnation,sequence));
 CREATE TABLE IF NOT EXISTS private_disconnect_applied (incarnation TEXT NOT NULL, connection_id TEXT NOT NULL, revision INTEGER NOT NULL, PRIMARY KEY(incarnation,connection_id));
 CREATE TABLE IF NOT EXISTS private_tick_applied (tenant TEXT NOT NULL,app_id TEXT NOT NULL,gateway TEXT NOT NULL,surface TEXT NOT NULL,chat TEXT NOT NULL,user_id TEXT NOT NULL,generation INTEGER NOT NULL,tick_id TEXT NOT NULL,observed_at TEXT NOT NULL,revision INTEGER NOT NULL,PRIMARY KEY(tenant,app_id,gateway,surface,chat,user_id,generation,tick_id));
 CREATE TABLE IF NOT EXISTS private_local_receipts (effect_id TEXT PRIMARY KEY REFERENCES private_effect(id), receipt BLOB NOT NULL);
 CREATE TABLE IF NOT EXISTS private_local_intents (effect_id TEXT PRIMARY KEY REFERENCES private_effect(id), body BLOB NOT NULL);
 CREATE TABLE IF NOT EXISTS private_settings_reconcile (version TEXT NOT NULL, tenant TEXT NOT NULL, app_id TEXT NOT NULL, gateway TEXT NOT NULL, surface TEXT NOT NULL, chat TEXT NOT NULL, user_id TEXT NOT NULL, generation INTEGER NOT NULL, state TEXT NOT NULL DEFAULT 'pending', applied_revision INTEGER NOT NULL DEFAULT 0, PRIMARY KEY(version,tenant,app_id,gateway,surface,chat,user_id,generation));
` + publicationSchema + migrationImportSchema + stagingCleanupSchema + upgradeCancellationSchema + upgradeRecoverySchema + nativeConnectionSchema + nativeCompactSchema + privateMemorySchema + privateProjectSchema + `
 CREATE TABLE IF NOT EXISTS native_carriers (incarnation TEXT PRIMARY KEY, owner_json BLOB NOT NULL, surface_json BLOB NOT NULL, generation INTEGER NOT NULL);
 CREATE TABLE IF NOT EXISTS native_commands (dispatch_id TEXT PRIMARY KEY REFERENCES dispatch(id), command_id TEXT NOT NULL UNIQUE, payload BLOB NOT NULL, payload_hash TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS native_command_receipts (command_id TEXT PRIMARY KEY REFERENCES native_commands(command_id), incarnation TEXT NOT NULL, payload BLOB NOT NULL);
 CREATE TABLE IF NOT EXISTS native_query_proofs (waiter_id TEXT PRIMARY KEY REFERENCES native_commands(command_id), source_id TEXT NOT NULL, source_hash TEXT NOT NULL, request_id TEXT NOT NULL, payload BLOB NOT NULL);
 CREATE TABLE IF NOT EXISTS native_events (incarnation TEXT NOT NULL, sequence INTEGER NOT NULL, payload_hash TEXT NOT NULL, command_id TEXT NOT NULL, payload BLOB NOT NULL, applied INTEGER NOT NULL DEFAULT 0, applied_revision INTEGER NOT NULL DEFAULT 0, PRIMARY KEY(incarnation,sequence));
 ` + appliedMigration + originMigration + ` UPDATE dispatch SET state='unknown' WHERE state='sending'; UPDATE private_effect SET state='unknown' WHERE state='sending'; ` + versionPragma); err != nil {
		_, _ = db.Exec(`ROLLBACK`)
		j.Close()
		return nil, err
	}
	return j, nil
}
func (j *Journal) Close() error {
	err := j.db.Close()
	lockErr := unlockJournal(j.lock)
	if err != nil {
		return err
	}
	return lockErr
}

func validOwner(o Owner) bool {
	return o.TenantID != "" && o.AppID != "" && o.GatewayID != "" && o.SurfaceID != "" && o.ChatID != "" && o.UserID != ""
}
func validateIngress(in Ingress) error {
	if !validOwner(in.Owner) || in.Generation < 1 || strings.TrimSpace(in.EventID) == "" || (in.Lane != "input" && in.Lane != "control" && in.Lane != "inspect") || !json.Valid(in.Payload) || ((in.OriginRouter == "") != (in.OriginSequence == 0)) || in.OriginSequence < 0 {
		return ErrJournalState
	}
	for _, s := range in.Sources {
		if strings.TrimSpace(s) == "" {
			return ErrJournalState
		}
	}
	return nil
}

// Admit returns only after SQLite has committed. Duplicate events must match
// their original lane, payload and sources byte for byte.
func (j *Journal) Admit(ctx context.Context, in Ingress) (int64, bool, error) {
	if err := validateIngress(in); err != nil {
		return 0, false, err
	}
	sources, err := json.Marshal(in.Sources)
	if err != nil {
		return 0, false, err
	}
	tx, err := j.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback()
	o := in.Owner
	res, err := tx.ExecContext(ctx, `INSERT INTO ingress(gateway,tenant,app_id,surface,chat,user_id,generation,event_id,lane,payload,sources,origin_router,origin_seq) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(tenant,app_id,event_id) DO NOTHING`, o.GatewayID, o.TenantID, o.AppID, o.SurfaceID, o.ChatID, o.UserID, in.Generation, in.EventID, in.Lane, []byte(in.Payload), sources, in.OriginRouter, in.OriginSequence)
	if err != nil {
		return 0, false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, false, err
	}
	var seq int64
	var lane string
	var savedOwner Owner
	var savedGeneration int64
	var payload, stored []byte
	var originRouter string
	var originSeq int64
	err = tx.QueryRowContext(ctx, `SELECT seq,gateway,surface,chat,user_id,generation,lane,payload,sources,origin_router,origin_seq FROM ingress WHERE tenant=? AND app_id=? AND event_id=?`, o.TenantID, o.AppID, in.EventID).Scan(&seq, &savedOwner.GatewayID, &savedOwner.SurfaceID, &savedOwner.ChatID, &savedOwner.UserID, &savedGeneration, &lane, &payload, &stored, &originRouter, &originSeq)
	if err != nil {
		return 0, false, err
	}
	if savedOwner.GatewayID != o.GatewayID || savedOwner.SurfaceID != o.SurfaceID || savedOwner.ChatID != o.ChatID || savedOwner.UserID != o.UserID || lane != in.Lane || string(payload) != string(in.Payload) || string(stored) != string(sources) || (originRouter != "" && (originRouter != in.OriginRouter || originSeq != in.OriginSequence)) {
		return 0, false, ErrJournalConflict
	}
	if n == 1 && in.Lane == "input" && in.OriginRouter != "" {
		_, err = tx.ExecContext(ctx, `INSERT INTO private_applied(seq,revision,canceled_by) SELECT ?,0,c.seq FROM ingress c JOIN private_applied a ON a.seq=c.seq AND a.route_discard=1 WHERE c.tenant=? AND c.app_id=? AND c.gateway=? AND c.surface=? AND c.chat=? AND c.user_id=? AND c.generation=? AND c.origin_router=? AND c.origin_seq>? ORDER BY c.origin_seq LIMIT 1`, seq, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, in.Generation, in.OriginRouter, in.OriginSequence)
		if err != nil {
			return 0, false, err
		}
		if err = insertCanceledStagingCleanup(ctx, tx, seq); err != nil {
			return 0, false, err
		}
	}
	if err = tx.Commit(); err != nil {
		return 0, false, err
	}
	return seq, n == 0, nil
}

// Pending lists unclaimed events in source order. Control is a separate lane,
// so a running or uncertain native input cannot prevent a stop command.
func (j *Journal) Pending(ctx context.Context, o Owner, generation int64, lane string, after int64, limit int) ([]JournalEntry, error) {
	if !validOwner(o) || generation < 1 || after < 0 || limit < 1 || limit > 1000 || (lane != "input" && lane != "control" && lane != "inspect") {
		return nil, ErrJournalState
	}
	rows, err := j.db.QueryContext(ctx, `SELECT seq,event_id,payload,sources FROM ingress WHERE gateway=? AND tenant=? AND app_id=? AND surface=? AND chat=? AND user_id=? AND generation=? AND lane=? AND seq>? AND NOT EXISTS (SELECT 1 FROM dispatch_input JOIN dispatch ON dispatch.id=dispatch_input.dispatch_id WHERE dispatch_input.seq=ingress.seq AND dispatch.state<>'failed') ORDER BY seq LIMIT ?`, o.GatewayID, o.TenantID, o.AppID, o.SurfaceID, o.ChatID, o.UserID, generation, lane, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []JournalEntry
	for rows.Next() {
		var e JournalEntry
		var sources []byte
		e.Ingress.Owner = o
		e.Ingress.Generation = generation
		e.Ingress.Lane = lane
		if err = rows.Scan(&e.Sequence, &e.Ingress.EventID, &e.Ingress.Payload, &sources); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(sources, &e.Ingress.Sources); err != nil {
			return nil, fmt.Errorf("decode sources: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
