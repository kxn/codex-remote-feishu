package daemon

import (
	"context"
	"net/http"
	"strings"
	"time"
)

func (a *App) handleFeishuAppFactsGet(w http.ResponseWriter, r *http.Request) {
	gatewayID := strings.TrimSpace(r.PathValue("id"))
	facts, ok := a.FeishuBotFacts(gatewayID)
	if !ok {
		writeAPIError(w, http.StatusNotFound, apiError{
			Code:    "feishu_facts_not_found",
			Message: "feishu app facts not found",
			Details: gatewayID,
		})
		return
	}
	writeJSON(w, http.StatusOK, facts)
}

func (a *App) handleFeishuAppFactsRefresh(w http.ResponseWriter, r *http.Request) {
	gatewayID := strings.TrimSpace(r.PathValue("id"))
	refreshCtx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	facts, err := a.RefreshFeishuBotFacts(refreshCtx, gatewayID)
	if err != nil {
		status := http.StatusBadGateway
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "missing gateway") {
			status = http.StatusNotFound
		}
		writeAPIError(w, status, apiError{
			Code:    "feishu_facts_refresh_failed",
			Message: "failed to refresh feishu app facts",
			Details: err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, facts)
}
