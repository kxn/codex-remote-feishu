package daemon

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/feishuapp"
)

const (
	defaultFeishuAutoConfigPlanTimeout = 20 * time.Second
)

var (
	feishuSetupFacade daemonFeishuSetupFacade = liveDaemonFeishuSetupFacade{}
)

type daemonFeishuSetupFacade interface {
	PlanAutoConfig(context.Context, feishu.LiveGatewayConfig) (feishu.AutoConfigPlan, error)
	LongConnectionStatus(context.Context, feishu.LiveGatewayConfig) (feishu.LongConnectionStatus, error)
	DescribeApp(context.Context, string, string) (feishuAppIdentity, error)
}

type liveDaemonFeishuSetupFacade struct{}

func (liveDaemonFeishuSetupFacade) PlanAutoConfig(ctx context.Context, cfg feishu.LiveGatewayConfig) (feishu.AutoConfigPlan, error) {
	client := feishu.NewSetupClient(feishu.SetupClientConfigFromLiveGatewayConfig(cfg))
	return client.PlanAppAutoConfig(ctx, feishuapp.DefaultManifest(), feishuapp.DefaultFixedPolicy())
}

func (liveDaemonFeishuSetupFacade) LongConnectionStatus(ctx context.Context, cfg feishu.LiveGatewayConfig) (feishu.LongConnectionStatus, error) {
	return feishu.NewSetupClient(feishu.SetupClientConfigFromLiveGatewayConfig(cfg)).GetLongConnectionStatus(ctx)
}

func (liveDaemonFeishuSetupFacade) DescribeApp(ctx context.Context, appID, appSecret string) (feishuAppIdentity, error) {
	appID = strings.TrimSpace(appID)
	appSecret = strings.TrimSpace(appSecret)
	if appID == "" || appSecret == "" {
		return feishuAppIdentity{}, errMissingFeishuAppCredentials
	}
	botInfo, err := feishu.NewSetupClient(feishu.SetupClientConfig{
		GatewayID: "feishu-onboarding-" + appID,
		AppID:     appID,
		AppSecret: appSecret,
	}).GetBotInfo(ctx)
	if err != nil {
		return feishuAppIdentity{}, err
	}
	return feishuAppIdentity{DisplayName: strings.TrimSpace(botInfo.AppName)}, nil
}

func (a *App) handleFeishuAppAutoConfigPlan(w http.ResponseWriter, r *http.Request) {
	summary, runtimeCfg, err := a.loadFeishuLiveGatewayTarget(r.PathValue("id"))
	if err != nil {
		a.writeFeishuAppTargetError(w, err)
		return
	}
	planCtx, cancel := context.WithTimeout(r.Context(), defaultFeishuAutoConfigPlanTimeout)
	defer cancel()
	plan, err := feishuSetupFacade.PlanAutoConfig(planCtx, runtimeCfg)
	if err != nil {
		a.writeFeishuAutoConfigGatewayError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, feishuAppAutoConfigPlanResponse{
		App:  summary,
		Plan: plan,
	})
}

func (a *App) planSavedFeishuAppAutoConfig(parent context.Context, loaded config.LoadedAppConfig, gatewayID string) *feishuAppAutoConfigPlanView {
	runtimeCfg, ok := a.runtimeGatewayConfigFor(loaded.Config, gatewayID)
	if !ok {
		return &feishuAppAutoConfigPlanView{Error: "saved feishu app is not available for automatic configuration"}
	}
	planCtx, cancel := context.WithTimeout(parent, defaultFeishuAutoConfigPlanTimeout)
	defer cancel()
	plan, err := feishuSetupFacade.PlanAutoConfig(planCtx, liveGatewayConfigFromRuntime(runtimeCfg))
	if err != nil {
		return &feishuAppAutoConfigPlanView{Error: feishuAutoConfigUserMessage()}
	}
	return &feishuAppAutoConfigPlanView{Plan: plan}
}

func (a *App) loadFeishuLiveGatewayTarget(gatewayID string) (adminFeishuAppSummary, feishu.LiveGatewayConfig, error) {
	loaded, err := a.loadAdminConfig()
	if err != nil {
		return adminFeishuAppSummary{}, feishu.LiveGatewayConfig{}, err
	}
	summary, ok, err := a.adminFeishuAppSummary(loaded, gatewayID)
	if err != nil {
		return adminFeishuAppSummary{}, feishu.LiveGatewayConfig{}, err
	}
	if !ok {
		return adminFeishuAppSummary{}, feishu.LiveGatewayConfig{}, errFeishuAppNotFound(gatewayID)
	}
	runtimeCfg, ok := a.runtimeGatewayConfigFor(loaded.Config, gatewayID)
	if !ok {
		return adminFeishuAppSummary{}, feishu.LiveGatewayConfig{}, errFeishuAppRuntimeUnavailable(gatewayID)
	}
	return summary, liveGatewayConfigFromRuntime(runtimeCfg), nil
}

func liveGatewayConfigFromRuntime(cfg feishu.GatewayAppConfig) feishu.LiveGatewayConfig {
	return feishu.LiveGatewayConfig{
		GatewayID:      cfg.GatewayID,
		AppID:          cfg.AppID,
		AppSecret:      cfg.AppSecret,
		Domain:         cfg.Domain,
		TempDir:        cfg.ImageTempDir,
		UseSystemProxy: cfg.UseSystemProxy,
	}
}

func (a *App) writeFeishuAppTargetError(w http.ResponseWriter, err error) {
	switch {
	case strings.HasPrefix(err.Error(), "feishu_app_not_found:"):
		writeAPIError(w, http.StatusNotFound, apiError{
			Code:    "feishu_app_not_found",
			Message: "feishu app not found",
			Details: strings.TrimPrefix(err.Error(), "feishu_app_not_found:"),
		})
	case strings.HasPrefix(err.Error(), "feishu_app_runtime_unavailable:"):
		writeAPIError(w, http.StatusConflict, apiError{
			Code:    "feishu_app_runtime_unavailable",
			Message: "feishu app is not available at runtime",
			Details: strings.TrimPrefix(err.Error(), "feishu_app_runtime_unavailable:"),
		})
	default:
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "config_unavailable",
			Message: "failed to load feishu app config",
			Details: err.Error(),
		})
	}
}

func (a *App) writeFeishuAutoConfigGatewayError(w http.ResponseWriter, err error) {
	writeAPIError(w, http.StatusBadGateway, apiError{
		Code:    "feishu_auto_config_failed",
		Message: feishuAutoConfigUserMessage(),
		Details: err.Error(),
	})
}

func feishuAutoConfigUserMessage() string {
	return "暂时无法完成飞书自动配置，请稍后重试。"
}
