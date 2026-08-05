package claudeworkspaceprofile

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/statestore"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const (
	StateVersion  = 1
	StateFileName = "claude-workspace-profile-state.json"
)

type Record = state.ClaudeWorkspaceProfileSnapshotRecord

func StatePath(stateDir string) string {
	return statestore.StatePath(stateDir, StateFileName)
}

func NewStore(path string) *Store {
	return &Store{Store: statestore.New[Record](path, statestore.Options[Record]{
		Version: StateVersion,
		Name:    "claude workspace profile",
		Equal:   func(left, right Record) bool { return left == right },
	})}
}

func LoadStore(path string) (*Store, error) {
	store := NewStore(path)
	if store.Path() == "" {
		return store, nil
	}
	raw, err := os.ReadFile(store.Path())
	if err != nil {
		if os.IsNotExist(err) {
			return store, nil
		}
		return nil, err
	}
	var persisted rawStateFile
	if err := json.Unmarshal(raw, &persisted); err != nil {
		return nil, err
	}
	if persisted.Version == 0 {
		persisted.Version = StateVersion
	}
	if persisted.Version != StateVersion {
		return nil, fmt.Errorf("unsupported claude workspace profile state version: %d", persisted.Version)
	}
	entries := map[string]Record{}
	for key, rawEntry := range persisted.Entries {
		key = strings.TrimSpace(key)
		if claudeSnapshotHasExtraneousFields(rawEntry) {
			store.MarkDirty()
		}
		var entry Record
		if err := json.Unmarshal(rawEntry, &entry); err != nil {
			return nil, err
		}
		entry = state.NormalizeClaudeWorkspaceProfileSnapshotRecord(entry)
		if key == "" || state.ClaudeWorkspaceProfileSnapshotRecordEmpty(entry) {
			store.MarkDirty()
			continue
		}
		entries[key] = entry
	}
	store.SetEntries(entries)
	return store, nil
}

type rawStateFile struct {
	Version int                        `json:"version"`
	Entries map[string]json.RawMessage `json:"entries,omitempty"`
}

func claudeSnapshotHasExtraneousFields(rawEntry json.RawMessage) bool {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(rawEntry, &fields); err != nil {
		return false
	}
	for key := range fields {
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "reasoningeffort", "reasoning_effort", "accessmode", "access_mode":
			continue
		default:
			return true
		}
	}
	return false
}

type Store struct {
	*statestore.Store[Record]
}

func (s *Store) Put(key string, entry Record) error {
	if s == nil {
		return nil
	}
	key = strings.TrimSpace(key)
	entry = state.NormalizeClaudeWorkspaceProfileSnapshotRecord(entry)
	if key == "" {
		return fmt.Errorf("claude workspace profile snapshot requires key")
	}
	if state.ClaudeWorkspaceProfileSnapshotRecordEmpty(entry) {
		entries := s.Entries()
		delete(entries, key)
		return s.Replace(entries)
	}
	entries := s.Entries()
	entries[key] = entry
	return s.Replace(entries)
}

func (s *Store) Delete(key string) error {
	if s == nil {
		return nil
	}
	entries := s.Entries()
	delete(entries, strings.TrimSpace(key))
	return s.Replace(entries)
}
