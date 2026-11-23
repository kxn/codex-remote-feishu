package platformingress

import "time"

type HumanInteractionEnvelope struct {
	GatewayRef string         `json:"gateway_ref"`
	AppID      string         `json:"app_id"`
	EventID    string         `json:"event_id"`
	Tenant     string         `json:"tenant"`
	SenderIDs  SenderIDs      `json:"sender_ids"`
	ChatID     string         `json:"chat_id"`
	MessageID  string         `json:"message_id"`
	Kind       string         `json:"kind"`
	Values     map[string]any `json:"values,omitempty"`
	Form       map[string]any `json:"form,omitempty"`
	Option     string         `json:"option,omitempty"`
	Options    []string       `json:"options,omitempty"`
	Input      string         `json:"input,omitempty"`
	ReceivedAt time.Time      `json:"received_at"`
}
