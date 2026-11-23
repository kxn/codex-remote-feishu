package privatechat

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

const privateMemorySchema = `
CREATE TABLE IF NOT EXISTS private_memory (owner_key TEXT NOT NULL, id TEXT NOT NULL, revision INTEGER NOT NULL, data BLOB NOT NULL, PRIMARY KEY(owner_key,id));
CREATE TABLE IF NOT EXISTS private_memory_versions (owner_key TEXT NOT NULL, id TEXT NOT NULL, revision INTEGER NOT NULL, data BLOB NOT NULL, PRIMARY KEY(owner_key,id,revision));
CREATE TABLE IF NOT EXISTS private_memory_mutations (owner_key TEXT NOT NULL, operation_id TEXT NOT NULL, request BLOB NOT NULL, result BLOB NOT NULL, PRIMARY KEY(owner_key,operation_id));
`

// Memory persists across native session, backend and working-directory changes.
// Its owner is the authenticated private conversation, never a runtime cwd.
type Memory struct {
	ID            string          `json:"id"`
	Revision      int64           `json:"revision"`
	State         string          `json:"state"`
	Applicability string          `json:"applicability"`
	Content       string          `json:"content,omitempty"`
	Sources       json.RawMessage `json:"sources"`
	UpdatedAt     string          `json:"updated_at"`
}

type MemoryMutation struct {
	OperationID      string `json:"operation_id"`
	ID               string `json:"id"`
	ExpectedRevision int64  `json:"expected_revision"`
	Revoke           bool   `json:"revoke,omitempty"`
	Applicability    string `json:"applicability,omitempty"`
	Content          string `json:"content,omitempty"`
}

func memoryOwnerKey(o Owner, generation int64) string {
	b, _ := json.Marshal(struct {
		Owner      Owner
		Generation int64
	}{o, generation})
	return string(b)
}

type memoryReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func readCurrentMemory(ctx context.Context, db memoryReader, o Owner, generation int64, id string) (Memory, error) {
	var m Memory
	var raw []byte
	err := db.QueryRowContext(ctx, `SELECT data FROM private_memory WHERE owner_key=? AND id=?`, memoryOwnerKey(o, generation), id).Scan(&raw)
	if err == nil {
		if json.Unmarshal(raw, &m) != nil || m.ID != id || m.Revision < 1 {
			return Memory{}, ErrJournalConflict
		}
		return m, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return m, err
	}
	// Imports contain only personal entries from the exact sealed source.
	// Shared team/role entries are rejected by the importer, not copied here.
	var payload, payloadHash, contentHash string
	var body []byte
	err = db.QueryRowContext(ctx, `SELECT m.payload,m.payload_hash,c.data,c.data_hash FROM private_migration_records m JOIN private_migration_imports i ON i.import_id=m.import_id JOIN private_migration_records c ON c.import_id=m.import_id AND c.source_table='content' AND c.source_key=json_extract(m.payload,'$.content_ref') WHERE i.tenant=? AND i.app_id=? AND i.gateway=? AND i.surface=? AND i.chat=? AND i.user_id=? AND i.generation=? AND m.source_table='memories' AND m.source_key=?`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation, id).Scan(&payload, &payloadHash, &body, &contentHash)
	if err != nil {
		return m, err
	}
	if migrationSHA([]byte(payload)) != payloadHash || migrationSHA(body) != contentHash || json.Unmarshal([]byte(payload), &m) != nil || m.ID != id || m.Revision < 1 {
		return Memory{}, ErrJournalConflict
	}
	m.Content = string(body)
	return m, nil
}

func (j *Journal) ReadMemory(ctx context.Context, o Owner, generation int64, id string, revision int64) (Memory, error) {
	if !validOwner(o) || generation < 1 || id == "" || revision < 1 {
		return Memory{}, ErrJournalState
	}
	m, err := readCurrentMemory(ctx, j.db, o, generation, id)
	if err != nil {
		return Memory{}, err
	}
	if m.Revision != revision || m.State != "valid" {
		return Memory{}, ErrJournalConflict
	}
	return m, nil
}

// ListMemoryIndex returns metadata only. A local replacement or tombstone
// shadows the immutable import, including after restart.
func (j *Journal) ListMemoryIndex(ctx context.Context, o Owner, generation int64, after string, limit int) ([]Memory, string, error) {
	if !validOwner(o) || generation < 1 || limit < 1 || limit > 100 {
		return nil, "", ErrJournalState
	}
	rows, err := j.db.QueryContext(ctx, `SELECT id FROM (SELECT id FROM private_memory WHERE owner_key=? AND json_extract(data,'$.state')='valid' UNION SELECT m.source_key AS id FROM private_migration_records m JOIN private_migration_imports i ON i.import_id=m.import_id WHERE i.tenant=? AND i.app_id=? AND i.gateway=? AND i.surface=? AND i.chat=? AND i.user_id=? AND i.generation=? AND m.source_table='memories' AND json_extract(m.payload,'$.state')='valid' AND NOT EXISTS(SELECT 1 FROM private_memory p WHERE p.owner_key=? AND p.id=m.source_key)) WHERE id>? ORDER BY id LIMIT ?`, memoryOwnerKey(o, generation), o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation, memoryOwnerKey(o, generation), after, limit)
	if err != nil {
		return nil, "", err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, "", err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, "", err
	}
	result := make([]Memory, 0, len(ids))
	for _, id := range ids {
		m, e := readCurrentMemory(ctx, j.db, o, generation, id)
		if e != nil {
			return nil, "", e
		}
		if m.State != "valid" {
			continue
		}
		m.Content = ""
		result = append(result, m)
	}
	next := ""
	if len(ids) == limit {
		next = ids[len(ids)-1]
	}
	return result, next, nil
}

