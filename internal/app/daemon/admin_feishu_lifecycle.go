package daemon

import (
	"context"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/config"
)

type feishuVerificationPersistMode string

const (
	feishuVerificationPersistIfMutable          feishuVerificationPersistMode = "if_mutable"
	feishuVerificationPersistOnboardingComplete feishuVerificationPersistMode = "onboarding_complete"
)

func findConfigFeishuApp(cfg config.AppConfig, gatewayID string) (config.FeishuAppConfig, bool) {
	index := indexOfConfigFeishuApp(cfg.Feishu.Apps, gatewayID)
	if index < 0 {
		return config.FeishuAppConfig{}, false
	}
	return cfg.Feishu.Apps[index], true
}

func fallbackPersistedFeishuAppSummary(gatewayID string, app config.FeishuAppConfig) adminFeishuAppSummary {
	return adminFeishuAppSummary{
		ID:        gatewayID,
		Name:      firstNonEmpty(trimmedString(daemonStringPtr(app.Name)), gatewayID),
		AppID:     app.AppID,
		HasSecret: trimmedString(daemonStringPtr(app.AppSecret)) != "",
		Enabled:   app.Enabled == nil || *app.Enabled,
		Persisted: true,
	}
}

func (a *App) loadPersistedFeishuAppSummaryOrFallback(
	loaded config.LoadedAppConfig,
	gatewayID string,
	fallback config.FeishuAppConfig,
) adminFeishuAppSummary {
	summary, ok, err := a.adminFeishuAppSummary(loaded, gatewayID)
	if err == nil && ok {
		return summary
	}
	if stringsFallback, found := findConfigFeishuApp(loaded.Config, gatewayID); found {
		fallback = stringsFallback
	}
	return fallbackPersistedFeishuAppSummary(gatewayID, fallback)
}

func (a *App) applyPersistedFeishuRuntime(
	loaded config.LoadedAppConfig,
	gatewayID string,
	fallback config.FeishuAppConfig,
) (adminFeishuAppSummary, error) {
	if err := a.applyRuntimeFeishuConfig(loaded.Config, gatewayID); err != nil {
		return a.loadPersistedFeishuAppSummaryOrFallback(loaded, gatewayID, fallback), err
	}
	return adminFeishuAppSummary{}, nil
}

func (a *App) verifyFeishuRuntimeConfig(
	ctx context.Context,
	loaded config.LoadedAppConfig,
	gatewayID string,
) (feishu.VerifyResult, error, bool, error) {
	runtimeCfg, ok := a.runtimeGatewayConfigFor(loaded.Config, gatewayID)
	if !ok {
		return feishu.VerifyResult{}, nil, false, nil
	}
	controller, err := a.gatewayController()
	if err != nil {
		return feishu.VerifyResult{}, nil, true, err
	}
	verifyCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	result, verifyErr := controller.Verify(verifyCtx, runtimeCfg)
	return result, verifyErr, true, nil
}

func (a *App) finalizePersistedFeishuVerification(
	loaded config.LoadedAppConfig,
	gatewayID string,
	verifyErr error,
	mode feishuVerificationPersistMode,
) (config.LoadedAppConfig, adminFeishuAppSummary, bool, error) {
	if verifyErr == nil && a.shouldPersistFeishuVerification(loaded, gatewayID, mode) {
		if err := a.persistFeishuVerificationSuccess(loaded.Path, gatewayID, mode, time.Now().UTC()); err != nil {
			return loaded, adminFeishuAppSummary{}, false, err
		}
		reloaded, err := a.loadAdminConfig()
		if err != nil {
			return loaded, adminFeishuAppSummary{}, false, err
		}
		loaded = reloaded
	}
	summary, ok, err := a.adminFeishuAppSummary(loaded, gatewayID)
	if err != nil {
		return loaded, adminFeishuAppSummary{}, false, err
	}
	return loaded, summary, ok, nil
}

func (a *App) shouldPersistFeishuVerification(
	loaded config.LoadedAppConfig,
	gatewayID string,
	mode feishuVerificationPersistMode,
) bool {
	switch mode {
	case feishuVerificationPersistOnboardingComplete:
		return true
	case feishuVerificationPersistIfMutable:
		admin := a.snapshotAdminRuntime()
		if readOnly, _ := feishuAppReadOnly(admin, gatewayID); readOnly {
			return false
		}
		return !isRuntimeOnlyFeishuApp(admin, loaded.Config, gatewayID)
	default:
		return false
	}
}

func (a *App) persistFeishuVerificationSuccess(
	path string,
	gatewayID string,
	mode feishuVerificationPersistMode,
	verifiedAt time.Time,
) error {
	switch mode {
	case feishuVerificationPersistOnboardingComplete:
		return a.markFeishuAppOnboardingCompleted(path, gatewayID, verifiedAt)
	case feishuVerificationPersistIfMutable:
		return a.markFeishuAppVerified(path, gatewayID, verifiedAt)
	default:
		return nil
	}
}
