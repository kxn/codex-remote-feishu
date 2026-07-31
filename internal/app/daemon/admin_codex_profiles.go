package daemon

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/profilecontextstate"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type codexProfilesResponse struct {
	Profiles []state.CodexProfileSummary `json:"profiles"`
}

type codexProfileResponse struct {
	Profile state.CodexProfileSummary `json:"profile"`
}

type codexContextPreferenceResponse struct {
	ContextPreference state.ProfileContextPreference `json:"contextPreference"`
}

type codexProfileReference struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

type codexProfileReferencesResponse struct {
	ProfileID  string                  `json:"profileID"`
	References []codexProfileReference `json:"references"`
}

type codexProfileWriteRequest struct {
	Name            *string `json:"name"`
	BaseURL         *string `json:"baseURL"`
	APIKey          *string `json:"apiKey"`
	Model           *string `json:"model"`
	ReviewModel     *string `json:"reviewModel"`
	ReasoningEffort *string `json:"reasoningEffort"`
}

type codexContextPreferenceWriteRequest struct {
	Mode string `json:"mode"`
}

func (a *App) handleCodexProfilesList(w http.ResponseWriter, _ *http.Request) {
	a.adminConfigMu.Lock()
	defer a.adminConfigMu.Unlock()
	profiles, err := a.codexProfileSummariesLocked()
	if err != nil {
		writeCodexProfileServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, codexProfilesResponse{Profiles: profiles})
}

func (a *App) handleCodexProfileCreate(w http.ResponseWriter, r *http.Request) {
	var req codexProfileWriteRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, apiError{Code: "invalid_request", Message: "failed to decode codex profile payload", Details: err.Error()})
		return
	}
	a.adminConfigMu.Lock()
	profile, err := a.createCodexProfileLocked(codexAPIProfileInputFromRequest(req))
	a.adminConfigMu.Unlock()
	if err != nil {
		writeCodexProfileServiceError(w, err)
		return
	}
	w.Header().Set("ETag", profile.ETag)
	writeJSON(w, http.StatusCreated, codexProfileResponse{Profile: profile})
}

func (a *App) handleCodexProfileUpdate(w http.ResponseWriter, r *http.Request) {
	profileID := strings.TrimSpace(r.PathValue("id"))
	if profileID == config.CodexNativeProfileID || profileID == config.CodexOAuthProfileID {
		writeAPIError(w, http.StatusConflict, apiError{Code: "codex_profile_read_only", Message: "this codex profile definition is read-only", Details: profileID})
		return
	}
	if strings.TrimSpace(r.Header.Get("If-Match")) == "" {
		writeAPIError(w, http.StatusPreconditionRequired, apiError{Code: "profile_revision_required", Message: "If-Match is required"})
		return
	}
	var req codexProfileWriteRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, apiError{Code: "invalid_request", Message: "failed to decode codex profile payload", Details: err.Error()})
		return
	}
	a.adminConfigMu.Lock()
	profile, currentETag, err := a.updateCodexProfileLocked(profileID, r.Header.Get("If-Match"), codexAPIProfileInputFromRequest(req))
	a.adminConfigMu.Unlock()
	if currentETag != "" {
		w.Header().Set("ETag", currentETag)
	}
	if err != nil {
		if errors.Is(err, profilecontextstate.ErrETagMismatch) {
			writeAPIError(w, http.StatusPreconditionFailed, apiError{
				Code: "profile_revision_conflict", Message: "the profile changed; reload and retry",
				Details: map[string]any{"profile": profile},
			})
			return
		}
		writeCodexProfileServiceError(w, err)
		return
	}
	w.Header().Set("ETag", profile.ETag)
	writeJSON(w, http.StatusOK, codexProfileResponse{Profile: profile})
}

