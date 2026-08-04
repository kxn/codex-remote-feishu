package feishuroomstate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const (
	StateVersion       = 2
	legacyStateVersion = 1
	// Keep the original path so version 1 upgrades in place without a second store.
	StateFileName = "feishu-room-primary.json"
)

type StateFile struct {
	Version int                                    `json:"version"`
	Entries map[string]state.FeishuRoomStateRecord `json:"entries,omitempty"`
}

type Store struct {
	path    string
	entries map[string]state.FeishuRoomStateRecord
	dirty   bool
}

func StatePath(stateDir string) string {
	stateDir = strings.TrimSpace(stateDir)
	if stateDir == "" {
		return ""
	}
	return filepath.Join(stateDir, StateFileName)
}

func NewStore(path string) *Store {
	return &Store{
		path:    strings.TrimSpace(path),
		entries: map[string]state.FeishuRoomStateRecord{},
	}
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
		persisted.Version = legacyStateVersion
	}
	if persisted.Version != legacyStateVersion && persisted.Version != StateVersion {
		return nil, fmt.Errorf("unsupported feishu room state version: %d", persisted.Version)
	}
	if persisted.Version == legacyStateVersion {
		store.dirty = true
	}
	for key, record := range persisted.Entries {
		normalized, ok := state.NormalizeFeishuRoomStateRecord(record)
		if !ok {
			store.dirty = true
			continue
		}
		canonicalKey := state.FeishuRoomKey(normalized.RoomID)
		if canonicalKey == "" {
			store.dirty = true
			continue
		}
		if strings.TrimSpace(key) != canonicalKey {
			store.dirty = true
		}
		if !sameRecord(record, normalized) {
			store.dirty = true
		}
		store.entries[canonicalKey] = normalized
	}
	return store, nil
}

func (s *Store) Entries() map[string]state.FeishuRoomStateRecord {
	if s == nil || len(s.entries) == 0 {
		return map[string]state.FeishuRoomStateRecord{}
	}
	values := make(map[string]state.FeishuRoomStateRecord, len(s.entries))
	for key, record := range s.entries {
		values[key] = record
	}
	return values
}

func (s *Store) Get(key string) (state.FeishuRoomStateRecord, bool) {
	if s == nil {
		return state.FeishuRoomStateRecord{}, false
	}
	record, ok := s.entries[state.FeishuRoomKey(key)]
	if !ok {
		return state.FeishuRoomStateRecord{}, false
	}
	return record, true
}

func (s *Store) Put(record state.FeishuRoomStateRecord) error {
	if s == nil {
		return nil
	}
	normalized, ok := state.NormalizeFeishuRoomStateRecord(record)
	if !ok {
		return fmt.Errorf("feishu room state requires room identity")
	}
	key := state.FeishuRoomKey(normalized.RoomID)
	if key == "" {
		return fmt.Errorf("feishu room state requires room identity")
	}
	entries := s.Entries()
	entries[key] = normalized
	return s.replaceEntries(entries)
}

func (s *Store) Delete(key string) error {
	if s == nil {
		return nil
	}
	key = state.FeishuRoomKey(key)
	if key == "" {
		return nil
	}
	if _, ok := s.entries[key]; !ok {
		return nil
	}
	entries := s.Entries()
	delete(entries, key)
	return s.replaceEntries(entries)
}

func (s *Store) ReplaceAll(records []state.FeishuRoomStateRecord) error {
	if s == nil {
		return nil
	}
	entries := make(map[string]state.FeishuRoomStateRecord, len(records))
	for _, record := range records {
		normalized, ok := state.NormalizeFeishuRoomStateRecord(record)
		if !ok {
			return fmt.Errorf("feishu room state requires room identity")
		}
		entries[state.FeishuRoomKey(normalized.RoomID)] = normalized
	}
	return s.replaceEntries(entries)
}

func (s *Store) replaceEntries(entries map[string]state.FeishuRoomStateRecord) error {
	if sameEntries(s.entries, entries) {
		return nil
	}
	previous := s.entries
	s.entries = entries
	if err := s.Save(); err != nil {
		s.entries = previous
		return err
	}
	return nil
}

func sameEntries(left, right map[string]state.FeishuRoomStateRecord) bool {
	if len(left) != len(right) {
		return false
	}
	for key, leftRecord := range left {
		rightRecord, ok := right[key]
		if !ok || !sameRecord(leftRecord, rightRecord) {
			return false
		}
	}
	return true
}

func sameRecord(left, right state.FeishuRoomStateRecord) bool {
	return left.RoomID == right.RoomID &&
		left.ChatID == right.ChatID &&
		left.WorkspaceKey == right.WorkspaceKey &&
		left.WorkspaceUpdatedBy == right.WorkspaceUpdatedBy &&
		left.WorkspaceUpdatedAt.Equal(right.WorkspaceUpdatedAt) &&
		left.WorkspaceResetGeneration == right.WorkspaceResetGeneration &&
		left.PrimaryGatewayID == right.PrimaryGatewayID &&
		left.PrimaryUpdatedBy == right.PrimaryUpdatedBy &&
		left.PrimaryUpdatedAt.Equal(right.PrimaryUpdatedAt) &&
		intPointerEqual(left.ConcurrencyLimit, right.ConcurrencyLimit)
}

func intPointerEqual(left, right *int) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
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
