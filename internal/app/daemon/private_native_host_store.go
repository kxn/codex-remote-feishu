package daemon

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"

	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"github.com/kxn/codex-remote-feishu/internal/atomicfile"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	_ "modernc.org/sqlite"
)

// privateCarrierRecord is a host fact, frozen before launching a wrapper. A
// sending launch without a recorded process remains uncertain after restart.
type privateCarrierRecord struct {
	Snapshot                  agentproto.PrivateNativeLaunchSnapshot `json:"snapshot"`
	Surface                   privatechat.Owner                      `json:"surface"`
	CodexProfileID            string                                 `json:"codexProfileId,omitempty"`
	OpenCodeProfileID         string                                 `json:"openCodeProfileId,omitempty"`
	OpenCodeAdmissionRef      *state.OpenCodeAdmissionRef            `json:"openCodeAdmissionRef,omitempty"`
	OpenCodeRuntimeAccessMode string                                 `json:"openCodeRuntimeAccessMode,omitempty"`
	ClaudeProfileID           string                                 `json:"claudeProfileId,omitempty"`
	ClaudeReasoningEffort     string                                 `json:"claudeReasoningEffort,omitempty"`
	ResumeSession             string                                 `json:"resumeSession,omitempty"`
	CodexAdmissionRef         *state.CodexAdmissionRef               `json:"codexAdmissionRef,omitempty"`
	MigrationResume           *privatechat.MigrationResumeIntent     `json:"migrationResume,omitempty"`
	UpgradeRecovery           *privatechat.UpgradeRecoveryPlan       `json:"upgradeRecovery,omitempty"`
	UpgradeRecoveryEffectID   string                                 `json:"upgradeRecoveryEffectId,omitempty"`
	SurfaceGeneration         int64                                  `json:"surfaceGeneration"`
	Inspection                privatechat.NativeEventRef             `json:"inspection"`
	StopEffectID              string                                 `json:"stopEffectId,omitempty"`
	StopInspectionID          string                                 `json:"stopInspectionId,omitempty"`
	StopInspectionOrdinal     int64                                  `json:"stopInspectionOrdinal,omitempty"`
	CredentialPath            string                                 `json:"credentialPath"`
	ConfigurationFingerprint  string                                 `json:"configurationFingerprint"`
	SnapshotHash              string                                 `json:"snapshotHash"`
	Process                   hostinventory.ProcessIdentity          `json:"process"`
	State                     string                                 `json:"state"`
}
type privateNativeHostStore struct {
	db            *sql.DB
	credentialDir string
}

