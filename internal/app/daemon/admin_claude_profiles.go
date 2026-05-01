package daemon

import (
	"net/http"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/config"
)

type adminClaudeSettingsView struct {
	Profiles []adminClaudeProfileView `json:"profiles,omitempty"`
}

type adminClaudeProfileView struct {
	ID           string `json:"id"`
	Name         string `json:"name,omitempty"`
	AuthMode     string `json:"authMode,omitempty"`
	BaseURL      string `json:"baseURL,omitempty"`
	HasAuthToken bool   `json:"hasAuthToken"`
	Model        string `json:"model,omitempty"`
	SmallModel   string `json:"smallModel,omitempty"`
	BuiltIn      bool   `json:"builtIn,omitempty"`
	Persisted    bool   `json:"persisted"`
	ReadOnly     bool   `json:"readOnly,omitempty"`
}

type claudeProfilesResponse struct {
	Profiles []adminClaudeProfileView `json:"profiles"`
}

type claudeProfileResponse struct {
	Profile adminClaudeProfileView `json:"profile"`
}

type claudeProfileWriteRequest struct {
	Name       *string `json:"name"`
	BaseURL    *string `json:"baseURL"`
	AuthToken  *string `json:"authToken"`
	Model      *string `json:"model"`
	SmallModel *string `json:"smallModel"`
}

func (a *App) handleClaudeProfilesList(w http.ResponseWriter, _ *http.Request) {
	loaded, err := a.loadAdminConfig()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "config_unavailable",
			Message: "failed to load config",
			Details: err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, claudeProfilesResponse{Profiles: adminClaudeProfilesView(loaded.Config)})
}

func (a *App) handleClaudeProfileCreate(w http.ResponseWriter, r *http.Request) {
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
		ID:         profileID,
		Name:       name,
		AuthMode:   config.ClaudeAuthModeAuthToken,
		BaseURL:    optionalStringValue(req.BaseURL),
		AuthToken:  optionalStringValue(req.AuthToken),
		Model:      optionalStringValue(req.Model),
		SmallModel: optionalStringValue(req.SmallModel),
	}
	if index := config.IndexOfClaudeProfile(updated.Claude.Profiles, profileID); index >= 0 {
		current := updated.Claude.Profiles[index]
		if strings.TrimSpace(profile.AuthToken) == "" {
			profile.AuthToken = strings.TrimSpace(current.AuthToken)
		}
		updated.Claude.Profiles[index] = profile
	} else {
		updated.Claude.Profiles = append(updated.Claude.Profiles, profile)
	}
	if err := config.WriteAppConfig(loaded.Path, updated); err != nil {
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

	writeJSON(w, http.StatusCreated, claudeProfileResponse{
		Profile: adminClaudeProfileViewFromConfig(config.ClaudeProfile{ClaudeProfileConfig: profile}),
	})
}

func (a *App) handleClaudeProfileUpdate(w http.ResponseWriter, r *http.Request) {
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
	if req.AuthToken != nil {
		current.AuthToken = optionalStringValue(req.AuthToken)
	}
	updated.Claude.Profiles[index] = current
	if err := config.WriteAppConfig(loaded.Path, updated); err != nil {
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

	writeJSON(w, http.StatusOK, claudeProfileResponse{
		Profile: adminClaudeProfileViewFromConfig(config.ClaudeProfile{ClaudeProfileConfig: current}),
	})
}

func (a *App) handleClaudeProfileDelete(w http.ResponseWriter, r *http.Request) {
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
	a.adminConfigMu.Unlock()
	a.mu.Lock()
	a.syncClaudeProfilesCatalogLocked(updated)
	a.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
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

func adminClaudeProfilesView(cfg config.AppConfig) []adminClaudeProfileView {
	profiles := config.ListClaudeProfiles(cfg)
	view := make([]adminClaudeProfileView, 0, len(profiles))
	for _, profile := range profiles {
		view = append(view, adminClaudeProfileViewFromConfig(profile))
	}
	return view
}

func adminClaudeProfileViewFromConfig(profile config.ClaudeProfile) adminClaudeProfileView {
	return adminClaudeProfileView{
		ID:           strings.TrimSpace(profile.ID),
		Name:         strings.TrimSpace(profile.Name),
		AuthMode:     config.NormalizeClaudeAuthMode(profile.AuthMode),
		BaseURL:      strings.TrimSpace(profile.BaseURL),
		HasAuthToken: strings.TrimSpace(profile.AuthToken) != "",
		Model:        strings.TrimSpace(profile.Model),
		SmallModel:   strings.TrimSpace(profile.SmallModel),
		BuiltIn:      profile.BuiltIn,
		Persisted:    !profile.BuiltIn,
		ReadOnly:     profile.BuiltIn,
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
