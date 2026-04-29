package claudeworkspaceprofile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const (
	StateVersion  = 1
	StateFileName = "claude-workspace-profile-state.json"
)

type StateFile struct {
	Version int                                                   `json:"version"`
	Entries map[string]state.ClaudeWorkspaceProfileSnapshotRecord `json:"entries,omitempty"`
}

type Store struct {
	path    string
	entries map[string]state.ClaudeWorkspaceProfileSnapshotRecord
	dirty   bool
}

func NewStore(path string) *Store {
	return &Store{
		path:    strings.TrimSpace(path),
		entries: map[string]state.ClaudeWorkspaceProfileSnapshotRecord{},
	}
}

func StatePath(stateDir string) string {
	stateDir = strings.TrimSpace(stateDir)
	if stateDir == "" {
		return ""
	}
	return filepath.Join(stateDir, StateFileName)
}

func LoadStore(path string) (*Store, error) {
	store := NewStore(path)
	if store.path == "" {
		return store, nil
	}
	raw, err := os.ReadFile(store.path)
	if err != nil {
		if os.IsNotExist(err) {
			return store, nil
		}
		return nil, err
	}
	var persisted StateFile
	if err := json.Unmarshal(raw, &persisted); err != nil {
		return nil, err
	}
	if persisted.Version == 0 {
		persisted.Version = StateVersion
	}
	if persisted.Version != StateVersion {
		return nil, fmt.Errorf("unsupported claude workspace profile state version: %d", persisted.Version)
	}
	for key, entry := range persisted.Entries {
		key = strings.TrimSpace(key)
		entry = state.NormalizeClaudeWorkspaceProfileSnapshotRecord(entry)
		if key == "" || state.ClaudeWorkspaceProfileSnapshotRecordEmpty(entry) {
			store.dirty = true
			continue
		}
		store.entries[key] = entry
	}
	return store, nil
}

func (s *Store) Entries() map[string]state.ClaudeWorkspaceProfileSnapshotRecord {
	if s == nil || len(s.entries) == 0 {
		return map[string]state.ClaudeWorkspaceProfileSnapshotRecord{}
	}
	values := make(map[string]state.ClaudeWorkspaceProfileSnapshotRecord, len(s.entries))
	for key, entry := range s.entries {
		values[key] = entry
	}
	return values
}

func (s *Store) Get(key string) (state.ClaudeWorkspaceProfileSnapshotRecord, bool) {
	if s == nil {
		return state.ClaudeWorkspaceProfileSnapshotRecord{}, false
	}
	entry, ok := s.entries[strings.TrimSpace(key)]
	if !ok {
		return state.ClaudeWorkspaceProfileSnapshotRecord{}, false
	}
	return entry, true
}

func (s *Store) Put(key string, entry state.ClaudeWorkspaceProfileSnapshotRecord) error {
	if s == nil {
		return nil
	}
	key = strings.TrimSpace(key)
	entry = state.NormalizeClaudeWorkspaceProfileSnapshotRecord(entry)
	if key == "" {
		return fmt.Errorf("claude workspace profile snapshot requires key")
	}
	if state.ClaudeWorkspaceProfileSnapshotRecordEmpty(entry) {
		delete(s.entries, key)
		return s.Save()
	}
	s.entries[key] = entry
	return s.Save()
}

func (s *Store) Delete(key string) error {
	if s == nil {
		return nil
	}
	delete(s.entries, strings.TrimSpace(key))
	return s.Save()
}

func (s *Store) Save() error {
	if s == nil || s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	persisted := StateFile{
		Version: StateVersion,
		Entries: s.Entries(),
	}
	raw, err := json.MarshalIndent(persisted, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmpFile, err := os.CreateTemp(filepath.Dir(s.path), filepath.Base(s.path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)
	if err := tmpFile.Chmod(0o600); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if _, err := tmpFile.Write(raw); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		return err
	}
	s.dirty = false
	return nil
}

func (s *Store) Dirty() bool {
	if s == nil {
		return false
	}
	return s.dirty
}
