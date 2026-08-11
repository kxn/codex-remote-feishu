package profilecontextstate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/atomicfile"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const (
	StateVersion  = 1
	StateFileName = "profile-context-preferences.json"
)

var (
	ErrPreconditionRequired = errors.New("profile preference precondition required")
	ErrETagMismatch         = errors.New("profile preference etag mismatch")
)

type Revision struct {
	ProfileID string `json:"profileID"`
	Revision  uint64 `json:"revision"`
	Mode      string `json:"mode"`
}

type Record struct {
	ProfileID       string     `json:"profileID"`
	CurrentRevision uint64     `json:"currentRevision"`
	Revisions       []Revision `json:"revisions"`
}

type StateFile struct {
	Version int               `json:"version"`
	Codex   map[string]Record `json:"codex,omitempty"`
	Claude  map[string]Record `json:"claude,omitempty"`
}

type Store struct {
	path   string
	codex  map[string]Record
	claude map[string]Record
	dirty  bool
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
		path:   strings.TrimSpace(path),
		codex:  map[string]Record{},
		claude: map[string]Record{},
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
		return nil, fmt.Errorf("parse profile context preference state: %w", err)
	}
	if persisted.Version != StateVersion {
		return nil, fmt.Errorf("unsupported profile context preference state version: %d", persisted.Version)
	}
	store.codex, err = validateRecords(persisted.Codex, validateCodexMode)
	if err != nil {
		return nil, fmt.Errorf("validate codex profile preferences: %w", err)
	}
	store.claude, err = validateRecords(persisted.Claude, validateClaudeMode)
	if err != nil {
		return nil, fmt.Errorf("validate claude profile preferences: %w", err)
	}
	return store, nil
}

func (s *Store) EnsureCodexProfile(profileID, mode string) error {
	return s.ensure(&s.codex, profileID, mode, validateCodexMode)
}

func (s *Store) EnsureClaudeProfile(profileID, mode string) error {
	return s.ensure(&s.claude, profileID, mode, validateClaudeMode)
}

func (s *Store) ApplyInitialPreferences(codex, claude map[string]string) error {
	if s == nil {
		return fmt.Errorf("profile context preference store is unavailable")
	}
	previousCodex := cloneRecords(s.codex)
	previousClaude := cloneRecords(s.claude)
	changed := false
	for profileID, mode := range codex {
		added, err := applyInitialPreference(s.codex, profileID, mode, validateCodexMode)
		if err != nil {
			return err
		}
		changed = changed || added
	}
	for profileID, mode := range claude {
		added, err := applyInitialPreference(s.claude, profileID, mode, validateClaudeMode)
		if err != nil {
			return err
		}
		changed = changed || added
	}
	if !changed {
		return nil
	}
	if err := s.Save(); err != nil {
		s.codex = previousCodex
		s.claude = previousClaude
		return err
	}
	return nil
}

func (s *Store) CodexCurrent(profileID string) (state.ProfileContextPreference, bool) {
	return currentPreference(s.codex, profileID, state.CodexContextPreferenceETag)
}

func (s *Store) ClaudeCurrent(profileID string) (state.ProfileContextPreference, bool) {
	return currentPreference(s.claude, profileID, state.ClaudeContextPreferenceETag)
}

func (s *Store) CodexRevision(profileID string, revision uint64) (state.ProfileContextPreference, bool) {
	return revisionPreference(s.codex, profileID, revision, state.CodexContextPreferenceETag)
}

func (s *Store) ClaudeRevision(profileID string, revision uint64) (state.ProfileContextPreference, bool) {
	return revisionPreference(s.claude, profileID, revision, state.ClaudeContextPreferenceETag)
}

func (s *Store) UpdateCodex(profileID, mode, expectedETag string) (state.ProfileContextPreference, bool, error) {
	return s.update(&s.codex, profileID, mode, expectedETag, validateCodexMode, state.CodexContextPreferenceETag)
}

func (s *Store) UpdateClaude(profileID, mode, expectedETag string) (state.ProfileContextPreference, bool, error) {
	return s.update(&s.claude, profileID, mode, expectedETag, validateClaudeMode, state.ClaudeContextPreferenceETag)
}

func (s *Store) PruneCodexHistory(profileID string, retained map[uint64]struct{}) error {
	return s.prune(&s.codex, profileID, retained)
}

func (s *Store) PruneClaudeHistory(profileID string, retained map[uint64]struct{}) error {
	return s.prune(&s.claude, profileID, retained)
}

