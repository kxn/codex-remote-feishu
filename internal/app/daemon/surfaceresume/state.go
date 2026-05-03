package surfaceresume

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/core/threadtitle"
)

const (
	StateVersion  = 1
	StateFileName = "surface-resume-state.json"
)

type Entry struct {
	SurfaceSessionID   string    `json:"surfaceSessionID"`
	GatewayID          string    `json:"gatewayID,omitempty"`
	ChatID             string    `json:"chatID,omitempty"`
	ActorUserID        string    `json:"actorUserID,omitempty"`
	ProductMode        string    `json:"productMode,omitempty"`
	Backend            string    `json:"backend,omitempty"`
	CodexProviderID    string    `json:"codexProviderID,omitempty"`
	ClaudeProfileID    string    `json:"claudeProfileID,omitempty"`
	Verbosity          string    `json:"verbosity,omitempty"`
	PlanMode           string    `json:"planMode,omitempty"`
	ResumeInstanceID   string    `json:"resumeInstanceID,omitempty"`
	ResumeThreadID     string    `json:"resumeThreadID,omitempty"`
	ResumeThreadTitle  string    `json:"resumeThreadTitle,omitempty"`
	ResumeThreadCWD    string    `json:"resumeThreadCWD,omitempty"`
	ResumeWorkspaceKey string    `json:"resumeWorkspaceKey,omitempty"`
	ResumeRouteMode    string    `json:"resumeRouteMode,omitempty"`
	ResumeHeadless     bool      `json:"resumeHeadless,omitempty"`
	UpdatedAt          time.Time `json:"updatedAt,omitempty"`
}

type StateFile struct {
	Version int              `json:"version"`
	Entries map[string]Entry `json:"entries,omitempty"`
}

type Store struct {
	path    string
	entries map[string]Entry
	dirty   bool
}

func NewStore(path string) *Store {
	return &Store{
		path:    strings.TrimSpace(path),
		entries: map[string]Entry{},
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
		return nil, fmt.Errorf("unsupported surface resume state version: %d", persisted.Version)
	}
	for key, entry := range persisted.Entries {
		normalized, ok := NormalizeEntry(entry)
		if !ok {
			store.dirty = true
			continue
		}
		if strings.TrimSpace(key) != normalized.SurfaceSessionID {
			store.dirty = true
		}
		store.entries[key] = normalized
	}
	if canonical, changed := CanonicalizeEntries(store.entries); changed {
		store.entries = canonical
		store.dirty = true
	}
	return store, nil
}

func (s *Store) Entries() map[string]Entry {
	if s == nil || len(s.entries) == 0 {
		return map[string]Entry{}
	}
	values := make(map[string]Entry, len(s.entries))
	for key, entry := range s.entries {
		values[key] = entry
	}
	return values
}

func (s *Store) Get(surfaceID string) (Entry, bool) {
	if s == nil {
		return Entry{}, false
	}
	entry, ok := s.entries[strings.TrimSpace(surfaceID)]
	if !ok {
		return Entry{}, false
	}
	return entry, true
}

func (s *Store) Put(entry Entry) error {
	if s == nil {
		return nil
	}
	normalized, ok := NormalizeEntry(entry)
	if !ok {
		return fmt.Errorf("surface resume entry requires surface id")
	}
	s.entries[normalized.SurfaceSessionID] = normalized
	if canonical, changed := CanonicalizeEntries(s.entries); changed {
		s.entries = canonical
		s.dirty = true
	}
	return s.Save()
}

