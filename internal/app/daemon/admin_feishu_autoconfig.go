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
	defaultFeishuAutoConfigPlanTimeout     = 20 * time.Second
	defaultFeishuAutoConfigApplyTimeout    = 30 * time.Second
	defaultFeishuAutoConfigPublishTimeout  = 45 * time.Second
	defaultFeishuAutoConfigCompleteTimeout = 75 * time.Second
)

var (
	feishuSetupFacade daemonFeishuSetupFacade = liveDaemonFeishuSetupFacade{}
)

type daemonFeishuSetupFacade interface {
	PlanAutoConfig(context.Context, feishu.LiveGatewayConfig) (feishu.AutoConfigPlan, error)
	ApplyAutoConfig(context.Context, feishu.LiveGatewayConfig) (feishu.AutoConfigApplyResult, error)
	PublishAutoConfig(context.Context, feishu.LiveGatewayConfig, feishu.AutoConfigPublishRequest) (feishu.AutoConfigPublishResult, error)
	CompleteAutoConfig(context.Context, feishu.LiveGatewayConfig, feishu.AutoConfigPublishRequest) (feishu.AutoConfigCompleteResult, error)
	LongConnectionStatus(context.Context, feishu.LiveGatewayConfig) (feishu.LongConnectionStatus, error)
	DescribeApp(context.Context, string, string) (feishuAppIdentity, error)
}

type liveDaemonFeishuSetupFacade struct{}

func (liveDaemonFeishuSetupFacade) PlanAutoConfig(ctx context.Context, cfg feishu.LiveGatewayConfig) (feishu.AutoConfigPlan, error) {
	client := feishu.NewSetupClient(feishu.SetupClientConfigFromLiveGatewayConfig(cfg))
	return client.PlanAppAutoConfig(ctx, feishuapp.DefaultManifest(), feishuapp.DefaultFixedPolicy())
}

func (liveDaemonFeishuSetupFacade) ApplyAutoConfig(ctx context.Context, cfg feishu.LiveGatewayConfig) (feishu.AutoConfigApplyResult, error) {
	client := feishu.NewSetupClient(feishu.SetupClientConfigFromLiveGatewayConfig(cfg))
	return client.ApplyAppAutoConfig(ctx, feishuapp.DefaultManifest(), feishuapp.DefaultFixedPolicy())
}

func (liveDaemonFeishuSetupFacade) PublishAutoConfig(ctx context.Context, cfg feishu.LiveGatewayConfig, req feishu.AutoConfigPublishRequest) (feishu.AutoConfigPublishResult, error) {
	client := feishu.NewSetupClient(feishu.SetupClientConfigFromLiveGatewayConfig(cfg))
	return client.PublishAppAutoConfig(ctx, feishuapp.DefaultManifest(), feishuapp.DefaultFixedPolicy(), req)
}

