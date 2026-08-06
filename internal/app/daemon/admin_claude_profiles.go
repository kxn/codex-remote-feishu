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

type adminClaudeSettingsView struct {
	Profiles []adminClaudeProfileView `json:"profiles,omitempty"`
}

type adminClaudeProfileView struct {
	ID                string                         `json:"id"`
	Name              string                         `json:"name,omitempty"`
	AuthMode          string                         `json:"authMode,omitempty"`
	BaseURL           string                         `json:"baseURL,omitempty"`
	HasAuthToken      bool                           `json:"hasAuthToken"`
	Model             string                         `json:"model,omitempty"`
	SmallModel        string                         `json:"smallModel,omitempty"`
	SubagentModel     string                         `json:"subagentModel,omitempty"`
	ReasoningEffort   string                         `json:"reasoningEffort,omitempty"`
	BuiltIn           bool                           `json:"builtIn,omitempty"`
	Persisted         bool                           `json:"persisted"`
	ReadOnly          bool                           `json:"readOnly,omitempty"`
	ContextPreference state.ProfileContextPreference `json:"contextPreference"`
}

type claudeProfilesResponse struct {
	Profiles []adminClaudeProfileView `json:"profiles"`
}

type claudeProfileResponse struct {
	Profile adminClaudeProfileView `json:"profile"`
}

type claudeContextPreferenceResponse struct {
	ContextPreference state.ProfileContextPreference `json:"contextPreference"`
}

type claudeProfileWriteRequest struct {
	Name            *string `json:"name"`
	BaseURL         *string `json:"baseURL"`
	AuthToken       *string `json:"authToken"`
	Model           *string `json:"model"`
	SmallModel      *string `json:"smallModel"`
	SubagentModel   *string `json:"subagentModel"`
	ReasoningEffort *string `json:"reasoningEffort"`
}

func (a *App) handleClaudeProfilesList(w http.ResponseWriter, _ *http.Request) {
	a.adminConfigMu.Lock()
	defer a.adminConfigMu.Unlock()
	loaded, err := a.loadAdminConfig()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "config_unavailable",
			Message: "failed to load config",
			Details: err.Error(),
		})
		return
	}
	views, err := a.adminClaudeProfilesViewWithContextLocked(loaded.Config)
	if err != nil {
		writeClaudeContextPreferenceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, claudeProfilesResponse{Profiles: views})
}

