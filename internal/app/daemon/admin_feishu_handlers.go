package daemon

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/feishuapp"
)

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
	if req.ID == "" {
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
	resolvedName := a.suggestFeishuAppName(r.Context(), trimmedString(req.Name), trimmedString(req.AppID), trimmedString(req.AppSecret), gatewayID)
	app := config.FeishuAppConfig{
		ID:      gatewayID,
		Name:    firstNonEmpty(resolvedName, trimmedString(req.AppID), gatewayID),
		AppID:   trimmedString(req.AppID),
		Enabled: daemonBoolPtr(enabled),
	}
	if secret := trimmedString(req.AppSecret); secret != "" {
		app.AppSecret = secret
	}
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
	a.clearAllFeishuAppTests(gatewayID)

	if err := a.applyRuntimeFeishuConfig(updated, gatewayID); err != nil {
		summary, _, summaryErr := a.adminFeishuAppSummary(config.LoadedAppConfig{Path: loaded.Path, Config: updated}, gatewayID)
		if summaryErr != nil {
			summary = adminFeishuAppSummary{
				ID:        gatewayID,
				Name:      firstNonEmpty(strings.TrimSpace(app.Name), gatewayID),
				AppID:     strings.TrimSpace(app.AppID),
				HasSecret: strings.TrimSpace(app.AppSecret) != "",
				Enabled:   app.Enabled == nil || *app.Enabled,
				Persisted: true,
			}
		}
		a.writeFeishuRuntimeApplyError(w, gatewayID, summary, feishuRuntimeApplyActionUpsert, "feishu config saved but runtime apply failed", err)
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
	writeJSON(w, http.StatusCreated, feishuAppResponse{
		App:      summary,
		Mutation: buildCreatedFeishuAppMutation(),
	})
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
	requestedName := trimmedString(req.Name)
	nextAppID := strings.TrimSpace(current.AppID)
	nextAppSecret := strings.TrimSpace(current.AppSecret)
	appIDChanged := false
	secretChanged := false
	if requestedName != "" {
		current.Name = requestedName
	}
	if appID := trimmedString(req.AppID); appID != "" && appID != current.AppID {
		current.AppID = appID
		nextAppID = appID
		appIDChanged = true
	}
	if secret := trimmedString(req.AppSecret); secret != "" && secret != current.AppSecret {
		current.AppSecret = secret
		nextAppSecret = secret
		secretChanged = true
	}
	if requestedName == "" && (appIDChanged || strings.TrimSpace(current.Name) == "") {
		current.Name = a.suggestFeishuAppName(r.Context(), "", nextAppID, nextAppSecret, current.Name)
	}
	if req.Enabled != nil {
		current.Enabled = daemonBoolPtr(*req.Enabled)
	}
	if appIDChanged || secretChanged {
		resetFeishuVerification(&current)
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
	if appIDChanged || secretChanged {
		if err := a.clearFeishuAppOnboardingState(gatewayID); err != nil {
			writeAPIError(w, http.StatusInternalServerError, apiError{
				Code:    "config_write_failed",
				Message: "failed to reset onboarding state after feishu app credential change",
				Details: err.Error(),
			})
			return
		}
	}
	a.clearFeishuAppWebTestRecipient(gatewayID)

	if err := a.applyRuntimeFeishuConfig(updated, gatewayID); err != nil {
		summary, _, summaryErr := a.adminFeishuAppSummary(config.LoadedAppConfig{Path: loaded.Path, Config: updated}, gatewayID)
		if summaryErr != nil {
			summary = adminFeishuAppSummary{
				ID:        gatewayID,
				Name:      firstNonEmpty(strings.TrimSpace(current.Name), gatewayID),
				AppID:     strings.TrimSpace(current.AppID),
				HasSecret: strings.TrimSpace(current.AppSecret) != "",
				Enabled:   current.Enabled == nil || *current.Enabled,
				Persisted: true,
			}
		}
		a.writeFeishuRuntimeApplyError(w, gatewayID, summary, feishuRuntimeApplyActionUpsert, "feishu config saved but runtime apply failed", err)
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
	writeJSON(w, http.StatusOK, feishuAppResponse{
		App:      summary,
		Mutation: buildFeishuAppMutation(appIDChanged, secretChanged),
	})
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
	deletedSummary, _, summaryErr := a.adminFeishuAppSummary(loaded, gatewayID)
	if summaryErr != nil {
		deletedSummary = adminFeishuAppSummary{
			ID:        gatewayID,
			Name:      firstNonEmpty(strings.TrimSpace(updated.Feishu.Apps[index].Name), gatewayID),
			AppID:     strings.TrimSpace(updated.Feishu.Apps[index].AppID),
			HasSecret: strings.TrimSpace(updated.Feishu.Apps[index].AppSecret) != "",
			Enabled:   updated.Feishu.Apps[index].Enabled == nil || *updated.Feishu.Apps[index].Enabled,
			Persisted: true,
		}
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
		deletedSummary.Persisted = false
		deletedSummary.RuntimeOnly = false
		a.writeFeishuRuntimeApplyError(w, gatewayID, deletedSummary, feishuRuntimeApplyActionRemove, "feishu config saved but runtime apply failed", err)
		return
	}
	if err := a.clearFeishuAppOnboardingState(gatewayID); err != nil {
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "config_write_failed",
			Message: "feishu app deleted but failed to clear onboarding state",
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
		a.clearAllFeishuAppTests(gatewayID)
		writeJSON(w, http.StatusBadGateway, feishuAppVerifyResponse{
			App:    summary,
			Result: result,
		})
		return
	}
	a.clearAllFeishuAppTests(gatewayID)
	a.maybeSendFeishuAppVerifySuccessNotices(r.Context(), gatewayID, strings.HasPrefix(r.URL.Path, "/api/setup/"))
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
	a.clearAllFeishuAppTests(gatewayID)
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

func (a *App) handleFeishuAppRetryApply(w http.ResponseWriter, r *http.Request) {
	gatewayID := canonicalGatewayID(r.PathValue("id"))
	pending, ok := a.feishuRuntimeApplyPendingState(gatewayID)
	if !ok {
		writeAPIError(w, http.StatusConflict, apiError{
			Code:    "feishu_runtime_apply_not_pending",
			Message: "feishu app does not have a saved-but-not-applied runtime change",
			Details: gatewayID,
		})
		return
	}
	loaded, err := a.loadAdminConfig()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "config_unavailable",
			Message: "failed to load config",
			Details: err.Error(),
		})
		return
	}
	if err := a.applyRuntimeFeishuConfig(loaded.Config, gatewayID); err != nil {
		summary, ok, summaryErr := a.adminFeishuAppSummary(loaded, gatewayID)
		if summaryErr != nil || !ok {
			summary = pendingFeishuAppSummary(gatewayID, pending)
		}
		a.writeFeishuRuntimeApplyError(w, gatewayID, summary, pending.Action, "failed to retry feishu runtime apply", err)
		return
	}
	a.clearAllFeishuAppTests(gatewayID)
	w.WriteHeader(http.StatusNoContent)
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
	a.clearAllFeishuAppTests(gatewayID)
	if err := a.applyRuntimeFeishuConfig(updated.Config, gatewayID); err != nil {
		summary, _, summaryErr := a.adminFeishuAppSummary(updated, gatewayID)
		if summaryErr != nil {
			summary = adminFeishuAppSummary{
				ID:        gatewayID,
				Persisted: true,
				Enabled:   enabled != nil && *enabled,
			}
		}
		a.writeFeishuRuntimeApplyError(w, gatewayID, summary, feishuRuntimeApplyActionUpsert, "feishu config saved but runtime apply failed", err)
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
