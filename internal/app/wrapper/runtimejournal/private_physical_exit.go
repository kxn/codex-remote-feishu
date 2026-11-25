package runtimejournal

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/url"
	"path/filepath"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

// VerifyPrivatePhysicalExit establishes a stopped carrier and its controlled
// process scopes under the exact journal write fence. It makes no claim about
// native command outcomes or remote business completion.
func VerifyPrivatePhysicalExit(ctx context.Context, snapshot agentproto.PrivateNativeLaunchSnapshot, stateDir string, carrier hostinventory.ProcessIdentity, watermark *int64) error {
	if err := snapshot.Validate(); err != nil {
		return err
	}
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
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	var launch string
	if err = tx.QueryRowContext(ctx, `SELECT payload_hash FROM launch_contracts WHERE incarnation=?`, snapshot.Owner.CarrierIncarnation).Scan(&launch); err != nil {
		return err
	}
	if launch != hash(raw) {
		return ErrConflict
	}
	var inventory []byte
	if err = tx.QueryRowContext(ctx, `SELECT payload FROM inventory_state WHERE incarnation=?`, snapshot.Owner.CarrierIncarnation).Scan(&inventory); err != nil {
		return err
	}
	var v struct {
		Version int                    `json:"version"`
		Owner   agentproto.NativeOwner `json:"native_owner"`
		State   struct {
			Scopes []hostinventory.Snapshot `json:"scopes"`
		} `json:"state"`
	}
	if json.Unmarshal(inventory, &v) != nil || v.Version != 1 || v.Owner != snapshot.Owner {
		return ErrConflict
	}
	st := v.State
	if len(st.Scopes) == 0 {
		return ErrConflict
	}
	for _, saved := range st.Scopes {
		if !saved.Complete {
			return ErrConflict
		}
		scope, e := hostinventory.Restore(saved)
		if e != nil {
			return e
		}
		if !saved.RootExitVerified && !(saved.ControlledStop && saved.WaitObserved) {
			checkCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
			e = scope.ObserveRootExitWithCarrier(checkCtx, carrier)
			cancel()
			if e != nil {
				_ = scope.Close()
				return e
			}
		}
		current := scope.Inspect()
		_ = scope.Close()
		if !current.Complete || current.RootAlive || len(current.ProcessIDs) != 0 {
			return ErrConflict
		}
	}
	var integrity string
	if err = tx.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&integrity); err != nil {
		return err
	}
	if integrity != "ok" {
		return ErrConflict
	}
	if watermark == nil {
		return ErrConflict
	}
	if err = tx.QueryRowContext(ctx, `SELECT coalesce(max(f.id),0) FROM frames f JOIN commands c ON c.id=f.command_id WHERE c.incarnation=?`, snapshot.Owner.CarrierIncarnation).Scan(watermark); err != nil {
		return err
	}
	return tx.Commit()
}