func (a *App) handleClaudeProfileCreate(w http.ResponseWriter, r *http.Request) {
	if err := a.profileCatalogMutationError(); err != nil {
		writeClaudeContextPreferenceError(w, err)
		return
	}
	var req claudeProfileWriteRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, apiError{
			Code:    "invalid_request",
			Message: "failed to decode claude profile payload",
			Details: err.Error(),
		})
		return
	}

	a.adminConfigMu.Lock()
	loaded, err := a.loadAdminConfig()
	if err != nil {
		a.adminConfigMu.Unlock()
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "config_unavailable",
			Message: "failed to load config",
			Details: err.Error(),
		})
		return
	}

	updated := loaded.Config
	name, _ := trimmedOptionalString(req.Name)
	profileID := config.ClaudeProfileIDFromName(name)
	if profileID == "" {
		a.adminConfigMu.Unlock()
		writeAPIError(w, http.StatusBadRequest, apiError{
			Code:    "claude_profile_name_required",
			Message: "claude profile name is required",
		})
		return
	}
	if config.IsBuiltInClaudeProfileID(profileID) {
		a.adminConfigMu.Unlock()
		writeAPIError(w, http.StatusConflict, apiError{
			Code:    "claude_profile_read_only",
			Message: "the built-in default claude profile cannot be replaced",
			Details: config.ClaudeDefaultProfileID,
		})
		return
	}

	profile := config.ClaudeProfileConfig{
		ID:              profileID,
		Name:            name,
		AuthMode:        config.ClaudeAuthModeAuthToken,
		BaseURL:         optionalStringValue(req.BaseURL),
		AuthToken:       optionalStringValue(req.AuthToken),
		Model:           optionalStringValue(req.Model),
		SmallModel:      optionalStringValue(req.SmallModel),
		SubagentModel:   optionalStringValue(req.SubagentModel),
		ReasoningEffort: config.NormalizeClaudeReasoningEffort(optionalStringValue(req.ReasoningEffort)),
	}
	if index := config.IndexOfClaudeProfile(updated.Claude.Profiles, profileID); index >= 0 {
		current := updated.Claude.Profiles[index]
		if strings.TrimSpace(profile.AuthToken) == "" {
			profile.AuthToken = strings.TrimSpace(current.AuthToken)
		}
		if req.ReasoningEffort == nil {
			profile.ReasoningEffort = config.NormalizeClaudeReasoningEffort(current.ReasoningEffort)
		}
		updated.Claude.Profiles[index] = profile
	} else {
		updated.Claude.Profiles = append(updated.Claude.Profiles, profile)
	}
	preferenceStore, err := a.profileContextPreferenceStore()
	if err != nil {
		a.adminConfigMu.Unlock()
		writeClaudeContextPreferenceError(w, err)
		return
	}
	_, preferenceExisted := preferenceStore.ClaudeCurrent(profile.ID)
	if err := preferenceStore.EnsureClaudeProfile(profile.ID, state.ClaudeContextModeDefault); err != nil {
		a.adminConfigMu.Unlock()
		writeClaudeContextPreferenceError(w, err)
		return
	}
	if err := config.WriteAppConfig(loaded.Path, updated); err != nil {
		if !preferenceExisted {
			if cleanupErr := preferenceStore.DeleteClaudeProfile(profile.ID); cleanupErr != nil {
				combined := fmt.Errorf("config_write_failed: %v; preference rollback failed: %w", err, cleanupErr)
				a.markProfileCatalogDegraded(combined)
				a.adminConfigMu.Unlock()
				writeAPIError(w, http.StatusInternalServerError, apiError{Code: "config_write_failed", Message: "failed to save claude profile config", Details: combined.Error()})
				return
			}
		}
		a.adminConfigMu.Unlock()
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "config_write_failed",
			Message: "failed to save claude profile config",
			Details: err.Error(),
		})
		return
	}
	a.adminConfigMu.Unlock()
	a.mu.Lock()
	a.syncClaudeProfilesCatalogLocked(updated)
	a.mu.Unlock()

	preference, _ := preferenceStore.ClaudeCurrent(profile.ID)
	view := adminClaudeProfileViewFromConfig(config.ClaudeProfile{ClaudeProfileConfig: profile})
	view.ContextPreference = preference
	writeJSON(w, http.StatusCreated, claudeProfileResponse{
		Profile: view,
	})
}

