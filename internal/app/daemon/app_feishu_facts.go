package daemon

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/feishufacts"
)

const defaultFeishuFactsRefreshEvery = 2 * time.Minute
const feishuFactsFreshTTL = 2 * time.Minute

var getFeishuBotInfo = feishu.GetBotInfo

func feishuFactsScopesFresh(record feishufacts.Record, now time.Time) bool {
	return !record.ScopesFetchedAt.IsZero() &&
		record.ScopesError == "" &&
		now.Sub(record.ScopesFetchedAt) <= feishuFactsFreshTTL
}

func (a *App) maybeStartFeishuFactsRefreshLocked(now time.Time) {
	a.feishuFactsState.mu.Lock()
	defer a.feishuFactsState.mu.Unlock()
	if a.feishuFactsState.refreshInFlight {
		return
	}
	if !a.feishuFactsState.nextRefresh.IsZero() && now.Before(a.feishuFactsState.nextRefresh) {
		return
	}
	a.feishuFactsState.refreshInFlight = true
	a.feishuFactsState.nextRefresh = now.Add(defaultFeishuFactsRefreshEvery)
	go a.refreshFeishuBotFactsBackground()
}

func (a *App) refreshFeishuBotFactsBackground() {
	defer func() {
		a.feishuFactsState.mu.Lock()
		a.feishuFactsState.refreshInFlight = false
		a.feishuFactsState.mu.Unlock()
	}()
	loaded, err := a.loadAdminConfig()
	if err != nil {
		log.Printf("feishu facts background refresh: load config failed: %v", err)
		return
	}
	for _, app := range a.runtimeGatewayApps(loaded.Config) {
		if strings.TrimSpace(app.AppID) == "" || strings.TrimSpace(app.AppSecret) == "" || !app.Enabled {
			continue
		}
		refreshCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		_, _ = a.RefreshFeishuBotFacts(refreshCtx, app.GatewayID)
		cancel()
	}
}

func (a *App) FeishuBotFacts(gatewayID string) (feishufacts.Record, bool) {
	gatewayID = canonicalGatewayID(gatewayID)
	if gatewayID == "" || a == nil || a.feishuFactsState.store == nil {
		return feishufacts.Record{}, false
	}
	a.feishuFactsState.mu.RLock()
	defer a.feishuFactsState.mu.RUnlock()
	return a.feishuFactsState.store.Get(gatewayID)
}

func (a *App) feishuFactAppName(gatewayID string) string {
	facts, ok := a.FeishuBotFacts(gatewayID)
	if !ok {
		return ""
	}
	return strings.TrimSpace(facts.AppName)
}

func (a *App) feishuFactBotOpenID(gatewayID string) string {
	facts, ok := a.FeishuBotFacts(gatewayID)
	if !ok {
		return ""
	}
	return strings.TrimSpace(facts.BotOpenID)
}

func (a *App) RefreshFeishuBotFacts(ctx context.Context, gatewayID string) (feishufacts.Record, error) {
	gatewayID = canonicalGatewayID(gatewayID)
	if gatewayID == "" {
		return feishufacts.Record{}, fmt.Errorf("feishu facts: missing gateway id")
	}
	loaded, err := a.loadAdminConfig()
	if err != nil {
		return feishufacts.Record{}, err
	}
	runtimeCfg, ok := a.runtimeGatewayConfigFor(loaded.Config, gatewayID)
	if !ok {
		return feishufacts.Record{}, fmt.Errorf("feishu facts: gateway %q not found", gatewayID)
	}

	cfg := feishu.LiveGatewayConfig{
		GatewayID: runtimeCfg.GatewayID,
		AppID:     runtimeCfg.AppID,
		AppSecret: runtimeCfg.AppSecret,
	}
	botInfo, botErr := getFeishuBotInfo(ctx, cfg)
	scopes, scopeErr := listFeishuAppConfiguredScopes(ctx, cfg)
	now := time.Now().UTC()

	record, _ := a.FeishuBotFacts(gatewayID)
	record.GatewayID = gatewayID
	record.AppID = strings.TrimSpace(runtimeCfg.AppID)
	if botErr == nil {
		record.AppName = strings.TrimSpace(botInfo.AppName)
		record.BotOpenID = strings.TrimSpace(botInfo.OpenID)
		record.BotError = ""
	} else {
		record.BotError = botErr.Error()
	}
	if scopeErr == nil {
		record.Scopes = feishuFactsScopesFromAppScopes(scopes)
		record.ScopesFetchedAt = now
		record.ScopesError = ""
	} else {
		record.ScopesError = scopeErr.Error()
	}
	record.FetchedAt = now
	if botErr != nil || scopeErr != nil {
		record.LastError = feishuFactsErrorText(botErr, scopeErr)
		record.LastErrorAt = now
	} else {
		record.LastError = ""
		record.LastErrorAt = time.Time{}
	}

	a.feishuFactsState.mu.Lock()
	if a.feishuFactsState.store != nil && a.feishuFactsState.writable() {
		if err := a.feishuFactsState.store.Put(record); err != nil {
			a.feishuFactsState.mu.Unlock()
			return record, err
		}
	}
	a.feishuFactsState.mu.Unlock()

	if botErr == nil && strings.TrimSpace(record.BotOpenID) != "" {
		if updater, ok := a.gateway.(interface{ SetBotOpenID(string, string) }); ok {
			updater.SetBotOpenID(gatewayID, record.BotOpenID)
		}
	}

	a.afterFeishuFactsRefresh(gatewayID, scopes, scopeErr)

	if scopeErr != nil {
		return record, scopeErr
	}
	if botErr != nil {
		return record, botErr
	}
	return record, nil
}

func (a *App) afterFeishuFactsRefresh(gatewayID string, scopes []feishu.AppScopeStatus, scopeErr error) {
	a.feishuRuntime.permissionMu.RLock()
	hasGaps := len(a.feishuRuntime.permissionGaps[gatewayID]) != 0
	a.feishuRuntime.permissionMu.RUnlock()
	if hasGaps {
		a.applyFeishuPermissionVerificationResult(gatewayID, scopes, scopeErr)
	}
}

func feishuFactsScopesFromAppScopes(scopes []feishu.AppScopeStatus) []feishufacts.ScopeStatus {
	values := make([]feishufacts.ScopeStatus, 0, len(scopes))
	for _, scope := range scopes {
		values = append(values, feishufacts.ScopeStatus{
			ScopeName:   strings.TrimSpace(scope.ScopeName),
			ScopeType:   strings.TrimSpace(scope.ScopeType),
			GrantStatus: scope.GrantStatus,
		})
	}
	return values
}

func appScopesFromFeishuFactsScopes(scopes []feishufacts.ScopeStatus) []feishu.AppScopeStatus {
	values := make([]feishu.AppScopeStatus, 0, len(scopes))
	for _, scope := range scopes {
		values = append(values, feishu.AppScopeStatus{
			ScopeName:   strings.TrimSpace(scope.ScopeName),
			ScopeType:   strings.TrimSpace(scope.ScopeType),
			GrantStatus: scope.GrantStatus,
		})
	}
	return values
}

func feishuFactsErrorText(botErr, scopeErr error) string {
	switch {
	case botErr != nil && scopeErr != nil:
		return fmt.Sprintf("bot info: %v; scopes: %v", botErr, scopeErr)
	case botErr != nil:
		return botErr.Error()
	case scopeErr != nil:
		return scopeErr.Error()
	default:
		return ""
	}
}
