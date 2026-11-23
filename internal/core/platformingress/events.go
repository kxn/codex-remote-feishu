package platformingress

import "time"

// MenuIngressEvent contains only verified SDK facts. A menu click carries no
// chat or message ID; downstream must resolve a destination with this identity.
type MenuIngressEvent struct {
	GatewayRef string    `json:"gateway_ref"`
	AppID      string    `json:"app_id"`
	EventID    string    `json:"event_id"`
	Tenant     string    `json:"tenant"`
	SenderIDs  SenderIDs `json:"sender_ids"`
	EventKey   string    `json:"event_key"`
	ClickedAt  time.Time `json:"clicked_at,omitempty"`
	ReceivedAt time.Time `json:"received_at"`
}

// PlatformIngressEvent is an observation. Recall events do not identify an
// operator; reactions may belong to apps; bot-added is not proof all bots joined.
type PlatformIngressEvent struct {
	GatewayRef     string    `json:"gateway_ref"`
	AppID          string    `json:"app_id"`
	EventID        string    `json:"event_id"`
	Tenant         string    `json:"tenant"`
	Kind           string    `json:"kind"`
	ChatID         string    `json:"chat_id,omitempty"`
	MessageID      string    `json:"message_id,omitempty"`
	OperatorType   string    `json:"operator_type,omitempty"`
	OperatorIDs    SenderIDs `json:"operator_ids,omitempty"`
	OperatorAppID  string    `json:"operator_app_id,omitempty"`
	OperatorTenant string    `json:"operator_tenant,omitempty"`
	Emoji          string    `json:"emoji,omitempty"`
	RecallType     string    `json:"recall_type,omitempty"`
	External       *bool     `json:"external,omitempty"`
	OccurredAt     time.Time `json:"occurred_at,omitempty"`
	ReceivedAt     time.Time `json:"received_at"`
}
