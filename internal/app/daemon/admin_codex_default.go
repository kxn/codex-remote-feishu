package daemon

import (
	"net/http"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/config"
)

type codexRemoteDefaultResponse struct {
	Model           string `json:"model"`
	ReasoningEffort string `json:"reasoningEffort"`
}

func (a *App) handleCodexRemoteDefaultGet(w http.ResponseWriter, _ *http.Request) {
	loaded, err := a.loadAdminConfig()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, apiError{Code: "config_unavailable", Message: "failed to load config", Details: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, codexRemoteDefaultResponse{Model: loaded.Config.Codex.DefaultModel, ReasoningEffort: loaded.Config.Codex.DefaultReasoningEffort})
}

func (a *App) handleCodexRemoteDefaultPut(w http.ResponseWriter, r *http.Request) {
	var value codexRemoteDefaultResponse
	if err := decodeJSONBody(r, &value); err != nil {
		writeAPIError(w, http.StatusBadRequest, apiError{Code: "invalid_request", Message: "failed to decode default model", Details: err.Error()})
		return
	}
	value.Model, value.ReasoningEffort = strings.TrimSpace(value.Model), strings.TrimSpace(value.ReasoningEffort)
	if err := config.ValidateCodexRemoteDefault(value.Model, value.ReasoningEffort); err != nil {
		writeAPIError(w, http.StatusBadRequest, apiError{Code: "codex_default_invalid", Message: err.Error()})
		return
	}
	a.adminConfigMu.Lock()
	loaded, err := a.loadAdminConfig()
	if err != nil {
		a.adminConfigMu.Unlock()
		writeAPIError(w, http.StatusInternalServerError, apiError{Code: "config_unavailable", Message: "failed to load config", Details: err.Error()})
		return
	}
	loaded.Config.Codex.DefaultModel = value.Model
	loaded.Config.Codex.DefaultReasoningEffort = value.ReasoningEffort
	if err := config.WriteAppConfig(loaded.Path, loaded.Config); err != nil {
		a.adminConfigMu.Unlock()
		writeAPIError(w, http.StatusInternalServerError, apiError{Code: "config_write_failed", Message: "failed to save default model", Details: err.Error()})
		return
	}
	a.mu.Lock()
	a.service.SetCodexRemoteDefault(value.Model, value.ReasoningEffort)
	a.mu.Unlock()
	a.adminConfigMu.Unlock()
	writeJSON(w, http.StatusOK, value)
}
