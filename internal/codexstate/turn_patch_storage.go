package codexstate

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/pathcompare"
)

func (s *TurnPatchStorage) ResolveThreadTarget(threadID string) (PatchThreadTarget, error) {
	var target PatchThreadTarget
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return target, ErrTurnPatchThreadRequired
	}
	target.ThreadID = threadID
	if path, err := s.resolveSQLiteRolloutPath(threadID); err == nil && path != "" {
		target.RolloutPath = path
		return target, nil
	}
	path, err := s.resolveSessionsRolloutPath(threadID)
	if err != nil {
		return target, err
	}
	target.RolloutPath = path
	return target, nil
}

func (s *TurnPatchStorage) PreviewLatestAssistantTurn(threadID string) (*TurnPatchPreview, error) {
	target, err := s.ResolveThreadTarget(threadID)
	if err != nil {
		return nil, err
	}
	snapshot, err := readRolloutSnapshot(target.RolloutPath)
	if err != nil {
		return nil, err
	}
	if snapshot.threadID != target.ThreadID {
		return nil, fmt.Errorf("%w: %s", ErrTurnPatchRolloutPathDrift, target.RolloutPath)
	}
	preview := &TurnPatchPreview{
		ThreadID:           target.ThreadID,
		RolloutPath:        target.RolloutPath,
		RolloutDigest:      snapshot.digest,
		TurnID:             snapshot.latestTurn.turnID,
		ReasoningLineCount: len(snapshot.latestTurn.reasoningLineIndex),
		Messages:           make([]TurnPatchPreviewMessage, 0, len(snapshot.latestTurn.messages)),
	}
	for _, message := range snapshot.latestTurn.messages {
		preview.Messages = append(preview.Messages, TurnPatchPreviewMessage{
			MessageKey: message.messageKey,
			Phase:      message.phase,
			Text:       message.text,
			IsFinal:    message.isFinal,
		})
	}
	return preview, nil
}

func (s *TurnPatchStorage) ApplyLatestTurnPatch(req ApplyLatestTurnPatchRequest) (*ApplyLatestTurnPatchResult, error) {
	target, err := s.ResolveThreadTarget(req.ThreadID)
	if err != nil {
		return nil, err
	}
	snapshot, err := readRolloutSnapshot(target.RolloutPath)
	if err != nil {
		return nil, err
	}
	if snapshot.threadID != target.ThreadID {
		return nil, fmt.Errorf("%w: %s", ErrTurnPatchRolloutPathDrift, target.RolloutPath)
	}
	if want := strings.TrimSpace(req.ExpectedRolloutDigest); want != "" && snapshot.digest != want {
		return nil, ErrTurnPatchRolloutDigestMismatch
	}
	if want := strings.TrimSpace(req.ExpectedTurnID); want != "" && snapshot.latestTurn.turnID != want {
		return nil, ErrTurnPatchLatestTurnNotFound
	}
	updatedRaw, replacedCount, removedReasoning, err := applyRolloutReplacements(snapshot, req.Replacements)
	if err != nil {
		return nil, err
	}
	patchID := s.newPatchID(target.ThreadID)
	backupPath := s.backupPath(patchID, target.RolloutPath)
	if err := writeAtomicFile(backupPath, snapshot.raw, 0o600); err != nil {
		return nil, err
	}
	record := turnPatchLedgerRecord{
		PatchID:             patchID,
		ThreadID:            target.ThreadID,
		TurnID:              snapshot.latestTurn.turnID,
		RolloutPath:         target.RolloutPath,
		BackupPath:          backupPath,
		ActorUserID:         strings.TrimSpace(req.ActorUserID),
		SurfaceSessionID:    strings.TrimSpace(req.SurfaceSessionID),
		CreatedAt:           s.now().UTC(),
		RolloutBeforeDigest: snapshot.digest,
	}
	if err := s.prepareTurnPatchLedger(record); err != nil {
		return nil, err
	}
	if err := writeAtomicFile(target.RolloutPath, updatedRaw, snapshot.mode); err != nil {
		_ = s.markTurnPatchFailed(record, err)
		return nil, err
	}
	if err := s.metadataReconciler.ReconcileLatestTurnPatch(ReconcileLatestTurnPatchRequest{
		ThreadID:    target.ThreadID,
		TurnID:      snapshot.latestTurn.turnID,
		RolloutPath: target.RolloutPath,
	}); err != nil {
		_ = writeAtomicFile(target.RolloutPath, snapshot.raw, snapshot.mode)
		_ = s.markTurnPatchFailed(record, err)
		return nil, err
	}
	record.CompletedAt = s.now().UTC()
	record.RolloutAfterDigest = sha256Hex(updatedRaw)
	if err := s.markTurnPatchApplied(record); err != nil {
		_ = writeAtomicFile(target.RolloutPath, snapshot.raw, snapshot.mode)
		_ = s.markTurnPatchFailed(record, err)
		return nil, err
	}
	return &ApplyLatestTurnPatchResult{
		PatchID:              patchID,
		ThreadID:             target.ThreadID,
		TurnID:               snapshot.latestTurn.turnID,
		RolloutPath:          target.RolloutPath,
		BackupPath:           backupPath,
		RolloutBeforeDigest:  snapshot.digest,
		RolloutAfterDigest:   record.RolloutAfterDigest,
		ReplacedMessageCount: replacedCount,
		RemovedReasoningLine: removedReasoning,
		AppliedAt:            record.CompletedAt,
	}, nil
}

