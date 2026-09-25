package feishuidentity

import "strings"

const (
	PlatformFeishu = "feishu"
	ScopeKindUser  = "user"
	ScopeKindChat  = "chat"
)

type SurfaceRef struct {
	Platform  string
	GatewayID string
	ScopeKind string
	ScopeID   string
}

func ParseSurfaceRef(surfaceID string) (SurfaceRef, bool) {
	parts := strings.Split(strings.TrimSpace(surfaceID), ":")
	if len(parts) != 4 || parts[0] != PlatformFeishu {
		return SurfaceRef{}, false
	}
	ref := SurfaceRef{
		Platform:  parts[0],
		GatewayID: strings.TrimSpace(parts[1]),
		ScopeKind: strings.TrimSpace(parts[2]),
		ScopeID:   strings.TrimSpace(parts[3]),
	}
	if !ref.valid() {
		return SurfaceRef{}, false
	}
	return ref, true
}

func (r SurfaceRef) SurfaceID() string {
	r.Platform = strings.TrimSpace(r.Platform)
	r.GatewayID = strings.TrimSpace(r.GatewayID)
	r.ScopeKind = strings.TrimSpace(r.ScopeKind)
	r.ScopeID = strings.TrimSpace(r.ScopeID)
	if !r.valid() {
		return ""
	}
	return strings.Join([]string{PlatformFeishu, r.GatewayID, r.ScopeKind, r.ScopeID}, ":")
}

func (r SurfaceRef) IsUser() bool {
	return r.valid() && strings.TrimSpace(r.ScopeKind) == ScopeKindUser
}

func (r SurfaceRef) IsChat() bool {
	return r.valid() && strings.TrimSpace(r.ScopeKind) == ScopeKindChat
}

// ChatID returns the room identity shared by all topic surfaces in the chat.
func (r SurfaceRef) ChatID() string {
	if !r.IsChat() {
		return ""
	}
	chatID, _, _ := strings.Cut(strings.TrimSpace(r.ScopeID), "@")
	return chatID
}

// TopicRootID returns the routing key, which may be a root message ID or a thread ID.
func (r SurfaceRef) TopicRootID() string {
	if !r.IsChat() {
		return ""
	}
	_, topicID, _ := strings.Cut(strings.TrimSpace(r.ScopeID), "@")
	return topicID
}

func (r SurfaceRef) valid() bool {
	if strings.TrimSpace(r.Platform) != PlatformFeishu {
		return false
	}
	if strings.TrimSpace(r.GatewayID) == "" || strings.TrimSpace(r.ScopeID) == "" {
		return false
	}
	switch strings.TrimSpace(r.ScopeKind) {
	case ScopeKindUser:
		return true
	case ScopeKindChat:
		chatID, topicID, hasTopic := strings.Cut(strings.TrimSpace(r.ScopeID), "@")
		return chatID != "" && (!hasTopic || (strings.TrimSpace(topicID) != "" && !strings.Contains(topicID, "@")))
	default:
		return false
	}
}
