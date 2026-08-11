package codexstate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/atomicfile"
)

type turnPatchLedgerRecord struct {
	PatchID             string    `json:"patch_id"`
	ThreadID            string    `json:"thread_id"`
	TurnID              string    `json:"turn_id"`
	RolloutPath         string    `json:"rollout_path"`
	BackupPath          string    `json:"backup_path"`
	ActorUserID         string    `json:"actor_user_id,omitempty"`
	SurfaceSessionID    string    `json:"surface_session_id,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
	CompletedAt         time.Time `json:"completed_at,omitempty"`
	RolledBackAt        time.Time `json:"rolled_back_at,omitempty"`
	RolloutBeforeDigest string    `json:"rollout_before_digest"`
	RolloutAfterDigest  string    `json:"rollout_after_digest,omitempty"`
	Status              string    `json:"status"`
	Error               string    `json:"error,omitempty"`
}

type turnPatchLatestPointer struct {
	ThreadID  string    `json:"thread_id"`
	PatchID   string    `json:"patch_id"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (s *TurnPatchStorage) prepareTurnPatchLedger(record turnPatchLedgerRecord) error {
	record.Status = turnPatchLedgerStatusPrepared
	return writeJSONAtomic(s.ledgerPath(record.PatchID), record)
}

func (s *TurnPatchStorage) markTurnPatchApplied(record turnPatchLedgerRecord) error {
	record.Status = turnPatchLedgerStatusApplied
	if err := writeJSONAtomic(s.ledgerPath(record.PatchID), record); err != nil {
		return err
	}
	return writeJSONAtomic(s.latestPointerPath(record.ThreadID), turnPatchLatestPointer{
		ThreadID:  record.ThreadID,
		PatchID:   record.PatchID,
		UpdatedAt: record.CompletedAt,
	})
}

func (s *TurnPatchStorage) markTurnPatchFailed(record turnPatchLedgerRecord, err error) error {
	record.Status = turnPatchLedgerStatusFailed
	if err != nil {
		record.Error = err.Error()
	}
	return writeJSONAtomic(s.ledgerPath(record.PatchID), record)
}

func (s *TurnPatchStorage) markTurnPatchRolledBack(record turnPatchLedgerRecord) error {
	record.Status = turnPatchLedgerStatusRolled
	if err := writeJSONAtomic(s.ledgerPath(record.PatchID), record); err != nil {
		return err
	}
	removeErr := os.Remove(s.latestPointerPath(record.ThreadID))
	if removeErr != nil && !os.IsNotExist(removeErr) {
		return removeErr
	}
	return nil
}

func (s *TurnPatchStorage) loadTurnPatchLedger(patchID string) (turnPatchLedgerRecord, error) {
	var record turnPatchLedgerRecord
	path := s.ledgerPath(patchID)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return record, fmt.Errorf("%w: %s", ErrTurnPatchPatchNotFound, patchID)
		}
		return record, err
	}
	if err := json.Unmarshal(raw, &record); err != nil {
		return record, err
	}
	return record, nil
}

func (s *TurnPatchStorage) loadLatestTurnPatch(threadID string) (turnPatchLatestPointer, error) {
	var pointer turnPatchLatestPointer
	raw, err := os.ReadFile(s.latestPointerPath(threadID))
	if err != nil {
		return pointer, err
	}
	if err := json.Unmarshal(raw, &pointer); err != nil {
		return pointer, err
	}
	return pointer, nil
}

func (s *TurnPatchStorage) ledgerPath(patchID string) string {
	return filepath.Join(s.patchStateDir, "ledgers", strings.TrimSpace(patchID)+".json")
}

func (s *TurnPatchStorage) latestPointerPath(threadID string) string {
	return filepath.Join(s.patchStateDir, "latest", safeThreadToken(threadID)+".json")
}

func (s *TurnPatchStorage) backupPath(patchID, rolloutPath string) string {
	ext := filepath.Ext(rolloutPath)
	if ext == "" {
		ext = ".jsonl"
	}
	name := strings.TrimSuffix(filepath.Base(rolloutPath), filepath.Ext(rolloutPath))
	return filepath.Join(s.patchStateDir, "backups", strings.TrimSpace(patchID)+"-"+name+ext+".bak")
}

func writeJSONAtomic(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return writeAtomicFile(path, raw, 0o600)
}

func writeAtomicFile(path string, raw []byte, mode os.FileMode) error {
	return atomicfile.Write(path, raw, mode)
}

func safeThreadToken(threadID string) string {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return "unknown"
	}
	var b strings.Builder
	for _, r := range threadID {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
		if b.Len() >= 96 {
			break
		}
	}
	if b.Len() == 0 {
		return "thread"
	}
	return b.String()
}
