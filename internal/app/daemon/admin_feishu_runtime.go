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

func (a *App) checkFeishuAppPublishReady(ctx context.Context, gatewayID string) (adminFeishuAppSummary, []string, error) {
	loaded, err := a.loadAdminConfig()
	if err != nil {
		return adminFeishuAppSummary{}, nil, err
	}
	summary, ok, err := a.adminFeishuAppSummary(loaded, gatewayID)
	if err != nil {
		return adminFeishuAppSummary{}, nil, err
	}
	if !ok {
		return adminFeishuAppSummary{}, nil, fmt.Errorf("feishu_app_not_found:%s", gatewayID)
	}
	index := indexOfConfigFeishuApp(loaded.Config.Feishu.Apps, gatewayID)
	if index < 0 {
		return summary, []string{"当前应用还没有持久化到本地配置。"}, nil
	}

	app := loaded.Config.Feishu.Apps[index]
	issues := make([]string, 0, 6)
	if strings.TrimSpace(app.AppID) == "" || strings.TrimSpace(app.AppSecret) == "" {
		issues = append(issues, "当前应用的 App ID / App Secret 还不完整。")
	}
	if app.Wizard.ConnectionVerifiedAt == nil {
		issues = append(issues, "还没有完成“创建并连接飞书应用”里的连接测试。")
	}
	if app.Wizard.ScopesExportedAt == nil {
		issues = append(issues, "还没有确认权限导入。")
	}
	if app.Wizard.EventsConfirmedAt == nil {
		issues = append(issues, "还没有确认事件订阅。")
	}
	if app.Wizard.CallbacksConfirmedAt == nil {
		issues = append(issues, "还没有确认回调长连接配置。")
	}
	if app.Wizard.MenusConfirmedAt == nil {
		issues = append(issues, "还没有确认机器人菜单配置。")
	}

	if runtimeCfg, ok := a.runtimeGatewayConfigFor(loaded.Config, gatewayID); ok {
		controller, controllerErr := a.gatewayController()
		if controllerErr != nil {
			issues = append(issues, "当前环境暂时无法执行飞书长连接验收。")
		} else {
			verifyCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			result, verifyErr := controller.Verify(verifyCtx, runtimeCfg)
			if verifyErr != nil {
				message := strings.TrimSpace(result.ErrorMessage)
				if message == "" {
					message = verifyErr.Error()
				}
				issues = append(issues, "飞书长连接验证失败："+message)
			} else if !result.Connected {
				message := strings.TrimSpace(result.ErrorMessage)
				if message == "" {
					message = "连接没有成功建立"
				}
				issues = append(issues, "飞书长连接验证失败："+message)
			}
		}
	} else {
		issues = append(issues, "当前应用还没有进入运行时长连接配置。")
	}

	return summary, issues, nil
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
