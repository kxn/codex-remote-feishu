package surfaceresume

import (
	"fmt"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/statestore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/core/threadtitle"
)

const (
	StateVersion                        = 1
	StateFileName                       = "surface-resume-state.json"
	CodexProfileSelectionStatusConflict = "profile_selection_conflict"
)

type Entry struct {
	SurfaceSessionID            string                      `json:"surfaceSessionID"`
	GatewayID                   string                      `json:"gatewayID,omitempty"`
	ChatID                      string                      `json:"chatID,omitempty"`
	ActorUserID                 string                      `json:"actorUserID,omitempty"`
	ProductMode                 string                      `json:"productMode,omitempty"`
	Backend                     string                      `json:"backend,omitempty"`
	LegacyCodexProviderID       string                      `json:"codexProviderID,omitempty"`
	CodexProfileID              string                      `json:"codexProfileID,omitempty"`
	CodexProfileSelectionStatus string                      `json:"codexProfileSelectionStatus,omitempty"`
	CodexAdmissionRef           *state.CodexAdmissionRef    `json:"codexAdmissionRef,omitempty"`
	ClaudeProfileID             string                      `json:"claudeProfileID,omitempty"`
	OpenCodeProfileID           string                      `json:"openCodeProfileID,omitempty"`
	OpenCodeAdmissionRef        *state.OpenCodeAdmissionRef `json:"openCodeAdmissionRef,omitempty"`
	Verbosity                   string                      `json:"verbosity,omitempty"`
	AccessMode                  string                      `json:"accessMode,omitempty"`
	PlanMode                    string                      `json:"planMode,omitempty"`
	PlanModeOverrideSet         bool                        `json:"planModeOverrideSet,omitempty"`
	ResumeInstanceID            string                      `json:"resumeInstanceID,omitempty"`
	ResumeThreadID              string                      `json:"resumeThreadID,omitempty"`
	ResumeThreadTitle           string                      `json:"resumeThreadTitle,omitempty"`
	ResumeThreadCWD             string                      `json:"resumeThreadCWD,omitempty"`
	ResumeWorkspaceKey          string                      `json:"resumeWorkspaceKey,omitempty"`
	ResumeRouteMode             string                      `json:"resumeRouteMode,omitempty"`
	ResumeHeadless              bool                        `json:"resumeHeadless,omitempty"`
	UpdatedAt                   time.Time                   `json:"updatedAt,omitempty"`
}

func StatePath(stateDir string) string {
	return statestore.StatePath(stateDir, StateFileName)
}

func NewStore(path string) *Store {
	return &Store{Store: statestore.New[Entry](path, statestore.Options[Entry]{
		Version:       StateVersion,
		Name:          "surface resume state",
		Equal:         sameEntryContentIncludingUpdatedAt,
		LoadNormalize: func(entry Entry) (Entry, bool) { return NormalizeEntry(entry) },
		LoadKey:       func(entry Entry) string { return entry.SurfaceSessionID },
		LoadEqual:     SameEntryContent,
		LoadPost:      CanonicalizeEntries,
	})}
}

func LoadStore(path string) (*Store, error) {
	store, err := statestore.Load[Entry](path, statestore.Options[Entry]{
		Version:       StateVersion,
		Name:          "surface resume state",
		Equal:         sameEntryContentIncludingUpdatedAt,
		LoadNormalize: func(entry Entry) (Entry, bool) { return NormalizeEntry(entry) },
		LoadKey:       func(entry Entry) string { return entry.SurfaceSessionID },
		LoadEqual:     SameEntryContent,
		LoadPost:      CanonicalizeEntries,
	})
	if err != nil {
		return nil, err
	}
	return &Store{Store: store}, nil
}

type Store struct {
	*statestore.Store[Entry]
}

func sameEntryContentIncludingUpdatedAt(left, right Entry) bool {
	return SameEntryContent(left, right) && left.UpdatedAt.Equal(right.UpdatedAt)
}

func (s *Store) Put(entry Entry) error {
	if s == nil {
		return nil
	}
	normalized, ok := NormalizeEntry(entry)
	if !ok {
		return fmt.Errorf("surface resume entry requires surface id")
	}
	entries := s.Entries()
	entries[normalized.SurfaceSessionID] = normalized
	if canonical, changed := CanonicalizeEntries(entries); changed {
		entries = canonical
	}
	return s.Replace(entries)
}

func (s *Store) Delete(surfaceID string) error {
	if s == nil {
		return nil
	}
	entries := s.Entries()
	delete(entries, strings.TrimSpace(surfaceID))
	return s.Replace(entries)
}

