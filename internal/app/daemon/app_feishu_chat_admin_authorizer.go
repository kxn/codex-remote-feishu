package daemon

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

const defaultFeishuChatAdminAuthorizeTimeout = 5 * time.Second

type feishuChatAdminAuthorizer struct {
	app *App

	mu       sync.Mutex
	checkers map[feishuChatAdminCheckerKey]*feishu.ChatAdminChecker
}

type feishuChatAdminCheckerKey struct {
	GatewayID      string
	AppID          string
	AppSecret      string
	Domain         string
	UseSystemProxy bool
}

func (a *feishuChatAdminAuthorizer) AuthorizeChatAdmin(ctx context.Context, req orchestrator.ChatAdminAuthorizationRequest) orchestrator.ChatAdminAuthorizationDecision {
	gatewayID := strings.TrimSpace(req.GatewayID)
	chatID := strings.TrimSpace(req.ChatID)
	actorOpenID := strings.TrimSpace(req.ActorOpenID)
	if chatID == "" {
		return orchestrator.ChatAdminAuthorizationDecision{Reason: feishu.ChatAdminDecisionMissingChat}
	}
	if actorOpenID == "" {
		return orchestrator.ChatAdminAuthorizationDecision{Reason: feishu.ChatAdminDecisionMissingActor}
	}
	if gatewayID == "" || a == nil || a.app == nil {
		return orchestrator.ChatAdminAuthorizationDecision{Reason: feishu.ChatAdminDecisionChatInfoUnavailable}
	}

	loaded, err := a.app.loadAdminConfig()
	if err != nil {
		return orchestrator.ChatAdminAuthorizationDecision{Reason: feishu.ChatAdminDecisionChatInfoUnavailable}
	}
	runtimeCfg, ok := a.app.runtimeGatewayConfigFor(loaded.Config, gatewayID)
	if !ok || !runtimeCfg.Enabled {
		return orchestrator.ChatAdminAuthorizationDecision{Reason: feishu.ChatAdminDecisionChatInfoUnavailable}
	}

	checkCtx, cancel := context.WithTimeout(ctx, defaultFeishuChatAdminAuthorizeTimeout)
	defer cancel()
	decision, err := a.chatAdminChecker(runtimeCfg).IsUserChatOwnerOrManager(checkCtx, chatID, feishu.ChatUserIdentity{
		ID:     actorOpenID,
		IDType: "open_id",
	})
	if err != nil {
		return orchestrator.ChatAdminAuthorizationDecision{Reason: firstNonEmpty(strings.TrimSpace(decision.Reason), feishu.ChatAdminDecisionChatInfoUnavailable)}
	}
	return orchestrator.ChatAdminAuthorizationDecision{
		Allowed: decision.Allowed,
		Reason:  decision.Reason,
	}
}

func (a *feishuChatAdminAuthorizer) chatAdminChecker(cfg feishu.GatewayAppConfig) *feishu.ChatAdminChecker {
	key := feishuChatAdminCheckerKey{
		GatewayID:      canonicalGatewayID(cfg.GatewayID),
		AppID:          strings.TrimSpace(cfg.AppID),
		AppSecret:      strings.TrimSpace(cfg.AppSecret),
		Domain:         strings.TrimSpace(cfg.Domain),
		UseSystemProxy: cfg.UseSystemProxy,
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.checkers == nil {
		a.checkers = map[feishuChatAdminCheckerKey]*feishu.ChatAdminChecker{}
	}
	checker := a.checkers[key]
	if checker == nil {
		checker = feishu.NewChatAdminCheckerFromLiveGatewayConfig(liveGatewayConfigFromRuntime(cfg), 0)
		a.checkers[key] = checker
	}
	return checker
}