func (a *App) handleCodexProfileDelete(w http.ResponseWriter, r *http.Request) {
	profileID := strings.TrimSpace(r.PathValue("id"))
	if profileID == config.CodexNativeProfileID || profileID == config.CodexOAuthProfileID {
		writeAPIError(w, http.StatusConflict, apiError{Code: "codex_profile_read_only", Message: "this codex profile cannot be deleted", Details: profileID})
		return
	}
	if strings.TrimSpace(r.Header.Get("If-Match")) == "" {
		writeAPIError(w, http.StatusPreconditionRequired, apiError{Code: "profile_revision_required", Message: "If-Match is required"})
		return
	}
	a.adminConfigMu.Lock()
	profile, currentETag, err := a.deleteCodexProfileLocked(profileID, r.Header.Get("If-Match"))
	a.adminConfigMu.Unlock()
	if currentETag != "" {
		w.Header().Set("ETag", currentETag)
	}
	if err != nil {
		if errors.Is(err, profilecontextstate.ErrETagMismatch) {
			writeAPIError(w, http.StatusPreconditionFailed, apiError{
				Code: "profile_revision_conflict", Message: "the profile changed; reload and retry",
				Details: map[string]any{"profile": profile},
			})
			return
		}
		writeCodexProfileServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleCodexProfileReferences(w http.ResponseWriter, r *http.Request) {
	profileID := strings.TrimSpace(r.PathValue("id"))
	a.adminConfigMu.Lock()
	profiles, err := a.codexProfileSummariesLocked()
	_, found := findCodexProfileSummary(profiles, profileID)
	var references []codexProfileReference
	if err == nil && !found {
		err = fmt.Errorf("profile_not_found")
	}
	if err == nil {
		references = a.codexProfileReferencesLocked(profileID)
	}
	a.adminConfigMu.Unlock()
	if err != nil {
		writeCodexProfileServiceError(w, err)
		return
	}
	if references == nil {
		references = []codexProfileReference{}
	}
	writeJSON(w, http.StatusOK, codexProfileReferencesResponse{ProfileID: profileID, References: references})
}

func (a *App) handleCodexContextPreferenceUpdate(w http.ResponseWriter, r *http.Request) {
	if strings.TrimSpace(r.Header.Get("If-Match")) == "" {
		writeAPIError(w, http.StatusPreconditionRequired, apiError{Code: "profile_preference_revision_required", Message: "If-Match is required"})
		return
	}
	var req codexContextPreferenceWriteRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, apiError{Code: "invalid_request", Message: "failed to decode codex context preference", Details: err.Error()})
		return
	}
	a.adminConfigMu.Lock()
	preference, currentETag, err := a.updateCodexContextPreferenceLocked(strings.TrimSpace(r.PathValue("id")), req.Mode, r.Header.Get("If-Match"))
	a.adminConfigMu.Unlock()
	if currentETag != "" {
		w.Header().Set("ETag", currentETag)
	}
	if err != nil {
		if errors.Is(err, profilecontextstate.ErrETagMismatch) {
			writeAPIError(w, http.StatusPreconditionFailed, apiError{
				Code: "profile_preference_revision_conflict", Message: "the preference changed; reload and retry",
				Details: map[string]any{"contextPreference": preference},
			})
			return
		}
		writeCodexProfileServiceError(w, err)
		return
	}
	w.Header().Set("ETag", preference.ETag)
	writeJSON(w, http.StatusOK, codexContextPreferenceResponse{ContextPreference: preference})
}

func (a *App) codexProfileSummariesLocked() ([]state.CodexProfileSummary, error) {
	a.mu.Lock()
	migrationErr := a.profileCatalogMigrationErr
	a.mu.Unlock()
	if migrationErr != nil {
		return nil, fmt.Errorf("profile_catalog_degraded: %w", migrationErr)
	}
	loaded, err := a.loadAdminConfig()
	if err != nil {
		return nil, fmt.Errorf("config_unavailable: %w", err)
	}
	preferenceStore, err := a.profileContextPreferenceStore()
	if err != nil {
		return nil, fmt.Errorf("profile_catalog_degraded: %w", err)
	}
	nativePreference, ok := preferenceStore.CodexCurrent(config.CodexNativeProfileID)
	if !ok {
		return nil, fmt.Errorf("profile_catalog_degraded: native context preference is missing")
	}
	profiles := []state.CodexProfileSummary{{
		ID:                config.CodexNativeProfileID,
		Kind:              state.CodexProfileKindNative,
		Name:              "本机默认",
		Available:         true,
		ContextEditable:   true,
		ContextPreference: nativePreference,
	}}
	for _, record := range config.NormalizeCodexAPIProfileRecords(loaded.Config.Codex.Profiles) {
		current, ok := config.CurrentCodexAPIProfile(record)
		if !ok {
			return nil, fmt.Errorf("profile_catalog_degraded: current revision missing for %s", record.ID)
		}
		preference, ok := preferenceStore.CodexCurrent(record.ID)
		if !ok {
			return nil, fmt.Errorf("profile_catalog_degraded: context preference missing for %s", record.ID)
		}
		profiles = append(profiles, codexAPIProfileSummary(current, preference))
	}
	return profiles, nil
}

