package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/feishuapp"
)

type feishuManifestResponse struct {
	Manifest feishuapp.Manifest `json:"manifest"`
}

type feishuAppsResponse struct {
	Apps []adminFeishuAppSummary `json:"apps"`
}

type feishuAppResponse struct {
	App adminFeishuAppSummary `json:"app"`
}

type feishuAppVerifyResponse struct {
	App    adminFeishuAppSummary `json:"app"`
	Result feishu.VerifyResult   `json:"result"`
}

type feishuAppWriteRequest struct {
	ID        string  `json:"id,omitempty"`
	Name      *string `json:"name,omitempty"`
	AppID     *string `json:"appId,omitempty"`
	AppSecret *string `json:"appSecret,omitempty"`
	Enabled   *bool   `json:"enabled,omitempty"`
}

type feishuAppWizardUpdateRequest struct {
	ScopesExported     *bool `json:"scopesExported,omitempty"`
	EventsConfirmed    *bool `json:"eventsConfirmed,omitempty"`
	CallbacksConfirmed *bool `json:"callbacksConfirmed,omitempty"`
	MenusConfirmed     *bool `json:"menusConfirmed,omitempty"`
	Published          *bool `json:"published,omitempty"`
}

type adminFeishuAppWizardView struct {
	CredentialsSavedAt   *time.Time `json:"credentialsSavedAt,omitempty"`
	ConnectionVerifiedAt *time.Time `json:"connectionVerifiedAt,omitempty"`
	ScopesExportedAt     *time.Time `json:"scopesExportedAt,omitempty"`
	EventsConfirmedAt    *time.Time `json:"eventsConfirmedAt,omitempty"`
	CallbacksConfirmedAt *time.Time `json:"callbacksConfirmedAt,omitempty"`
	MenusConfirmedAt     *time.Time `json:"menusConfirmedAt,omitempty"`
	PublishedAt          *time.Time `json:"publishedAt,omitempty"`
}

type adminFeishuAppSummary struct {
	ID              string                    `json:"id"`
	Name            string                    `json:"name,omitempty"`
	AppID           string                    `json:"appId,omitempty"`
	HasSecret       bool                      `json:"hasSecret"`
	Enabled         bool                      `json:"enabled"`
	VerifiedAt      *time.Time                `json:"verifiedAt,omitempty"`
	Wizard          *adminFeishuAppWizardView `json:"wizard,omitempty"`
	Persisted       bool                      `json:"persisted"`
	RuntimeOnly     bool                      `json:"runtimeOnly,omitempty"`
	RuntimeOverride bool                      `json:"runtimeOverride,omitempty"`
	ReadOnly        bool                      `json:"readOnly,omitempty"`
	ReadOnlyReason  string                    `json:"readOnlyReason,omitempty"`
	Status          *feishu.GatewayStatus     `json:"status,omitempty"`
}

func (a *App) handleFeishuManifest(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, feishuManifestResponse{Manifest: feishuapp.DefaultManifest()})
}

func (a *App) handleFeishuAppsList(w http.ResponseWriter, _ *http.Request) {
	loaded, err := a.loadAdminConfig()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "config_unavailable",
			Message: "failed to load config",
			Details: err.Error(),
		})
		return
	}
	apps, err := a.adminFeishuApps(loaded)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "feishu_apps_unavailable",
			Message: "failed to build feishu app list",
			Details: err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, feishuAppsResponse{Apps: apps})
}

