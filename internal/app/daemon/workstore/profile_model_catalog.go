package workstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

const SchemaProfileModelCatalogV1 = `CREATE TABLE IF NOT EXISTS profile_model_catalogs(namespace_key TEXT PRIMARY KEY,revision INTEGER NOT NULL CHECK(revision>0),payload TEXT NOT NULL CHECK(json_valid(payload)));`

// Key names the caller's semantic connection/profile namespace and page. A
// business worker, native turn or expiring cache lease is not its owner.
type ProfileModelCatalog struct {
	Key           string                          `json:"key"`
	Snapshot      agentproto.ModelCatalogSnapshot `json:"snapshot"`
	EvidenceID    string                          `json:"evidence_id,omitempty"`
	RefreshedAt   time.Time                       `json:"refreshed_at,omitempty"`
	LastAttemptID string                          `json:"last_attempt_id,omitempty"`
	AttemptedAt   time.Time                       `json:"attempted_at,omitempty"`
	LastError     string                          `json:"last_error,omitempty"`
	Refreshing    bool                            `json:"refreshing"`
	Revision      int64                           `json:"revision"`
}

func (t *Tx) GetProfileModelCatalog(key string) (ProfileModelCatalog, error) {
	var value ProfileModelCatalog
	if strings.TrimSpace(key) == "" {
		return value, fmt.Errorf("profile model catalog namespace is required")
	}
	var raw string
	err := t.tx.QueryRowContext(t.ctx, "SELECT payload FROM profile_model_catalogs WHERE namespace_key=?", key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return value, ErrNotFound
	}
	if err != nil {
		return value, err
	}
	err = json.Unmarshal([]byte(raw), &value)
	return value, err
}

// Starting a refresh preserves the last successful page indefinitely. After a
// daemon restart the caller may register a new read-only attempt explicitly.
func (t *Tx) BeginProfileModelCatalogRefresh(key, attemptID string, now time.Time) (ProfileModelCatalog, error) {
	var value ProfileModelCatalog
	if strings.TrimSpace(key) == "" || strings.TrimSpace(attemptID) == "" || now.IsZero() {
		return value, fmt.Errorf("catalog refresh requires namespace, attempt and time")
	}
	value, err := t.GetProfileModelCatalog(key)
	if errors.Is(err, ErrNotFound) {
		value = ProfileModelCatalog{Key: key}
	} else if err != nil {
		return value, err
	}
	if value.LastAttemptID == attemptID {
		return value, nil
	}
	previous := value.Revision
	value.LastAttemptID = attemptID
	value.AttemptedAt = now.UTC()
	value.LastError = ""
	value.Refreshing = true
	value.Revision++
	return value, t.putProfileModelCatalog(value, previous)
}

// Only the registered current attempt can finish. A failure updates refresh
// metadata without replacing the previous successful native page or evidence.
func (t *Tx) CompleteProfileModelCatalogRefresh(key, attemptID string, snapshot *agentproto.ModelCatalogSnapshot, evidence, errorText string, now time.Time) (ProfileModelCatalog, error) {
	var value ProfileModelCatalog
	if strings.TrimSpace(key) == "" || strings.TrimSpace(attemptID) == "" || now.IsZero() {
		return value, fmt.Errorf("catalog completion requires namespace, attempt and time")
	}
	value, err := t.GetProfileModelCatalog(key)
	if err != nil {
		return value, err
	}
	if value.LastAttemptID != attemptID {
		return value, ErrConflict
	}
	if !value.Refreshing {
		return value, nil
	}
	if errorText == "" && snapshot != nil {
		errorText = snapshot.ErrorMessage
		if snapshot.Unsupported && errorText == "" {
			errorText = "native model catalog is unsupported"
		}
	}
	if errorText == "" && (snapshot == nil || strings.TrimSpace(evidence) == "") {
		return value, fmt.Errorf("successful catalog completion requires native snapshot and evidence")
	}
	previous := value.Revision
	if errorText == "" {
		value.Snapshot = *agentproto.CloneModelCatalogSnapshot(snapshot)
		if value.Snapshot.RefreshedAt.IsZero() {
			value.Snapshot.RefreshedAt = now.UTC()
		}
		value.EvidenceID = evidence
		value.RefreshedAt = value.Snapshot.RefreshedAt
	}
	value.LastError = errorText
	value.Refreshing = false
	value.Revision++
	return value, t.putProfileModelCatalog(value, previous)
}

func (t *Tx) putProfileModelCatalog(value ProfileModelCatalog, previous int64) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	var result sql.Result
	if previous == 0 {
		result, err = t.tx.ExecContext(t.ctx, "INSERT INTO profile_model_catalogs(namespace_key,revision,payload) VALUES(?,?,?)", value.Key, value.Revision, string(raw))
	} else {
		result, err = t.tx.ExecContext(t.ctx, "UPDATE profile_model_catalogs SET revision=?,payload=? WHERE namespace_key=? AND revision=?", value.Revision, string(raw), value.Key, previous)
	}
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrConflict
	}
	return nil
}