func (a *App) handleClaudeProfileUpdate(w http.ResponseWriter, r *http.Request) {
	if err := a.profileCatalogMutationError(); err != nil {
		writeClaudeContextPreferenceError(w, err)
		return
	}
	profileID := config.CanonicalClaudeProfileID(r.PathValue("id"))
	if config.IsBuiltInClaudeProfileID(profileID) {
		writeAPIError(w, http.StatusConflict, apiError{
			Code:    "claude_profile_read_only",
			Message: "the built-in default claude profile cannot be edited",
			Details: config.ClaudeDefaultProfileID,
		})
		return
	}

	var req claudeProfileWriteRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, apiError{
			Code:    "invalid_request",
			Message: "failed to decode claude profile payload",
			Details: err.Error(),
		})
		return
	}

	a.adminConfigMu.Lock()
	loaded, err := a.loadAdminConfig()
	if err != nil {
		a.adminConfigMu.Unlock()
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "config_unavailable",
			Message: "failed to load config",
			Details: err.Error(),
		})
		return
	}

	updated := loaded.Config
	index := config.IndexOfClaudeProfile(updated.Claude.Profiles, profileID)
	if index < 0 {
		a.adminConfigMu.Unlock()
		writeAPIError(w, http.StatusNotFound, apiError{
			Code:    "claude_profile_not_found",
			Message: "claude profile not found",
			Details: profileID,
		})
		return
	}

	current := updated.Claude.Profiles[index]
	previousProfileID := current.ID
	if req.Name != nil {
		name := optionalStringValue(req.Name)
		if name == "" {
			a.adminConfigMu.Unlock()
			writeAPIError(w, http.StatusBadRequest, apiError{
				Code:    "claude_profile_name_required",
				Message: "claude profile name is required",
			})
			return
		}
		nextID := config.ClaudeProfileIDFromName(name)
		if config.IsBuiltInClaudeProfileID(nextID) {
			a.adminConfigMu.Unlock()
			writeAPIError(w, http.StatusConflict, apiError{
				Code:    "claude_profile_read_only",
				Message: "the built-in default claude profile cannot be replaced",
				Details: config.ClaudeDefaultProfileID,
			})
			return
		}
		if nextID != profileID {
			if existingIndex := config.IndexOfClaudeProfile(updated.Claude.Profiles, nextID); existingIndex >= 0 && existingIndex != index {
				a.adminConfigMu.Unlock()
				writeAPIError(w, http.StatusConflict, apiError{
					Code:    "duplicate_claude_profile_name",
					Message: "claude profile name already exists",
					Details: name,
				})
				return
			}
			current.ID = nextID
		}
		current.Name = name
	}
	current.AuthMode = config.ClaudeAuthModeAuthToken
	if req.BaseURL != nil {
		current.BaseURL = optionalStringValue(req.BaseURL)
	}
	if req.Model != nil {
		current.Model = optionalStringValue(req.Model)
	}
	if req.SmallModel != nil {
		current.SmallModel = optionalStringValue(req.SmallModel)
	}
	if req.SubagentModel != nil {
		current.SubagentModel = optionalStringValue(req.SubagentModel)
	}
	if req.AuthToken != nil {
		current.AuthToken = optionalStringValue(req.AuthToken)
	}
	if req.ReasoningEffort != nil {
		current.ReasoningEffort = config.NormalizeClaudeReasoningEffort(optionalStringValue(req.ReasoningEffort))
	}
	preferenceStore, err := a.profileContextPreferenceStore()
	if err != nil {
		a.adminConfigMu.Unlock()
		writeClaudeContextPreferenceError(w, err)
		return
	}
	preferenceRenamed := current.ID != previousProfileID
	if preferenceRenamed {
		if err := preferenceStore.RenameClaudeProfile(previousProfileID, current.ID); err != nil {
			a.adminConfigMu.Unlock()
			writeClaudeContextPreferenceError(w, err)
			return
		}
	}
	updated.Claude.Profiles[index] = current
	if err := config.WriteAppConfig(loaded.Path, updated); err != nil {
		if preferenceRenamed {
			if rollbackErr := preferenceStore.RenameClaudeProfile(current.ID, previousProfileID); rollbackErr != nil {
				combined := fmt.Errorf("config_write_failed: %v; preference rollback failed: %w", err, rollbackErr)
				a.markProfileCatalogDegraded(combined)
				a.adminConfigMu.Unlock()
				writeAPIError(w, http.StatusInternalServerError, apiError{Code: "config_write_failed", Message: "failed to save claude profile config", Details: combined.Error()})
				return
			}
		}
		a.adminConfigMu.Unlock()
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "config_write_failed",
			Message: "failed to save claude profile config",
			Details: err.Error(),
		})
		return
	}
	a.adminConfigMu.Unlock()
	a.mu.Lock()
	a.syncClaudeProfilesCatalogLocked(updated)
	a.mu.Unlock()

	preference, _ := preferenceStore.ClaudeCurrent(current.ID)
	view := adminClaudeProfileViewFromConfig(config.ClaudeProfile{ClaudeProfileConfig: current})
	view.ContextPreference = preference
	writeJSON(w, http.StatusOK, claudeProfileResponse{
		Profile: view,
	})
}

