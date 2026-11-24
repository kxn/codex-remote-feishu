package runtimejournal

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

// VerifyLegacySessionProvenance reads an existing, stopped carrier journal. It
// proves one exact launch/owner/session relation, not native quiescence.
func VerifyLegacySessionProvenance(ctx context.Context, snapshot agentproto.RuntimeLaunchSnapshot, stateDir, session string) (string, string, error) {
	if session == "" || snapshot.CarrierIncarnation == "" || snapshot.Owner.ScopeID == "" || snapshot.Owner.WorkerSessionID == "" {
		return "", "", ErrConflict
	}
	path, err := ValidatePath(snapshot.JournalPath, stateDir, snapshot.CWD)
	if err != nil {
		return "", "", err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", "", err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return "", "", ErrConflict
	}
	uri := (&url.URL{Scheme: "file", Path: filepath.ToSlash(path), RawQuery: "mode=ro"}).String()
	db, err := sql.Open("sqlite", uri)
	if err != nil {
		return "", "", err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err = db.ExecContext(ctx, `PRAGMA query_only=ON`); err != nil {
		return "", "", err
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return "", "", err
	}
	defer tx.Rollback()
	snapshotRaw, err := json.Marshal(snapshot)
	if err != nil {
		return "", "", err
	}
	expected := hash(snapshotRaw)
	var stored string
	if err = tx.QueryRowContext(ctx, `SELECT payload_hash FROM launch_contracts WHERE incarnation=?`, snapshot.CarrierIncarnation).Scan(&stored); err != nil {
		return "", "", err
	}
	if stored != expected {
		return "", "", fmt.Errorf("legacy launch journal mismatch: %w", ErrConflict)
	}
	var payload []byte
	var eventHash string
	err = tx.QueryRowContext(ctx, `SELECT hash,payload FROM events WHERE incarnation=? AND json_extract(payload,'$.threadId')=? AND json_extract(payload,'$.execution.scopeId')=? AND json_extract(payload,'$.execution.workerSessionId')=? AND json_extract(payload,'$.execution.sessionGeneration')=? ORDER BY sequence LIMIT 1`, snapshot.CarrierIncarnation, session, snapshot.Owner.ScopeID, snapshot.Owner.WorkerSessionID, snapshot.Owner.SessionGeneration).Scan(&eventHash, &payload)
	if err != nil {
		return "", "", err
	}
	var event agentproto.Event
	if json.Unmarshal(payload, &event) != nil || event.Execution == nil || event.Journal == nil {
		return "", "", ErrConflict
	}
	// A turn event may add an attempt and run, but every owner dimension must
	// still match the frozen launch; worker and session generation alone are
	// insufficient to prove the original personal context.
	owner := *event.Execution
	owner.AttemptID, owner.RunRef = "", ""
	if owner != snapshot.Owner {
		return "", "", ErrConflict
	}
	actual, err := agentproto.RuntimeEventPayloadHash(event)
	if err != nil {
		return "", "", err
	}
	if actual != eventHash || event.Journal.PayloadHash != eventHash || event.Journal.CarrierIncarnation != snapshot.CarrierIncarnation || event.ThreadID != session {
		return "", "", ErrConflict
	}
	if err = tx.Commit(); err != nil {
		return "", "", err
	}
	return expected, eventHash, nil
}
