package botcapabilitysettings

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
	StateFileName = "bot-capability-settings.json"
)

type StateFile struct {
	Version int                                          `json:"version"`
	Entries map[string]state.BotCapabilitySettingsRecord `json:"entries,omitempty"`
}

type Store struct {
	path    string
	entries map[string]state.BotCapabilitySettingsRecord
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
		entries: map[string]state.BotCapabilitySettingsRecord{},
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
		persisted.Version = StateVersion
	}
	if persisted.Version != StateVersion {
		return nil, fmt.Errorf("unsupported bot capability settings state version: %d", persisted.Version)
	}
	for key, record := range persisted.Entries {
		normalized, ok := state.NormalizeBotCapabilitySettingsRecord(record)
		if !ok {
			store.dirty = true
			continue
		}
		canonicalKey := state.BotCapabilitySettingsKey(normalized.GatewayID)
		if canonicalKey == "" {
			store.dirty = true
			continue
		}
		if strings.TrimSpace(key) != canonicalKey {
			store.dirty = true
		}
		store.entries[canonicalKey] = normalized
	}
	return store, nil
}

func (s *Store) Entries() map[string]state.BotCapabilitySettingsRecord {
	if s == nil || len(s.entries) == 0 {
		return map[string]state.BotCapabilitySettingsRecord{}
	}
	values := make(map[string]state.BotCapabilitySettingsRecord, len(s.entries))
	for key, record := range s.entries {
		values[key] = record
	}
	return values
}

func (s *Store) Get(key string) (state.BotCapabilitySettingsRecord, bool) {
	if s == nil {
		return state.BotCapabilitySettingsRecord{}, false
	}
	record, ok := s.entries[strings.TrimSpace(key)]
	if !ok {
		return state.BotCapabilitySettingsRecord{}, false
	}
	return record, true
}

func (s *Store) Put(record state.BotCapabilitySettingsRecord) error {
	if s == nil {
		return nil
	}
	record = state.CanonicalizeBotCapabilityProfileSelection(record)
	normalized, ok := state.NormalizeBotCapabilitySettingsRecord(record)
	if !ok {
		return fmt.Errorf("bot capability settings requires gateway id")
	}
	key := state.BotCapabilitySettingsKey(normalized.GatewayID)
	if key == "" {
		return fmt.Errorf("bot capability settings requires gateway id")
	}
	entries := s.Entries()
	entries[key] = normalized
	return s.replaceEntries(entries)
}

func (s *Store) ReplaceAll(records []state.BotCapabilitySettingsRecord) error {
	if s == nil {
		return nil
	}
	entries := make(map[string]state.BotCapabilitySettingsRecord, len(records))
	for _, record := range records {
		record = state.CanonicalizeBotCapabilityProfileSelection(record)
		normalized, ok := state.NormalizeBotCapabilitySettingsRecord(record)
		if !ok {
			return fmt.Errorf("bot capability settings requires gateway id")
		}
		entries[state.BotCapabilitySettingsKey(normalized.GatewayID)] = normalized
	}
	return s.replaceEntries(entries)
}

func (s *Store) replaceEntries(entries map[string]state.BotCapabilitySettingsRecord) error {
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

func sameEntries(left, right map[string]state.BotCapabilitySettingsRecord) bool {
	if len(left) != len(right) {
		return false
	}
	for key, leftRecord := range left {
		rightRecord, ok := right[key]
		if !ok || leftRecord != rightRecord {
			return false
		}
	}
	return true
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
