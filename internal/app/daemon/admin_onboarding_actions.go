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

func (a *App) handleFeishuAppOnboardingStepComplete(w http.ResponseWriter, r *http.Request) {
	gatewayID := canonicalGatewayID(r.PathValue("id"))
	step := strings.TrimSpace(r.PathValue("step"))
	if err := a.writeFeishuAppOnboardingStep(gatewayID, step, time.Now().UTC()); err != nil {
		status := http.StatusBadRequest
		code := "onboarding_step_write_failed"
		message := "failed to persist onboarding app step"
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

func (a *App) handleFeishuAppPermissionSkip(w http.ResponseWriter, r *http.Request) {
	gatewayID := canonicalGatewayID(r.PathValue("id"))
	if err := a.writeFeishuAppPermissionDecision(gatewayID, onboardingDecisionPermissionSkipped, time.Now().UTC()); err != nil {
		status := http.StatusBadRequest
		code := "onboarding_permission_write_failed"
		message := "failed to persist onboarding permission decision"
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

func (a *App) handleFeishuAppPermissionReset(w http.ResponseWriter, r *http.Request) {
	gatewayID := canonicalGatewayID(r.PathValue("id"))
	if err := a.clearFeishuAppPermissionDecision(gatewayID); err != nil {
		status := http.StatusBadRequest
		code := "onboarding_permission_write_failed"
		message := "failed to reset onboarding permission decision"
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

func (a *App) writeFeishuAppPermissionDecision(gatewayID, decision string, decidedAt time.Time) error {
	gatewayID = canonicalGatewayID(gatewayID)
	decision = strings.TrimSpace(decision)
	if gatewayID == "" {
		return invalidOnboardingDecisionError("permission", decision)
	}
	if decision != onboardingDecisionPermissionSkipped {
		return invalidOnboardingDecisionError("permission", decision)
	}
	if err := a.ensureFeishuAppExists(gatewayID); err != nil {
		return err
	}

	return a.updateOnboardingConfig(func(cfg *config.AppConfig) error {
		if cfg.Admin.Onboarding.Apps == nil {
			cfg.Admin.Onboarding.Apps = map[string]config.FeishuAppOnboardingState{}
		}
		state := cfg.Admin.Onboarding.Apps[gatewayID]
		state.PermissionDecision = &config.OnboardingDecision{
			Value:     onboardingDecisionPermissionSkipped,
			DecidedAt: daemonTimePtr(decidedAt.UTC()),
		}
		cfg.Admin.Onboarding.Apps[gatewayID] = state
		return nil
	})
}

func (a *App) clearFeishuAppPermissionDecision(gatewayID string) error {
	gatewayID = canonicalGatewayID(gatewayID)
	if gatewayID == "" {
		return invalidOnboardingDecisionError("permission", "")
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
		state.PermissionDecision = nil
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

func (a *App) writeFeishuAppOnboardingStep(gatewayID, step string, confirmedAt time.Time) error {
	gatewayID = canonicalGatewayID(gatewayID)
	step = strings.TrimSpace(step)
	if gatewayID == "" {
		return invalidOnboardingStepError(step)
	}
	if step != onboardingStageEvents && step != onboardingStageCallback && step != onboardingStageMenu {
		return invalidOnboardingStepError(step)
	}

	if err := a.ensureFeishuAppExists(gatewayID); err != nil {
		return err
	}

	return a.updateOnboardingConfig(func(cfg *config.AppConfig) error {
		if cfg.Admin.Onboarding.Apps == nil {
			cfg.Admin.Onboarding.Apps = map[string]config.FeishuAppOnboardingState{}
		}
		state := cfg.Admin.Onboarding.Apps[gatewayID]
		value := confirmedAt.UTC()
		switch step {
		case onboardingStageEvents:
			state.EventsConfirmedAt = daemonTimePtr(value)
		case onboardingStageCallback:
			state.CallbackConfirmedAt = daemonTimePtr(value)
		case onboardingStageMenu:
			state.MenuConfirmedAt = daemonTimePtr(value)
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

func invalidOnboardingStepError(step string) error {
	return errPlain("invalid onboarding step: " + strings.TrimSpace(step))
}

func feishuAppOnboardingStateEmpty(state config.FeishuAppOnboardingState) bool {
	return state.EventsConfirmedAt == nil &&
		state.CallbackConfirmedAt == nil &&
		state.MenuConfirmedAt == nil &&
		state.PermissionDecision == nil
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
