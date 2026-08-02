package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkevent "github.com/larksuite/oapi-sdk-go/v3/service/event/v1"
)

type LongConnectionStatus struct {
	OnlineInstanceCount int       `json:"onlineInstanceCount"`
	CheckedAt           time.Time `json:"checkedAt"`
}

type BotInfo struct {
	AppName string
	OpenID  string
}

type botInfoHTTPResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Bot  struct {
		AppName string `json:"app_name"`
		OpenID  string `json:"open_id"`
	} `json:"bot"`
}

func GetLongConnectionStatus(ctx context.Context, cfg LiveGatewayConfig) (LongConnectionStatus, error) {
	return NewSetupClient(SetupClientConfigFromLiveGatewayConfig(cfg)).GetLongConnectionStatus(ctx)
}

func (c *SetupClient) GetLongConnectionStatus(ctx context.Context) (LongConnectionStatus, error) {
	_, broker := c.sdk()
	resp, err := DoSDK(ctx, broker, CallSpec{
		GatewayID:  broker.gatewayID,
		API:        "event.v1.connection.get",
		Class:      CallClassMetaHTTP,
		Priority:   CallPriorityInteractive,
		Retry:      RetrySafe,
		Permission: PermissionFailFast,
	}, func(callCtx context.Context, sdkClient *lark.Client) (*larkevent.GetConnectionResp, error) {
		return sdkClient.Event.V1.Connection.Get(callCtx)
	})
	if err != nil {
		return LongConnectionStatus{}, err
	}
	if resp == nil {
		return LongConnectionStatus{}, fmt.Errorf("event.v1.connection.get returned nil response")
	}
	if !resp.Success() {
		return LongConnectionStatus{}, newAPIError("event.v1.connection.get", resp.ApiResp, resp.CodeError)
	}
	var count int
	if resp.Data != nil && resp.Data.OnlineInstanceCnt != nil {
		count = *resp.Data.OnlineInstanceCnt
	}
	return LongConnectionStatus{
		OnlineInstanceCount: count,
		CheckedAt:           time.Now().UTC(),
	}, nil
}

func GetBotInfo(ctx context.Context, cfg LiveGatewayConfig) (BotInfo, error) {
	return NewSetupClient(SetupClientConfigFromLiveGatewayConfig(cfg)).GetBotInfo(ctx)
}

func (c *SetupClient) GetBotInfo(ctx context.Context) (BotInfo, error) {
	_, broker := c.sdk()
	apiResp, err := DoSDK(ctx, broker, CallSpec{
		GatewayID:  broker.gatewayID,
		API:        "bot.v3.info",
		Class:      CallClassMetaHTTP,
		Priority:   CallPriorityInteractive,
		Retry:      RetrySafe,
		Permission: PermissionFailFast,
	}, func(callCtx context.Context, sdkClient *lark.Client) (*larkcore.ApiResp, error) {
		return sdkClient.Get(callCtx, "/open-apis/bot/v3/info", nil, larkcore.AccessTokenTypeTenant)
	})
	if err != nil {
		return BotInfo{}, err
	}
	if apiResp == nil {
		return BotInfo{}, fmt.Errorf("bot.v3.info returned nil response")
	}
	var resp botInfoHTTPResponse
	if err := json.Unmarshal(apiResp.RawBody, &resp); err != nil {
		return BotInfo{}, err
	}
	if resp.Code != 0 {
		return BotInfo{}, newAPIError("bot.v3.info", apiResp, larkcore.CodeError{Code: resp.Code, Msg: resp.Msg})
	}
	return BotInfo{
		AppName: strings.TrimSpace(resp.Bot.AppName),
		OpenID:  strings.TrimSpace(resp.Bot.OpenID),
	}, nil
}