func openPrivateNativeHostStore(path string) (*privateNativeHostStore, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("absolute host store path required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = f.Chmod(0600); err != nil {
		f.Close()
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	dsn := (&url.URL{Scheme: "file", Path: filepath.ToSlash(path), RawQuery: "_pragma=busy_timeout(5000)&_pragma=synchronous(FULL)&_txlock=immediate"}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		db.Close()
		return nil, err
	}
	var version int
	if err = db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version > 1 {
		db.Close()
		return nil, fmt.Errorf("invalid host store version %d: %v", version, err)
	}
	if version == 0 {
		var tables int
		if err = db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`).Scan(&tables); err != nil || tables != 0 {
			db.Close()
			return nil, fmt.Errorf("nonempty unversioned host store")
		}
	}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS carriers (instance TEXT PRIMARY KEY, incarnation TEXT NOT NULL UNIQUE, record BLOB NOT NULL, credential_hash BLOB NOT NULL, state TEXT NOT NULL); PRAGMA user_version=1`)
	if err != nil {
		db.Close()
		return nil, err
	}
	return &privateNativeHostStore{db: db, credentialDir: filepath.Join(filepath.Dir(path), "private-credentials")}, nil
}
func (s *privateNativeHostStore) Close() error { return s.db.Close() }
func (s *privateNativeHostStore) prepare(ctx context.Context, rec privateCarrierRecord) (string, error) {
	if err := rec.Snapshot.Validate(); err != nil {
		return "", err
	}
	if rec.SurfaceGeneration <= 0 {
		return "", fmt.Errorf("surface generation required")
	}
	b, _ := json.Marshal(rec.Snapshot)
	sum := sha256.Sum256(b)
	rec.SnapshotHash = hex.EncodeToString(sum[:])
	rec.State = "prepared"
	rec.Process = hostinventory.ProcessIdentity{}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", err
	}
	credential := hex.EncodeToString(secret)
	hash := sha256.Sum256([]byte(credential))
	if err := os.MkdirAll(s.credentialDir, 0700); err != nil {
		return "", err
	}
	rec.CredentialPath = filepath.Join(s.credentialDir, hex.EncodeToString(hash[:])+".secret")
	if err := atomicfile.Write(rec.CredentialPath, []byte(credential), 0600); err != nil {
		return "", err
	}
	body, err := json.Marshal(rec)
	if err != nil {
		os.Remove(rec.CredentialPath)
		return "", err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO carriers(instance,incarnation,record,credential_hash,state) VALUES(?,?,?,?,?)`, rec.Snapshot.Owner.InstanceID, rec.Snapshot.Owner.CarrierIncarnation, body, hash[:], rec.State)
	if err != nil {
		os.Remove(rec.CredentialPath)
		return "", err
	}
	return credential, nil
}
func (s *privateNativeHostStore) lookup(ctx context.Context, instance string) (privateCarrierRecord, error) {
	var rec privateCarrierRecord
	var body []byte
	err := s.db.QueryRowContext(ctx, `SELECT record FROM carriers WHERE instance=?`, instance).Scan(&body)
	if err != nil {
		return rec, err
	}
	err = json.Unmarshal(body, &rec)
	if err != nil {
		return rec, err
	}
	b, _ := json.Marshal(rec.Snapshot)
	sum := sha256.Sum256(b)
	if rec.SnapshotHash != hex.EncodeToString(sum[:]) {
		return rec, fmt.Errorf("corrupt host snapshot")
	}
	return rec, nil
}
func (s *privateNativeHostStore) authorize(hello agentproto.Hello, credential string) error {
	var hash []byte
	var body []byte
	err := s.db.QueryRow(`SELECT credential_hash,record FROM carriers WHERE instance=?`, hello.Instance.InstanceID).Scan(&hash, &body)
	if err == sql.ErrNoRows {
		if credential != "" || hello.PrivateNativeProtocolVersion != 0 {
			return fmt.Errorf("unknown private carrier")
		}
		return nil
	}
	if err != nil {
		return err
	}
	var rec privateCarrierRecord
	if json.Unmarshal(body, &rec) != nil {
		return fmt.Errorf("invalid carrier record")
	}
	o := rec.Snapshot.Owner
	given := sha256.Sum256([]byte(credential))
	if credential == "" || subtle.ConstantTimeCompare(hash, given[:]) != 1 || hello.Probe || hello.PrivateNativeProtocolVersion != agentproto.PrivateNativeProtocolVersion || hello.CarrierIncarnation != o.CarrierIncarnation || agentproto.EffectiveHelloBackend(hello) != o.Backend || rec.State == "retired" || rec.State == "aborted" {
		return fmt.Errorf("private carrier authentication failed")
	}
	return nil
}
func (s *privateNativeHostStore) transition(ctx context.Context, instance, from, to string, process hostinventory.ProcessIdentity) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var body []byte
	var state string
	if err = tx.QueryRowContext(ctx, `SELECT record,state FROM carriers WHERE instance=?`, instance).Scan(&body, &state); err != nil {
		return err
	}
	var rec privateCarrierRecord
	if err = json.Unmarshal(body, &rec); err != nil {
		return err
	}
	if state != from {
		if state == to && process.PID != 0 && rec.Process == process {
			return tx.Commit()
		}
		return fmt.Errorf("host transition conflict")
	}
	rec.State = to
	if process.PID != 0 {
		rec.Process = process
	}
	body, err = json.Marshal(rec)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE carriers SET state=?,record=? WHERE instance=? AND state=?`, to, body, instance, from); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *privateNativeHostStore) byState(ctx context.Context, state string) ([]privateCarrierRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT record FROM carriers WHERE state=? ORDER BY instance`, state)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []privateCarrierRecord
	for rows.Next() {
		var raw []byte
		var rec privateCarrierRecord
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &rec); err != nil {
			return nil, err
		}
		if rec.State != state {
			return nil, fmt.Errorf("corrupt carrier state")
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (s *privateNativeHostStore) seal(ctx context.Context, instance string, ref privatechat.NativeEventRef) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var raw []byte
	var state string
	if err = tx.QueryRowContext(ctx, `SELECT record,state FROM carriers WHERE instance=?`, instance).Scan(&raw, &state); err != nil {
		return err
	}
	if state != "draining" {
		return fmt.Errorf("carrier not draining")
	}
	var rec privateCarrierRecord
	if err = json.Unmarshal(raw, &rec); err != nil {
		return err
	}
	if ref.Incarnation != rec.Snapshot.Owner.CarrierIncarnation || ref.Sequence == 0 || ref.PayloadHash == "" {
		return fmt.Errorf("inspection reference mismatch")
	}
	rec.State = "sealed"
	rec.Inspection = ref
	raw, err = json.Marshal(rec)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE carriers SET record=?,state='sealed' WHERE instance=? AND state='draining'`, raw, instance); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *privateNativeHostStore) credential(ctx context.Context, rec privateCarrierRecord) (string, error) {
	if filepath.Dir(rec.CredentialPath) != s.credentialDir {
		return "", fmt.Errorf("credential path outside host store")
	}
	info, err := os.Lstat(rec.CredentialPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return "", fmt.Errorf("invalid credential file")
	}
	f, err := os.Open(rec.CredentialPath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) || opened.Size() != 64 {
		return "", fmt.Errorf("invalid credential identity")
	}
	raw, err := io.ReadAll(io.LimitReader(f, 65))
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	var expected []byte
	if err = s.db.QueryRowContext(ctx, `SELECT credential_hash FROM carriers WHERE instance=? AND incarnation=?`, rec.Snapshot.Owner.InstanceID, rec.Snapshot.Owner.CarrierIncarnation).Scan(&expected); err != nil {
		return "", err
	}
	if subtle.ConstantTimeCompare(sum[:], expected) != 1 {
		return "", fmt.Errorf("credential mismatch")
	}
	return string(raw), nil
}

