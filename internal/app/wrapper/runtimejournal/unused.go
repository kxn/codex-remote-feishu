package runtimejournal

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
	"net/url"
	"path/filepath"
)

// VerifyUnusedCarrier is read-only and requires the caller to prove exact OS
// process exit first. No session/turn command or native frame may have existed.
func VerifyUnusedCarrier(ctx context.Context, snapshot agentproto.RuntimeLaunchSnapshot, stateDir string) error {
	path, err := ValidatePath(snapshot.JournalPath, stateDir, snapshot.CWD)
	if err != nil {
		return err
	}
	lock, err := relayruntime.AcquireLock(ctx, path+".lock", false)
	if err != nil {
		return err
	}
	defer lock.Release()
	db, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: filepath.ToSlash(path), RawQuery: "mode=ro"}).String())
	if err != nil {
		return err
	}
	defer db.Close()
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	var fingerprint, integrity string
	if err = db.QueryRowContext(ctx, `SELECT payload_hash FROM launch_contracts WHERE incarnation=?`, snapshot.CarrierIncarnation).Scan(&fingerprint); err != nil {
		return err
	}
	if fingerprint != hash(raw) {
		return fmt.Errorf("unused carrier launch contract mismatch")
	}
	var used bool
	if err = db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM commands WHERE incarnation=? AND coalesce(json_extract(payload,'$.kind'),'') NOT IN ('model.list','runtime.inspect')) OR EXISTS(SELECT 1 FROM frames f JOIN commands c ON c.id=f.command_id WHERE c.incarnation=? AND f.native_method<>'model/list')`, snapshot.CarrierIncarnation, snapshot.CarrierIncarnation).Scan(&used); err != nil {
		return err
	}
	if used {
		return fmt.Errorf("unused carrier journal contains native execution")
	}
	if err = db.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&integrity); err != nil {
		return err
	}
	if integrity != "ok" {
		return fmt.Errorf("unused carrier journal integrity failed")
	}
	return nil
}
