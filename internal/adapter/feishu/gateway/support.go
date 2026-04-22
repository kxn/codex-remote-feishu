package gateway

import (
	"context"
	"strings"
	"time"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

const (
	PlatformFeishu             = "feishu"
	ScopeKindUser              = "user"
	ScopeKindChat              = "chat"
	inboundMessageParseTimeout = 30 * time.Second
)

type SurfaceRef struct {
	Platform  string
	GatewayID string
	ScopeKind string
	ScopeID   string
}

type feishuTextContent struct {
	Text string `json:"text"`
}

type feishuPostContent struct {
	Title   string             `json:"title"`
	Content [][]feishuPostNode `json:"content"`
}

type feishuLocalizedPostContent struct {
	ZhCN feishuPostContent `json:"zh_cn"`
}

type feishuPostNode struct {
	Tag       string `json:"tag"`
	Text      string `json:"text"`
	Href      string `json:"href"`
	UserID    string `json:"user_id"`
	UserName  string `json:"user_name"`
	ImageKey  string `json:"image_key"`
	EmojiType string `json:"emoji_type"`
	Language  string `json:"language"`
}

func (r SurfaceRef) SurfaceID() string {
	if !r.valid() {
		return ""
	}
	return strings.Join([]string{
		PlatformFeishu,
		normalizeGatewayID(r.GatewayID),
		r.ScopeKind,
		r.ScopeID,
	}, ":")
}

func (r SurfaceRef) valid() bool {
	if strings.TrimSpace(r.Platform) != PlatformFeishu {
		return false
	}
	if strings.TrimSpace(r.GatewayID) == "" {
		return false
	}
	if strings.TrimSpace(r.ScopeID) == "" {
		return false
	}
	switch strings.TrimSpace(r.ScopeKind) {
	case ScopeKindUser, ScopeKindChat:
		return true
	default:
		return false
	}
}

func normalizeGatewayID(gatewayID string) string {
	return strings.TrimSpace(gatewayID)
}

func newFeishuTimeoutContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	base := context.Background()
	if parent != nil {
		base = parent
	}
	if timeout <= 0 {
		return context.WithCancel(base)
	}
	return context.WithTimeout(base, timeout)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func referencedMessageID(message *larkim.EventMessage) string {
	if message == nil {
		return ""
	}
	targetMessageID := strings.TrimSpace(stringPtr(message.ParentId))
	if targetMessageID == "" {
		targetMessageID = strings.TrimSpace(stringPtr(message.RootId))
	}
	return targetMessageID
}