func (a *App) handleFeishuAppCreate(w http.ResponseWriter, r *http.Request) {
	var req feishuAppWriteRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, apiError{
			Code:    "invalid_request",
			Message: "failed to decode feishu app payload",
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

	admin := a.snapshotAdminRuntime()
	updated := loaded.Config
	gatewayID := canonicalGatewayID(req.ID)
	if strings.TrimSpace(req.ID) == "" {
		gatewayID = nextGatewayID(updated.Feishu.Apps, admin, req)
	}
	if readOnly, reason := feishuAppReadOnly(admin, gatewayID); readOnly {
		a.adminConfigMu.Unlock()
		writeAPIError(w, http.StatusConflict, apiError{
			Code:    "runtime_override_read_only",
			Message: "the runtime override app cannot be edited from web config",
			Details: reason,
		})
		return
	}
	if indexOfConfigFeishuApp(updated.Feishu.Apps, gatewayID) >= 0 {
		a.adminConfigMu.Unlock()
		writeAPIError(w, http.StatusConflict, apiError{
			Code:    "duplicate_gateway_id",
			Message: "feishu app id already exists",
			Details: gatewayID,
		})
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	now := time.Now().UTC()
	app := config.FeishuAppConfig{
		ID:      gatewayID,
		Name:    firstNonEmpty(trimmedString(req.Name), trimmedString(req.AppID), gatewayID),
		AppID:   trimmedString(req.AppID),
		Enabled: daemonBoolPtr(enabled),
	}
	if secret := trimmedString(req.AppSecret); secret != "" {
		app.AppSecret = secret
	}
	markFeishuCredentialsSaved(&app, now)
	updated.Feishu.Apps = append(updated.Feishu.Apps, app)
	if err := config.WriteAppConfig(loaded.Path, updated); err != nil {
		a.adminConfigMu.Unlock()
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "config_write_failed",
			Message: "failed to save feishu app config",
			Details: err.Error(),
		})
		return
	}
	a.adminConfigMu.Unlock()

	if err := a.applyRuntimeFeishuConfig(updated, gatewayID); err != nil {
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "gateway_apply_failed",
			Message: "feishu config saved but runtime apply failed",
			Details: err.Error(),
		})
		return
	}

	summary, ok, err := a.adminFeishuAppSummary(config.LoadedAppConfig{Path: loaded.Path, Config: updated}, gatewayID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "feishu_app_unavailable",
			Message: "failed to load saved feishu app",
			Details: err.Error(),
		})
		return
	}
	if !ok {
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "feishu_app_missing",
			Message: "saved feishu app could not be reloaded",
			Details: gatewayID,
		})
		return
	}
	writeJSON(w, http.StatusCreated, feishuAppResponse{App: summary})
}

func (a *App) handleFeishuAppUpdate(w http.ResponseWriter, r *http.Request) {
	gatewayID := canonicalGatewayID(r.PathValue("id"))
	var req feishuAppWriteRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, apiError{
			Code:    "invalid_request",
			Message: "failed to decode feishu app payload",
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
	admin := a.snapshotAdminRuntime()
	if readOnly, reason := feishuAppReadOnly(admin, gatewayID); readOnly {
		a.adminConfigMu.Unlock()
		writeAPIError(w, http.StatusConflict, apiError{
			Code:    "runtime_override_read_only",
			Message: "the runtime override app cannot be edited from web config",
			Details: reason,
		})
		return
	}
	updated := loaded.Config
	index := indexOfConfigFeishuApp(updated.Feishu.Apps, gatewayID)
	if index < 0 {
		a.adminConfigMu.Unlock()
		writeAPIError(w, http.StatusNotFound, apiError{
			Code:    "feishu_app_not_found",
			Message: "feishu app not found",
			Details: gatewayID,
		})
		return
	}

	current := updated.Feishu.Apps[index]
	appIDChanged := false
	secretChanged := false
	if name := trimmedString(req.Name); name != "" {
		current.Name = name
	}
	if appID := trimmedString(req.AppID); appID != "" && appID != current.AppID {
		current.AppID = appID
		appIDChanged = true
	}
	if secret := trimmedString(req.AppSecret); secret != "" && secret != current.AppSecret {
		current.AppSecret = secret
		secretChanged = true
	}
	if req.Enabled != nil {
		current.Enabled = daemonBoolPtr(*req.Enabled)
	}
	if appIDChanged || secretChanged {
		now := time.Now().UTC()
		markFeishuCredentialsSaved(&current, now)
		resetFeishuVerification(&current)
		if appIDChanged {
			resetFeishuWizardManualSteps(&current)
		}
	}
	updated.Feishu.Apps[index] = current
	if err := config.WriteAppConfig(loaded.Path, updated); err != nil {
		a.adminConfigMu.Unlock()
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "config_write_failed",
			Message: "failed to save feishu app config",
			Details: err.Error(),
		})
		return
	}
	a.adminConfigMu.Unlock()

	if err := a.applyRuntimeFeishuConfig(updated, gatewayID); err != nil {
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "gateway_apply_failed",
			Message: "feishu config saved but runtime apply failed",
			Details: err.Error(),
		})
		return
	}

	summary, _, err := a.adminFeishuAppSummary(config.LoadedAppConfig{Path: loaded.Path, Config: updated}, gatewayID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "feishu_app_unavailable",
			Message: "failed to load updated feishu app",
			Details: err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, feishuAppResponse{App: summary})
}

