package runtimejournal

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func (s *Store) NativeWriteWatermark() (int64, error) {
	var value int64
	err := s.db.QueryRow(`SELECT coalesce(max(f.id),0) FROM frames f JOIN commands c ON c.id=f.command_id WHERE c.incarnation=?`, s.incarnation).Scan(&value)
	return value, err
}

// VerifyRetiredWriteFence is read-only. It is used only after an independently
// verified exact carrier OS identity has exited; it never reopens for replay.
func VerifyRetiredWriteFence(ctx context.Context, snapshot agentproto.RuntimeLaunchSnapshot, stateDir string, watermark int64) error {
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	return verifyRetiredWriteFence(ctx, snapshot.JournalPath, stateDir, snapshot.CWD, snapshot.CarrierIncarnation, raw, watermark, true)
}

// VerifyRetiredPrivateWriteFence verifies the exact private launch and journal
// after an independently proven carrier exit. It is not itself an exit proof.
func VerifyRetiredPrivateWriteFence(ctx context.Context, snapshot agentproto.PrivateNativeLaunchSnapshot, stateDir string, watermark int64) error {
	if err := snapshot.Validate(); err != nil {
		return err
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	return verifyRetiredWriteFence(ctx, snapshot.JournalPath, stateDir, snapshot.CWD, snapshot.Owner.CarrierIncarnation, raw, watermark, true)
}
func verifyRetiredWriteFence(ctx context.Context, journalPath, stateDir, cwd, incarnation string, raw []byte, watermark int64, allowEmpty bool) error {
	if watermark < 0 || (watermark == 0 && !allowEmpty) {
		return fmt.Errorf("no durable native write watermark")
	}
	path, err := ValidatePath(journalPath, stateDir, cwd)
	if err != nil {
		return err
	}
	lock, err := relayruntime.AcquireLock(ctx, path+".lock", false)
	if err != nil {
		return err
	}
	defer lock.Release()
	dsn := (&url.URL{Scheme: "file", Path: filepath.ToSlash(path), RawQuery: "mode=ro"}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	var fingerprint string
	if err = db.QueryRowContext(ctx, `SELECT payload_hash FROM launch_contracts WHERE incarnation=?`, incarnation).Scan(&fingerprint); err != nil {
		return err
	}
	if fingerprint != hash(raw) {
		return fmt.Errorf("retired journal launch contract changed")
	}
	var count int
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM frames f JOIN commands c ON c.id=f.command_id WHERE c.incarnation=? AND f.id>? AND f.native_method NOT IN ('thread/read','thread/goal/get','model/list','thread/goal/clear','turn/interrupt','session/cancel','interrupt')`, incarnation, watermark).Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return fmt.Errorf("retired carrier has effectful native frames after its quiescence proof")
	}
	// A watermark cannot refer to a missing or truncated journal prefix.
	var maximum int64
	if err = db.QueryRowContext(ctx, `SELECT coalesce(max(f.id),0) FROM frames f JOIN commands c ON c.id=f.command_id WHERE c.incarnation=?`, incarnation).Scan(&maximum); err != nil {
		return err
	}
	if maximum < watermark {
		return fmt.Errorf("retired native journal was truncated")
	}
	var integrity string
	if err = db.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&integrity); err != nil {
		return err
	}
	if !strings.EqualFold(integrity, "ok") {
		return fmt.Errorf("retired native journal integrity failed")
	}
	return nil
}