func (a *App) handleClaudeProfileDelete(w http.ResponseWriter, r *http.Request) {
	if err := a.profileCatalogMutationError(); err != nil {
		writeClaudeContextPreferenceError(w, err)
		return
	}
	profileID := config.CanonicalClaudeProfileID(r.PathValue("id"))
	if config.IsBuiltInClaudeProfileID(profileID) {
		writeAPIError(w, http.StatusConflict, apiError{
			Code:    "claude_profile_read_only",
			Message: "the built-in default claude profile cannot be deleted",
			Details: config.ClaudeDefaultProfileID,
		})
		return
	}

	a.adminConfigMu.Lock()
	loaded, err := a.loadAdminConfig()
	if err != nil {
		a.adminConfigMu.Unlock()
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "config_unavailable",
			Message: "failed to load config",
			Details: err.Error(),
		})
		return
	}

	updated := loaded.Config
	index := config.IndexOfClaudeProfile(updated.Claude.Profiles, profileID)
	if index < 0 {
		a.adminConfigMu.Unlock()
		writeAPIError(w, http.StatusNotFound, apiError{
			Code:    "claude_profile_not_found",
			Message: "claude profile not found",
			Details: profileID,
		})
		return
	}

	updated.Claude.Profiles = append(updated.Claude.Profiles[:index], updated.Claude.Profiles[index+1:]...)
	if err := config.WriteAppConfig(loaded.Path, updated); err != nil {
		a.adminConfigMu.Unlock()
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "config_write_failed",
			Message: "failed to save claude profile config",
			Details: err.Error(),
		})
		return
	}
	preferenceStore, storeErr := a.profileContextPreferenceStore()
	if storeErr == nil {
		storeErr = preferenceStore.DeleteClaudeProfile(profileID)
	}
	if storeErr != nil {
		storeErr = a.rollbackProfileConfig(loaded.Path, loaded.Config, fmt.Errorf("profile_preference_write_failed: %w", storeErr))
		a.adminConfigMu.Unlock()
		writeClaudeContextPreferenceError(w, storeErr)
		return
	}
	a.adminConfigMu.Unlock()
	a.mu.Lock()
	a.syncClaudeProfilesCatalogLocked(updated)
	a.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleClaudeContextPreferenceUpdate(w http.ResponseWriter, r *http.Request) {
	if err := a.profileCatalogMutationError(); err != nil {
		writeClaudeContextPreferenceError(w, err)
		return
	}
	if strings.TrimSpace(r.Header.Get("If-Match")) == "" {
		writeAPIError(w, http.StatusPreconditionRequired, apiError{Code: "profile_preference_revision_required", Message: "If-Match is required"})
		return
	}
	var req codexContextPreferenceWriteRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, apiError{Code: "invalid_request", Message: "failed to decode claude context preference", Details: err.Error()})
		return
	}
	profileID := config.CanonicalClaudeProfileID(r.PathValue("id"))
	if profileID == "" {
		profileID = config.ClaudeDefaultProfileID
	}
	a.adminConfigMu.Lock()
	loaded, err := a.loadAdminConfig()
	if err == nil && !config.IsBuiltInClaudeProfileID(profileID) && config.IndexOfClaudeProfile(loaded.Config.Claude.Profiles, profileID) < 0 {
		err = fmt.Errorf("claude_profile_not_found")
	}
	var preference state.ProfileContextPreference
	currentETag := ""
	if err == nil {
		store, storeErr := a.profileContextPreferenceStore()
		if storeErr != nil {
			err = storeErr
		} else {
			current, _ := store.ClaudeCurrent(profileID)
			preference = current
			currentETag = current.ETag
			var updated state.ProfileContextPreference
			updated, _, err = store.UpdateClaude(profileID, req.Mode, r.Header.Get("If-Match"))
			if err == nil {
				preference = updated
			}
		}
	}
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
		writeClaudeContextPreferenceError(w, err)
		return
	}
	w.Header().Set("ETag", preference.ETag)
	writeJSON(w, http.StatusOK, claudeContextPreferenceResponse{ContextPreference: preference})
}

