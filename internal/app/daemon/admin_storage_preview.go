package daemon

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	previewpkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/preview"
)

var newPreviewDriveAdminService = func(cfg feishu.GatewayAppConfig) previewpkg.PreviewDriveAdminService {
	var api = feishu.NewLarkDrivePreviewAPI(cfg.GatewayID, nil)
	if strings.TrimSpace(cfg.AppID) != "" && strings.TrimSpace(cfg.AppSecret) != "" {
		api = feishu.NewLarkDrivePreviewAPI(cfg.GatewayID, feishu.NewLarkClient(cfg.AppID, cfg.AppSecret))
	}
	return previewpkg.NewDriveMarkdownPreviewer(api, previewpkg.MarkdownPreviewConfig{
		StatePath: cfg.PreviewStatePath,
		GatewayID: cfg.GatewayID,
	})
}

type previewDriveStatusResponse struct {
	GatewayID string                         `json:"gatewayId"`
	Name      string                         `json:"name,omitempty"`
	Summary   previewpkg.PreviewDriveSummary `json:"summary"`
}

type previewDriveCleanupRequest struct {
	OlderThanHours int `json:"olderThanHours,omitempty"`
}

type previewDriveCleanupResponse struct {
	GatewayID      string                               `json:"gatewayId"`
	Name           string                               `json:"name,omitempty"`
	OlderThanHours int                                  `json:"olderThanHours"`
	Result         previewpkg.PreviewDriveCleanupResult `json:"result"`
}

func (a *App) handlePreviewDriveStatus(w http.ResponseWriter, r *http.Request) {
	runtimeCfg, err := a.previewDriveRuntimeConfig(strings.TrimSpace(r.PathValue("id")))
	if err != nil {
		writePreviewDriveRuntimeError(w, err)
		return
	}
	admin := newPreviewDriveAdminService(runtimeCfg)
	if admin == nil {
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "preview_drive_admin_unavailable",
			Message: "preview drive admin service is not available",
		})
		return
	}
	summary, err := admin.Summary(r.Context())
	if err != nil {
		writePreviewDriveAdminError(w, "failed to summarize preview drive inventory", "preview_drive_summary_failed", err)
		return
	}
	writeJSON(w, http.StatusOK, previewDriveStatusResponse{
		GatewayID: runtimeCfg.GatewayID,
		Name:      strings.TrimSpace(runtimeCfg.Name),
		Summary:   summary,
	})
}

func (a *App) handlePreviewDriveCleanup(w http.ResponseWriter, r *http.Request) {
	req := previewDriveCleanupRequest{OlderThanHours: defaultImageStagingCleanupHours}
	if r.Body != nil && r.Body != http.NoBody {
		defer r.Body.Close()
		if body, err := io.ReadAll(r.Body); err != nil {
			writeAPIError(w, http.StatusBadRequest, apiError{
				Code:    "invalid_request",
				Message: "failed to read preview drive cleanup payload",
				Details: err.Error(),
			})
			return
		} else if len(strings.TrimSpace(string(body))) > 0 {
			r.Body = io.NopCloser(strings.NewReader(string(body)))
			if err := decodeJSONBody(r, &req); err != nil && !errors.Is(err, io.EOF) {
				writeAPIError(w, http.StatusBadRequest, apiError{
					Code:    "invalid_request",
					Message: "failed to decode preview drive cleanup payload",
					Details: err.Error(),
				})
				return
			}
		}
	}
	if req.OlderThanHours <= 0 {
		req.OlderThanHours = defaultImageStagingCleanupHours
	}

	runtimeCfg, err := a.previewDriveRuntimeConfig(strings.TrimSpace(r.PathValue("id")))
	if err != nil {
		writePreviewDriveRuntimeError(w, err)
		return
	}
	admin := newPreviewDriveAdminService(runtimeCfg)
	if admin == nil {
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "preview_drive_admin_unavailable",
			Message: "preview drive admin service is not available",
		})
		return
	}
	result, err := admin.CleanupBefore(r.Context(), time.Now().Add(-time.Duration(req.OlderThanHours)*time.Hour))
	if err != nil {
		writePreviewDriveAdminError(w, "failed to cleanup preview drive files", "preview_drive_cleanup_failed", err)
		return
	}
	writeJSON(w, http.StatusOK, previewDriveCleanupResponse{
		GatewayID:      runtimeCfg.GatewayID,
		Name:           strings.TrimSpace(runtimeCfg.Name),
		OlderThanHours: req.OlderThanHours,
		Result:         result,
	})
}

func (a *App) previewDriveRuntimeConfig(gatewayID string) (feishu.GatewayAppConfig, error) {
	gatewayID = strings.TrimSpace(gatewayID)
	if gatewayID == "" {
		return feishu.GatewayAppConfig{}, errors.New("preview_drive_gateway_id_required")
	}
	loaded, err := a.loadAdminConfig()
	if err != nil {
		return feishu.GatewayAppConfig{}, err
	}
	runtimeCfg, ok := a.runtimeGatewayConfigFor(loaded.Config, gatewayID)
	if !ok {
		return feishu.GatewayAppConfig{}, errors.New("preview_drive_gateway_not_found")
	}
	return runtimeCfg, nil
}

func writePreviewDriveRuntimeError(w http.ResponseWriter, err error) {
	switch err.Error() {
	case "preview_drive_gateway_id_required":
		writeAPIError(w, http.StatusBadRequest, apiError{
			Code:    "preview_drive_gateway_id_required",
			Message: "preview drive gateway id is required",
		})
	case "preview_drive_gateway_not_found":
		writeAPIError(w, http.StatusNotFound, apiError{
			Code:    "preview_drive_gateway_not_found",
			Message: "preview drive gateway not found",
		})
	default:
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "config_load_failed",
			Message: "failed to load preview drive runtime config",
			Details: err.Error(),
		})
	}
}

func writePreviewDriveAdminError(w http.ResponseWriter, message, code string, err error) {
	status := http.StatusInternalServerError
	if strings.Contains(err.Error(), "api is not available") {
		status = http.StatusConflict
		code = "preview_drive_api_unavailable"
	}
	writeAPIError(w, status, apiError{
		Code:    code,
		Message: message,
		Details: err.Error(),
	})
}
