package feishu

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	gatewaypkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/gateway"
	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

type ChatInfo struct {
	BotCount int
	ChatMode string
}

type ChatInfoRequest struct {
	GatewayID        string
	SurfaceSessionID string
	ChatID           string
}

type ChatInfoReader interface {
	ReadChatInfo(context.Context, ChatInfoRequest) (ChatInfo, error)
}

func (g *LiveGateway) ReadChatInfo(ctx context.Context, req ChatInfoRequest) (ChatInfo, error) {
	if g == nil {
		return ChatInfo{}, fmt.Errorf("im.v1.chat.get failed: gateway not configured")
	}
	gatewayID := gatewaypkg.NormalizeGatewayID(req.GatewayID)
	if gatewayID != "" && gatewayID != g.config.GatewayID {
		return ChatInfo{}, fmt.Errorf("im.v1.chat.get failed: gateway mismatch: request=%s gateway=%s", gatewayID, g.config.GatewayID)
	}
	return g.GetChatInfo(ctx, req.ChatID)
}

func (g *LiveGateway) GetChatInfo(ctx context.Context, chatID string) (ChatInfo, error) {
	if g == nil {
		return ChatInfo{}, fmt.Errorf("im.v1.chat.get failed: gateway not configured")
	}
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return ChatInfo{}, fmt.Errorf("im.v1.chat.get failed: missing chat id")
	}
	resp, err := DoSDK(ctx, g.broker, CallSpec{
		GatewayID:  g.config.GatewayID,
		API:        "im.v1.chat.get",
		Class:      CallClassIMRead,
		Priority:   CallPriorityReadAssist,
		Retry:      RetrySafe,
		Permission: PermissionFailFast,
	}, func(callCtx context.Context, sdkClient *lark.Client) (*larkim.GetChatResp, error) {
		req := larkim.NewGetChatReqBuilder().ChatId(chatID).Build()
		return sdkClient.Im.V1.Chat.Get(callCtx, req)
	})
	if err != nil {
		return ChatInfo{}, err
	}
	if resp == nil {
		return ChatInfo{}, fmt.Errorf("im.v1.chat.get returned nil response")
	}
	if !resp.Success() {
		return ChatInfo{}, newAPIError("im.v1.chat.get", resp.ApiResp, resp.CodeError)
	}
	if resp.Data == nil {
		return ChatInfo{}, fmt.Errorf("im.v1.chat.get returned nil data")
	}
	rawBotCount := strings.TrimSpace(scopeStringValue(resp.Data.BotCount))
	if rawBotCount == "" {
		return ChatInfo{}, fmt.Errorf("im.v1.chat.get returned empty bot_count")
	}
	botCount, err := strconv.Atoi(rawBotCount)
	if err != nil {
		return ChatInfo{}, fmt.Errorf("im.v1.chat.get returned invalid bot_count %q: %w", rawBotCount, err)
	}
	return ChatInfo{
		BotCount: botCount,
		ChatMode: strings.TrimSpace(scopeStringValue(resp.Data.ChatMode)),
	}, nil
}