func (a *App) adminClaudeProfilesViewWithContextLocked(cfg config.AppConfig) ([]adminClaudeProfileView, error) {
	store, err := a.profileContextPreferenceStore()
	if err != nil {
		return nil, err
	}
	profiles := config.ListClaudeProfiles(cfg)
	views := make([]adminClaudeProfileView, 0, len(profiles))
	for _, profile := range profiles {
		preference, ok := store.ClaudeCurrent(profile.ID)
		if !ok {
			return nil, fmt.Errorf("claude profile context preference missing for %s", profile.ID)
		}
		view := adminClaudeProfileViewFromConfig(profile)
		view.ContextPreference = preference
		views = append(views, view)
	}
	return views, nil
}

func writeClaudeContextPreferenceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, profilecontextstate.ErrPreconditionRequired):
		writeAPIError(w, http.StatusPreconditionRequired, apiError{Code: "profile_preference_revision_required", Message: "If-Match is required"})
	case errors.Is(err, profilecontextstate.ErrETagMismatch):
		writeAPIError(w, http.StatusPreconditionFailed, apiError{Code: "profile_preference_revision_conflict", Message: "the preference changed; reload and retry"})
	case strings.Contains(err.Error(), "not_found"):
		writeAPIError(w, http.StatusNotFound, apiError{Code: "claude_profile_not_found", Message: "claude profile not found"})
	case strings.Contains(err.Error(), "unavailable"), strings.Contains(err.Error(), "degraded"):
		writeAPIError(w, http.StatusServiceUnavailable, apiError{Code: "profile_catalog_degraded", Message: "profile context preferences are unavailable", Details: err.Error()})
	case strings.Contains(err.Error(), "write_failed"), strings.Contains(err.Error(), "rollback failed"):
		writeAPIError(w, http.StatusInternalServerError, apiError{Code: "profile_write_failed", Message: "failed to save profile context state", Details: err.Error()})
	default:
		writeAPIError(w, http.StatusBadRequest, apiError{Code: "invalid_context_preference", Message: "invalid profile context preference", Details: err.Error()})
	}
}

func adminPersistedClaudeSettingsView(cfg config.AppConfig) adminClaudeSettingsView {
	profiles := config.NormalizeClaudeProfiles(cfg.Claude.Profiles)
	view := adminClaudeSettingsView{Profiles: make([]adminClaudeProfileView, 0, len(profiles))}
	for _, profile := range profiles {
		view.Profiles = append(view.Profiles, adminClaudeProfileViewFromConfig(config.ClaudeProfile{ClaudeProfileConfig: profile}))
	}
	if len(view.Profiles) == 0 {
		view.Profiles = nil
	}
	return view
}

func adminClaudeProfileViewFromConfig(profile config.ClaudeProfile) adminClaudeProfileView {
	return adminClaudeProfileView{
		ID:              strings.TrimSpace(profile.ID),
		Name:            strings.TrimSpace(profile.Name),
		AuthMode:        config.NormalizeClaudeAuthMode(profile.AuthMode),
		BaseURL:         strings.TrimSpace(profile.BaseURL),
		HasAuthToken:    strings.TrimSpace(profile.AuthToken) != "",
		Model:           strings.TrimSpace(profile.Model),
		SmallModel:      strings.TrimSpace(profile.SmallModel),
		SubagentModel:   strings.TrimSpace(profile.SubagentModel),
		ReasoningEffort: config.NormalizeClaudeReasoningEffort(profile.ReasoningEffort),
		BuiltIn:         profile.BuiltIn,
		Persisted:       !profile.BuiltIn,
		ReadOnly:        profile.BuiltIn,
	}
}

func optionalStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func trimmedOptionalString(value *string) (string, bool) {
	if value == nil {
		return "", false
	}
	return strings.TrimSpace(*value), true
}
