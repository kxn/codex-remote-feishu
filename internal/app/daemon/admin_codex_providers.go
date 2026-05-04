package daemon

import (
	"errors"
	"net/http"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/config"
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
	loaded, err := a.loadAdminConfig()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "config_unavailable",
			Message: "failed to load config",
			Details: err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, codexProvidersResponse{Providers: adminCodexProvidersView(loaded.Config)})
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
	requested := codexProviderConfigFromRequest(req)
	providerID := config.CodexProviderIDFromName(strings.TrimSpace(requested.Name))
	profileIndex := config.IndexOfCodexProvider(updated.Codex.Providers, providerID)

	var provider config.CodexProviderConfig
	if profileIndex >= 0 {
		provider, err = config.PrepareCodexProviderUpdate(updated.Codex.Providers, providerID, requested)
	} else {
		provider, err = config.PrepareCodexProviderCreate(updated.Codex.Providers, requested)
	}
	if err != nil {
		a.adminConfigMu.Unlock()
		writeCodexProviderConfigError(w, err)
		return
	}

	if index := config.IndexOfCodexProvider(updated.Codex.Providers, provider.ID); index >= 0 {
		updated.Codex.Providers[index] = provider
	} else {
		updated.Codex.Providers = append(updated.Codex.Providers, provider)
	}
	if err := config.WriteAppConfig(loaded.Path, updated); err != nil {
		a.adminConfigMu.Unlock()
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "config_write_failed",
			Message: "failed to save codex provider config",
			Details: err.Error(),
		})
		return
	}
	a.adminConfigMu.Unlock()
	a.mu.Lock()
	a.syncCodexProvidersCatalogLocked(updated)
	a.mu.Unlock()

	writeJSON(w, http.StatusCreated, codexProviderResponse{
		Provider: adminCodexProviderViewFromConfig(config.CodexProvider{CodexProviderConfig: provider}),
	})
}

func (a *App) handleCodexProviderUpdate(w http.ResponseWriter, r *http.Request) {
	providerID := config.CanonicalCodexProviderID(r.PathValue("id"))
	if config.IsBuiltInCodexProviderID(providerID) {
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
	provider, err := config.PrepareCodexProviderUpdate(updated.Codex.Providers, providerID, codexProviderConfigFromRequest(req))
	if err != nil {
		a.adminConfigMu.Unlock()
		writeCodexProviderConfigError(w, err)
		return
	}

	index := config.IndexOfCodexProvider(updated.Codex.Providers, providerID)
	if index < 0 {
		a.adminConfigMu.Unlock()
		writeAPIError(w, http.StatusNotFound, apiError{
			Code:    "codex_provider_not_found",
			Message: "codex provider not found",
			Details: providerID,
		})
		return
	}
	updated.Codex.Providers[index] = provider
	if err := config.WriteAppConfig(loaded.Path, updated); err != nil {
		a.adminConfigMu.Unlock()
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "config_write_failed",
			Message: "failed to save codex provider config",
			Details: err.Error(),
		})
		return
	}
	a.adminConfigMu.Unlock()
	a.mu.Lock()
	a.syncCodexProvidersCatalogLocked(updated)
	a.mu.Unlock()

	writeJSON(w, http.StatusOK, codexProviderResponse{
		Provider: adminCodexProviderViewFromConfig(config.CodexProvider{CodexProviderConfig: provider}),
	})
}

func (a *App) handleCodexProviderDelete(w http.ResponseWriter, r *http.Request) {
	providerID := config.CanonicalCodexProviderID(r.PathValue("id"))
	if config.IsBuiltInCodexProviderID(providerID) {
		writeAPIError(w, http.StatusConflict, apiError{
			Code:    "codex_provider_read_only",
			Message: "the built-in default codex provider cannot be deleted",
			Details: config.CodexDefaultProviderID,
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
	index := config.IndexOfCodexProvider(updated.Codex.Providers, providerID)
	if index < 0 {
		a.adminConfigMu.Unlock()
		writeAPIError(w, http.StatusNotFound, apiError{
			Code:    "codex_provider_not_found",
			Message: "codex provider not found",
			Details: providerID,
		})
		return
	}

	updated.Codex.Providers = append(updated.Codex.Providers[:index], updated.Codex.Providers[index+1:]...)
	if err := config.WriteAppConfig(loaded.Path, updated); err != nil {
		a.adminConfigMu.Unlock()
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "config_write_failed",
			Message: "failed to save codex provider config",
			Details: err.Error(),
		})
		return
	}
	a.adminConfigMu.Unlock()
	a.mu.Lock()
	a.syncCodexProvidersCatalogLocked(updated)
	a.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func adminPersistedCodexSettingsView(cfg config.AppConfig) adminCodexSettingsView {
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