func (s *Store) ReplaceAll(entries map[string]Entry) error {
	if s == nil {
		return nil
	}
	normalizedEntries := make(map[string]Entry, len(entries))
	for _, entry := range entries {
		normalized, ok := NormalizeEntry(entry)
		if !ok {
			return fmt.Errorf("surface resume entry requires surface id")
		}
		normalizedEntries[normalized.SurfaceSessionID] = normalized
	}
	if canonical, changed := CanonicalizeEntries(normalizedEntries); changed {
		normalizedEntries = canonical
	}
	return s.Replace(normalizedEntries)
}

func NormalizeEntry(entry Entry) (Entry, bool) {
	entry.SurfaceSessionID = strings.TrimSpace(entry.SurfaceSessionID)
	entry.GatewayID = strings.TrimSpace(entry.GatewayID)
	entry.ChatID = strings.TrimSpace(entry.ChatID)
	entry.ActorUserID = strings.TrimSpace(entry.ActorUserID)
	entry.ProductMode = string(state.NormalizeProductMode(state.ProductMode(strings.TrimSpace(entry.ProductMode))))
	entry.LegacyCodexProviderID = strings.TrimSpace(entry.LegacyCodexProviderID)
	entry.CodexProfileID = strings.TrimSpace(entry.CodexProfileID)
	entry.CodexProfileSelectionStatus = strings.TrimSpace(entry.CodexProfileSelectionStatus)
	entry.CodexAdmissionRef = normalizeCodexAdmissionRef(entry.CodexAdmissionRef)
	entry.ClaudeProfileID = strings.TrimSpace(entry.ClaudeProfileID)
	entry.OpenCodeProfileID = strings.TrimSpace(entry.OpenCodeProfileID)
	entry.OpenCodeAdmissionRef = normalizeOpenCodeAdmissionRef(entry.OpenCodeAdmissionRef)
	codexProfileID := entry.CodexProfileID
	if codexProfileID == "" {
		codexProfileID = state.CodexProfileIDFromLegacyProviderID(entry.LegacyCodexProviderID)
	}
	rawContract := state.PersistedSurfaceBackendContract(
		state.ProductMode(entry.ProductMode),
		agentproto.Backend(strings.TrimSpace(entry.Backend)),
		codexProfileID,
		entry.ClaudeProfileID,
		entry.OpenCodeProfileID,
	)
	entry.Backend = string(rawContract.Backend)
	entry.CodexProfileID = state.EffectiveSurfaceCodexProfileID(rawContract)
	entry.LegacyCodexProviderID = ""
	entry.ClaudeProfileID = state.EffectiveSurfaceClaudeProfileID(rawContract)
	entry.OpenCodeProfileID = state.EffectiveSurfaceOpenCodeProfileID(rawContract)
	entry = CanonicalizeEntryProfileSelection(entry)
	if entry.CodexProfileID == "" {
		entry.CodexProfileID = ""
		entry.CodexProfileSelectionStatus = ""
		entry.CodexAdmissionRef = nil
	}
	entry.Verbosity = string(state.NormalizeSurfaceVerbosity(state.SurfaceVerbosity(strings.TrimSpace(entry.Verbosity))))
	entry.AccessMode = strings.TrimSpace(entry.AccessMode)
	if entry.PlanModeOverrideSet {
		entry.PlanMode = string(state.NormalizePlanModeSetting(state.PlanModeSetting(strings.TrimSpace(entry.PlanMode))))
	} else {
		entry.PlanMode = ""
	}
	entry.ResumeInstanceID = strings.TrimSpace(entry.ResumeInstanceID)
	entry.ResumeThreadID = strings.TrimSpace(entry.ResumeThreadID)
	entry.ResumeThreadCWD = state.ResolveWorkspaceClaimKey(entry.ResumeThreadCWD)
	entry.ResumeWorkspaceKey = state.ResolveWorkspaceClaimKey(entry.ResumeWorkspaceKey)
	entry.ResumeThreadTitle = threadtitle.NormalizeStoredInput(entry.ResumeThreadTitle, threadtitle.Context{
		ThreadID:     entry.ResumeThreadID,
		ThreadCWD:    entry.ResumeThreadCWD,
		WorkspaceKey: entry.ResumeWorkspaceKey,
	})
	entry.ResumeRouteMode = strings.TrimSpace(entry.ResumeRouteMode)
	if entry.ResumeThreadID == "" {
		entry.ResumeHeadless = false
	}
	if !state.IsHeadlessProductMode(state.ProductMode(entry.ProductMode)) {
		entry.ResumeHeadless = false
	}
	if entry.ResumeHeadless {
		entry.ResumeWorkspaceKey = state.ResolveHeadlessResumeWorkspaceKey(entry.ResumeWorkspaceKey, entry.ResumeThreadCWD)
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
		strings.TrimSpace(left.CodexProfileID) == strings.TrimSpace(right.CodexProfileID) &&
		strings.TrimSpace(left.CodexProfileSelectionStatus) == strings.TrimSpace(right.CodexProfileSelectionStatus) &&
		sameCodexAdmissionRef(left.CodexAdmissionRef, right.CodexAdmissionRef) &&
		strings.TrimSpace(left.ClaudeProfileID) == strings.TrimSpace(right.ClaudeProfileID) &&
		strings.TrimSpace(left.OpenCodeProfileID) == strings.TrimSpace(right.OpenCodeProfileID) &&
		sameOpenCodeAdmissionRef(left.OpenCodeAdmissionRef, right.OpenCodeAdmissionRef) &&
		strings.TrimSpace(left.Verbosity) == strings.TrimSpace(right.Verbosity) &&
		strings.TrimSpace(left.AccessMode) == strings.TrimSpace(right.AccessMode) &&
		strings.TrimSpace(left.PlanMode) == strings.TrimSpace(right.PlanMode) &&
		left.PlanModeOverrideSet == right.PlanModeOverrideSet &&
		strings.TrimSpace(left.ResumeInstanceID) == strings.TrimSpace(right.ResumeInstanceID) &&
		strings.TrimSpace(left.ResumeThreadID) == strings.TrimSpace(right.ResumeThreadID) &&
		strings.TrimSpace(left.ResumeThreadTitle) == strings.TrimSpace(right.ResumeThreadTitle) &&
		strings.TrimSpace(left.ResumeThreadCWD) == strings.TrimSpace(right.ResumeThreadCWD) &&
		strings.TrimSpace(left.ResumeWorkspaceKey) == strings.TrimSpace(right.ResumeWorkspaceKey) &&
		strings.TrimSpace(left.ResumeRouteMode) == strings.TrimSpace(right.ResumeRouteMode) &&
		left.ResumeHeadless == right.ResumeHeadless
}

func CanonicalizeEntryProfileSelection(entry Entry) Entry {
	mode := state.NormalizeProductMode(state.ProductMode(strings.TrimSpace(entry.ProductMode)))
	backend := state.NormalizeSurfaceBackend(mode, agentproto.Backend(strings.TrimSpace(entry.Backend)))
	if !state.IsHeadlessProductMode(mode) || backend != agentproto.BackendOpenCode {
		entry.OpenCodeProfileID = ""
		entry.OpenCodeAdmissionRef = nil
	} else {
		entry.OpenCodeProfileID = state.NormalizeOpenCodeProfileID(entry.OpenCodeProfileID)
		if state.NormalizeOpenCodeProfileID(entry.OpenCodeProfileID) == state.DefaultOpenCodeProfileID {
			entry.OpenCodeAdmissionRef = nil
		}
	}
	if !state.IsHeadlessProductMode(mode) || backend != agentproto.BackendCodex {
		entry.LegacyCodexProviderID = ""
		entry.CodexProfileID = ""
		entry.CodexProfileSelectionStatus = ""
		entry.CodexAdmissionRef = nil
		return entry
	}
	entry.CodexProfileID = strings.TrimSpace(entry.CodexProfileID)
	if entry.CodexProfileID == "" {
		entry.CodexProfileID = state.CodexProfileIDFromLegacyProviderID(entry.LegacyCodexProviderID)
	}
	entry.CodexProfileID = state.NormalizeCodexProfileID(entry.CodexProfileID)
	entry.LegacyCodexProviderID = ""
	return entry
}

func normalizeCodexAdmissionRef(value *state.CodexAdmissionRef) *state.CodexAdmissionRef {
	if value == nil {
		return nil
	}
	normalized := *value
	normalized.ProfileRef.ID = strings.TrimSpace(normalized.ProfileRef.ID)
	normalized.ContextPreferenceRef.ProfileID = strings.TrimSpace(normalized.ContextPreferenceRef.ProfileID)
	if normalized.ProfileRef.ID == "" || normalized.ProfileRef.Revision == 0 ||
		normalized.ContextPreferenceRef.ProfileID != normalized.ProfileRef.ID || normalized.ContextPreferenceRef.Revision == 0 {
		return nil
	}
	return &normalized
}

func normalizeOpenCodeAdmissionRef(value *state.OpenCodeAdmissionRef) *state.OpenCodeAdmissionRef {
	return state.NormalizeOpenCodeAdmissionRef(value)
}

func sameCodexAdmissionRef(left, right *state.CodexAdmissionRef) bool {
	left = normalizeCodexAdmissionRef(left)
	right = normalizeCodexAdmissionRef(right)
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func sameOpenCodeAdmissionRef(left, right *state.OpenCodeAdmissionRef) bool {
	left = normalizeOpenCodeAdmissionRef(left)
	right = normalizeOpenCodeAdmissionRef(right)
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
