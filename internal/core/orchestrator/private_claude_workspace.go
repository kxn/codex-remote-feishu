package orchestrator

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// PrivateClaudeSnapshotChange is a proposed write to the existing workspace
// profile store. It is not visible to another surface until its host receipt.
type PrivateClaudeSnapshotChange struct {
	SurfaceID string
	Key       string
	Token     string
	Before    state.ClaudeWorkspaceProfileSnapshotRecord
	After     state.ClaudeWorkspaceProfileSnapshotRecord
}

// TakePrivateClaudeSnapshotChanges is called under the private processor lock.
func (s *Service) TakePrivateClaudeSnapshotChanges(surface string) ([]PrivateClaudeSnapshotChange, error) {
	changes := s.privateClaudeSnapshotChanges
	s.privateClaudeSnapshotChanges = nil
	for _, c := range changes {
		if c.SurfaceID != surface || strings.TrimSpace(c.Key) == "" {
			return nil, fmt.Errorf("private workspace snapshot owner mismatch")
		}
	}
	return changes, nil
}

// SetPrivateClaudeSnapshotOverlay retains this owner's unconfirmed preference
// without exposing it as committed shared store state.
func (s *Service) SetPrivateClaudeSnapshotOverlay(change PrivateClaudeSnapshotChange) {
	if s.privateClaudeSnapshotOverlay == nil {
		s.privateClaudeSnapshotOverlay = make(map[string]map[string]PrivateClaudeSnapshotChange)
	}
	if s.privateClaudeSnapshotOverlay[change.SurfaceID] == nil {
		s.privateClaudeSnapshotOverlay[change.SurfaceID] = make(map[string]PrivateClaudeSnapshotChange)
	}
	s.privateClaudeSnapshotOverlay[change.SurfaceID][change.Key] = change
}

func (s *Service) ClearPrivateClaudeSnapshotOverlay(change PrivateClaudeSnapshotChange) {
	if overlay := s.privateClaudeSnapshotOverlay[change.SurfaceID]; change.Token != "" && overlay != nil && overlay[change.Key].Token == change.Token {
		delete(overlay, change.Key)
	}
}

// ApplyPrivateClaudeSnapshotReceipt projects the actual persisted value.
func (s *Service) ApplyPrivateClaudeSnapshotReceipt(key string, record state.ClaudeWorkspaceProfileSnapshotRecord) error {
	if !s.config.PrivateApplication || strings.TrimSpace(key) == "" {
		return fmt.Errorf("invalid private workspace snapshot")
	}
	record = state.NormalizeClaudeWorkspaceProfileSnapshotRecord(record)
	if s.root.ClaudeWorkspaceProfileSnapshots == nil {
		s.root.ClaudeWorkspaceProfileSnapshots = make(map[string]state.ClaudeWorkspaceProfileSnapshotRecord)
	}
	if state.ClaudeWorkspaceProfileSnapshotRecordEmpty(record) {
		delete(s.root.ClaudeWorkspaceProfileSnapshots, key)
	} else {
		s.root.ClaudeWorkspaceProfileSnapshots[key] = record
	}
	return nil
}