func (a *App) createCodexProfileLocked(input config.CodexAPIProfileInput) (state.CodexProfileSummary, error) {
	if err := a.profileCatalogMutationError(); err != nil {
		return state.CodexProfileSummary{}, err
	}
	loaded, err := a.loadAdminConfig()
	if err != nil {
		return state.CodexProfileSummary{}, fmt.Errorf("config_unavailable: %w", err)
	}
	record, err := config.PrepareCodexAPIProfileCreate(loaded.Config.Codex.Profiles, input)
	if err != nil {
		return state.CodexProfileSummary{}, err
	}
	preferenceStore, err := a.profileContextPreferenceStore()
	if err != nil {
		return state.CodexProfileSummary{}, fmt.Errorf("profile_catalog_degraded: %w", err)
	}
	if err := preferenceStore.EnsureCodexProfile(record.ID, state.CodexContextModeDefault); err != nil {
		return state.CodexProfileSummary{}, fmt.Errorf("profile_preference_write_failed: %w", err)
	}
	updated := loaded.Config
	updated.Codex.Profiles = append(updated.Codex.Profiles, record)
	updated.Codex.Providers = nil
	if err := config.WriteAppConfig(loaded.Path, updated); err != nil {
		if cleanupErr := preferenceStore.DeleteCodexProfile(record.ID); cleanupErr != nil {
			combined := fmt.Errorf("config_write_failed: %v; preference rollback failed: %w", err, cleanupErr)
			a.markProfileCatalogDegraded(combined)
			return state.CodexProfileSummary{}, combined
		}
		return state.CodexProfileSummary{}, fmt.Errorf("config_write_failed: %w", err)
	}
	current, _ := config.CurrentCodexAPIProfile(record)
	preference, _ := preferenceStore.CodexCurrent(record.ID)
	a.syncCodexProfilesAfterMutation(updated)
	return codexAPIProfileSummary(current, preference), nil
}

func (a *App) updateCodexProfileLocked(profileID, expectedETag string, input config.CodexAPIProfileInput) (state.CodexProfileSummary, string, error) {
	if err := a.profileCatalogMutationError(); err != nil {
		return state.CodexProfileSummary{}, "", err
	}
	loaded, err := a.loadAdminConfig()
	if err != nil {
		return state.CodexProfileSummary{}, "", fmt.Errorf("config_unavailable: %w", err)
	}
	index := config.IndexOfCodexAPIProfile(loaded.Config.Codex.Profiles, profileID)
	if index < 0 {
		return state.CodexProfileSummary{}, "", fmt.Errorf("profile_not_found")
	}
	record := loaded.Config.Codex.Profiles[index]
	current, ok := config.CurrentCodexAPIProfile(record)
	if !ok {
		return state.CodexProfileSummary{}, "", fmt.Errorf("profile_catalog_degraded: current revision missing")
	}
	currentETag := state.CodexProfileDefinitionETag(profileID, current.Revision)
	preferenceStore, err := a.profileContextPreferenceStore()
	if err != nil {
		return state.CodexProfileSummary{}, currentETag, fmt.Errorf("profile_catalog_degraded: %w", err)
	}
	preference, ok := preferenceStore.CodexCurrent(profileID)
	if !ok {
		return state.CodexProfileSummary{}, currentETag, fmt.Errorf("profile_catalog_degraded: context preference missing")
	}
	currentSummary := codexAPIProfileSummary(current, preference)
	if expectedETag != currentETag {
		return currentSummary, currentETag, profilecontextstate.ErrETagMismatch
	}
	if err := config.ValidateCodexAPIProfileNameUnique(loaded.Config.Codex.Profiles, profileID, input.Name); err != nil {
		return state.CodexProfileSummary{}, currentETag, err
	}
	record, changed, err := config.PrepareCodexAPIProfileUpdate(record, input)
	if err != nil {
		return state.CodexProfileSummary{}, currentETag, err
	}
	if changed {
		updated := loaded.Config
		updated.Codex.Profiles[index] = record
		updated.Codex.Providers = nil
		if err := config.WriteAppConfig(loaded.Path, updated); err != nil {
			return state.CodexProfileSummary{}, currentETag, fmt.Errorf("config_write_failed: %w", err)
		}
		a.syncCodexProfilesAfterMutation(updated)
	}
	current, _ = config.CurrentCodexAPIProfile(record)
	return codexAPIProfileSummary(current, preference), state.CodexProfileDefinitionETag(profileID, current.Revision), nil
}