func (s *Store) Delete(surfaceID string) error {
	if s == nil {
		return nil
	}
	delete(s.entries, strings.TrimSpace(surfaceID))
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

func NormalizeEntry(entry Entry) (Entry, bool) {
	entry.SurfaceSessionID = strings.TrimSpace(entry.SurfaceSessionID)
	entry.GatewayID = strings.TrimSpace(entry.GatewayID)
	entry.ChatID = strings.TrimSpace(entry.ChatID)
	entry.ActorUserID = strings.TrimSpace(entry.ActorUserID)
	entry.ProductMode = string(state.NormalizeProductMode(state.ProductMode(strings.TrimSpace(entry.ProductMode))))
	entry.CodexProviderID = strings.TrimSpace(entry.CodexProviderID)
	entry.ClaudeProfileID = strings.TrimSpace(entry.ClaudeProfileID)
	backend := agentproto.Backend(strings.TrimSpace(entry.Backend))
	if state.IsHeadlessProductMode(state.ProductMode(entry.ProductMode)) &&
		entry.ClaudeProfileID != "" &&
		(strings.TrimSpace(entry.Backend) == "" ||
			(agentproto.NormalizeBackend(backend) == agentproto.BackendCodex && strings.TrimSpace(entry.CodexProviderID) == "")) {
		backend = agentproto.BackendClaude
	}
	rawContract := state.NormalizeSurfaceBackendContract(state.SurfaceBackendContract{
		ProductMode:     state.ProductMode(entry.ProductMode),
		Backend:         backend,
		CodexProviderID: entry.CodexProviderID,
		ClaudeProfileID: entry.ClaudeProfileID,
	})
	entry.Backend = string(rawContract.Backend)
	entry.CodexProviderID = state.EffectiveSurfaceCodexProviderID(rawContract)
	entry.ClaudeProfileID = state.EffectiveSurfaceClaudeProfileID(rawContract)
	entry.Verbosity = string(state.NormalizeSurfaceVerbosity(state.SurfaceVerbosity(strings.TrimSpace(entry.Verbosity))))
	entry.PlanMode = ""
	entry.ResumeInstanceID = strings.TrimSpace(entry.ResumeInstanceID)
	entry.ResumeThreadID = strings.TrimSpace(entry.ResumeThreadID)
	entry.ResumeThreadCWD = state.NormalizeWorkspaceKey(entry.ResumeThreadCWD)
	entry.ResumeWorkspaceKey = state.NormalizeWorkspaceKey(entry.ResumeWorkspaceKey)
	entry.ResumeThreadTitle = threadtitle.NormalizeStoredInput(entry.ResumeThreadTitle, threadtitle.Context{
		ThreadID:     entry.ResumeThreadID,
		ThreadCWD:    entry.ResumeThreadCWD,
		WorkspaceKey: entry.ResumeWorkspaceKey,
	})
	entry.ResumeRouteMode = strings.TrimSpace(entry.ResumeRouteMode)
	if entry.ResumeThreadID == "" && entry.ResumeRouteMode == string(state.RouteModePinned) {
		if entry.ResumeWorkspaceKey != "" {
			entry.ResumeRouteMode = string(state.RouteModeNewThreadReady)
		} else {
			entry.ResumeRouteMode = string(state.RouteModeUnbound)
		}
	}
	if entry.ResumeThreadID == "" {
		entry.ResumeHeadless = false
	}
	if !state.IsHeadlessProductMode(state.ProductMode(entry.ProductMode)) {
		entry.ResumeHeadless = false
	}
	if entry.SurfaceSessionID == "" {
		return Entry{}, false
	}
	if !entry.UpdatedAt.IsZero() {
		entry.UpdatedAt = entry.UpdatedAt.UTC()
	}
	return entry, true
}

func SameEntryContent(left, right Entry) bool {
	return strings.TrimSpace(left.SurfaceSessionID) == strings.TrimSpace(right.SurfaceSessionID) &&
		strings.TrimSpace(left.GatewayID) == strings.TrimSpace(right.GatewayID) &&
		strings.TrimSpace(left.ChatID) == strings.TrimSpace(right.ChatID) &&
		strings.TrimSpace(left.ActorUserID) == strings.TrimSpace(right.ActorUserID) &&
		strings.TrimSpace(left.ProductMode) == strings.TrimSpace(right.ProductMode) &&
		state.NormalizeHeadlessBackend(agentproto.Backend(left.Backend)) == state.NormalizeHeadlessBackend(agentproto.Backend(right.Backend)) &&
		strings.TrimSpace(left.CodexProviderID) == strings.TrimSpace(right.CodexProviderID) &&
		strings.TrimSpace(left.ClaudeProfileID) == strings.TrimSpace(right.ClaudeProfileID) &&
		strings.TrimSpace(left.Verbosity) == strings.TrimSpace(right.Verbosity) &&
		strings.TrimSpace(left.PlanMode) == strings.TrimSpace(right.PlanMode) &&
		strings.TrimSpace(left.ResumeInstanceID) == strings.TrimSpace(right.ResumeInstanceID) &&
		strings.TrimSpace(left.ResumeThreadID) == strings.TrimSpace(right.ResumeThreadID) &&
		strings.TrimSpace(left.ResumeThreadTitle) == strings.TrimSpace(right.ResumeThreadTitle) &&
		state.NormalizeWorkspaceKey(left.ResumeThreadCWD) == state.NormalizeWorkspaceKey(right.ResumeThreadCWD) &&
		state.NormalizeWorkspaceKey(left.ResumeWorkspaceKey) == state.NormalizeWorkspaceKey(right.ResumeWorkspaceKey) &&
		strings.TrimSpace(left.ResumeRouteMode) == strings.TrimSpace(right.ResumeRouteMode) &&
		left.ResumeHeadless == right.ResumeHeadless
}
