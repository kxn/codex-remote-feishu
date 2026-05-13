package daemon

import (
	"net/http"
)

func (a *App) handleDesktopSessionStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.desktopSessionStatusPayload())
}

func (a *App) handleDesktopSessionQuit(w http.ResponseWriter, _ *http.Request) {
	if err := a.requestDesktopSessionQuit(); err != nil {
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "desktop_session_quit_failed",
			Message: "failed to request desktop session quit",
			Details: err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusAccepted, a.desktopSessionStatusPayload())
}
