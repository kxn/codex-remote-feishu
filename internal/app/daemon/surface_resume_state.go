package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const (
	surfaceResumeStateVersion = 1
	surfaceResumeStateFile    = "surface-resume-state.json"
)

type SurfaceResumeEntry struct {
	SurfaceSessionID   string    `json:"surfaceSessionID"`
	GatewayID          string    `json:"gatewayID,omitempty"`
	ChatID             string    `json:"chatID,omitempty"`
	ActorUserID        string    `json:"actorUserID,omitempty"`
	ProductMode        string    `json:"productMode,omitempty"`
	ResumeInstanceID   string    `json:"resumeInstanceID,omitempty"`
	ResumeThreadID     string    `json:"resumeThreadID,omitempty"`
	ResumeThreadTitle  string    `json:"resumeThreadTitle,omitempty"`
	ResumeThreadCWD    string    `json:"resumeThreadCWD,omitempty"`
	ResumeWorkspaceKey string    `json:"resumeWorkspaceKey,omitempty"`
	ResumeRouteMode    string    `json:"resumeRouteMode,omitempty"`
	ResumeHeadless     bool      `json:"resumeHeadless,omitempty"`
	UpdatedAt          time.Time `json:"updatedAt,omitempty"`
}

type surfaceResumeState struct {
	Version int                           `json:"version"`
	Entries map[string]SurfaceResumeEntry `json:"entries,omitempty"`
}

type surfaceResumeStore struct {
	path    string
	entries map[string]SurfaceResumeEntry
}

func surfaceResumeStatePath(stateDir string) string {
	stateDir = strings.TrimSpace(stateDir)
	if stateDir == "" {
		return ""
	}
	return filepath.Join(stateDir, surfaceResumeStateFile)
}

func loadSurfaceResumeStore(path string) (*surfaceResumeStore, error) {
	store := &surfaceResumeStore{
		path:    strings.TrimSpace(path),
		entries: map[string]SurfaceResumeEntry{},
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
	var persisted surfaceResumeState
	if err := json.Unmarshal(raw, &persisted); err != nil {
		return nil, err
	}
	if persisted.Version == 0 {
		persisted.Version = surfaceResumeStateVersion
	}
	if persisted.Version != surfaceResumeStateVersion {
		return nil, fmt.Errorf("unsupported surface resume state version: %d", persisted.Version)
	}
	for key, entry := range persisted.Entries {
		normalized, ok := normalizeSurfaceResumeEntry(entry)
		if !ok {
			continue
		}
		store.entries[key] = normalized
	}
	return store, nil
}

func (s *surfaceResumeStore) Entries() map[string]SurfaceResumeEntry {
	if s == nil || len(s.entries) == 0 {
		return map[string]SurfaceResumeEntry{}
	}
	values := make(map[string]SurfaceResumeEntry, len(s.entries))
	for key, entry := range s.entries {
		values[key] = entry
	}
	return values
}

func (s *surfaceResumeStore) Get(surfaceID string) (SurfaceResumeEntry, bool) {
	if s == nil {
		return SurfaceResumeEntry{}, false
	}
	entry, ok := s.entries[strings.TrimSpace(surfaceID)]
	if !ok {
		return SurfaceResumeEntry{}, false
	}
	return entry, true
}

func (s *surfaceResumeStore) Put(entry SurfaceResumeEntry) error {
	if s == nil {
		return nil
	}
	normalized, ok := normalizeSurfaceResumeEntry(entry)
	if !ok {
		return fmt.Errorf("surface resume entry requires surface id")
	}
	s.entries[normalized.SurfaceSessionID] = normalized
	return s.save()
}

func (s *surfaceResumeStore) Delete(surfaceID string) error {
	if s == nil {
		return nil
	}
	delete(s.entries, strings.TrimSpace(surfaceID))
	return s.save()
}

func (s *surfaceResumeStore) save() error {
	if s == nil || s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	persisted := surfaceResumeState{
		Version: surfaceResumeStateVersion,
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
	return os.Rename(tmpPath, s.path)
}

func normalizeSurfaceResumeEntry(entry SurfaceResumeEntry) (SurfaceResumeEntry, bool) {
	entry.SurfaceSessionID = strings.TrimSpace(entry.SurfaceSessionID)
	entry.GatewayID = strings.TrimSpace(entry.GatewayID)
	entry.ChatID = strings.TrimSpace(entry.ChatID)
	entry.ActorUserID = strings.TrimSpace(entry.ActorUserID)
	entry.ProductMode = string(state.NormalizeProductMode(state.ProductMode(strings.TrimSpace(entry.ProductMode))))
	entry.ResumeInstanceID = strings.TrimSpace(entry.ResumeInstanceID)
	entry.ResumeThreadID = strings.TrimSpace(entry.ResumeThreadID)
	entry.ResumeThreadCWD = state.NormalizeWorkspaceKey(entry.ResumeThreadCWD)
	entry.ResumeWorkspaceKey = state.NormalizeWorkspaceKey(entry.ResumeWorkspaceKey)
	entry.ResumeThreadTitle = normalizeResumeThreadTitle(entry.ResumeThreadTitle, entry.ResumeThreadID, entry.ResumeThreadCWD, entry.ResumeWorkspaceKey)
	entry.ResumeRouteMode = strings.TrimSpace(entry.ResumeRouteMode)
	if entry.SurfaceSessionID == "" {
		return SurfaceResumeEntry{}, false
	}
	if !entry.UpdatedAt.IsZero() {
		entry.UpdatedAt = entry.UpdatedAt.UTC()
	}
	return entry, true
}

func sameSurfaceResumeEntryContent(left, right SurfaceResumeEntry) bool {
	return strings.TrimSpace(left.SurfaceSessionID) == strings.TrimSpace(right.SurfaceSessionID) &&
		strings.TrimSpace(left.GatewayID) == strings.TrimSpace(right.GatewayID) &&
		strings.TrimSpace(left.ChatID) == strings.TrimSpace(right.ChatID) &&
		strings.TrimSpace(left.ActorUserID) == strings.TrimSpace(right.ActorUserID) &&
		strings.TrimSpace(left.ProductMode) == strings.TrimSpace(right.ProductMode) &&
		strings.TrimSpace(left.ResumeInstanceID) == strings.TrimSpace(right.ResumeInstanceID) &&
		strings.TrimSpace(left.ResumeThreadID) == strings.TrimSpace(right.ResumeThreadID) &&
		strings.TrimSpace(left.ResumeThreadTitle) == strings.TrimSpace(right.ResumeThreadTitle) &&
		state.NormalizeWorkspaceKey(left.ResumeThreadCWD) == state.NormalizeWorkspaceKey(right.ResumeThreadCWD) &&
		state.NormalizeWorkspaceKey(left.ResumeWorkspaceKey) == state.NormalizeWorkspaceKey(right.ResumeWorkspaceKey) &&
		strings.TrimSpace(left.ResumeRouteMode) == strings.TrimSpace(right.ResumeRouteMode) &&
		left.ResumeHeadless == right.ResumeHeadless
}