func (s *Store) RenameClaudeProfile(currentID, nextID string) error {
	currentID = strings.TrimSpace(currentID)
	nextID = strings.TrimSpace(nextID)
	if currentID == nextID {
		return nil
	}
	record, exists := s.claude[currentID]
	if !exists {
		return fmt.Errorf("claude profile context preference not found")
	}
	if _, conflict := s.claude[nextID]; conflict || nextID == "" {
		return fmt.Errorf("claude profile context preference rename conflict")
	}
	previous := cloneRecords(s.claude)
	delete(s.claude, currentID)
	record.ProfileID = nextID
	for index := range record.Revisions {
		record.Revisions[index].ProfileID = nextID
	}
	s.claude[nextID] = record
	if err := s.Save(); err != nil {
		s.claude = previous
		return err
	}
	return nil
}

func (s *Store) DeleteClaudeProfile(profileID string) error {
	return s.delete(&s.claude, profileID)
}

func (s *Store) DeleteCodexProfile(profileID string) error {
	return s.delete(&s.codex, profileID)
}

func (s *Store) Dirty() bool {
	return s != nil && s.dirty
}

func (s *Store) Save() error {
	if s == nil || s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	persisted := StateFile{Version: StateVersion, Codex: cloneRecords(s.codex), Claude: cloneRecords(s.claude)}
	raw, err := json.MarshalIndent(persisted, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := atomicfile.Write(s.path, raw, 0o600); err != nil {
		return err
	}
	s.dirty = false
	return nil
}

func (s *Store) ensure(records *map[string]Record, profileID, mode string, validateMode func(string) bool) error {
	profileID = strings.TrimSpace(profileID)
	mode = strings.TrimSpace(mode)
	if profileID == "" || !validateMode(mode) {
		return fmt.Errorf("invalid profile context preference")
	}
	if _, exists := (*records)[profileID]; exists {
		return nil
	}
	previous := cloneRecords(*records)
	(*records)[profileID] = Record{
		ProfileID:       profileID,
		CurrentRevision: 1,
		Revisions:       []Revision{{ProfileID: profileID, Revision: 1, Mode: mode}},
	}
	if err := s.Save(); err != nil {
		*records = previous
		return err
	}
	return nil
}

func (s *Store) update(
	records *map[string]Record,
	profileID, mode, expectedETag string,
	validateMode func(string) bool,
	etag func(string, uint64) string,
) (state.ProfileContextPreference, bool, error) {
	profileID = strings.TrimSpace(profileID)
	mode = strings.TrimSpace(mode)
	record, exists := (*records)[profileID]
	if !exists {
		return state.ProfileContextPreference{}, false, fmt.Errorf("profile context preference not found")
	}
	if strings.TrimSpace(expectedETag) == "" {
		return state.ProfileContextPreference{}, false, ErrPreconditionRequired
	}
	current, ok := currentRevision(record)
	if !ok {
		return state.ProfileContextPreference{}, false, fmt.Errorf("profile context preference current revision is missing")
	}
	if expectedETag != etag(profileID, current.Revision) {
		return state.ProfileContextPreference{}, false, ErrETagMismatch
	}
	if !validateMode(mode) {
		return state.ProfileContextPreference{}, false, fmt.Errorf("invalid profile context preference mode")
	}
	if current.Mode == mode {
		return publicPreference(current, etag), false, nil
	}
	previous := cloneRecords(*records)
	next := Revision{ProfileID: profileID, Revision: current.Revision + 1, Mode: mode}
	record.CurrentRevision = next.Revision
	record.Revisions = append(append([]Revision{}, record.Revisions...), next)
	(*records)[profileID] = record
	if err := s.Save(); err != nil {
		*records = previous
		return state.ProfileContextPreference{}, false, err
	}
	return publicPreference(next, etag), true, nil
}

func (s *Store) prune(records *map[string]Record, profileID string, retained map[uint64]struct{}) error {
	profileID = strings.TrimSpace(profileID)
	record, exists := (*records)[profileID]
	if !exists {
		return nil
	}
	revisions := make([]Revision, 0, len(record.Revisions))
	for _, revision := range record.Revisions {
		_, keep := retained[revision.Revision]
		if revision.Revision == record.CurrentRevision || keep {
			revisions = append(revisions, revision)
		}
	}
	if len(revisions) == len(record.Revisions) {
		return nil
	}
	previous := cloneRecords(*records)
	record.Revisions = revisions
	(*records)[profileID] = record
	if err := s.Save(); err != nil {
		*records = previous
		return err
	}
	return nil
}

func (s *Store) delete(records *map[string]Record, profileID string) error {
	profileID = strings.TrimSpace(profileID)
	if _, exists := (*records)[profileID]; !exists {
		return nil
	}
	previous := cloneRecords(*records)
	delete(*records, profileID)
	if err := s.Save(); err != nil {
		*records = previous
		return err
	}
	return nil
}

func currentPreference(records map[string]Record, profileID string, etag func(string, uint64) string) (state.ProfileContextPreference, bool) {
	record, ok := records[strings.TrimSpace(profileID)]
	if !ok {
		return state.ProfileContextPreference{}, false
	}
	revision, ok := currentRevision(record)
	if !ok {
		return state.ProfileContextPreference{}, false
	}
	return publicPreference(revision, etag), true
}

func revisionPreference(records map[string]Record, profileID string, revision uint64, etag func(string, uint64) string) (state.ProfileContextPreference, bool) {
	profileID = strings.TrimSpace(profileID)
	record, ok := records[profileID]
	if !ok {
		return state.ProfileContextPreference{}, false
	}
	for _, current := range record.Revisions {
		if current.Revision == revision {
			return publicPreference(current, etag), true
		}
	}
	return state.ProfileContextPreference{}, false
}

func publicPreference(revision Revision, etag func(string, uint64) string) state.ProfileContextPreference {
	return state.ProfileContextPreference{
		ProfileID: revision.ProfileID,
		Revision:  revision.Revision,
		ETag:      etag(revision.ProfileID, revision.Revision),
		Mode:      revision.Mode,
	}
}

func currentRevision(record Record) (Revision, bool) {
	for _, revision := range record.Revisions {
		if revision.ProfileID == record.ProfileID && revision.Revision == record.CurrentRevision {
			return revision, true
		}
	}
	return Revision{}, false
}

func validateRecords(records map[string]Record, validateMode func(string) bool) (map[string]Record, error) {
	validated := make(map[string]Record, len(records))
	for rawKey, record := range records {
		key := strings.TrimSpace(rawKey)
		record.ProfileID = strings.TrimSpace(record.ProfileID)
		if key == "" || rawKey != key || record.ProfileID != key || record.CurrentRevision == 0 {
			return nil, fmt.Errorf("invalid profile preference record %q", key)
		}
		seen := make(map[uint64]struct{}, len(record.Revisions))
		maxRevision := uint64(0)
		for _, revision := range record.Revisions {
			if revision.ProfileID != key || revision.Revision == 0 || revision.Mode != strings.TrimSpace(revision.Mode) || !validateMode(revision.Mode) {
				return nil, fmt.Errorf("invalid profile preference revision for %q", key)
			}
			if _, exists := seen[revision.Revision]; exists {
				return nil, fmt.Errorf("duplicate profile preference revision for %q", key)
			}
			seen[revision.Revision] = struct{}{}
			if revision.Revision > maxRevision {
				maxRevision = revision.Revision
			}
		}
		if _, ok := currentRevision(record); !ok || record.CurrentRevision != maxRevision {
			return nil, fmt.Errorf("missing current profile preference revision for %q", key)
		}
		validated[key] = record
	}
	return validated, nil
}

func cloneRecords(records map[string]Record) map[string]Record {
	cloned := make(map[string]Record, len(records))
	for key, record := range records {
		record.Revisions = append([]Revision{}, record.Revisions...)
		cloned[key] = record
	}
	return cloned
}

func validateCodexMode(mode string) bool {
	switch strings.TrimSpace(mode) {
	case state.CodexContextModeDefault, state.CodexContextModePrice272K, state.CodexContextModeExtended:
		return true
	default:
		return false
	}
}

func validateClaudeMode(mode string) bool {
	switch strings.TrimSpace(mode) {
	case state.ClaudeContextModeDefault, state.ClaudeContextModeExtended:
		return true
	default:
		return false
	}
}

func applyInitialPreference(records map[string]Record, profileID, mode string, validateMode func(string) bool) (bool, error) {
	profileID = strings.TrimSpace(profileID)
	mode = strings.TrimSpace(mode)
	if profileID == "" || !validateMode(mode) {
		return false, fmt.Errorf("invalid initial profile context preference")
	}
	if record, exists := records[profileID]; exists {
		current, ok := currentRevision(record)
		if !ok || current.Mode != mode {
			return false, fmt.Errorf("initial profile context preference conflicts for %s", profileID)
		}
		return false, nil
	}
	records[profileID] = Record{
		ProfileID:       profileID,
		CurrentRevision: 1,
		Revisions:       []Revision{{ProfileID: profileID, Revision: 1, Mode: mode}},
	}
	return true, nil
}
