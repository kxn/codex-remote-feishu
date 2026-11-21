package workstore

import (
	"encoding/json"
	"fmt"
	"strings"
)

func UnverifiedHostCWDKey(path string) string { return "cwd-unverified-host:" + path }

// This is only a possible local interpretation, never host attribution.
func CanonicalPotentialLocalCWD(path string) string {
	value := NormalizeRuntimeResourceKey("cwd:possible:" + path)
	return strings.TrimPrefix(value, "cwd:possible:")
}

func (t *Tx) ListMigrationResourceFences(id, recordKey string, limit int) ([]MigrationResourceFence, error) {
	if limit <= 0 || limit > 256 {
		limit = 128
	}
	query := `SELECT resource_key,migration_id,record_key,reason,evidence FROM work_migration_resource_fences WHERE migration_id=? AND released=0`
	args := []any{id}
	if recordKey != "" {
		query += ` AND record_key=?`
		args = append(args, recordKey)
	}
	query += ` ORDER BY record_key,resource_key LIMIT ?`
	args = append(args, limit)
	rows, err := t.tx.QueryContext(t.ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []MigrationResourceFence
	for rows.Next() {
		var v MigrationResourceFence
		var raw string
		if err = rows.Scan(&v.ResourceKey, &v.MigrationID, &v.RecordKey, &v.Reason, &raw); err != nil {
			return nil, err
		}
		v.Evidence = []byte(raw)
		result = append(result, v)
	}
	return result, rows.Err()
}
func (t *Tx) NarrowMigrationHostFences(id, key, hostID string, evidence json.RawMessage) error {
	if hostID == "" || !migrationEvidenceObject(evidence) {
		return fmt.Errorf("host narrowing requires a trusted host attestation")
	}
	resolution, _ := json.Marshal(map[string]any{"kind": "actual_host_attributed", "host_id": hostID, "evidence": json.RawMessage(evidence)})
	_, err := t.tx.ExecContext(t.ctx, `UPDATE work_migration_resource_fences SET released=1,resolution=? WHERE migration_id=? AND record_key=? AND resource_key LIKE 'cwd-unverified-host:%' AND released=0`, string(resolution), id, key)
	return err
}