func (liveDaemonFeishuSetupFacade) CompleteAutoConfig(ctx context.Context, cfg feishu.LiveGatewayConfig, req feishu.AutoConfigPublishRequest) (feishu.AutoConfigCompleteResult, error) {
	client := feishu.NewSetupClient(feishu.SetupClientConfigFromLiveGatewayConfig(cfg))
	return client.CompleteAppAutoConfig(ctx, feishuapp.DefaultManifest(), feishuapp.DefaultFixedPolicy(), req)
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

func (a *App) handleFeishuAppAutoConfigApply(w http.ResponseWriter, r *http.Request) {
	summary, runtimeCfg, err := a.loadFeishuLiveGatewayTarget(r.PathValue("id"))
	if err != nil {
		a.writeFeishuAppTargetError(w, err)
		return
	}
	applyCtx, cancel := context.WithTimeout(r.Context(), defaultFeishuAutoConfigApplyTimeout)
	defer cancel()
	result, err := feishuSetupFacade.ApplyAutoConfig(applyCtx, runtimeCfg)
	if err != nil {
		a.writeFeishuAutoConfigGatewayError(w, err)
		return
	}
	if err := a.clearFeishuAppAutoConfigDecision(summary.ID); err != nil {
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "config_write_failed",
			Message: "feishu auto-config applied but failed to reset onboarding decision",
			Details: err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, feishuAppAutoConfigApplyResponse{
		App:    summary,
		Result: result,
	})
}

func (a *App) handleFeishuAppAutoConfigPublish(w http.ResponseWriter, r *http.Request) {
	summary, runtimeCfg, err := a.loadFeishuLiveGatewayTarget(r.PathValue("id"))
	if err != nil {
		a.writeFeishuAppTargetError(w, err)
		return
	}
	var req feishuAppAutoConfigPublishRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, apiError{
			Code:    "invalid_request",
			Message: "failed to decode feishu auto-config publish payload",
			Details: err.Error(),
		})
		return
	}
	publishCtx, cancel := context.WithTimeout(r.Context(), defaultFeishuAutoConfigPublishTimeout)
	defer cancel()
	result, err := feishuSetupFacade.PublishAutoConfig(publishCtx, runtimeCfg, feishu.AutoConfigPublishRequest{
		Remark:    strings.TrimSpace(req.Remark),
		Changelog: strings.TrimSpace(req.Changelog),
		Version:   strings.TrimSpace(req.Version),
	})
	if err != nil {
		a.writeFeishuAutoConfigGatewayError(w, err)
		return
	}
	if err := a.clearFeishuAppAutoConfigDecision(summary.ID); err != nil {
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "config_write_failed",
			Message: "feishu auto-config publish succeeded but failed to reset onboarding decision",
			Details: err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, feishuAppAutoConfigPublishResponse{
		App:    summary,
		Result: result,
	})
}

func (a *App) handleFeishuAppAutoConfigComplete(w http.ResponseWriter, r *http.Request) {
	summary, runtimeCfg, err := a.loadFeishuLiveGatewayTarget(r.PathValue("id"))
	if err != nil {
		a.writeFeishuAppTargetError(w, err)
		return
	}
	var req feishuAppAutoConfigPublishRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, apiError{
			Code:    "invalid_request",
			Message: "failed to decode feishu auto-config complete payload",
			Details: err.Error(),
		})
		return
	}
	completeCtx, cancel := context.WithTimeout(r.Context(), defaultFeishuAutoConfigCompleteTimeout)
	defer cancel()
	result, err := feishuSetupFacade.CompleteAutoConfig(completeCtx, runtimeCfg, feishu.AutoConfigPublishRequest{
		Remark:    strings.TrimSpace(req.Remark),
		Changelog: strings.TrimSpace(req.Changelog),
		Version:   strings.TrimSpace(req.Version),
	})
	if err != nil {
		a.writeFeishuAutoConfigGatewayError(w, err)
		return
	}
	if err := a.clearFeishuAppAutoConfigDecision(summary.ID); err != nil {
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "config_write_failed",
			Message: "feishu auto-config completed but failed to reset onboarding decision",
			Details: err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, feishuAppAutoConfigCompleteResponse{
		App:    summary,
		Result: result,
	})
}

func (a *App) completeSavedFeishuAppAutoConfig(parent context.Context, loaded config.LoadedAppConfig, gatewayID string) *feishuAppAutoConfigCompleteView {
	runtimeCfg, ok := a.runtimeGatewayConfigFor(loaded.Config, gatewayID)
	if !ok {
		return &feishuAppAutoConfigCompleteView{Error: "saved feishu app is not available for automatic configuration"}
	}
	completeCtx, cancel := context.WithTimeout(parent, defaultFeishuAutoConfigCompleteTimeout)
	defer cancel()
	result, err := feishuSetupFacade.CompleteAutoConfig(completeCtx, liveGatewayConfigFromRuntime(runtimeCfg), feishu.AutoConfigPublishRequest{})
	if err != nil {
		return &feishuAppAutoConfigCompleteView{Error: feishuAutoConfigUserMessage()}
	}
	if err := a.clearFeishuAppAutoConfigDecision(gatewayID); err != nil {
		return &feishuAppAutoConfigCompleteView{Result: &result, Error: feishuAutoConfigUserMessage()}
	}
	return &feishuAppAutoConfigCompleteView{Result: &result}
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
