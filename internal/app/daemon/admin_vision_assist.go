package daemon

import (
	"net/http"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/singleturn"
)

type visionAssistResponse struct {
	Configured bool                        `json:"configured"`
	Settings   config.VisionAssistSettings `json:"settings"`
}

func (a *App) handleVisionAssistGet(w http.ResponseWriter, _ *http.Request) {
	loaded, err := a.loadAdminConfig()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "config_unavailable",
			Message: "failed to load config",
			Details: err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, visionAssistResponse{
		Configured: visionAssistConfigured(loaded.Config.VisionAssist),
		Settings:   loaded.Config.VisionAssist,
	})
}

func (a *App) handleVisionAssistPut(w http.ResponseWriter, r *http.Request) {
	var settings config.VisionAssistSettings
	if err := decodeJSONBody(r, &settings); err != nil {
		writeAPIError(w, http.StatusBadRequest, apiError{
			Code:    "invalid_request",
			Message: "failed to decode vision assist settings",
			Details: err.Error(),
		})
		return
	}
	settings.Protocol = strings.TrimSpace(settings.Protocol)
	settings.BaseURL = strings.TrimRight(strings.TrimSpace(settings.BaseURL), "/")
	settings.APIKeyEnv = strings.TrimSpace(settings.APIKeyEnv)
	settings.Model = strings.TrimSpace(settings.Model)
	if settings.Protocol != "" {
		switch singleturn.Protocol(settings.Protocol) {
		case singleturn.ProtocolOpenAIChat,
			singleturn.ProtocolOpenAIResponses,
			singleturn.ProtocolAnthropic,
			singleturn.ProtocolGemini:
		default:
			writeAPIError(w, http.StatusBadRequest, apiError{
				Code:    "vision_assist_protocol_invalid",
				Message: "unsupported vision assist protocol",
				Details: settings.Protocol,
			})
			return
		}
	}
	if settings.Protocol != "" && settings.BaseURL == "" {
		writeAPIError(w, http.StatusBadRequest, apiError{
			Code:    "vision_assist_base_url_required",
			Message: "base url is required when protocol is set",
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
	loaded.Config.VisionAssist = settings
	if err := config.WriteAppConfig(loaded.Path, loaded.Config); err != nil {
		a.adminConfigMu.Unlock()
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "config_write_failed",
			Message: "failed to save vision assist settings",
			Details: err.Error(),
		})
		return
	}
	a.adminConfigMu.Unlock()
	writeJSON(w, http.StatusOK, visionAssistResponse{
		Configured: visionAssistConfigured(settings),
		Settings:   settings,
	})
}

func visionAssistConfigured(settings config.VisionAssistSettings) bool {
	return strings.TrimSpace(settings.Protocol) != "" &&
		strings.TrimSpace(settings.BaseURL) != ""
}