func (a *App) handleFeishuAppWizardUpdate(w http.ResponseWriter, r *http.Request) {
	gatewayID := canonicalGatewayID(r.PathValue("id"))
	var req feishuAppWizardUpdateRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, apiError{
			Code:    "invalid_request",
			Message: "failed to decode feishu wizard payload",
			Details: err.Error(),
		})
		return
	}

	updated, err := a.updateFeishuAppWizard(gatewayID, req, time.Now().UTC())
	if err != nil {
		a.writeFeishuMutationError(w, gatewayID, err)
		return
	}
	summary, _, err := a.adminFeishuAppSummary(updated, gatewayID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "feishu_app_unavailable",
			Message: "failed to load feishu app after wizard update",
			Details: gatewayID,
		})
		return
	}
	writeJSON(w, http.StatusOK, feishuAppResponse{App: summary})
}

func (a *App) handleFeishuAppDelete(w http.ResponseWriter, r *http.Request) {
	gatewayID := canonicalGatewayID(r.PathValue("id"))

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
	admin := a.snapshotAdminRuntime()
	if readOnly, reason := feishuAppReadOnly(admin, gatewayID); readOnly {
		a.adminConfigMu.Unlock()
		writeAPIError(w, http.StatusConflict, apiError{
			Code:    "runtime_override_read_only",
			Message: "the runtime override app cannot be deleted from web config",
			Details: reason,
		})
		return
	}
	updated := loaded.Config
	index := indexOfConfigFeishuApp(updated.Feishu.Apps, gatewayID)
	if index < 0 {
		a.adminConfigMu.Unlock()
		writeAPIError(w, http.StatusNotFound, apiError{
			Code:    "feishu_app_not_found",
			Message: "feishu app not found",
			Details: gatewayID,
		})
		return
	}
	updated.Feishu.Apps = append(updated.Feishu.Apps[:index], updated.Feishu.Apps[index+1:]...)
	if err := config.WriteAppConfig(loaded.Path, updated); err != nil {
		a.adminConfigMu.Unlock()
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "config_write_failed",
			Message: "failed to save feishu app config",
			Details: err.Error(),
		})
		return
	}
	a.adminConfigMu.Unlock()

	if err := a.applyRuntimeFeishuConfig(updated, gatewayID); err != nil {
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "gateway_apply_failed",
			Message: "feishu config saved but runtime apply failed",
			Details: err.Error(),
		})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleFeishuAppVerify(w http.ResponseWriter, r *http.Request) {
	gatewayID := canonicalGatewayID(r.PathValue("id"))
	loaded, err := a.loadAdminConfig()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "config_unavailable",
			Message: "failed to load config",
			Details: err.Error(),
		})
		return
	}

	runtimeCfg, ok := a.runtimeGatewayConfigFor(loaded.Config, gatewayID)
	if !ok {
		writeAPIError(w, http.StatusNotFound, apiError{
			Code:    "feishu_app_not_found",
			Message: "feishu app not found",
			Details: gatewayID,
		})
		return
	}
	controller, err := a.gatewayController()
	if err != nil {
		writeAPIError(w, http.StatusNotImplemented, apiError{
			Code:    "gateway_controller_unavailable",
			Message: "current gateway does not support runtime feishu management",
			Details: err.Error(),
		})
		return
	}
	verifyCtx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	result, verifyErr := controller.Verify(verifyCtx, runtimeCfg)

	admin := a.snapshotAdminRuntime()
	if readOnly, _ := feishuAppReadOnly(admin, gatewayID); verifyErr == nil && !readOnly && !isRuntimeOnlyFeishuApp(admin, loaded.Config, gatewayID) {
		if err := a.markFeishuAppVerified(loaded.Path, gatewayID, time.Now().UTC()); err != nil {
			writeAPIError(w, http.StatusInternalServerError, apiError{
				Code:    "config_write_failed",
				Message: "feishu app verified but failed to persist verification time",
				Details: err.Error(),
			})
			return
		}
		loaded, err = a.loadAdminConfig()
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, apiError{
				Code:    "config_unavailable",
				Message: "failed to reload config after verification",
				Details: err.Error(),
			})
			return
		}
	}

	summary, ok, err := a.adminFeishuAppSummary(loaded, gatewayID)
	if err != nil || !ok {
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "feishu_app_unavailable",
			Message: "failed to load feishu app after verification",
			Details: gatewayID,
		})
		return
	}

	if verifyErr != nil {
		writeJSON(w, http.StatusBadGateway, feishuAppVerifyResponse{
			App:    summary,
			Result: result,
		})
		return
	}
	writeJSON(w, http.StatusOK, feishuAppVerifyResponse{
		App:    summary,
		Result: result,
	})
}

