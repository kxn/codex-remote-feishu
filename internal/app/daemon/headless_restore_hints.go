package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	headlessRestoreHintsStateVersion = 1
	headlessRestoreHintsStateFile    = "headless-restore-hints.json"
)

type HeadlessRestoreHint struct {
	SurfaceSessionID string    `json:"surfaceSessionID"`
	GatewayID        string    `json:"gatewayID,omitempty"`
	ChatID           string    `json:"chatID,omitempty"`
	ActorUserID      string    `json:"actorUserID,omitempty"`
	ThreadID         string    `json:"threadID"`
	ThreadTitle      string    `json:"threadTitle,omitempty"`
	ThreadCWD        string    `json:"threadCWD,omitempty"`
	UpdatedAt        time.Time `json:"updatedAt,omitempty"`
}

type headlessRestoreHintsState struct {
	Version int                            `json:"version"`
	Entries map[string]HeadlessRestoreHint `json:"entries,omitempty"`
}

type headlessRestoreHintStore struct {
	path    string
	entries map[string]HeadlessRestoreHint
}

func headlessRestoreHintsStatePath(stateDir string) string {
	stateDir = strings.TrimSpace(stateDir)
	if stateDir == "" {
		return ""
	}
	return filepath.Join(stateDir, headlessRestoreHintsStateFile)
}

func loadHeadlessRestoreHintStore(path string) (*headlessRestoreHintStore, error) {
	store := &headlessRestoreHintStore{
		path:    strings.TrimSpace(path),
		entries: map[string]HeadlessRestoreHint{},
	}
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
	var state headlessRestoreHintsState
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, err
	}
	if state.Version == 0 {
		state.Version = headlessRestoreHintsStateVersion
	}
	if state.Version != headlessRestoreHintsStateVersion {
		return nil, fmt.Errorf("unsupported headless restore hints state version: %d", state.Version)
	}
	for key, hint := range state.Entries {
		normalized, ok := normalizeHeadlessRestoreHint(hint)
		if !ok {
			continue
		}
		store.entries[key] = normalized
	}
	return store, nil
}

func (s *headlessRestoreHintStore) Entries() map[string]HeadlessRestoreHint {
	if s == nil || len(s.entries) == 0 {
		return map[string]HeadlessRestoreHint{}
	}
	values := make(map[string]HeadlessRestoreHint, len(s.entries))
	for key, hint := range s.entries {
		values[key] = hint
	}
	return values
}

func (s *headlessRestoreHintStore) Get(surfaceID string) (HeadlessRestoreHint, bool) {
	if s == nil {
		return HeadlessRestoreHint{}, false
	}
	hint, ok := s.entries[strings.TrimSpace(surfaceID)]
	if !ok {
		return HeadlessRestoreHint{}, false
	}
	return hint, true
}

func (s *headlessRestoreHintStore) Put(hint HeadlessRestoreHint) error {
	if s == nil {
		return nil
	}
	normalized, ok := normalizeHeadlessRestoreHint(hint)
	if !ok {
		return fmt.Errorf("headless restore hint requires surface id and thread id")
	}
	s.entries[normalized.SurfaceSessionID] = normalized
	return s.save()
}

func (s *headlessRestoreHintStore) Delete(surfaceID string) error {
	if s == nil {
		return nil
	}
	delete(s.entries, strings.TrimSpace(surfaceID))
	return s.save()
}

func (s *headlessRestoreHintStore) save() error {
	if s == nil || s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	state := headlessRestoreHintsState{
		Version: headlessRestoreHintsStateVersion,
		Entries: s.Entries(),
	}
	raw, err := json.MarshalIndent(state, "", "  ")
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
	return os.Rename(tmpPath, s.path)
}

func normalizeHeadlessRestoreHint(hint HeadlessRestoreHint) (HeadlessRestoreHint, bool) {
	hint.SurfaceSessionID = strings.TrimSpace(hint.SurfaceSessionID)
	hint.GatewayID = strings.TrimSpace(hint.GatewayID)
	hint.ChatID = strings.TrimSpace(hint.ChatID)
	hint.ActorUserID = strings.TrimSpace(hint.ActorUserID)
	hint.ThreadID = strings.TrimSpace(hint.ThreadID)
	hint.ThreadCWD = strings.TrimSpace(hint.ThreadCWD)
	hint.ThreadTitle = normalizeResumeThreadTitle(hint.ThreadTitle, hint.ThreadID, hint.ThreadCWD, "")
	if hint.SurfaceSessionID == "" || hint.ThreadID == "" {
		return HeadlessRestoreHint{}, false
	}
	if !hint.UpdatedAt.IsZero() {
		hint.UpdatedAt = hint.UpdatedAt.UTC()
	}
	return hint, true
}
