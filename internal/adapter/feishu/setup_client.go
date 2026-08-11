package feishu

import (
	"strings"

	gatewaypkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/gateway"
	lark "github.com/larksuite/oapi-sdk-go/v3"
)

type SetupClientConfig struct {
	GatewayID      string
	AppID          string
	AppSecret      string
	Domain         string
	UseSystemProxy bool
}

type SetupClient struct {
	config    SetupClientConfig
	sdkClient *lark.Client
	broker    *FeishuCallBroker
}

func SetupClientConfigFromLiveGatewayConfig(cfg LiveGatewayConfig) SetupClientConfig {
	return SetupClientConfig{
		GatewayID:      cfg.GatewayID,
		AppID:          cfg.AppID,
		AppSecret:      cfg.AppSecret,
		Domain:         cfg.Domain,
		UseSystemProxy: cfg.UseSystemProxy,
	}
}

func (c SetupClientConfig) liveGatewayConfig() LiveGatewayConfig {
	return LiveGatewayConfig{
		GatewayID:      c.GatewayID,
		AppID:          c.AppID,
		AppSecret:      c.AppSecret,
		Domain:         c.Domain,
		UseSystemProxy: c.UseSystemProxy,
	}
}

func NewSetupClient(config SetupClientConfig) *SetupClient {
	config.GatewayID = gatewaypkg.NormalizeGatewayID(config.GatewayID)
	sdkClient := NewLarkClientWithOpenBaseURL(config.AppID, config.AppSecret, setupHTTPDomain(config))
	return &SetupClient{
		config:    config,
		sdkClient: sdkClient,
		broker:    NewFeishuCallBroker(config.GatewayID, sdkClient),
	}
}

func (c *SetupClient) liveGatewayConfig() LiveGatewayConfig {
	if c == nil {
		return LiveGatewayConfig{}
	}
	return c.config.liveGatewayConfig()
}

func (c *SetupClient) sdk() (*lark.Client, *FeishuCallBroker) {
	if c == nil {
		return nil, nil
	}
	return c.sdkClient, c.broker
}

func setupHTTPDomain(cfg SetupClientConfig) string {
	if strings.TrimSpace(cfg.Domain) != "" {
		return strings.TrimSpace(cfg.Domain)
	}
	return "https://open.feishu.cn"
}