// MutateMemory records an exact CAS and idempotency receipt together. Provenance
// is supplied by the authenticated host, never accepted from agent arguments.
func (j *Journal) MutateMemory(ctx context.Context, o Owner, generation int64, req MemoryMutation, provenance json.RawMessage) (Memory, error) {
	if !validOwner(o) || generation < 1 || strings.TrimSpace(req.OperationID) == "" || len(req.OperationID) > 256 || strings.TrimSpace(req.ID) == "" || len(req.ID) > 256 || req.ExpectedRevision < 0 || req.ExpectedRevision == 1<<63-1 || !json.Valid(provenance) {
		return Memory{}, ErrJournalState
	}
	if !req.Revoke && (strings.TrimSpace(req.Content) == "" || len(req.Content) > 64<<10 || !utf8.ValidString(req.Content) || strings.TrimSpace(req.Applicability) == "" || len(req.Applicability) > 1024) {
		return Memory{}, ErrJournalState
	}
	if req.Revoke && (req.ExpectedRevision == 0 || req.Content != "" || req.Applicability != "") {
		return Memory{}, ErrJournalState
	}
	raw, _ := json.Marshal(req)
	key := memoryOwnerKey(o, generation)
	tx, err := j.db.BeginTx(ctx, nil)
	if err != nil {
		return Memory{}, err
	}
	defer tx.Rollback()
	var prior, receipt []byte
	err = tx.QueryRowContext(ctx, `SELECT request,result FROM private_memory_mutations WHERE owner_key=? AND operation_id=?`, key, req.OperationID).Scan(&prior, &receipt)
	if err == nil {
		var saved Memory
		if string(prior) != string(raw) || json.Unmarshal(receipt, &saved) != nil {
			return Memory{}, ErrJournalConflict
		}
		return saved, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Memory{}, err
	}
	current, err := readCurrentMemory(ctx, tx, o, generation, req.ID)
	if errors.Is(err, sql.ErrNoRows) {
		if req.ExpectedRevision != 0 {
			return Memory{}, ErrJournalConflict
		}
	} else if err != nil {
		return Memory{}, err
	} else if current.Revision != req.ExpectedRevision {
		return Memory{}, ErrJournalConflict
	}
	// Every write requires a committed private owner. A fabricated owner cannot
	// create memory records even if an internal caller forgets admission.
	var exists int
	if err = tx.QueryRowContext(ctx, `SELECT 1 FROM private_checkpoint WHERE tenant=? AND app_id=? AND gateway=? AND surface=? AND chat=? AND user_id=? AND generation=?`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation).Scan(&exists); err != nil {
		return Memory{}, err
	}
	m := Memory{ID: req.ID, Revision: req.ExpectedRevision + 1, State: "valid", Applicability: req.Applicability, Content: req.Content, Sources: append(json.RawMessage(nil), provenance...), UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if req.Revoke {
		m.State = "revoked"
		m.Applicability = current.Applicability
	}
	data, err := json.Marshal(m)
	if err != nil {
		return Memory{}, err
	}
	// Preserve the original imported version before the first local change.
	if current.Revision > 0 {
		old, _ := json.Marshal(current)
		if _, err = tx.ExecContext(ctx, `INSERT INTO private_memory_versions(owner_key,id,revision,data) VALUES(?,?,?,?) ON CONFLICT DO NOTHING`, key, current.ID, current.Revision, old); err != nil {
			return Memory{}, err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO private_memory_versions(owner_key,id,revision,data) VALUES(?,?,?,?)`, key, m.ID, m.Revision, data); err != nil {
		return Memory{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO private_memory(owner_key,id,revision,data) VALUES(?,?,?,?) ON CONFLICT(owner_key,id) DO UPDATE SET revision=excluded.revision,data=excluded.data`, key, m.ID, m.Revision, data); err != nil {
		return Memory{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO private_memory_mutations(owner_key,operation_id,request,result) VALUES(?,?,?,?)`, key, req.OperationID, raw, data); err != nil {
		return Memory{}, err
	}
	if err = tx.Commit(); err != nil {
		return Memory{}, err
	}
	return m, nil
}
