package daemon

import (
	"net/http"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/singleturn"
)

type visionAssistResponse struct {
	Configured bool                        `json:"configured"`
	HasAPIKey  bool                        `json:"hasAPIKey"`
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
	writeVisionAssistResponse(w, loaded.Config.VisionAssist)
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
	if settings.Protocol == "" {
		// 协议必须有值才能工作；OpenAI Chat 是最通用的默认。
		settings.Protocol = string(singleturn.ProtocolOpenAIChat)
	}
	settings.BaseURL = strings.TrimRight(strings.TrimSpace(settings.BaseURL), "/")
	settings.APIKey = strings.TrimSpace(settings.APIKey)
	settings.Model = strings.TrimSpace(settings.Model)
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
	if settings.BaseURL == "" {
		writeAPIError(w, http.StatusBadRequest, apiError{
			Code:    "vision_assist_base_url_required",
			Message: "base url is required",
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
	// 与其他 profile 一致：PUT 未提供 APIKey 时保留现有明文 key。
	if settings.APIKey == "" {
		settings.APIKey = strings.TrimSpace(loaded.Config.VisionAssist.APIKey)
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
	writeVisionAssistResponse(w, settings)
}

func visionAssistConfigured(settings config.VisionAssistSettings) bool {
	return strings.TrimSpace(settings.Protocol) != "" &&
		strings.TrimSpace(settings.BaseURL) != ""
}

// writeVisionAssistResponse 不回显明文 API key，只以 hasAPIKey 表示是否已配置。
func writeVisionAssistResponse(w http.ResponseWriter, settings config.VisionAssistSettings) {
	writeJSON(w, http.StatusOK, visionAssistResponse{
		Configured: visionAssistConfigured(settings),
		HasAPIKey:  strings.TrimSpace(settings.APIKey) != "",
		Settings: config.VisionAssistSettings{
			Protocol: settings.Protocol,
			BaseURL:  settings.BaseURL,
			Model:    settings.Model,
		},
	})
}
