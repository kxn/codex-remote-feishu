package daemon

import (
	"log"
	"net/http"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/install"
)

var detectAutostart = install.DetectAutostart
var applyAutostart = install.ApplyAutostart
var disableAutostart = install.DisableAutostart

type autostartResponse = install.AutostartStatus

func (a *App) handleAutostartDetect(w http.ResponseWriter, _ *http.Request) {
	payload, err := detectAutostart(a.installStatePath())
	if err != nil {
		log.Printf("autostart detect failed: %v", err)
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "autostart_detect_failed",
			Message: "自动运行状态暂时无法读取，请稍后重试。",
		})
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func (a *App) handleAutostartApply(w http.ResponseWriter, _ *http.Request) {
	currentBinary, err := a.currentBinaryPath()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "autostart_apply_failed",
			Message: "failed to resolve current binary path",
			Details: err.Error(),
		})
		return
	}
	payload, err := applyAutostart(install.AutostartApplyOptions{
		StatePath:       a.installStatePath(),
		InstalledBinary: currentBinary,
		CurrentVersion:  a.currentBinaryVersion(),
	})
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, apiError{
			Code:    "autostart_apply_failed",
			Message: "failed to enable autostart",
			Details: err.Error(),
		})
		return
	}
	if err := a.writeOnboardingMachineDecision("autostart", onboardingDecisionAutostartEnabled, time.Now().UTC()); err != nil {
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "config_write_failed",
			Message: "autostart enabled but failed to persist onboarding decision",
			Details: err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func (a *App) handleAutostartDisable(w http.ResponseWriter, _ *http.Request) {
	status, err := disableAutostart(a.installStatePath())
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, apiError{
			Code:    "autostart_disable_failed",
			Message: "failed to disable autostart",
			Details: err.Error(),
		})
		return
	}
	if err := a.clearOnboardingMachineDecision("autostart"); err != nil {
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "config_write_failed",
			Message: "autostart disabled but failed to reset onboarding decision",
			Details: err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, status)
}