func (a *App) deleteCodexProfileLocked(profileID, expectedETag string) (state.CodexProfileSummary, string, error) {
	if err := a.profileCatalogMutationError(); err != nil {
		return state.CodexProfileSummary{}, "", err
	}
	loaded, err := a.loadAdminConfig()
	if err != nil {
		return state.CodexProfileSummary{}, "", fmt.Errorf("config_unavailable: %w", err)
	}
	index := config.IndexOfCodexAPIProfile(loaded.Config.Codex.Profiles, profileID)
	if index < 0 {
		return state.CodexProfileSummary{}, "", fmt.Errorf("profile_not_found")
	}
	current, ok := config.CurrentCodexAPIProfile(loaded.Config.Codex.Profiles[index])
	if !ok {
		return state.CodexProfileSummary{}, "", fmt.Errorf("profile_catalog_degraded: current revision missing")
	}
	currentETag := state.CodexProfileDefinitionETag(profileID, current.Revision)
	preferenceStore, err := a.profileContextPreferenceStore()
	if err != nil {
		return state.CodexProfileSummary{}, currentETag, fmt.Errorf("profile_catalog_degraded: %w", err)
	}
	preference, ok := preferenceStore.CodexCurrent(profileID)
	if !ok {
		return state.CodexProfileSummary{}, currentETag, fmt.Errorf("profile_catalog_degraded: context preference missing")
	}
	currentSummary := codexAPIProfileSummary(current, preference)
	if expectedETag != currentETag {
		return currentSummary, currentETag, profilecontextstate.ErrETagMismatch
	}
	if references := a.codexProfileReferencesLocked(profileID); len(references) > 0 {
		return currentSummary, currentETag, fmt.Errorf("profile_in_use")
	}
	updated := loaded.Config
	updated.Codex.Profiles = append(append([]config.CodexAPIProfileRecord{}, updated.Codex.Profiles[:index]...), updated.Codex.Profiles[index+1:]...)
	updated.Codex.Providers = nil
	if err := config.WriteAppConfig(loaded.Path, updated); err != nil {
		return currentSummary, currentETag, fmt.Errorf("config_write_failed: %w", err)
	}
	if err := preferenceStore.DeleteCodexProfile(profileID); err != nil {
		rollbackErr := a.rollbackProfileConfig(loaded.Path, loaded.Config, fmt.Errorf("profile_preference_write_failed: %w", err))
		return currentSummary, currentETag, rollbackErr
	}
	a.syncCodexProfilesAfterMutation(updated)
	return currentSummary, currentETag, nil
}

func (a *App) codexProfileReferencesLocked(profileID string) []codexProfileReference {
	profileID = strings.TrimSpace(profileID)
	a.mu.Lock()
	defer a.mu.Unlock()
	references := make([]codexProfileReference, 0)
	if a.botCapabilitySettingsState.store != nil {
		for _, record := range a.botCapabilitySettingsState.store.Entries() {
			if selectedCodexProfileID(record.CodexProfileID, record.CodexProviderID) == profileID {
				references = append(references, codexProfileReference{Kind: "bot_default", Name: record.GatewayID, Reason: "selected as bot default"})
			}
		}
	}
	if a.surfaceResumeRuntime.store != nil {
		for _, entry := range a.surfaceResumeRuntime.store.Entries() {
			if selectedCodexProfileID(entry.CodexProfileID, entry.CodexProviderID) == profileID {
				references = append(references, codexProfileReference{Kind: "surface_desired", Name: entry.SurfaceSessionID, Reason: "selected for surface"})
			}
			if entry.CodexAdmissionRef != nil && entry.CodexAdmissionRef.ProfileRef.ID == profileID {
				references = append(references, codexProfileReference{Kind: "surface_recovery", Name: entry.SurfaceSessionID, Reason: "retained for recovery"})
			}
		}
	}
	return references
}

