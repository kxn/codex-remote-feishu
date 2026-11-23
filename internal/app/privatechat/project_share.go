package privatechat

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

const privateProjectSchema = `CREATE TABLE IF NOT EXISTS private_project_share_drafts(id TEXT PRIMARY KEY,tenant TEXT NOT NULL,app_id TEXT NOT NULL,gateway TEXT NOT NULL,surface TEXT NOT NULL,chat TEXT NOT NULL,user_id TEXT NOT NULL,generation INTEGER NOT NULL,project_id TEXT NOT NULL,payload BLOB NOT NULL,payload_hash TEXT NOT NULL,created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS private_project_share_receipts(draft_id TEXT PRIMARY KEY REFERENCES private_project_share_drafts(id),receipt BLOB NOT NULL,receipt_hash TEXT NOT NULL);`

type ProjectShareDraft struct {
	ID, ProjectID, Text, ResourceID, VersionID, MemoryID string
	MemoryRevision                                       int64
	CreatedAt                                            time.Time
}

// PrepareProjectShare records a proposed exact payload only. It grants no
// authority to the caller; an authenticated person must confirm its card.
func (j *Journal) PrepareProjectShare(ctx context.Context, o Owner, generation int64, operationID string, d ProjectShareDraft) (ProjectShareDraft, error) {
	var zero ProjectShareDraft
	if !validOwner(o) || generation < 1 || strings.TrimSpace(operationID) == "" || len(operationID) > 200 || d.ProjectID == "" || !utf8.ValidString(d.Text) || len(d.Text) > 3000 || (strings.TrimSpace(d.Text) == "" && (d.ResourceID == "" || d.VersionID == "")) {
		return zero, ErrJournalState
	}
	if (d.ResourceID == "") != (d.VersionID == "") || (d.MemoryID == "") != (d.MemoryRevision == 0) {
		return zero, ErrJournalState
	}
	identity, _ := json.Marshal([]any{o, generation, operationID})
	idHash := sha256.Sum256(identity)
	d.ID = "private-project-share:" + hex.EncodeToString(idHash[:])
	d.CreatedAt = time.Time{}
	raw, err := json.Marshal(d)
	if err != nil {
		return zero, err
	}
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	now := time.Now().UTC()
	_, err = j.db.ExecContext(ctx, `INSERT INTO private_project_share_drafts(id,tenant,app_id,gateway,surface,chat,user_id,generation,project_id,payload,payload_hash,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING`, d.ID, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation, d.ProjectID, raw, digest, now.Format(time.RFC3339Nano))
	if err != nil {
		return zero, err
	}
	old, err := j.ProjectShareDraft(ctx, o, generation, d.ID)
	if err != nil {
		return zero, err
	}
	old.CreatedAt = time.Time{}
	oldRaw, _ := json.Marshal(old)
	if string(oldRaw) != string(raw) {
		return zero, ErrJournalConflict
	}
	return old, nil
}
func (j *Journal) ProjectShareDraft(ctx context.Context, o Owner, generation int64, id string) (ProjectShareDraft, error) {
	var d ProjectShareDraft
	if !validOwner(o) || generation < 1 || id == "" {
		return d, ErrJournalState
	}
	var raw []byte
	var hash string
	err := j.db.QueryRowContext(ctx, `SELECT payload,payload_hash FROM private_project_share_drafts WHERE id=? AND tenant=? AND app_id=? AND gateway=? AND surface=? AND chat=? AND user_id=? AND generation=?`, id, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation).Scan(&raw, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return d, ErrJournalState
	}
	if err != nil {
		return d, err
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != hash || json.Unmarshal(raw, &d) != nil || d.ID != id {
		return ProjectShareDraft{}, ErrJournalConflict
	}
	return d, nil
}
func (j *Journal) ListProjectShareDrafts(ctx context.Context, o Owner, generation int64, after string, limit int) ([]ProjectShareDraft, string, error) {
	if !validOwner(o) || generation < 1 || limit < 1 || limit > 100 {
		return nil, "", ErrJournalState
	}
	rows, err := j.db.QueryContext(ctx, `SELECT id,payload,payload_hash FROM private_project_share_drafts WHERE tenant=? AND app_id=? AND gateway=? AND surface=? AND chat=? AND user_id=? AND generation=? AND id>? AND NOT EXISTS (SELECT 1 FROM private_project_share_receipts r WHERE r.draft_id=private_project_share_drafts.id) ORDER BY id LIMIT ?`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation, after, limit+1)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	var out []ProjectShareDraft
	next := ""
	for rows.Next() {
		var id, hash string
		var raw []byte
		if err = rows.Scan(&id, &raw, &hash); err != nil {
			return nil, "", err
		}
		if len(out) == limit {
			next = out[len(out)-1].ID
			break
		}
		sum := sha256.Sum256(raw)
		var d ProjectShareDraft
		if hex.EncodeToString(sum[:]) != hash || json.Unmarshal(raw, &d) != nil || d.ID != id {
			return nil, "", ErrJournalConflict
		}
		out = append(out, d)
	}
	return out, next, rows.Err()
}

func ProjectShareDraftDigest(d ProjectShareDraft) (string, error) {
	d.CreatedAt = time.Time{}
	raw, err := json.Marshal(d)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// IngressForEffect returns the original authenticated private input which
// committed this effect, scoped to the exact owner and epoch.
func (j *Journal) IngressForEffect(ctx context.Context, o Owner, generation int64, effectID string) (JournalEntry, error) {
	var entry JournalEntry
	var sources []byte
	if !validOwner(o) || generation < 1 || effectID == "" {
		return entry, ErrJournalState
	}
	err := j.db.QueryRowContext(ctx, `SELECT i.seq,i.event_id,i.lane,i.payload,i.sources FROM private_effect e JOIN private_applied a ON a.revision=e.revision JOIN ingress i ON i.seq=a.seq WHERE e.id=? AND e.tenant=? AND e.app_id=? AND e.gateway=? AND e.surface=? AND e.chat=? AND e.user_id=? AND e.generation=? AND i.tenant=e.tenant AND i.app_id=e.app_id AND i.gateway=e.gateway AND i.surface=e.surface AND i.chat=e.chat AND i.user_id=e.user_id AND i.generation=e.generation`, effectID, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation).Scan(&entry.Sequence, &entry.Ingress.EventID, &entry.Ingress.Lane, &entry.Ingress.Payload, &sources)
	if err != nil {
		return entry, err
	}
	entry.Ingress.Owner = o
	entry.Ingress.Generation = generation
	if json.Unmarshal(sources, &entry.Ingress.Sources) != nil {
		return JournalEntry{}, ErrJournalConflict
	}
	return entry, nil
}

func (j *Journal) RecordProjectShareReceipt(ctx context.Context, o Owner, generation int64, id string, receipt []byte) error {
	if len(receipt) == 0 || !json.Valid(receipt) {
		return ErrJournalState
	}
	if _, err := j.ProjectShareDraft(ctx, o, generation, id); err != nil {
		return err
	}
	sum := sha256.Sum256(receipt)
	hash := hex.EncodeToString(sum[:])
	_, err := j.db.ExecContext(ctx, `INSERT INTO private_project_share_receipts(draft_id,receipt,receipt_hash) VALUES(?,?,?) ON CONFLICT(draft_id) DO NOTHING`, id, receipt, hash)
	if err != nil {
		return err
	}
	var old string
	err = j.db.QueryRowContext(ctx, `SELECT receipt_hash FROM private_project_share_receipts WHERE draft_id=?`, id).Scan(&old)
	if err != nil {
		return err
	}
	if old != hash {
		return ErrJournalConflict
	}
	return nil
}