func (a *App) handleFeishuAppReconnect(w http.ResponseWriter, r *http.Request) {
	gatewayID := canonicalGatewayID(r.PathValue("id"))
	loaded, err := a.loadAdminConfig()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "config_unavailable",
			Message: "failed to load config",
			Details: err.Error(),
		})
		return
	}
	if _, ok, err := a.adminFeishuAppSummary(loaded, gatewayID); err != nil || !ok {
		writeAPIError(w, http.StatusNotFound, apiError{
			Code:    "feishu_app_not_found",
			Message: "feishu app not found",
			Details: gatewayID,
		})
		return
	}
	if err := a.applyRuntimeFeishuConfig(loaded.Config, gatewayID); err != nil {
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "gateway_apply_failed",
			Message: "failed to reconnect feishu runtime",
			Details: err.Error(),
		})
		return
	}
	summary, _, err := a.adminFeishuAppSummary(loaded, gatewayID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "feishu_app_unavailable",
			Message: "failed to load feishu app after reconnect",
			Details: gatewayID,
		})
		return
	}
	writeJSON(w, http.StatusOK, feishuAppResponse{App: summary})
}

func (a *App) handleFeishuAppEnable(w http.ResponseWriter, r *http.Request) {
	a.handleFeishuAppRuntimeAction(w, r, daemonBoolPtr(true))
}

func (a *App) handleFeishuAppDisable(w http.ResponseWriter, r *http.Request) {
	a.handleFeishuAppRuntimeAction(w, r, daemonBoolPtr(false))
}

func (a *App) handleFeishuAppRuntimeAction(w http.ResponseWriter, r *http.Request, enabled *bool) {
	gatewayID := canonicalGatewayID(r.PathValue("id"))
	_, updated, err := a.setFeishuAppEnabled(gatewayID, enabled)
	if err != nil {
		a.writeFeishuMutationError(w, gatewayID, err)
		return
	}
	if err := a.applyRuntimeFeishuConfig(updated.Config, gatewayID); err != nil {
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "gateway_apply_failed",
			Message: "failed to apply feishu runtime config",
			Details: err.Error(),
		})
		return
	}
	summary, _, err := a.adminFeishuAppSummary(updated, gatewayID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "feishu_app_unavailable",
			Message: "failed to load feishu app after runtime action",
			Details: gatewayID,
		})
		return
	}
	writeJSON(w, http.StatusOK, feishuAppResponse{App: summary})
}

func (a *App) handleFeishuAppScopesJSON(w http.ResponseWriter, r *http.Request) {
	gatewayID := canonicalGatewayID(r.PathValue("id"))
	loaded, err := a.loadAdminConfig()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "config_unavailable",
			Message: "failed to load config",
			Details: err.Error(),
		})
		return
	}
	if _, ok, err := a.adminFeishuAppSummary(loaded, gatewayID); err != nil || !ok {
		writeAPIError(w, http.StatusNotFound, apiError{
			Code:    "feishu_app_not_found",
			Message: "feishu app not found",
			Details: gatewayID,
		})
		return
	}
	writeJSON(w, http.StatusOK, feishuapp.DefaultManifest().Scopes)
}

