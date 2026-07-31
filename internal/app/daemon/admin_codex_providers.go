package daemon

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type adminCodexSettingsView struct {
	Providers []adminCodexProviderView `json:"providers,omitempty"`
}

type adminCodexProviderView struct {
	ID              string `json:"id"`
	Name            string `json:"name,omitempty"`
	BaseURL         string `json:"baseURL,omitempty"`
	HasAPIKey       bool   `json:"hasApiKey"`
	Model           string `json:"model,omitempty"`
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
	BuiltIn         bool   `json:"builtIn,omitempty"`
	Persisted       bool   `json:"persisted"`
	ReadOnly        bool   `json:"readOnly,omitempty"`
}

type codexProvidersResponse struct {
	Providers []adminCodexProviderView `json:"providers"`
}

type codexProviderResponse struct {
	Provider adminCodexProviderView `json:"provider"`
}

type codexProviderWriteRequest struct {
	Name            *string `json:"name"`
	BaseURL         *string `json:"baseURL"`
	APIKey          *string `json:"apiKey"`
	Model           *string `json:"model"`
	ReasoningEffort *string `json:"reasoningEffort"`
}

func (a *App) handleCodexProvidersList(w http.ResponseWriter, _ *http.Request) {
	a.adminConfigMu.Lock()
	profiles, err := a.codexProfileSummariesLocked()
	a.adminConfigMu.Unlock()
	if err != nil {
		writeCodexProfileServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, codexProvidersResponse{Providers: legacyCodexProviderViews(profiles)})
}

func (a *App) handleCodexProviderCreate(w http.ResponseWriter, r *http.Request) {
	var req codexProviderWriteRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, apiError{
			Code:    "invalid_request",
			Message: "failed to decode codex provider payload",
			Details: err.Error(),
		})
		return
	}

	a.adminConfigMu.Lock()
	profile, err := a.createCodexProfileLocked(codexAPIProfileInputFromLegacyRequest(req))
	a.adminConfigMu.Unlock()
	if err != nil {
		writeCodexProfileServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, codexProviderResponse{
		Provider: legacyCodexProviderView(profile),
	})
}

func (a *App) handleCodexProviderUpdate(w http.ResponseWriter, r *http.Request) {
	providerID := strings.TrimSpace(r.PathValue("id"))
	if config.IsBuiltInCodexProviderID(providerID) || providerID == config.CodexNativeProfileID {
		writeAPIError(w, http.StatusConflict, apiError{
			Code:    "codex_provider_read_only",
			Message: "the built-in default codex provider cannot be edited",
			Details: config.CodexDefaultProviderID,
		})
		return
	}

	var req codexProviderWriteRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, apiError{
			Code:    "invalid_request",
			Message: "failed to decode codex provider payload",
			Details: err.Error(),
		})
		return
	}

	a.adminConfigMu.Lock()
	profiles, err := a.codexProfileSummariesLocked()
	current, found := findCodexProfileSummary(profiles, providerID)
	if err == nil && !found {
		err = fmt.Errorf("profile_not_found")
	}
	var profile state.CodexProfileSummary
	if err == nil {
		input := codexAPIProfileInputFromLegacyRequest(req)
		input.ReviewModel = current.ReviewModel
		profile, _, err = a.updateCodexProfileLocked(providerID, current.ETag, input)
	}
	a.adminConfigMu.Unlock()
	if err != nil {
		writeCodexProfileServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, codexProviderResponse{
		Provider: legacyCodexProviderView(profile),
	})
}

