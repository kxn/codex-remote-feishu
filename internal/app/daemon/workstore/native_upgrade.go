package workstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	codexupgrade "github.com/kxn/codex-remote-feishu/internal/app/codexupgrade/contract"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"time"
)

type NativeUpgradeInstall struct {
	RecoveredVerified bool                               `json:"recovered_verified,omitempty"`
	RecoveryEvidence  string                             `json:"recovery_evidence,omitempty"`
	Started           bool                               `json:"started"`
	Scope             *hostinventory.Snapshot            `json:"scope,omitempty"`
	Result            *codexupgrade.TrackedInstallResult `json:"result,omitempty"`
}
type NativeUpgrade struct {
	HostBootIdentity        string                    `json:"host_boot_identity"`
	ResumeStage             string                    `json:"resume_stage,omitempty"`
	ID                      string                    `json:"id"`
	HostID                  string                    `json:"host_id"`
	TargetVersion           string                    `json:"target_version"`
	InstallationFingerprint string                    `json:"installation_fingerprint"`
	PrincipalID             string                    `json:"principal_id"`
	Source                  PrivateActionSource       `json:"source"`
	Installation            codexupgrade.Installation `json:"installation"`
	Stage                   string                    `json:"stage"`
	FrozenWorkers           []work.WorkerSession      `json:"workers"`
	Global                  NativeUpgradeInstall      `json:"global"`
	Private                 NativeUpgradeInstall      `json:"private"`
	ManagedVersion          string                    `json:"managed_version,omitempty"`
	ManagedBinary           string                    `json:"managed_binary,omitempty"`
	LastError               string                    `json:"last_error,omitempty"`
	Revision                int64                     `json:"revision"`
	CreatedAt               time.Time                 `json:"created_at"`
	UpdatedAt               time.Time                 `json:"updated_at"`
}

func (t *Tx) GetNativeUpgrade(id string) (NativeUpgrade, error) {
	var v NativeUpgrade
	var raw string
	err := t.tx.QueryRowContext(t.ctx, "SELECT payload FROM native_upgrades WHERE id=?", id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	if err == nil {
		err = json.Unmarshal([]byte(raw), &v)
	}
	return v, err
}
func (t *Tx) ActiveNativeUpgrade(host string) (NativeUpgrade, error) {
	var v NativeUpgrade
	var raw string
	err := t.tx.QueryRowContext(t.ctx, "SELECT payload FROM native_upgrades WHERE host_id=? AND json_extract(payload,'$.stage')<>'completed'", host).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	if err == nil {
		err = json.Unmarshal([]byte(raw), &v)
	}
	return v, err
}
func (t *Tx) PutNativeUpgrade(v NativeUpgrade, expected int64) error {
	if v.ID == "" || v.HostID == "" || v.Stage == "" || v.TargetVersion == "" || v.InstallationFingerprint == "" || v.Revision != expected+1 {
		return fmt.Errorf("invalid native upgrade")
	}
	if expected == 0 {
		_, err := t.tx.ExecContext(t.ctx, "INSERT INTO native_upgrades(id,host_id,payload) VALUES(?,?,?)", v.ID, v.HostID, mustJSON(v))
		return err
	}
	old, err := t.GetNativeUpgrade(v.ID)
	if err != nil {
		return err
	}
	if old.HostID != v.HostID || old.TargetVersion != v.TargetVersion || old.InstallationFingerprint != v.InstallationFingerprint {
		return ErrConflict
	}
	result, err := t.tx.ExecContext(t.ctx, "UPDATE native_upgrades SET payload=? WHERE id=? AND json_extract(payload,'$.revision')=?", mustJSON(v), v.ID, expected)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count != 1 {
		return ErrConflict
	}
	return err
}
func (t *Tx) AssertNativeUpgradeAvailable(backend work.Backend, host string) error {
	if backend != work.BackendCodex {
		return nil
	}
	v, err := t.ActiveNativeUpgrade(host)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("Codex host upgrade %s retains admission fence (%s)", v.ID, v.Stage)
}
func (t *Tx) NativeUpgradeWorkers(host string) ([]work.WorkerSession, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT w.payload FROM workers w JOIN locations l ON l.id=json_extract(w.payload,'$.location_id') WHERE json_extract(l.payload,'$.host_id')=? AND json_extract(w.payload,'$.backend')='codex' AND json_extract(w.payload,'$.state')<>'retired' ORDER BY w.id`, host)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []work.WorkerSession
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var v work.WorkerSession
		if err = json.Unmarshal([]byte(raw), &v); err != nil {
			return nil, err
		}
		values = append(values, v)
	}
	return values, rows.Err()
}
func (t *Tx) LatestCompletedNativeUpgrade(host string) (NativeUpgrade, error) {
	var v NativeUpgrade
	var raw string
	err := t.tx.QueryRowContext(t.ctx, "SELECT payload FROM native_upgrades WHERE host_id=? AND json_extract(payload,'$.stage')='completed' ORDER BY json_extract(payload,'$.updated_at') DESC,id DESC LIMIT 1", host).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	if err == nil {
		err = json.Unmarshal([]byte(raw), &v)
	}
	return v, err
}