func (a *App) adminFeishuApps(loaded config.LoadedAppConfig) ([]adminFeishuAppSummary, error) {
	admin := a.snapshotAdminRuntime()
	runtimeApps := effectiveFeishuApps(loaded.Config, admin.services)
	runtimeMap := make(map[string]config.FeishuAppConfig, len(runtimeApps))
	for _, app := range runtimeApps {
		runtimeMap[canonicalGatewayID(app.ID)] = app
	}
	statuses := make(map[string]feishu.GatewayStatus)
	for _, status := range gatewayStatuses(a.gateway) {
		statuses[canonicalGatewayID(status.GatewayID)] = status
	}

	summaries := make([]adminFeishuAppSummary, 0, len(loaded.Config.Feishu.Apps)+1)
	seen := map[string]bool{}
	for _, app := range loaded.Config.Feishu.Apps {
		gatewayID := canonicalGatewayID(app.ID)
		runtimeApp, ok := runtimeMap[gatewayID]
		if !ok {
			runtimeApp = app
		}
		readOnly, reason := feishuAppReadOnly(admin, gatewayID)
		summary := buildFeishuAppSummary(gatewayID, app, runtimeApp, statuses[gatewayID], true, false, readOnly, reason)
		summaries = append(summaries, summary)
		seen[gatewayID] = true
	}

	if admin.envOverrideActive {
		gatewayID := canonicalGatewayID(admin.envOverrideGatewayID)
		if !seen[gatewayID] {
			if runtimeApp, ok := runtimeMap[gatewayID]; ok {
				readOnly, reason := feishuAppReadOnly(admin, gatewayID)
				summaries = append(summaries, buildFeishuAppSummary(gatewayID, config.FeishuAppConfig{}, runtimeApp, statuses[gatewayID], false, true, readOnly, reason))
			}
		}
	}
	return summaries, nil
}

func (a *App) adminFeishuAppSummary(loaded config.LoadedAppConfig, gatewayID string) (adminFeishuAppSummary, bool, error) {
	apps, err := a.adminFeishuApps(loaded)
	if err != nil {
		return adminFeishuAppSummary{}, false, err
	}
	for _, app := range apps {
		if canonicalGatewayID(app.ID) == canonicalGatewayID(gatewayID) {
			return app, true, nil
		}
	}
	return adminFeishuAppSummary{}, false, nil
}

func buildFeishuAppSummary(gatewayID string, persisted config.FeishuAppConfig, runtime config.FeishuAppConfig, status feishu.GatewayStatus, persistedConfig bool, runtimeOnly bool, readOnly bool, reason string) adminFeishuAppSummary {
	summary := adminFeishuAppSummary{
		ID:              gatewayID,
		Name:            firstNonEmpty(strings.TrimSpace(runtime.Name), strings.TrimSpace(persisted.Name), gatewayID),
		AppID:           firstNonEmpty(strings.TrimSpace(runtime.AppID), strings.TrimSpace(persisted.AppID)),
		HasSecret:       strings.TrimSpace(firstNonEmpty(strings.TrimSpace(runtime.AppSecret), strings.TrimSpace(persisted.AppSecret))) != "",
		Enabled:         runtime.Enabled == nil || *runtime.Enabled,
		VerifiedAt:      persisted.VerifiedAt,
		Wizard:          adminWizardStateView(persisted.Wizard),
		Persisted:       persistedConfig,
		RuntimeOnly:     runtimeOnly,
		RuntimeOverride: readOnly,
		ReadOnly:        readOnly,
		ReadOnlyReason:  reason,
	}
	if status.GatewayID != "" {
		statusCopy := status
		summary.Status = &statusCopy
	}
	return summary
}

func adminWizardStateView(state config.FeishuAppWizardState) *adminFeishuAppWizardView {
	if state.CredentialsSavedAt == nil &&
		state.ConnectionVerifiedAt == nil &&
		state.ScopesExportedAt == nil &&
		state.EventsConfirmedAt == nil &&
		state.CallbacksConfirmedAt == nil &&
		state.MenusConfirmedAt == nil &&
		state.PublishedAt == nil {
		return nil
	}
	return &adminFeishuAppWizardView{
		CredentialsSavedAt:   state.CredentialsSavedAt,
		ConnectionVerifiedAt: state.ConnectionVerifiedAt,
		ScopesExportedAt:     state.ScopesExportedAt,
		EventsConfirmedAt:    state.EventsConfirmedAt,
		CallbacksConfirmedAt: state.CallbacksConfirmedAt,
		MenusConfirmedAt:     state.MenusConfirmedAt,
		PublishedAt:          state.PublishedAt,
	}
}

func feishuAppReadOnly(admin adminRuntimeState, gatewayID string) (bool, string) {
	if !admin.envOverrideActive {
		return false, ""
	}
	if canonicalGatewayID(admin.envOverrideGatewayID) != canonicalGatewayID(gatewayID) {
		return false, ""
	}
	return true, "managed by FEISHU_APP_ID/FEISHU_APP_SECRET runtime override"
}

func isRuntimeOnlyFeishuApp(admin adminRuntimeState, cfg config.AppConfig, gatewayID string) bool {
	if indexOfConfigFeishuApp(cfg.Feishu.Apps, gatewayID) >= 0 {
		return false
	}
	return admin.envOverrideActive && canonicalGatewayID(admin.envOverrideGatewayID) == canonicalGatewayID(gatewayID)
}