func (a *App) handleCodexProviderDelete(w http.ResponseWriter, r *http.Request) {
	providerID := strings.TrimSpace(r.PathValue("id"))
	if config.IsBuiltInCodexProviderID(providerID) || providerID == config.CodexNativeProfileID {
		writeAPIError(w, http.StatusConflict, apiError{
			Code:    "codex_provider_read_only",
			Message: "the built-in default codex provider cannot be deleted",
			Details: config.CodexDefaultProviderID,
		})
		return
	}

	a.adminConfigMu.Lock()
	profiles, err := a.codexProfileSummariesLocked()
	current, found := findCodexProfileSummary(profiles, providerID)
	if err == nil && !found {
		err = fmt.Errorf("profile_not_found")
	}
	if err == nil {
		_, _, err = a.deleteCodexProfileLocked(providerID, current.ETag)
	}
	a.adminConfigMu.Unlock()
	if err != nil {
		writeCodexProfileServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func adminPersistedCodexSettingsView(cfg config.AppConfig) adminCodexSettingsView {
	if len(cfg.Codex.Profiles) > 0 {
		view := adminCodexSettingsView{Providers: make([]adminCodexProviderView, 0, len(cfg.Codex.Profiles))}
		for _, record := range config.NormalizeCodexAPIProfileRecords(cfg.Codex.Profiles) {
			profile, ok := config.CurrentCodexAPIProfile(record)
			if ok {
				view.Providers = append(view.Providers, legacyCodexProviderView(codexAPIProfileSummary(profile, state.ProfileContextPreference{})))
			}
		}
		return view
	}
	providers := config.NormalizeCodexProviders(cfg.Codex.Providers)
	view := adminCodexSettingsView{Providers: make([]adminCodexProviderView, 0, len(providers))}
	for _, provider := range providers {
		view.Providers = append(view.Providers, adminCodexProviderViewFromConfig(config.CodexProvider{CodexProviderConfig: provider}))
	}
	if len(view.Providers) == 0 {
		view.Providers = nil
	}
	return view
}

func legacyCodexProviderViews(profiles []state.CodexProfileSummary) []adminCodexProviderView {
	views := make([]adminCodexProviderView, 0, len(profiles))
	for _, profile := range profiles {
		if profile.Kind == state.CodexProfileKindOAuth {
			continue
		}
		views = append(views, legacyCodexProviderView(profile))
	}
	return views
}

func legacyCodexProviderView(profile state.CodexProfileSummary) adminCodexProviderView {
	if profile.Kind == state.CodexProfileKindNative {
		return adminCodexProviderView{
			ID:       config.CodexDefaultProviderID,
			Name:     config.CodexDefaultProviderName,
			BuiltIn:  true,
			ReadOnly: true,
		}
	}
	return adminCodexProviderView{
		ID:              profile.ID,
		Name:            profile.Name,
		BaseURL:         profile.BaseURL,
		HasAPIKey:       profile.HasAPIKey,
		Model:           profile.Model,
		ReasoningEffort: profile.ReasoningEffort,
		Persisted:       true,
	}
}

func findCodexProfileSummary(profiles []state.CodexProfileSummary, profileID string) (state.CodexProfileSummary, bool) {
	for _, profile := range profiles {
		if profile.ID == strings.TrimSpace(profileID) {
			return profile, true
		}
	}
	return state.CodexProfileSummary{}, false
}

func codexAPIProfileInputFromLegacyRequest(req codexProviderWriteRequest) config.CodexAPIProfileInput {
	return config.CodexAPIProfileInput{
		Name:            optionalStringValue(req.Name),
		BaseURL:         optionalStringValue(req.BaseURL),
		APIKey:          rawOptionalStringValue(req.APIKey),
		Model:           optionalStringValue(req.Model),
		ReasoningEffort: optionalStringValue(req.ReasoningEffort),
	}
}

func adminCodexProvidersView(cfg config.AppConfig) []adminCodexProviderView {
	providers := config.ListCodexProviders(cfg)
	view := make([]adminCodexProviderView, 0, len(providers))
	for _, provider := range providers {
		view = append(view, adminCodexProviderViewFromConfig(provider))
	}
	return view
}

func adminCodexProviderViewFromConfig(provider config.CodexProvider) adminCodexProviderView {
	return adminCodexProviderView{
		ID:              strings.TrimSpace(provider.ID),
		Name:            strings.TrimSpace(provider.Name),
		BaseURL:         strings.TrimSpace(provider.BaseURL),
		HasAPIKey:       strings.TrimSpace(provider.APIKey) != "",
		Model:           strings.TrimSpace(provider.Model),
		ReasoningEffort: config.NormalizeCodexReasoningEffort(provider.ReasoningEffort),
		BuiltIn:         provider.BuiltIn,
		Persisted:       !provider.BuiltIn,
		ReadOnly:        provider.BuiltIn,
	}
}

func codexProviderConfigFromRequest(req codexProviderWriteRequest) config.CodexProviderConfig {
	return config.CodexProviderConfig{
		Name:            optionalStringValue(req.Name),
		BaseURL:         optionalStringValue(req.BaseURL),
		APIKey:          optionalStringValue(req.APIKey),
		Model:           optionalStringValue(req.Model),
		ReasoningEffort: optionalStringValue(req.ReasoningEffort),
	}
}

func writeCodexProviderConfigError(w http.ResponseWriter, err error) {
	switch {
	case err == nil:
		writeAPIError(w, http.StatusBadRequest, apiError{
			Code:    "invalid_codex_provider",
			Message: "invalid codex provider config",
		})
	case errors.Is(err, http.ErrMissingFile):
		writeAPIError(w, http.StatusBadRequest, apiError{
			Code:    "invalid_codex_provider",
			Message: "invalid codex provider config",
		})
	case strings.Contains(err.Error(), "not found"):
		writeAPIError(w, http.StatusNotFound, apiError{
			Code:    "codex_provider_not_found",
			Message: "codex provider not found",
		})
	case strings.Contains(err.Error(), "name is required"):
		writeAPIError(w, http.StatusBadRequest, apiError{
			Code:    "codex_provider_name_required",
			Message: "codex provider name is required",
		})
	case strings.Contains(err.Error(), "baseURL is required"):
		writeAPIError(w, http.StatusBadRequest, apiError{
			Code:    "codex_provider_base_url_required",
			Message: "codex provider baseURL is required",
		})
	case strings.Contains(err.Error(), "apiKey is required"):
		writeAPIError(w, http.StatusBadRequest, apiError{
			Code:    "codex_provider_api_key_required",
			Message: "codex provider apiKey is required",
		})
	case strings.Contains(err.Error(), "reasoningEffort is invalid"):
		writeAPIError(w, http.StatusBadRequest, apiError{
			Code:    "codex_provider_reasoning_effort_invalid",
			Message: "codex provider reasoningEffort is invalid",
		})
	case strings.Contains(err.Error(), "cannot be replaced"):
		writeAPIError(w, http.StatusConflict, apiError{
			Code:    "codex_provider_read_only",
			Message: "the built-in default codex provider cannot be replaced",
			Details: config.CodexDefaultProviderID,
		})
	case strings.Contains(err.Error(), "is reserved"):
		writeAPIError(w, http.StatusConflict, apiError{
			Code:    "codex_provider_reserved_name",
			Message: "this codex provider name cannot be used",
		})
	case strings.Contains(err.Error(), "already exists"):
		writeAPIError(w, http.StatusConflict, apiError{
			Code:    "duplicate_codex_provider_name",
			Message: "codex provider name already exists",
		})
	default:
		writeAPIError(w, http.StatusBadRequest, apiError{
			Code:    "invalid_codex_provider",
			Message: "invalid codex provider config",
			Details: err.Error(),
		})
	}
}
