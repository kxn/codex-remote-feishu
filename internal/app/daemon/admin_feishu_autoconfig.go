package daemon

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/feishuapp"
)

const (
	defaultFeishuAutoConfigPlanTimeout    = 20 * time.Second
	defaultFeishuAutoConfigApplyTimeout   = 30 * time.Second
	defaultFeishuAutoConfigPublishTimeout = 45 * time.Second
)

var (
	planFeishuAppAutoConfig = func(ctx context.Context, cfg feishu.LiveGatewayConfig) (feishu.AutoConfigPlan, error) {
		return feishu.PlanAppAutoConfig(ctx, cfg, feishuapp.DefaultManifest(), feishuapp.DefaultFixedPolicy())
	}
	applyFeishuAppAutoConfig = func(ctx context.Context, cfg feishu.LiveGatewayConfig) (feishu.AutoConfigApplyResult, error) {
		return feishu.ApplyAppAutoConfig(ctx, cfg, feishuapp.DefaultManifest(), feishuapp.DefaultFixedPolicy())
	}
	publishFeishuAppAutoConfig = func(ctx context.Context, cfg feishu.LiveGatewayConfig, req feishu.AutoConfigPublishRequest) (feishu.AutoConfigPublishResult, error) {
		return feishu.PublishAppAutoConfig(ctx, cfg, feishuapp.DefaultManifest(), feishuapp.DefaultFixedPolicy(), req)
	}
)

func (a *App) handleFeishuAppAutoConfigPlan(w http.ResponseWriter, r *http.Request) {
	summary, runtimeCfg, err := a.loadFeishuAutoConfigTarget(r.PathValue("id"))
	if err != nil {
		a.writeFeishuAutoConfigError(w, err)
		return
	}
	planCtx, cancel := context.WithTimeout(r.Context(), defaultFeishuAutoConfigPlanTimeout)
	defer cancel()
	plan, err := planFeishuAppAutoConfig(planCtx, runtimeCfg)
	if err != nil {
		a.writeFeishuAutoConfigGatewayError(w, "failed to build feishu auto-config plan", err)
		return
	}
	writeJSON(w, http.StatusOK, feishuAppAutoConfigPlanResponse{
		App:  summary,
		Plan: plan,
	})
}

func (a *App) handleFeishuAppAutoConfigApply(w http.ResponseWriter, r *http.Request) {
	summary, runtimeCfg, err := a.loadFeishuAutoConfigTarget(r.PathValue("id"))
	if err != nil {
		a.writeFeishuAutoConfigError(w, err)
		return
	}
	applyCtx, cancel := context.WithTimeout(r.Context(), defaultFeishuAutoConfigApplyTimeout)
	defer cancel()
	result, err := applyFeishuAppAutoConfig(applyCtx, runtimeCfg)
	if err != nil {
		a.writeFeishuAutoConfigGatewayError(w, "failed to apply feishu auto-config", err)
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
	summary, runtimeCfg, err := a.loadFeishuAutoConfigTarget(r.PathValue("id"))
	if err != nil {
		a.writeFeishuAutoConfigError(w, err)
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
	result, err := publishFeishuAppAutoConfig(publishCtx, runtimeCfg, feishu.AutoConfigPublishRequest{
		Remark:    strings.TrimSpace(req.Remark),
		Changelog: strings.TrimSpace(req.Changelog),
		Version:   strings.TrimSpace(req.Version),
	})
	if err != nil {
		a.writeFeishuAutoConfigGatewayError(w, "failed to publish feishu auto-config changes", err)
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

func (a *App) loadFeishuAutoConfigTarget(gatewayID string) (adminFeishuAppSummary, feishu.LiveGatewayConfig, error) {
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

func (a *App) writeFeishuAutoConfigError(w http.ResponseWriter, err error) {
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

func (a *App) writeFeishuAutoConfigGatewayError(w http.ResponseWriter, message string, err error) {
	writeAPIError(w, http.StatusBadGateway, apiError{
		Code:    "feishu_auto_config_failed",
		Message: message,
		Details: err.Error(),
	})
}
