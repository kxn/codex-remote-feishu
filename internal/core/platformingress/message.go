package platformingress

import (
	"fmt"
	"strings"
	"time"
)

type SenderIDs struct {
	OpenID  string `json:"open_id,omitempty"`
	UserID  string `json:"user_id,omitempty"`
	UnionID string `json:"union_id,omitempty"`
}
type RawMention struct {
	Key  string    `json:"key"`
	IDs  SenderIDs `json:"ids"`
	Type string    `json:"type"`
	Name string    `json:"name,omitempty"`
}

// RawIngressEnvelope is copied from the verified SDK event plus the receiving
// gateway configuration. It deliberately excludes callback tokens/credentials.
type RawIngressEnvelope struct {
	MessageCreatedAt time.Time    `json:"message_created_at,omitempty"`
	GatewayRef       string       `json:"gateway_ref"`
	AppID            string       `json:"app_id"`
	EventID          string       `json:"event_id"`
	Tenant           string       `json:"tenant"`
	SenderTenant     string       `json:"sender_tenant,omitempty"`
	ChatID           string       `json:"chat_id"`
	ChatType         string       `json:"chat_type"`
	MessageID        string       `json:"message_id"`
	RootID           string       `json:"root_id,omitempty"`
	ParentID         string       `json:"parent_id,omitempty"`
	ThreadID         string       `json:"thread_id,omitempty"`
	SenderType       string       `json:"sender_type"`
	SenderIDs        SenderIDs    `json:"sender_ids"`
	MessageType      string       `json:"message_type"`
	RawContent       string       `json:"raw_content"`
	Mentions         []RawMention `json:"mentions,omitempty"`
	ReceivedAt       time.Time    `json:"received_at"`
}

func (v RawIngressEnvelope) Validate() error {
	for _, field := range []string{v.GatewayRef, v.AppID, v.EventID, v.Tenant, v.ChatID, v.MessageID, v.SenderType, v.MessageType} {
		if strings.TrimSpace(field) == "" {
			return fmt.Errorf("required identifier or value is empty")
		}
	}
	if v.ChatType != "p2p" && v.ChatType != "group" {
		return fmt.Errorf("invalid chat type")
	}
	if v.ReceivedAt.IsZero() {
		return fmt.Errorf("ingress receipt time required")
	}
	return nil
}

// MembershipObservedAt dates the platform membership fact, not the delayed
// callback delivery. Replays cannot refresh a later revocation.
func (v RawIngressEnvelope) MembershipObservedAt() time.Time {
	if !v.MessageCreatedAt.IsZero() {
		return v.MessageCreatedAt
	}
	return v.ReceivedAt
}
