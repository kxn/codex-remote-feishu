package daemon

import (
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
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "autostart_detect_failed",
			Message: "failed to detect autostart state",
			Details: err.Error(),
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