func (a *App) snapshotAdminRuntime() adminRuntimeState {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.admin
}

func (a *App) gatewayController() (feishu.GatewayController, error) {
	controller, ok := a.gateway.(feishu.GatewayController)
	if !ok {
		return nil, errors.New("gateway controller not available")
	}
	return controller, nil
}

func (a *App) runtimeGatewayConfigFor(cfg config.AppConfig, gatewayID string) (feishu.GatewayAppConfig, bool) {
	admin := a.snapshotAdminRuntime()
	for _, app := range runtimeGatewayApps(cfg, admin.services, a.headlessRuntime.Paths) {
		if canonicalGatewayID(app.GatewayID) == canonicalGatewayID(gatewayID) {
			return app, true
		}
	}
	return feishu.GatewayAppConfig{}, false
}

func (a *App) applyRuntimeFeishuConfig(cfg config.AppConfig, gatewayID string) error {
	controller, err := a.gatewayController()
	if err != nil {
		return err
	}
	if runtimeCfg, ok := a.runtimeGatewayConfigFor(cfg, gatewayID); ok {
		return controller.UpsertApp(context.Background(), runtimeCfg)
	}
	return controller.RemoveApp(context.Background(), gatewayID)
}

func (a *App) markFeishuAppVerified(path, gatewayID string, verifiedAt time.Time) error {
	a.adminConfigMu.Lock()
	defer a.adminConfigMu.Unlock()
	loaded, err := a.loadAdminConfig()
	if err != nil {
		return err
	}
	index := indexOfConfigFeishuApp(loaded.Config.Feishu.Apps, gatewayID)
	if index < 0 {
		return nil
	}
	updated := loaded.Config
	value := verifiedAt.UTC()
	updated.Feishu.Apps[index].VerifiedAt = &value
	updated.Feishu.Apps[index].Wizard.ConnectionVerifiedAt = &value
	return config.WriteAppConfig(path, updated)
}

func (a *App) updateFeishuAppWizard(gatewayID string, req feishuAppWizardUpdateRequest, at time.Time) (config.LoadedAppConfig, error) {
	a.adminConfigMu.Lock()
	defer a.adminConfigMu.Unlock()

	loaded, err := a.loadAdminConfig()
	if err != nil {
		return config.LoadedAppConfig{}, err
	}
	admin := a.snapshotAdminRuntime()
	if readOnly, reason := feishuAppReadOnly(admin, gatewayID); readOnly {
		return config.LoadedAppConfig{}, fmt.Errorf("runtime_override_read_only:%s", reason)
	}
	updated := loaded.Config
	index := indexOfConfigFeishuApp(updated.Feishu.Apps, gatewayID)
	if index < 0 {
		return config.LoadedAppConfig{}, fmt.Errorf("feishu_app_not_found:%s", gatewayID)
	}

	current := updated.Feishu.Apps[index]
	applyWizardToggle(&current.Wizard.ScopesExportedAt, req.ScopesExported, at)
	applyWizardToggle(&current.Wizard.EventsConfirmedAt, req.EventsConfirmed, at)
	applyWizardToggle(&current.Wizard.CallbacksConfirmedAt, req.CallbacksConfirmed, at)
	applyWizardToggle(&current.Wizard.MenusConfirmedAt, req.MenusConfirmed, at)
	applyWizardToggle(&current.Wizard.PublishedAt, req.Published, at)
	updated.Feishu.Apps[index] = current

	if err := config.WriteAppConfig(loaded.Path, updated); err != nil {
		return config.LoadedAppConfig{}, err
	}
	return config.LoadedAppConfig{Path: loaded.Path, Config: updated}, nil
}

func markFeishuCredentialsSaved(app *config.FeishuAppConfig, at time.Time) {
	if app == nil {
		return
	}
	if strings.TrimSpace(app.AppID) == "" || strings.TrimSpace(app.AppSecret) == "" {
		return
	}
	value := at.UTC()
	app.Wizard.CredentialsSavedAt = &value
}

func resetFeishuVerification(app *config.FeishuAppConfig) {
	if app == nil {
		return
	}
	app.VerifiedAt = nil
	app.Wizard.ConnectionVerifiedAt = nil
}