func (s *privateNativeHostStore) stopIntent(ctx context.Context, instance, effect string) (privateCarrierRecord, error) {
	var rec privateCarrierRecord
	if effect == "" {
		return rec, fmt.Errorf("stop effect required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return rec, err
	}
	defer tx.Rollback()
	var raw []byte
	var state string
	if err = tx.QueryRowContext(ctx, `SELECT record,state FROM carriers WHERE instance=?`, instance).Scan(&raw, &state); err != nil {
		return rec, err
	}
	if err = json.Unmarshal(raw, &rec); err != nil {
		return rec, err
	}
	if state != "draining" && state != "sealed" && state != "retired" {
		return rec, fmt.Errorf("carrier not draining")
	}
	if rec.StopEffectID != "" && rec.StopEffectID != effect {
		return rec, fmt.Errorf("another stop is in progress")
	}
	if rec.StopEffectID == "" {
		rec.StopEffectID = effect
		raw, err = json.Marshal(rec)
		if err != nil {
			return rec, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE carriers SET record=? WHERE instance=?`, raw, instance); err != nil {
			return rec, err
		}
	}
	return rec, tx.Commit()
}
func (s *privateNativeHostStore) inspectionIntent(ctx context.Context, instance, effect string, clear bool) (privateCarrierRecord, error) {
	var rec privateCarrierRecord
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return rec, err
	}
	defer tx.Rollback()
	var raw []byte
	var state string
	if err = tx.QueryRowContext(ctx, `SELECT record,state FROM carriers WHERE instance=?`, instance).Scan(&raw, &state); err != nil {
		return rec, err
	}
	if err = json.Unmarshal(raw, &rec); err != nil {
		return rec, err
	}
	if state != "draining" || rec.StopEffectID != effect {
		return rec, fmt.Errorf("stop intent changed")
	}
	if clear {
		rec.StopInspectionID = ""
	} else if rec.StopInspectionID == "" {
		rec.StopInspectionOrdinal++
		sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", effect, rec.StopInspectionOrdinal)))
		rec.StopInspectionID = "private-inspect-" + hex.EncodeToString(sum[:20])
	}
	raw, err = json.Marshal(rec)
	if err != nil {
		return rec, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE carriers SET record=? WHERE instance=?`, raw, instance); err != nil {
		return rec, err
	}
	return rec, tx.Commit()
}
