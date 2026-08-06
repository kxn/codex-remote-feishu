package daemon

import (
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
		input.SubagentModel = current.SubagentModel
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
