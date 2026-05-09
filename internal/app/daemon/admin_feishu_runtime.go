package daemon

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/config"
)

func (a *App) adminFeishuApps(loaded config.LoadedAppConfig) ([]adminFeishuAppSummary, error) {
	admin := a.snapshotAdminRuntime()
	runtimeApps := effectiveFeishuApps(loaded.Config, admin.services)
	pendingApply := a.snapshotFeishuRuntimeApplyPending()
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
		if pending, ok := pendingApply[gatewayID]; ok {
			summary = applyFeishuRuntimePending(summary, pending)
		}
		summaries = append(summaries, summary)
		seen[gatewayID] = true
	}

	if admin.envOverrideActive {
		gatewayID := canonicalGatewayID(admin.envOverrideGatewayID)
		if !seen[gatewayID] {
			if runtimeApp, ok := runtimeMap[gatewayID]; ok {
				readOnly, reason := feishuAppReadOnly(admin, gatewayID)
				summary := buildFeishuAppSummary(gatewayID, config.FeishuAppConfig{}, runtimeApp, statuses[gatewayID], false, true, readOnly, reason)
				if pending, ok := pendingApply[gatewayID]; ok {
					summary = applyFeishuRuntimePending(summary, pending)
				}
				summaries = append(summaries, summary)
				seen[gatewayID] = true
			}
		}
	}

	for gatewayID, pending := range pendingApply {
		if seen[gatewayID] {
			continue
		}
		summary := pendingFeishuAppSummary(gatewayID, pending)
		if status, ok := statuses[gatewayID]; ok && status.GatewayID != "" {
			statusCopy := status
			summary.Name = firstNonEmpty(strings.TrimSpace(status.Name), summary.Name)
			summary.Enabled = !status.Disabled
			summary.Status = &statusCopy
		}
		summaries = append(summaries, applyFeishuRuntimePending(summary, pending))
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
		ConsoleLinks:    buildFeishuAppConsoleLinks(firstNonEmpty(strings.TrimSpace(runtime.AppID), strings.TrimSpace(persisted.AppID))),
		HasSecret:       strings.TrimSpace(firstNonEmpty(strings.TrimSpace(runtime.AppSecret), strings.TrimSpace(persisted.AppSecret))) != "",
		Enabled:         runtime.Enabled == nil || *runtime.Enabled,
		VerifiedAt:      persisted.VerifiedAt,
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
		if err := controller.UpsertApp(context.Background(), runtimeCfg); err != nil {
			return err
		}
		a.clearFeishuRuntimeApplyPending(gatewayID)
		return nil
	}
	if err := controller.RemoveApp(context.Background(), gatewayID); err != nil {
		return err
	}
	a.clearFeishuPermissionGaps(gatewayID)
	a.clearFeishuRuntimeApplyPending(gatewayID)
	return nil
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
	return config.WriteAppConfig(path, updated)
}

func (a *App) markFeishuAppOnboardingCompleted(path, gatewayID string, verifiedAt time.Time) error {
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
	return config.WriteAppConfig(path, updated)
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