func (s *TurnPatchStorage) RollbackLatestTurnPatch(req RollbackLatestTurnPatchRequest) (*RollbackLatestTurnPatchResult, error) {
	record, err := s.resolveRollbackLedger(req)
	if err != nil {
		return nil, err
	}
	if record.Status != turnPatchLedgerStatusApplied {
		return nil, ErrTurnPatchNotLatest
	}
	if actor := strings.TrimSpace(req.ActorUserID); actor != "" && record.ActorUserID != "" && record.ActorUserID != actor {
		return nil, ErrTurnPatchActorMismatch
	}
	target, err := s.ResolveThreadTarget(record.ThreadID)
	if err != nil {
		return nil, err
	}
	if !sameTurnPatchRolloutPath(target.RolloutPath, record.RolloutPath) {
		return nil, ErrTurnPatchRolloutPathDrift
	}
	snapshot, err := readRolloutSnapshot(record.RolloutPath)
	if err != nil {
		return nil, err
	}
	if snapshot.digest != record.RolloutAfterDigest {
		return nil, ErrTurnPatchRollbackDrift
	}
	backupRaw, err := os.ReadFile(record.BackupPath)
	if err != nil {
		return nil, err
	}
	if err := writeAtomicFile(record.RolloutPath, backupRaw, snapshot.mode); err != nil {
		return nil, err
	}
	record.RolledBackAt = s.now().UTC()
	if err := s.markTurnPatchRolledBack(record); err != nil {
		return nil, err
	}
	return &RollbackLatestTurnPatchResult{
		PatchID:             record.PatchID,
		ThreadID:            record.ThreadID,
		TurnID:              record.TurnID,
		RolloutPath:         record.RolloutPath,
		BackupPath:          record.BackupPath,
		RolloutBeforeDigest: record.RolloutBeforeDigest,
		RolloutAfterDigest:  record.RolloutAfterDigest,
		RolledBackAt:        record.RolledBackAt,
	}, nil
}

func sameTurnPatchRolloutPath(left, right string) bool {
	return pathcompare.SameCleanPlatformPath(left, right)
}

func (s *TurnPatchStorage) resolveRollbackLedger(req RollbackLatestTurnPatchRequest) (turnPatchLedgerRecord, error) {
	patchID := strings.TrimSpace(req.PatchID)
	if patchID == "" {
		threadID := strings.TrimSpace(req.ThreadID)
		if threadID == "" {
			return turnPatchLedgerRecord{}, ErrTurnPatchPatchIDRequired
		}
		pointer, err := s.loadLatestTurnPatch(threadID)
		if err != nil {
			if os.IsNotExist(err) {
				return turnPatchLedgerRecord{}, ErrTurnPatchNotLatest
			}
			return turnPatchLedgerRecord{}, err
		}
		patchID = strings.TrimSpace(pointer.PatchID)
	}
	record, err := s.loadTurnPatchLedger(patchID)
	if err != nil {
		return turnPatchLedgerRecord{}, err
	}
	if threadID := strings.TrimSpace(req.ThreadID); threadID != "" && record.ThreadID != threadID {
		return turnPatchLedgerRecord{}, ErrTurnPatchNotLatest
	}
	pointer, err := s.loadLatestTurnPatch(record.ThreadID)
	if err != nil {
		if os.IsNotExist(err) {
			return turnPatchLedgerRecord{}, ErrTurnPatchNotLatest
		}
		return turnPatchLedgerRecord{}, err
	}
	if strings.TrimSpace(pointer.PatchID) != record.PatchID {
		return turnPatchLedgerRecord{}, ErrTurnPatchNotLatest
	}
	return record, nil
}

func (s *TurnPatchStorage) resolveSQLiteRolloutPath(threadID string) (string, error) {
	if s == nil || s.sqliteCatalog == nil {
		return "", ErrTurnPatchRolloutNotFound
	}
	path, err := s.sqliteCatalog.ThreadRolloutPath(threadID)
	if err != nil {
		return "", err
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return "", ErrTurnPatchRolloutNotFound
	}
	resolvedThreadID, err := readRolloutThreadID(path)
	if err != nil {
		return "", err
	}
	if resolvedThreadID != threadID {
		if s.logf != nil {
			s.logf("turn patch sqlite rollout path drifted for thread %s: %s -> %s", threadID, path, resolvedThreadID)
		}
		return "", ErrTurnPatchRolloutPathDrift
	}
	return path, nil
}

func (s *TurnPatchStorage) resolveSessionsRolloutPath(threadID string) (string, error) {
	root := filepath.Clean(strings.TrimSpace(s.sessionsRoot))
	if root == "" {
		return "", ErrTurnPatchRolloutNotFound
	}
	type candidate struct {
		path string
		info fs.FileInfo
	}
	var best candidate
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		name := filepath.Base(path)
		if !strings.Contains(name, threadID) || filepath.Ext(name) != ".jsonl" {
			return nil
		}
		resolvedThreadID, err := readRolloutThreadID(path)
		if err != nil {
			return nil
		}
		if resolvedThreadID != threadID {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if best.path == "" || info.ModTime().After(best.info.ModTime()) || (info.ModTime().Equal(best.info.ModTime()) && path > best.path) {
			best = candidate{path: path, info: info}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if best.path == "" {
		return "", ErrTurnPatchRolloutNotFound
	}
	return best.path, nil
}

func (s *TurnPatchStorage) newPatchID(threadID string) string {
	return fmt.Sprintf("turn-patch-%s-%s", s.now().UTC().Format("20060102T150405.000000000Z"), safeThreadToken(threadID))
}
