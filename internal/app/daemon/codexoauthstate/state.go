package codexoauthstate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/codexprofile"
	"github.com/kxn/codex-remote-feishu/internal/atomicfile"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

const (
	StateVersion  = 1
	StateFileName = "codex-oauth-profile.json"
)

type StateFile struct {
	Version                  int                           `json:"version"`
	Profile                  *state.CodexOAuthProfileState `json:"profile,omitempty"`
	LastKnownStatus          string                        `json:"lastKnownStatus,omitempty"`
	LastConfirmedLifecycleID string                        `json:"lastConfirmedLifecycleID,omitempty"`
}

type Store struct {
	path                     string
	profile                  *state.CodexOAuthProfileState
	lastKnownStatus          string
	lastConfirmedLifecycleID string
}

func StatePath(stateDir string) string {
	stateDir = strings.TrimSpace(stateDir)
	if stateDir == "" {
		return ""
	}
	return filepath.Join(stateDir, StateFileName)
}

func NewStore(path string) *Store {
	return &Store{path: strings.TrimSpace(path)}
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
		return nil, fmt.Errorf("parse codex oauth profile state: %w", err)
	}
	if persisted.Version != StateVersion {
		return nil, fmt.Errorf("unsupported codex oauth profile state version: %d", persisted.Version)
	}
	if persisted.Profile != nil {
		profile := *persisted.Profile
		if err := validateProfile(profile); err != nil {
			return nil, err
		}
		store.profile = &profile
	}
	store.lastKnownStatus = normalizeKnownStatus(persisted.LastKnownStatus)
	store.lastConfirmedLifecycleID = strings.TrimSpace(persisted.LastConfirmedLifecycleID)
	return store, nil
}

func (s *Store) Current() (state.CodexOAuthProfileState, bool) {
	if s == nil || s.profile == nil {
		return state.CodexOAuthProfileState{}, false
	}
	return *s.profile, true
}

func (s *Store) ApplyProbe(observation codexprofile.OAuthProbeObservation, checkedAt time.Time, daemonLifecycleID string, forceAuthGeneration bool) (state.CodexOAuthProfileState, bool, error) {
	if s == nil {
		return state.CodexOAuthProfileState{}, false, fmt.Errorf("codex oauth profile store is unavailable")
	}
	checkedAt = checkedAt.UTC()
	if checkedAt.IsZero() {
		checkedAt = time.Now().UTC()
	}
	status := string(observation.Result.Status)
	if status != string(codexprofile.OAuthProbeStatusDetected) && status != string(codexprofile.OAuthProbeStatusMissing) && status != string(codexprofile.OAuthProbeStatusUnknown) {
		return state.CodexOAuthProfileState{}, false, fmt.Errorf("invalid codex oauth probe status")
	}

	previous, existed := s.Current()
	previousKnownStatus := s.lastKnownStatus
	previousLifecycleID := s.lastConfirmedLifecycleID
	next := previous
	if !existed {
		next = state.CodexOAuthProfileState{
			ProfileID: state.OAuthCodexProfileID,
			Revision:  1,
		}
		if status != string(codexprofile.OAuthProbeStatusUnknown) {
			next.AuthGeneration = 1
		}
	}

	semanticChanged := !existed
	authChanged := existed && s.authEvidenceChanged(previous, observation, daemonLifecycleID, forceAuthGeneration)
	availabilityChanged := existed && status == string(codexprofile.OAuthProbeStatusDetected) && previous.AvailabilityCode != observation.Result.AvailabilityCode
	capabilityChanged := existed && observation.CapabilitySet != "" && previous.CapabilitySet != observation.CapabilitySet
	if authChanged {
		next.AuthGeneration++
	}
	if existed && (authChanged || availabilityChanged || capabilityChanged) {
		next.Revision++
		semanticChanged = true
	}

	next.Status = status
	next.LastCheckedAt = checkedAt
	if status == string(codexprofile.OAuthProbeStatusUnknown) {
		next.LastProbeErrorCode = xutil.FirstNonEmpty(observation.Result.LastProbeErrorCode, codexprofile.ErrorOAuthProbeUnknown)
	} else {
		next.LastProbeErrorCode = ""
		next.AccountHint = strings.TrimSpace(observation.Result.AccountHint)
		next.PlanType = strings.TrimSpace(observation.Result.PlanType)
		next.AvailabilityCode = strings.TrimSpace(observation.Result.AvailabilityCode)
		if observation.CapabilitySet != "" {
			next.CapabilitySet = strings.TrimSpace(observation.CapabilitySet)
		}
		s.lastKnownStatus = status
	}
	if status == string(codexprofile.OAuthProbeStatusDetected) {
		s.lastConfirmedLifecycleID = strings.TrimSpace(daemonLifecycleID)
	}
	s.profile = &next
	if err := s.save(); err != nil {
		s.lastKnownStatus = previousKnownStatus
		s.lastConfirmedLifecycleID = previousLifecycleID
		s.profile = nil
		if existed {
			old := previous
			s.profile = &old
		}
		return state.CodexOAuthProfileState{}, false, err
	}
	return next, semanticChanged, nil
}

func (s *Store) authEvidenceChanged(previous state.CodexOAuthProfileState, observation codexprofile.OAuthProbeObservation, daemonLifecycleID string, force bool) bool {
	status := string(observation.Result.Status)
	if status == string(codexprofile.OAuthProbeStatusUnknown) {
		return false
	}
	if force && status == string(codexprofile.OAuthProbeStatusDetected) {
		return true
	}
	if status == string(codexprofile.OAuthProbeStatusDetected) && strings.TrimSpace(daemonLifecycleID) != "" && strings.TrimSpace(daemonLifecycleID) != s.lastConfirmedLifecycleID {
		return true
	}
	if s.lastKnownStatus != "" && s.lastKnownStatus != status {
		return true
	}
	return status == string(codexprofile.OAuthProbeStatusDetected) && previous.AccountHint != "" && observation.Result.AccountHint != "" && previous.AccountHint != strings.TrimSpace(observation.Result.AccountHint)
}

func (s *Store) save() error {
	if s.path == "" {
		return nil
	}
	persisted := StateFile{
		Version:                  StateVersion,
		Profile:                  s.profile,
		LastKnownStatus:          s.lastKnownStatus,
		LastConfirmedLifecycleID: s.lastConfirmedLifecycleID,
	}
	raw, err := json.MarshalIndent(persisted, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return atomicfile.Write(s.path, raw, 0o600)
}

func validateProfile(profile state.CodexOAuthProfileState) error {
	if profile.ProfileID != state.OAuthCodexProfileID || profile.Revision == 0 {
		return fmt.Errorf("invalid codex oauth profile state")
	}
	status := profile.Status
	if status != string(codexprofile.OAuthProbeStatusDetected) && status != string(codexprofile.OAuthProbeStatusMissing) && status != string(codexprofile.OAuthProbeStatusUnknown) {
		return fmt.Errorf("invalid codex oauth profile status")
	}
	return nil
}

func normalizeKnownStatus(value string) string {
	value = strings.TrimSpace(value)
	if value == string(codexprofile.OAuthProbeStatusDetected) || value == string(codexprofile.OAuthProbeStatusMissing) {
		return value
	}
	return ""
}
