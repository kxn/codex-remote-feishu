package feishu

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

const (
	defaultChatAdminCacheTTL = 30 * time.Second

	ChatAdminDecisionOwner               = "chat_owner"
	ChatAdminDecisionUserManager         = "chat_user_manager"
	ChatAdminDecisionNotAdmin            = "actor_not_chat_admin"
	ChatAdminDecisionMissingActor        = "missing_actor"
	ChatAdminDecisionMissingChat         = "missing_chat"
	ChatAdminDecisionChatInfoUnavailable = "chat_info_unavailable"
)

type ChatUserIdentity struct {
	ID     string
	IDType string
}

type ChatAdminDecision struct {
	Allowed bool
	Reason  string
}

type ChatInfo struct {
	ChatID              string
	OwnerID             string
	UserManagerIDList   []string
	BotManagerIDList    []string
	RequestedUserIDType string
}

type ChatAdminChecker struct {
	setup *SetupClient
	ttl   time.Duration
	now   func() time.Time

	mu    sync.Mutex
	cache map[chatInfoCacheKey]chatInfoCacheEntry
}

type chatInfoCacheKey struct {
	ChatID     string
	UserIDType string
}

type chatInfoCacheEntry struct {
	info      ChatInfo
	expiresAt time.Time
}

func NewChatAdminChecker(config SetupClientConfig, ttl time.Duration) *ChatAdminChecker {
	if ttl <= 0 {
		ttl = defaultChatAdminCacheTTL
	}
	return &ChatAdminChecker{
		setup: NewSetupClient(config),
		ttl:   ttl,
		now:   func() time.Time { return time.Now().UTC() },
		cache: map[chatInfoCacheKey]chatInfoCacheEntry{},
	}
}

func NewChatAdminCheckerFromLiveGatewayConfig(config LiveGatewayConfig, ttl time.Duration) *ChatAdminChecker {
	return NewChatAdminChecker(SetupClientConfigFromLiveGatewayConfig(config), ttl)
}

func (c *ChatAdminChecker) GetChatInfo(ctx context.Context, chatID, userIDType string) (ChatInfo, error) {
	chatID = strings.TrimSpace(chatID)
	userIDType = normalizeChatAdminUserIDType(userIDType)
	if chatID == "" {
		return ChatInfo{}, fmt.Errorf("chat info unavailable: missing chat id")
	}
	key := chatInfoCacheKey{ChatID: chatID, UserIDType: userIDType}
	if info, ok := c.cachedChatInfo(key); ok {
		return info, nil
	}
	info, err := c.fetchChatInfo(ctx, chatID, userIDType)
	if err != nil {
		return ChatInfo{}, err
	}
	c.storeChatInfo(key, info)
	return info, nil
}

func (c *ChatAdminChecker) IsUserChatOwnerOrManager(ctx context.Context, chatID string, actor ChatUserIdentity) (ChatAdminDecision, error) {
	chatID = strings.TrimSpace(chatID)
	actor.ID = strings.TrimSpace(actor.ID)
	actor.IDType = normalizeChatAdminUserIDType(actor.IDType)
	if chatID == "" {
		return ChatAdminDecision{Reason: ChatAdminDecisionMissingChat}, nil
	}
	if actor.ID == "" {
		return ChatAdminDecision{Reason: ChatAdminDecisionMissingActor}, nil
	}
	info, err := c.GetChatInfo(ctx, chatID, actor.IDType)
	if err != nil {
		return ChatAdminDecision{Reason: ChatAdminDecisionChatInfoUnavailable}, err
	}
	if strings.TrimSpace(info.OwnerID) == actor.ID {
		return ChatAdminDecision{Allowed: true, Reason: ChatAdminDecisionOwner}, nil
	}
	if stringSliceContains(info.UserManagerIDList, actor.ID) {
		return ChatAdminDecision{Allowed: true, Reason: ChatAdminDecisionUserManager}, nil
	}
	return ChatAdminDecision{Reason: ChatAdminDecisionNotAdmin}, nil
}

func (c *ChatAdminChecker) cachedChatInfo(key chatInfoCacheKey) (ChatInfo, bool) {
	if c == nil {
		return ChatInfo{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.cache[key]
	if !ok || !c.now().Before(entry.expiresAt) {
		return ChatInfo{}, false
	}
	return entry.info, true
}

func (c *ChatAdminChecker) storeChatInfo(key chatInfoCacheKey, info ChatInfo) {
	if c == nil || c.ttl <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cache == nil {
		c.cache = map[chatInfoCacheKey]chatInfoCacheEntry{}
	}
	c.cache[key] = chatInfoCacheEntry{
		info:      info,
		expiresAt: c.now().Add(c.ttl),
	}
}

func (c *ChatAdminChecker) fetchChatInfo(ctx context.Context, chatID, userIDType string) (ChatInfo, error) {
	if c == nil || c.setup == nil {
		return ChatInfo{}, fmt.Errorf("chat info unavailable: checker not configured")
	}
	_, broker := c.setup.sdk()
	resp, err := DoSDK(ctx, broker, CallSpec{
		GatewayID:   broker.gatewayID,
		API:         "im.v1.chat.get",
		Class:       CallClassIMRead,
		Priority:    CallPriorityInteractive,
		ResourceKey: FeishuResourceKey{ReceiveTarget: chatID},
		Retry:       RetrySafe,
		Permission:  PermissionFailFast,
	}, func(callCtx context.Context, sdkClient *lark.Client) (*larkim.GetChatResp, error) {
		req := larkim.NewGetChatReqBuilder().
			ChatId(chatID).
			UserIdType(userIDType).
			Build()
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
	return chatInfoFromSDKResp(resp, chatID, userIDType), nil
}

func chatInfoFromSDKResp(resp *larkim.GetChatResp, fallbackChatID, userIDType string) ChatInfo {
	info := ChatInfo{
		RequestedUserIDType: userIDType,
	}
	if resp != nil && resp.Data != nil {
		info.OwnerID = strings.TrimSpace(stringValue(resp.Data.OwnerId))
		info.UserManagerIDList = trimStringSlice(resp.Data.UserManagerIdList)
		info.BotManagerIDList = trimStringSlice(resp.Data.BotManagerIdList)
	}
	if info.ChatID == "" {
		info.ChatID = strings.TrimSpace(fallbackChatID)
	}
	return info
}

func normalizeChatAdminUserIDType(value string) string {
	switch strings.TrimSpace(value) {
	case "user_id", "union_id":
		return strings.TrimSpace(value)
	default:
		return "open_id"
	}
}

func trimStringSlice(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

func stringSliceContains(values []string, target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
}
