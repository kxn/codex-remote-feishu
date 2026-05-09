package daemon

import (
	"net/http"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/config"
)

type onboardingDecisionRequest struct {
	Decision string `json:"decision,omitempty"`
}

func (a *App) handleOnboardingMachineDecision(w http.ResponseWriter, r *http.Request) {
	kind := strings.TrimSpace(r.PathValue("kind"))
	var req onboardingDecisionRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, apiError{
			Code:    "invalid_request",
			Message: "failed to decode onboarding decision payload",
			Details: err.Error(),
		})
		return
	}
	if err := a.writeOnboardingMachineDecision(kind, strings.TrimSpace(req.Decision), time.Now().UTC()); err != nil {
		writeAPIError(w, http.StatusBadRequest, apiError{
			Code:    "onboarding_decision_write_failed",
			Message: "failed to persist onboarding machine decision",
			Details: err.Error(),
		})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleFeishuAppAutoConfigDefer(w http.ResponseWriter, r *http.Request) {
	gatewayID := canonicalGatewayID(r.PathValue("id"))
	if err := a.writeFeishuAppAutoConfigDecision(gatewayID, onboardingDecisionDeferred, time.Now().UTC()); err != nil {
		status := http.StatusBadRequest
		code := "onboarding_auto_config_write_failed"
		message := "failed to persist onboarding auto-config decision"
		if strings.HasPrefix(err.Error(), "feishu_app_not_found:") {
			status = http.StatusNotFound
			code = "feishu_app_not_found"
			message = "feishu app not found"
		}
		writeAPIError(w, status, apiError{
			Code:    code,
			Message: message,
			Details: err.Error(),
		})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleFeishuAppAutoConfigReset(w http.ResponseWriter, r *http.Request) {
	gatewayID := canonicalGatewayID(r.PathValue("id"))
	if err := a.clearFeishuAppAutoConfigDecision(gatewayID); err != nil {
		status := http.StatusBadRequest
		code := "onboarding_auto_config_write_failed"
		message := "failed to reset onboarding auto-config decision"
		if strings.HasPrefix(err.Error(), "feishu_app_not_found:") {
			status = http.StatusNotFound
			code = "feishu_app_not_found"
			message = "feishu app not found"
		}
		writeAPIError(w, status, apiError{
			Code:    code,
			Message: message,
			Details: err.Error(),
		})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleFeishuAppMenuConfirm(w http.ResponseWriter, r *http.Request) {
	gatewayID := canonicalGatewayID(r.PathValue("id"))
	if err := a.writeFeishuAppMenuDecision(gatewayID, onboardingDecisionMenuConfirmed, time.Now().UTC()); err != nil {
		status := http.StatusBadRequest
		code := "onboarding_menu_write_failed"
		message := "failed to persist onboarding menu decision"
		if strings.HasPrefix(err.Error(), "feishu_app_not_found:") {
			status = http.StatusNotFound
			code = "feishu_app_not_found"
			message = "feishu app not found"
		}
		writeAPIError(w, status, apiError{
			Code:    code,
			Message: message,
			Details: err.Error(),
		})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleFeishuAppMenuReset(w http.ResponseWriter, r *http.Request) {
	gatewayID := canonicalGatewayID(r.PathValue("id"))
	if err := a.clearFeishuAppMenuDecision(gatewayID); err != nil {
		status := http.StatusBadRequest
		code := "onboarding_menu_write_failed"
		message := "failed to reset onboarding menu decision"
		if strings.HasPrefix(err.Error(), "feishu_app_not_found:") {
			status = http.StatusNotFound
			code = "feishu_app_not_found"
			message = "feishu app not found"
		}
		writeAPIError(w, status, apiError{
			Code:    code,
			Message: message,
			Details: err.Error(),
		})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) writeOnboardingMachineDecision(kind, decision string, decidedAt time.Time) error {
	kind = strings.TrimSpace(kind)
	decision = strings.TrimSpace(decision)
	switch kind {
	case "autostart":
		if decision != onboardingDecisionAutostartEnabled && decision != onboardingDecisionDeferred {
			return invalidOnboardingDecisionError(kind, decision)
		}
	case "vscode":
		switch decision {
		case onboardingDecisionVSCodeManaged, onboardingDecisionDeferred, onboardingDecisionVSCodeRemoteOnly:
		default:
			return invalidOnboardingDecisionError(kind, decision)
		}
	default:
		return invalidOnboardingDecisionError(kind, decision)
	}
	return a.updateOnboardingConfig(func(cfg *config.AppConfig) error {
		record := &config.OnboardingDecision{
			Value:     decision,
			DecidedAt: daemonTimePtr(decidedAt.UTC()),
		}
		switch kind {
		case "autostart":
			cfg.Admin.Onboarding.AutostartDecision = record
		case "vscode":
			cfg.Admin.Onboarding.VSCodeDecision = record
		}
		return nil
	})
}

func (a *App) writeFeishuAppAutoConfigDecision(gatewayID, decision string, decidedAt time.Time) error {
	gatewayID = canonicalGatewayID(gatewayID)
	decision = strings.TrimSpace(decision)
	if gatewayID == "" {
		return invalidOnboardingDecisionError("auto_config", decision)
	}
	if decision != onboardingDecisionDeferred {
		return invalidOnboardingDecisionError("auto_config", decision)
	}
	if err := a.ensureFeishuAppExists(gatewayID); err != nil {
		return err
	}

	return a.updateOnboardingConfig(func(cfg *config.AppConfig) error {
		if cfg.Admin.Onboarding.Apps == nil {
			cfg.Admin.Onboarding.Apps = map[string]config.FeishuAppOnboardingState{}
		}
		state := cfg.Admin.Onboarding.Apps[gatewayID]
		state.AutoConfigDecision = &config.OnboardingDecision{
			Value:     onboardingDecisionDeferred,
			DecidedAt: daemonTimePtr(decidedAt.UTC()),
		}
		cfg.Admin.Onboarding.Apps[gatewayID] = state
		return nil
	})
}

func (a *App) clearFeishuAppAutoConfigDecision(gatewayID string) error {
	gatewayID = canonicalGatewayID(gatewayID)
	if gatewayID == "" {
		return invalidOnboardingDecisionError("auto_config", "")
	}
	if err := a.ensureFeishuAppExists(gatewayID); err != nil {
		return err
	}

	return a.updateOnboardingConfig(func(cfg *config.AppConfig) error {
		if cfg.Admin.Onboarding.Apps == nil {
			return nil
		}
		state, ok := cfg.Admin.Onboarding.Apps[gatewayID]
		if !ok {
			return nil
		}
		state.AutoConfigDecision = nil
		if feishuAppOnboardingStateEmpty(state) {
			delete(cfg.Admin.Onboarding.Apps, gatewayID)
			if len(cfg.Admin.Onboarding.Apps) == 0 {
				cfg.Admin.Onboarding.Apps = nil
			}
			return nil
		}
		cfg.Admin.Onboarding.Apps[gatewayID] = state
		return nil
	})
}

func (a *App) writeFeishuAppMenuDecision(gatewayID, decision string, decidedAt time.Time) error {
	gatewayID = canonicalGatewayID(gatewayID)
	decision = strings.TrimSpace(decision)
	if gatewayID == "" {
		return invalidOnboardingDecisionError("menu", decision)
	}
	if decision != onboardingDecisionMenuConfirmed {
		return invalidOnboardingDecisionError("menu", decision)
	}
	if err := a.ensureFeishuAppExists(gatewayID); err != nil {
		return err
	}

	return a.updateOnboardingConfig(func(cfg *config.AppConfig) error {
		if cfg.Admin.Onboarding.Apps == nil {
			cfg.Admin.Onboarding.Apps = map[string]config.FeishuAppOnboardingState{}
		}
		state := cfg.Admin.Onboarding.Apps[gatewayID]
		state.MenuDecision = &config.OnboardingDecision{
			Value:     onboardingDecisionMenuConfirmed,
			DecidedAt: daemonTimePtr(decidedAt.UTC()),
		}
		cfg.Admin.Onboarding.Apps[gatewayID] = state
		return nil
	})
}

func (a *App) clearFeishuAppMenuDecision(gatewayID string) error {
	gatewayID = canonicalGatewayID(gatewayID)
	if gatewayID == "" {
		return invalidOnboardingDecisionError("menu", "")
	}
	if err := a.ensureFeishuAppExists(gatewayID); err != nil {
		return err
	}

	return a.updateOnboardingConfig(func(cfg *config.AppConfig) error {
		if cfg.Admin.Onboarding.Apps == nil {
			return nil
		}
		state, ok := cfg.Admin.Onboarding.Apps[gatewayID]
		if !ok {
			return nil
		}
		state.MenuDecision = nil
		if feishuAppOnboardingStateEmpty(state) {
			delete(cfg.Admin.Onboarding.Apps, gatewayID)
			if len(cfg.Admin.Onboarding.Apps) == 0 {
				cfg.Admin.Onboarding.Apps = nil
			}
			return nil
		}
		cfg.Admin.Onboarding.Apps[gatewayID] = state
		return nil
	})
}

func (a *App) clearFeishuAppOnboardingState(gatewayID string) error {
	gatewayID = canonicalGatewayID(gatewayID)
	if gatewayID == "" {
		return nil
	}
	return a.updateOnboardingConfig(func(cfg *config.AppConfig) error {
		if len(cfg.Admin.Onboarding.Apps) == 0 {
			return nil
		}
		delete(cfg.Admin.Onboarding.Apps, gatewayID)
		if len(cfg.Admin.Onboarding.Apps) == 0 {
			cfg.Admin.Onboarding.Apps = nil
		}
		return nil
	})
}

func (a *App) ensureFeishuAppExists(gatewayID string) error {
	loaded, err := a.loadAdminConfig()
	if err != nil {
		return err
	}
	if _, ok, err := a.adminFeishuAppSummary(loaded, gatewayID); err != nil {
		return err
	} else if !ok {
		return errFeishuAppNotFound(gatewayID)
	}
	return nil
}

func (a *App) updateOnboardingConfig(update func(*config.AppConfig) error) error {
	a.adminConfigMu.Lock()
	defer a.adminConfigMu.Unlock()

	loaded, err := a.loadAdminConfig()
	if err != nil {
		return err
	}
	cfg := loaded.Config
	if err := update(&cfg); err != nil {
		return err
	}
	return config.WriteAppConfig(loaded.Path, cfg)
}

func invalidOnboardingDecisionError(kind, decision string) error {
	return errPlain("invalid onboarding decision for " + strings.TrimSpace(kind) + ": " + strings.TrimSpace(decision))
}

func feishuAppOnboardingStateEmpty(state config.FeishuAppOnboardingState) bool {
	return state.AutoConfigDecision == nil &&
		state.MenuDecision == nil
}

func daemonTimePtr(value time.Time) *time.Time {
	return &value
}

func errPlain(message string) error {
	return plainError(strings.TrimSpace(message))
}

type plainError string

func (e plainError) Error() string {
	return string(e)
}
