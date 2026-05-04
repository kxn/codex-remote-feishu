package daemon

import (
	"net/http"
	"strings"
	"time"
)

const (
	feishuRuntimeApplyActionUpsert = "upsert"
	feishuRuntimeApplyActionRemove = "remove"
)

type feishuRuntimeApplyPendingState struct {
	GatewayID string
	Action    string
	Error     string
	UpdatedAt time.Time
}

func (a *App) snapshotFeishuRuntimeApplyPending() map[string]feishuRuntimeApplyPendingState {
	a.feishuRuntime.mu.RLock()
	defer a.feishuRuntime.mu.RUnlock()
	if len(a.feishuRuntime.runtimeApply) == 0 {
		return map[string]feishuRuntimeApplyPendingState{}
	}
	values := make(map[string]feishuRuntimeApplyPendingState, len(a.feishuRuntime.runtimeApply))
	for gatewayID, pending := range a.feishuRuntime.runtimeApply {
		values[gatewayID] = pending
	}
	return values
}

func (a *App) feishuRuntimeApplyPendingState(gatewayID string) (feishuRuntimeApplyPendingState, bool) {
	a.feishuRuntime.mu.RLock()
	defer a.feishuRuntime.mu.RUnlock()
	pending, ok := a.feishuRuntime.runtimeApply[canonicalGatewayID(gatewayID)]
	return pending, ok
}

func (a *App) markFeishuRuntimeApplyPending(summary adminFeishuAppSummary, action string, err error) feishuRuntimeApplyPendingState {
	now := time.Now().UTC()
	gatewayID := canonicalGatewayID(summary.ID)
	pending := feishuRuntimeApplyPendingState{
		GatewayID: gatewayID,
		Action:    strings.TrimSpace(action),
		Error:     strings.TrimSpace(err.Error()),
		UpdatedAt: now,
	}
	a.feishuRuntime.mu.Lock()
	a.feishuRuntime.runtimeApply[gatewayID] = pending
	a.feishuRuntime.mu.Unlock()
	return pending
}

func (a *App) clearFeishuRuntimeApplyPending(gatewayID string) {
	gatewayID = canonicalGatewayID(gatewayID)
	a.feishuRuntime.mu.Lock()
	delete(a.feishuRuntime.runtimeApply, gatewayID)
	a.feishuRuntime.mu.Unlock()
}

func applyFeishuRuntimePending(summary adminFeishuAppSummary, pending feishuRuntimeApplyPendingState) adminFeishuAppSummary {
	updatedAt := pending.UpdatedAt
	summary.RuntimeApply = &adminFeishuRuntimeApplyView{
		Pending:        true,
		Action:         pending.Action,
		Error:          pending.Error,
		UpdatedAt:      &updatedAt,
		RetryAvailable: true,
	}
	return summary
}

func pendingFeishuAppSummary(gatewayID string, pending feishuRuntimeApplyPendingState) adminFeishuAppSummary {
	gatewayID = canonicalGatewayID(firstNonEmpty(gatewayID, pending.GatewayID))
	return adminFeishuAppSummary{
		ID:      gatewayID,
		Name:    gatewayID,
		Enabled: true,
	}
}

func (a *App) writeFeishuRuntimeApplyError(w http.ResponseWriter, gatewayID string, summary adminFeishuAppSummary, action string, message string, err error) {
	pending := a.markFeishuRuntimeApplyPending(summary, action, err)
	summary = applyFeishuRuntimePending(summary, pending)
	writeAPIError(w, http.StatusInternalServerError, apiError{
		Code:      "gateway_apply_failed",
		Message:   message,
		Retryable: true,
		Details: feishuRuntimeApplyErrorDetails{
			GatewayID: canonicalGatewayID(gatewayID),
			App:       &summary,
		},
	})
}
