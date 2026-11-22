package workstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// The connection contract includes actual authentication/endpoint/capabilities,
// while thread model, reasoning and context preferences do not change it.
func (t *Tx) RuntimeCatalogConnection(key RuntimeModelCatalogKey) (string, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT c.data FROM operations o JOIN content c ON c.id=json_extract(o.payload,'$.request_ref') WHERE o.kind='worker.launch' AND json_extract(o.payload,'$.target_id')=? AND json_extract(o.payload,'$.target_generation')=? LIMIT 2`, key.WorkerID, key.SessionGeneration)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return "", err
		}
		var record struct {
			Snapshot struct {
				CarrierIncarnation string `json:"carrierIncarnation"`
			} `json:"snapshot"`
			Policy struct {
				ID string `json:"connectionContractId"`
			} `json:"codex_resume"`
		}
		if err := json.Unmarshal(raw, &record); err != nil {
			return "", err
		}
		if record.Snapshot.CarrierIncarnation == key.CarrierIncarnation {
			return record.Policy.ID, nil
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return "", ErrNotFound
}

// A page may be reused only for the same Bot, profile and frozen connection.
// Source command/launch provenance remains intact instead of relabelling it as
// a response from a newer worker. Account/profile changes cannot hit this key.
func (t *Tx) RuntimeCatalogForConnection(binding work.RoleBotBinding, profile, connection, cursor string, now time.Time) (RuntimeModelCatalog, error) {
	var value RuntimeModelCatalog
	if connection == "" {
		return value, ErrNotFound
	}
	var raw string
	err := t.tx.QueryRowContext(t.ctx, `SELECT m.payload FROM runtime_model_catalogs m JOIN carrier_registrations r ON r.incarnation=m.incarnation JOIN operations o ON o.kind='worker.launch' AND json_extract(o.payload,'$.target_id')=json_extract(m.payload,'$.key.worker_id') AND json_extract(o.payload,'$.target_generation')=json_extract(m.payload,'$.key.session_generation') JOIN content c ON c.id=json_extract(o.payload,'$.request_ref') WHERE json_extract(CAST(c.data AS TEXT),'$.snapshot.carrierIncarnation')=m.incarnation AND json_extract(r.payload,'$.owner.roleId')=? AND json_extract(r.payload,'$.owner.botGeneration')=? AND json_extract(m.payload,'$.key.bot_generation')=? AND json_extract(m.payload,'$.key.profile_id')=? AND json_extract(m.payload,'$.key.backend')='codex' AND json_extract(CAST(c.data AS TEXT),'$.codex_resume.connectionContractId')=? AND coalesce(json_extract(m.payload,'$.query.cursor'),'')=? AND coalesce(json_extract(m.payload,'$.snapshot.errorMessage'),'')='' AND coalesce(json_extract(m.payload,'$.snapshot.unsupported'),0)=0 AND julianday(json_extract(m.payload,'$.snapshot.refreshedAt'))>=julianday(?) ORDER BY julianday(json_extract(m.payload,'$.snapshot.refreshedAt')) DESC,m.rowid DESC LIMIT 1`, binding.RoleID, binding.IdentityGeneration, binding.IdentityGeneration, profile, connection, cursor, now.Add(-5*time.Minute).UTC().Format(time.RFC3339Nano)).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	if err != nil {
		return value, err
	}
	if err = json.Unmarshal([]byte(raw), &value); err != nil {
		return value, err
	}
	if value.Snapshot.RefreshedAt.IsZero() || value.Snapshot.RefreshedAt.Before(now.Add(-5*time.Minute)) || value.Snapshot.Unsupported || value.Snapshot.ErrorMessage != "" {
		return value, ErrNotFound
	}
	actual, err := t.GetRoleBotBinding(binding.RoleID)
	if err != nil {
		return value, err
	}
	if actual != binding {
		return value, ErrScopeDenied
	}
	return value, nil
}