func (a *App) updateCodexContextPreferenceLocked(profileID, mode, expectedETag string) (state.ProfileContextPreference, string, error) {
	if err := a.profileCatalogMutationError(); err != nil {
		return state.ProfileContextPreference{}, "", err
	}
	profiles, err := a.codexProfileSummariesLocked()
	if err != nil {
		return state.ProfileContextPreference{}, "", err
	}
	found := false
	for _, profile := range profiles {
		if profile.ID == profileID {
			found = true
			break
		}
	}
	if !found {
		return state.ProfileContextPreference{}, "", fmt.Errorf("profile_not_found")
	}
	preferenceStore, err := a.profileContextPreferenceStore()
	if err != nil {
		return state.ProfileContextPreference{}, "", fmt.Errorf("profile_catalog_degraded: %w", err)
	}
	current, _ := preferenceStore.CodexCurrent(profileID)
	updated, _, err := preferenceStore.UpdateCodex(profileID, mode, expectedETag)
	if err != nil {
		return current, current.ETag, err
	}
	return updated, updated.ETag, nil
}

func codexAPIProfileSummary(profile config.CodexAPIProfileSecretConfig, preference state.ProfileContextPreference) state.CodexProfileSummary {
	statusCode := config.CodexAPIProfileStatus(profile)
	return state.CodexProfileSummary{
		ID:                profile.ID,
		Revision:          profile.Revision,
		ETag:              state.CodexProfileDefinitionETag(profile.ID, profile.Revision),
		Kind:              state.CodexProfileKindAPI,
		Name:              profile.Name,
		BaseURL:           config.PublicCodexAPIProfileBaseURL(profile),
		Model:             profile.Model,
		ReviewModel:       profile.ReviewModel,
		ReasoningEffort:   profile.ReasoningEffort,
		StatusCode:        statusCode,
		Available:         statusCode == "" && profile.APIKey != "",
		HasAPIKey:         profile.APIKey != "",
		Editable:          true,
		Deletable:         true,
		ContextEditable:   true,
		ContextPreference: preference,
	}
}

func codexAPIProfileInputFromRequest(req codexProfileWriteRequest) config.CodexAPIProfileInput {
	return config.CodexAPIProfileInput{
		Name:            optionalStringValue(req.Name),
		BaseURL:         optionalStringValue(req.BaseURL),
		APIKey:          rawOptionalStringValue(req.APIKey),
		Model:           optionalStringValue(req.Model),
		ReviewModel:     optionalStringValue(req.ReviewModel),
		ReasoningEffort: optionalStringValue(req.ReasoningEffort),
	}
}

func rawOptionalStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func (a *App) syncCodexProfilesAfterMutation(cfg config.AppConfig) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.syncCodexProvidersCatalogLocked(cfg)
}

func writeCodexProfileServiceError(w http.ResponseWriter, err error) {
	message := err.Error()
	switch {
	case strings.Contains(message, "profile_not_found"):
		writeAPIError(w, http.StatusNotFound, apiError{Code: "codex_profile_not_found", Message: "codex profile not found"})
	case strings.Contains(message, "already exists"):
		writeAPIError(w, http.StatusConflict, apiError{Code: "duplicate_codex_profile_name", Message: "codex profile name already exists"})
	case strings.Contains(message, "catalog limit"):
		writeAPIError(w, http.StatusConflict, apiError{Code: "codex_profile_limit_reached", Message: "codex profile catalog limit reached"})
	case strings.Contains(message, "profile_in_use"):
		writeAPIError(w, http.StatusConflict, apiError{Code: "codex_profile_in_use", Message: "codex profile is still referenced"})
	case strings.Contains(message, "config_unavailable"), strings.Contains(message, "profile_catalog_degraded"):
		writeAPIError(w, http.StatusServiceUnavailable, apiError{Code: "profile_catalog_degraded", Message: "codex profile catalog is unavailable", Details: message})
	case strings.Contains(message, "write_failed"):
		writeAPIError(w, http.StatusInternalServerError, apiError{Code: "profile_write_failed", Message: "failed to save codex profile", Details: message})
	default:
		writeAPIError(w, http.StatusBadRequest, apiError{Code: "invalid_codex_profile", Message: "invalid codex profile", Details: message})
	}
}
