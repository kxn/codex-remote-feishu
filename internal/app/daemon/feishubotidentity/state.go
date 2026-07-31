package feishubotidentity

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	StateVersion  = 1
	StateFileName = "feishu-bot-identities.json"
)

type Record struct {
	GatewayID  string             `json:"gatewayID"`
	AppID      string             `json:"appID"`
	Generation uint64             `json:"generation"`
	UpdatedAt  time.Time          `json:"updatedAt,omitempty"`
	Pending    *PendingTransition `json:"pending,omitempty"`
}

type PendingTransition struct {
	DesiredAppID string    `json:"desiredAppID,omitempty"`
	StartedAt    time.Time `json:"startedAt,omitempty"`
}

type StateFile struct {
	Version int               `json:"version"`
	Entries map[string]Record `json:"entries,omitempty"`
}

type Store struct {
	path    string
	entries map[string]Record
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
		entries: map[string]Record{},
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
		return nil, fmt.Errorf("unsupported feishu bot identity state version: %d", persisted.Version)
	}
	for key, record := range persisted.Entries {
		normalized, ok := NormalizeRecord(record)
		if !ok {
			store.dirty = true
			continue
		}
		if strings.TrimSpace(key) != normalized.GatewayID || !sameRecord(record, normalized) {
			store.dirty = true
		}
		store.entries[normalized.GatewayID] = normalized
	}
	return store, nil
}

func NormalizeRecord(record Record) (Record, bool) {
	record.GatewayID = strings.TrimSpace(record.GatewayID)
	record.AppID = strings.TrimSpace(record.AppID)
	if record.GatewayID == "" || record.AppID == "" {
		return Record{}, false
	}
	if record.Generation == 0 {
		record.Generation = 1
	}
	if !record.UpdatedAt.IsZero() {
		record.UpdatedAt = record.UpdatedAt.UTC()
	}
	if record.Pending != nil {
		pending := PendingTransition{
			DesiredAppID: strings.TrimSpace(record.Pending.DesiredAppID),
			StartedAt:    record.Pending.StartedAt,
		}
		if !pending.StartedAt.IsZero() {
			pending.StartedAt = pending.StartedAt.UTC()
		}
		record.Pending = &pending
	}
	return record, true
}

func (s *Store) Entries() map[string]Record {
	if s == nil || len(s.entries) == 0 {
		return map[string]Record{}
	}
	entries := make(map[string]Record, len(s.entries))
	for key, record := range s.entries {
		entries[key] = cloneRecord(record)
	}
	return entries
}

func (s *Store) Get(gatewayID string) (Record, bool) {
	if s == nil {
		return Record{}, false
	}
	record, ok := s.entries[strings.TrimSpace(gatewayID)]
	return cloneRecord(record), ok
}

func (s *Store) Put(record Record) error {
	if s == nil {
		return nil
	}
	normalized, ok := NormalizeRecord(record)
	if !ok {
		return fmt.Errorf("feishu bot identity requires gateway id and app id")
	}
	entries := s.Entries()
	entries[normalized.GatewayID] = normalized
	return s.replaceEntries(entries)
}

func (s *Store) Delete(gatewayID string) error {
	if s == nil {
		return nil
	}
	gatewayID = strings.TrimSpace(gatewayID)
	if gatewayID == "" {
		return nil
	}
	if _, ok := s.entries[gatewayID]; !ok {
		return nil
	}
	entries := s.Entries()
	delete(entries, gatewayID)
	return s.replaceEntries(entries)
}

func (s *Store) replaceEntries(entries map[string]Record) error {
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

func sameEntries(left, right map[string]Record) bool {
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

func sameRecord(left, right Record) bool {
	if strings.TrimSpace(left.GatewayID) == strings.TrimSpace(right.GatewayID) &&
		strings.TrimSpace(left.AppID) == strings.TrimSpace(right.AppID) &&
		left.Generation == right.Generation &&
		left.UpdatedAt.Equal(right.UpdatedAt) {
		return samePending(left.Pending, right.Pending)
	}
	return false
}

func cloneRecord(record Record) Record {
	if record.Pending == nil {
		return record
	}
	pending := *record.Pending
	record.Pending = &pending
	return record
}

func samePending(left, right *PendingTransition) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return strings.TrimSpace(left.DesiredAppID) == strings.TrimSpace(right.DesiredAppID) &&
		left.StartedAt.Equal(right.StartedAt)
}

func (s *Store) Save() error {
	if s == nil || s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	persisted := StateFile{Version: StateVersion, Entries: s.Entries()}
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
	return s != nil && s.dirty
}
