package daemon

import (
	"context"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

const defaultFeishuPermissionRefreshEvery = 2 * time.Minute
const feishuPrimaryPermissionCacheTTL = 2 * time.Minute

var listFeishuAppConfiguredScopes = feishu.ListAppConfiguredScopes

type feishuPermissionGapRecord struct {
	Scope           string
	ScopeType       string
	ApplyURL        string
	LastErrorCode   int
	LastErrorMsg    string
	FirstSeenAt     time.Time
	LastSeenAt      time.Time
	HitCount        int
	LastSourceAPI   string
	LastRequestID   string
	LastVerifiedAt  time.Time
	LastVerifyError string
}

func (a *App) observeFeishuPermissionError(gatewayID string, err error) bool {
	gatewayID = canonicalGatewayID(gatewayID)
	if gatewayID == "" {
		return false
	}
	gap, ok := feishu.ExtractPermissionGap(err)
	if !ok || strings.TrimSpace(gap.Scope) == "" {
		return false
	}
	now := time.Now().UTC()
	key := feishuPermissionGapKey(gap.Scope, gap.ScopeType)
	a.feishuRuntime.permissionMu.Lock()
	defer a.feishuRuntime.permissionMu.Unlock()
	if a.feishuRuntime.permissionGaps[gatewayID] == nil {
		a.feishuRuntime.permissionGaps[gatewayID] = map[string]*feishuPermissionGapRecord{}
	}
	record := a.feishuRuntime.permissionGaps[gatewayID][key]
	if record == nil {
		record = &feishuPermissionGapRecord{
			Scope:       strings.TrimSpace(gap.Scope),
			ScopeType:   strings.TrimSpace(gap.ScopeType),
			ApplyURL:    strings.TrimSpace(gap.ApplyURL),
			FirstSeenAt: now,
		}
		a.feishuRuntime.permissionGaps[gatewayID][key] = record
	}
	record.LastSeenAt = now
	record.HitCount++
	record.LastErrorCode = gap.ErrorCode
	record.LastErrorMsg = strings.TrimSpace(gap.ErrorMessage)
	record.LastSourceAPI = strings.TrimSpace(gap.SourceAPI)
	record.LastRequestID = strings.TrimSpace(gap.RequestID)
	if strings.TrimSpace(gap.ApplyURL) != "" {
		record.ApplyURL = strings.TrimSpace(gap.ApplyURL)
	}
	return true
}

func feishuPermissionGapKey(scope, scopeType string) string {
	return strings.TrimSpace(scope) + "|" + strings.TrimSpace(scopeType)
}

func (a *App) snapshotFeishuPermissionGaps(gatewayID string) []control.PermissionGapSummary {
	gatewayID = canonicalGatewayID(gatewayID)
	if gatewayID == "" {
		return nil
	}
	a.feishuRuntime.permissionMu.RLock()
	defer a.feishuRuntime.permissionMu.RUnlock()
	records := a.feishuRuntime.permissionGaps[gatewayID]
	if len(records) == 0 {
		return nil
	}
	values := make([]control.PermissionGapSummary, 0, len(records))
	for _, record := range records {
		if record == nil || strings.TrimSpace(record.Scope) == "" {
			continue
		}
		values = append(values, control.PermissionGapSummary{
			Scope:        record.Scope,
			ScopeType:    record.ScopeType,
			ApplyURL:     record.ApplyURL,
			SourceAPI:    record.LastSourceAPI,
			ErrorCode:    record.LastErrorCode,
			FirstSeenAt:  record.FirstSeenAt,
			LastSeenAt:   record.LastSeenAt,
			LastVerified: record.LastVerifiedAt,
			HitCount:     record.HitCount,
		})
	}
	sort.Slice(values, func(i, j int) bool {
		if !values[i].LastSeenAt.Equal(values[j].LastSeenAt) {
			return values[i].LastSeenAt.After(values[j].LastSeenAt)
		}
		return values[i].Scope < values[j].Scope
	})
	return values
}

func (a *App) populateSnapshotFeishuPermissionGaps(snapshot *control.Snapshot, surfaceID string) {
	if snapshot == nil {
		return
	}
	gatewayID := a.service.SurfaceGatewayID(surfaceID)
	snapshot.PermissionGaps = a.snapshotFeishuPermissionGaps(gatewayID)
}

func (a *App) clearFeishuPermissionGaps(gatewayID string) {
	gatewayID = canonicalGatewayID(gatewayID)
	if gatewayID == "" {
		return
	}
	a.feishuRuntime.permissionMu.Lock()
	delete(a.feishuRuntime.permissionGaps, gatewayID)
	a.feishuRuntime.permissionMu.Unlock()
}

func (a *App) maybeStartFeishuPermissionRefreshLocked(now time.Time) {
	if a.feishuRuntime.permissionRefreshInFlight {
		return
	}
	if a.feishuRuntime.permissionRefreshEvery <= 0 {
		a.feishuRuntime.permissionRefreshEvery = defaultFeishuPermissionRefreshEvery
	}
	a.feishuRuntime.permissionMu.RLock()
	hasGaps := len(a.feishuRuntime.permissionGaps) != 0
	a.feishuRuntime.permissionMu.RUnlock()
	if !hasGaps {
		return
	}
	if !a.feishuRuntime.permissionNextRefresh.IsZero() && now.Before(a.feishuRuntime.permissionNextRefresh) {
		return
	}
	a.feishuRuntime.permissionRefreshInFlight = true
	a.feishuRuntime.permissionNextRefresh = now.Add(a.feishuRuntime.permissionRefreshEvery)
	go a.refreshFeishuPermissionGaps()
}

func (a *App) refreshFeishuPermissionGaps() {
	defer func() {
		a.mu.Lock()
		a.feishuRuntime.permissionRefreshInFlight = false
		a.mu.Unlock()
	}()

	a.feishuRuntime.permissionMu.RLock()
	gatewayIDs := make([]string, 0, len(a.feishuRuntime.permissionGaps))
	for gatewayID := range a.feishuRuntime.permissionGaps {
		gatewayIDs = append(gatewayIDs, gatewayID)
	}
	a.feishuRuntime.permissionMu.RUnlock()

	for _, gatewayID := range gatewayIDs {
		verifyCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		scopes, err := a.loadFeishuAppConfiguredScopes(verifyCtx, gatewayID)
		cancel()
		a.applyFeishuPermissionVerificationResult(gatewayID, scopes, err)
	}
}

func (a *App) loadFeishuAppConfiguredScopes(ctx context.Context, gatewayID string) ([]feishu.AppScopeStatus, error) {
	loaded, err := a.loadAdminConfig()
	if err != nil {
		return nil, err
	}
	runtimeCfg, ok := a.runtimeGatewayConfigFor(loaded.Config, gatewayID)
	if !ok {
		return nil, nil
	}
	return listFeishuAppConfiguredScopes(ctx, feishu.LiveGatewayConfig{
		GatewayID: runtimeCfg.GatewayID,
		AppID:     runtimeCfg.AppID,
		AppSecret: runtimeCfg.AppSecret,
	})
}

func (a *App) applyFeishuPermissionVerificationResult(gatewayID string, scopes []feishu.AppScopeStatus, err error) {
	gatewayID = canonicalGatewayID(gatewayID)
	if gatewayID == "" {
		return
	}
	now := time.Now().UTC()
	granted := map[string]bool{}
	for _, item := range scopes {
		if feishuScopeStatusGranted(item) {
			granted[feishuPermissionGapKey(item.ScopeName, item.ScopeType)] = true
			granted[feishuPermissionGapKey(item.ScopeName, "")] = true
		}
	}
	a.feishuRuntime.permissionMu.Lock()
	defer a.feishuRuntime.permissionMu.Unlock()
	records := a.feishuRuntime.permissionGaps[gatewayID]
	if len(records) == 0 {
		return
	}
	for key, record := range records {
		if record == nil {
			delete(records, key)
			continue
		}
		record.LastVerifiedAt = now
		record.LastVerifyError = ""
		if err != nil {
			record.LastVerifyError = err.Error()
			continue
		}
		if granted[feishuPermissionGapKey(record.Scope, record.ScopeType)] || granted[feishuPermissionGapKey(record.Scope, "")] {
			delete(records, key)
		}
	}
	if len(records) == 0 {
		delete(a.feishuRuntime.permissionGaps, gatewayID)
	}
	if err != nil {
		log.Printf("feishu permission verification failed: gateway=%s err=%v", gatewayID, err)
		return
	}
	if clearer, ok := a.gateway.(feishu.PermissionBlockController); ok {
		clearer.ClearGrantedPermissionBlocks(gatewayID, scopes)
	}
}

func feishuScopeStatusGranted(status feishu.AppScopeStatus) bool {
	status.ScopeName = strings.TrimSpace(status.ScopeName)
	if status.ScopeName == "" {
		return false
	}
	// The upstream SDK exposes grant_status without an inline enum table.
	// Keep the auto-clear condition intentionally narrow.
	return status.GrantStatus == 1
}

func (a *App) CheckPrimaryBotPermission(ctx context.Context, req orchestrator.PrimaryBotPermissionRequest) orchestrator.PrimaryBotPermissionDecision {
	gatewayID := canonicalGatewayID(req.GatewayID)
	if gatewayID == "" {
		return orchestrator.PrimaryBotPermissionDecision{Allowed: false, Reason: "missing_gateway"}
	}
	now := time.Now().UTC()
	if !req.ForceRefresh {
		if cached, ok := a.cachedPrimaryBotPermission(gatewayID, now); ok {
			return primaryPermissionDecisionFromCache(cached)
		}
	}
	checkCtx := ctx
	if checkCtx == nil {
		checkCtx = context.Background()
	}
	checkCtx, cancel := context.WithTimeout(checkCtx, 20*time.Second)
	defer cancel()
	scopes, err := a.loadFeishuAppConfiguredScopes(checkCtx, gatewayID)
	decision := primaryPermissionDecisionFromScopes(scopes, err)
	a.storePrimaryBotPermissionCache(gatewayID, decision, now, req.ForceRefresh)
	return decision
}

func (a *App) cachedPrimaryBotPermission(gatewayID string, now time.Time) (feishuPrimaryPermissionCacheRecord, bool) {
	a.feishuRuntime.permissionMu.RLock()
	defer a.feishuRuntime.permissionMu.RUnlock()
	cached, ok := a.feishuRuntime.primaryPermissionCache[gatewayID]
	if !ok || cached.ExpiresAt.IsZero() || !now.Before(cached.ExpiresAt) {
		return feishuPrimaryPermissionCacheRecord{}, false
	}
	return cached, true
}

func (a *App) storePrimaryBotPermissionCache(gatewayID string, decision orchestrator.PrimaryBotPermissionDecision, now time.Time, forceRefreshed bool) {
	a.feishuRuntime.permissionMu.Lock()
	defer a.feishuRuntime.permissionMu.Unlock()
	if a.feishuRuntime.primaryPermissionCache == nil {
		a.feishuRuntime.primaryPermissionCache = map[string]feishuPrimaryPermissionCacheRecord{}
	}
	record := feishuPrimaryPermissionCacheRecord{
		GatewayID:      gatewayID,
		Allowed:        decision.Allowed,
		Scope:          strings.TrimSpace(decision.Scope),
		CheckedAt:      now,
		ExpiresAt:      now.Add(feishuPrimaryPermissionCacheTTL),
		LastReason:     strings.TrimSpace(decision.Reason),
		ForceRefreshed: forceRefreshed,
	}
	if decision.Err != nil {
		record.LastErr = decision.Err.Error()
	}
	a.feishuRuntime.primaryPermissionCache[gatewayID] = record
}

func primaryPermissionDecisionFromCache(record feishuPrimaryPermissionCacheRecord) orchestrator.PrimaryBotPermissionDecision {
	decision := orchestrator.PrimaryBotPermissionDecision{
		Allowed: record.Allowed,
		Scope:   strings.TrimSpace(record.Scope),
		Reason:  strings.TrimSpace(record.LastReason),
	}
	if decision.Reason == "" && !decision.Allowed {
		decision.Reason = "cached_missing"
	}
	return decision
}

func primaryPermissionDecisionFromScopes(scopes []feishu.AppScopeStatus, err error) orchestrator.PrimaryBotPermissionDecision {
	if err != nil {
		return orchestrator.PrimaryBotPermissionDecision{Allowed: false, Reason: "scope_read_failed", Err: err}
	}
	for _, item := range scopes {
		if !feishuScopeStatusGranted(item) {
			continue
		}
		scope := strings.TrimSpace(item.ScopeName)
		switch scope {
		case "im:message.group_msg", "im:message.group_msg:readonly":
			return orchestrator.PrimaryBotPermissionDecision{Allowed: true, Scope: scope}
		}
	}
	return orchestrator.PrimaryBotPermissionDecision{Allowed: false, Reason: "missing_group_message_scope"}
}
