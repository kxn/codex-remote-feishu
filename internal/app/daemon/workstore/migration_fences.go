package workstore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type MigrationResourceFence struct {
	ResourceKey, MigrationID, RecordKey, Reason string
	Evidence                                    json.RawMessage
}

func NormalizeRuntimeResourceKey(key string) string {
	if !strings.HasPrefix(key, "cwd:") {
		return key
	}
	host, path, ok := strings.Cut(strings.TrimPrefix(key, "cwd:"), ":")
	if !ok || !filepath.IsAbs(path) {
		return key
	}
	path = filepath.Clean(path)
	probe := path
	var suffix []string
	for {
		if real, err := filepath.EvalSymlinks(probe); err == nil {
			path = real
			for i := len(suffix) - 1; i >= 0; i-- {
				path = filepath.Join(path, suffix[i])
			}
			break
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			break
		}
		suffix = append(suffix, filepath.Base(probe))
		probe = parent
	}
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	return "cwd:" + host + ":" + path
}
func (t *Tx) PutMigrationResourceFence(v MigrationResourceFence) error {
	if v.ResourceKey == "" || v.MigrationID == "" || v.RecordKey == "" || v.Reason == "" || !json.Valid(v.Evidence) {
		return fmt.Errorf("migration resource fence requires exact source and evidence")
	}
	v.ResourceKey = NormalizeRuntimeResourceKey(v.ResourceKey)
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO work_migration_resource_fences(resource_key,migration_id,record_key,reason,evidence) VALUES(?,?,?,?,?) ON CONFLICT(resource_key,migration_id,record_key) DO NOTHING`, v.ResourceKey, v.MigrationID, v.RecordKey, v.Reason, string(v.Evidence))
	return err
}
func (t *Tx) AssertMigrationResourceAvailable(key string) error {
	var count int
	unknown, unknownCanonical := "", ""
	if strings.HasPrefix(key, "cwd:") {
		_, path, ok := strings.Cut(strings.TrimPrefix(key, "cwd:"), ":")
		if ok {
			unknown = UnverifiedHostCWDKey(path)
			unknownCanonical = UnverifiedHostCWDKey(CanonicalPotentialLocalCWD(path))
		}
	}
	err := t.tx.QueryRowContext(t.ctx, `SELECT count(*) FROM work_migration_resource_fences WHERE resource_key IN (?,?,?,?) AND released=0`, key, NormalizeRuntimeResourceKey(key), unknown, unknownCanonical).Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("resource has unresolved legacy native work; exact recovery proof required")
	}
	return nil
}
func (t *Tx) ResolveMigrationResourceFence(migrationID, recordKey string, resolution json.RawMessage) error {
	// This is a trusted host recovery port, not an Agent or UI assertion. The
	// caller must persist real native, continuation and background proof first.
	var proof struct {
		Kind                             string `json:"kind"`
		Native, Continuation, Background json.RawMessage
	}
	if json.Unmarshal(resolution, &proof) != nil || proof.Kind != "host_verified_legacy_quiescence" || !migrationEvidenceObject(proof.Native) || !migrationEvidenceObject(proof.Continuation) || !migrationEvidenceObject(proof.Background) || string(proof.Native) == "null" || string(proof.Continuation) == "null" || string(proof.Background) == "null" {
		return fmt.Errorf("legacy resource release requires all trusted evidence domains")
	}
	_, err := t.tx.ExecContext(t.ctx, `UPDATE work_migration_resource_fences SET released=1,resolution=? WHERE migration_id=? AND record_key=? AND released=0`, string(resolution), migrationID, recordKey)
	return err
}
func (t *Tx) ObserveLegacyRuntime(instanceID, kind string, payload json.RawMessage, at time.Time) error {
	if instanceID == "" || kind == "" || !json.Valid(payload) || at.IsZero() {
		return fmt.Errorf("legacy observation requires exact instance and payload")
	}
	sum := sha256.Sum256(payload)
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO work_legacy_observations(instance_id,kind,payload_hash,payload,observed_at) VALUES(?,?,?,?,?) ON CONFLICT(instance_id,kind,payload_hash) DO NOTHING`, instanceID, kind, hex.EncodeToString(sum[:]), string(payload), at.UTC().Format(time.RFC3339Nano))
	return err
}

type LegacyRuntimeObservation struct {
	ID                            int64
	InstanceID, Kind, PayloadHash string
	Payload                       json.RawMessage
	ObservedAt                    string
}

func (t *Tx) ListLegacyRuntimeObservations(instanceID string, after int64, limit int) ([]LegacyRuntimeObservation, error) {
	if limit <= 0 || limit > 256 {
		limit = 128
	}
	rows, err := t.tx.QueryContext(t.ctx, `SELECT id,instance_id,kind,payload_hash,payload,observed_at FROM work_legacy_observations WHERE instance_id=? AND id>? ORDER BY id LIMIT ?`, instanceID, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []LegacyRuntimeObservation
	for rows.Next() {
		var v LegacyRuntimeObservation
		var raw string
		if err = rows.Scan(&v.ID, &v.InstanceID, &v.Kind, &v.PayloadHash, &raw, &v.ObservedAt); err != nil {
			return nil, err
		}
		v.Payload = []byte(raw)
		result = append(result, v)
	}
	return result, rows.Err()
}

func migrationEvidenceObject(raw json.RawMessage) bool {
	var value map[string]json.RawMessage
	return json.Unmarshal(raw, &value) == nil && len(value) > 0
}

func MigrationNativeResourceKey(backend, namespace, sessionID string) string {
	data, _ := json.Marshal([]string{backend, namespace, sessionID})
	sum := sha256.Sum256(data)
	return "native:legacy_" + hex.EncodeToString(sum[:20])
}
