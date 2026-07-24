package daemon

import (
	"context"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

func TestFeishuChatAdminAuthorizerRejectsMissingActorWithoutLoadingConfig(t *testing.T) {
	loadedConfig := false
	authorizer := &feishuChatAdminAuthorizer{
		app: &App{
			admin: adminRuntimeState{
				loadConfig: func() (config.LoadedAppConfig, error) {
					loadedConfig = true
					return config.LoadedAppConfig{}, nil
				},
			},
		},
	}

	decision := authorizer.AuthorizeChatAdmin(context.Background(), orchestrator.ChatAdminAuthorizationRequest{
		GatewayID: "feishu-main",
		ChatID:    "oc_chat",
	})

	if decision.Allowed {
		t.Fatal("expected missing actor to be rejected")
	}
	if decision.Reason != feishu.ChatAdminDecisionMissingActor {
		t.Fatalf("reason = %q, want %q", decision.Reason, feishu.ChatAdminDecisionMissingActor)
	}
	if loadedConfig {
		t.Fatal("missing actor path should not load admin config")
	}
}