func resetFeishuWizardManualSteps(app *config.FeishuAppConfig) {
	if app == nil {
		return
	}
	app.Wizard.ScopesExportedAt = nil
	app.Wizard.EventsConfirmedAt = nil
	app.Wizard.CallbacksConfirmedAt = nil
	app.Wizard.MenusConfirmedAt = nil
	app.Wizard.PublishedAt = nil
}

func applyWizardToggle(target **time.Time, enabled *bool, at time.Time) {
	if enabled == nil || target == nil {
		return
	}
	if *enabled {
		value := at.UTC()
		*target = &value
		return
	}
	*target = nil
}

func (a *App) setFeishuAppEnabled(gatewayID string, enabled *bool) (config.LoadedAppConfig, config.LoadedAppConfig, error) {
	a.adminConfigMu.Lock()
	defer a.adminConfigMu.Unlock()

	loaded, err := a.loadAdminConfig()
	if err != nil {
		return config.LoadedAppConfig{}, config.LoadedAppConfig{}, err
	}
	admin := a.snapshotAdminRuntime()
	if readOnly, reason := feishuAppReadOnly(admin, gatewayID); readOnly {
		return config.LoadedAppConfig{}, config.LoadedAppConfig{}, fmt.Errorf("runtime_override_read_only:%s", reason)
	}

	updated := loaded.Config
	index := indexOfConfigFeishuApp(updated.Feishu.Apps, gatewayID)
	if index < 0 {
		return config.LoadedAppConfig{}, config.LoadedAppConfig{}, fmt.Errorf("feishu_app_not_found:%s", gatewayID)
	}
	if enabled != nil {
		updated.Feishu.Apps[index].Enabled = daemonBoolPtr(*enabled)
	}
	if err := config.WriteAppConfig(loaded.Path, updated); err != nil {
		return config.LoadedAppConfig{}, config.LoadedAppConfig{}, err
	}
	return loaded, config.LoadedAppConfig{Path: loaded.Path, Config: updated}, nil
}

func (a *App) writeFeishuMutationError(w http.ResponseWriter, gatewayID string, err error) {
	switch {
	case strings.HasPrefix(err.Error(), "runtime_override_read_only:"):
		writeAPIError(w, http.StatusConflict, apiError{
			Code:    "runtime_override_read_only",
			Message: "the runtime override app cannot be edited from web config",
			Details: strings.TrimPrefix(err.Error(), "runtime_override_read_only:"),
		})
	case strings.HasPrefix(err.Error(), "feishu_app_not_found:"):
		writeAPIError(w, http.StatusNotFound, apiError{
			Code:    "feishu_app_not_found",
			Message: "feishu app not found",
			Details: gatewayID,
		})
	default:
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "config_write_failed",
			Message: "failed to update feishu app config",
			Details: err.Error(),
		})
	}
}

func decodeJSONBody(r *http.Request, target any) error {
	if r.Body == nil || r.Body == http.NoBody {
		return nil
	}
	defer r.Body.Close()
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil
	}
	return json.Unmarshal(raw, target)
}

func indexOfConfigFeishuApp(apps []config.FeishuAppConfig, gatewayID string) int {
	for i, app := range apps {
		if canonicalGatewayID(app.ID) == canonicalGatewayID(gatewayID) {
			return i
		}
	}
	return -1
}

func nextGatewayID(apps []config.FeishuAppConfig, admin adminRuntimeState, req feishuAppWriteRequest) string {
	base := sanitizeGatewayPath(firstNonEmpty(trimmedString(req.Name), trimmedString(req.AppID), "app"))
	if base == "" {
		base = "app"
	}
	if canonicalGatewayID(base) == canonicalGatewayID(admin.envOverrideGatewayID) && admin.envOverrideActive {
		base = base + "-config"
	}
	exists := func(candidate string) bool {
		if indexOfConfigFeishuApp(apps, candidate) >= 0 {
			return true
		}
		if admin.envOverrideActive && canonicalGatewayID(candidate) == canonicalGatewayID(admin.envOverrideGatewayID) {
			return true
		}
		return false
	}
	if !exists(base) {
		return base
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d", base, i)
		if !exists(candidate) {
			return candidate
		}
	}
}

func trimmedString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func canonicalGatewayID(gatewayID string) string {
	gatewayID = strings.TrimSpace(gatewayID)
	if gatewayID == "" {
		return feishu.LegacyDefaultGatewayID
	}
	return gatewayID
}

func daemonBoolPtr(value bool) *bool {
	return &value
}
