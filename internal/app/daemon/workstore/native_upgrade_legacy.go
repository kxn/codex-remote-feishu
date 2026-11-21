package workstore

import (
	"encoding/json"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (t *Tx) AssertNativeUpgradeLegacySettled(host string) error {
	var count int
	err := t.tx.QueryRowContext(t.ctx, `SELECT count(*) FROM work_migration_resource_fences WHERE released=0 AND coalesce(json_extract(evidence,'$.native.backend'),'') IN ('','codex') AND coalesce(json_extract(evidence,'$.native.host_id'),'') IN ('',?)`, host).Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("legacy native execution may share this Codex installation; actual host and stop evidence required")
	}
	return nil
}

// Discovery during maintenance cannot silently restore a completed-pool claim.
func (t *Tx) ObserveNativeUpgradeLegacyConflict(backend work.Backend, host string) error {
	if backend != "" && backend != work.BackendCodex {
		return nil
	}
	query := `SELECT payload FROM native_upgrades WHERE json_extract(payload,'$.stage') NOT IN ('completed','draining','unknown')`
	var args []any
	if host != "" {
		query += " AND host_id=?"
		args = append(args, host)
	}
	rows, err := t.tx.QueryContext(t.ctx, query, args...)
	if err != nil {
		return err
	}
	var values []NativeUpgrade
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return err
		}
		var v NativeUpgrade
		if err = json.Unmarshal([]byte(raw), &v); err != nil {
			rows.Close()
			return err
		}
		values = append(values, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, v := range values {
		before := v.Revision
		v.ResumeStage = v.Stage
		v.Stage = "unknown"
		v.LastError = "legacy native reconnected during upgrade; installation sharing and stop evidence require reconciliation"
		v.Revision++
		if err = t.PutNativeUpgrade(v, before); err != nil {
			return err
		}
	}
	return nil
}
