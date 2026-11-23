package platformingress

import "time"

type MenuInteractionEnvelope struct {
	GatewayRef string    `json:"gateway_ref"`
	AppID      string    `json:"app_id"`
	EventID    string    `json:"event_id"`
	Tenant     string    `json:"tenant"`
	SenderIDs  SenderIDs `json:"sender_ids"`
	EventKey   string    `json:"event_key"`
	ClickedAt  time.Time `json:"clicked_at,omitempty"`
	ReceivedAt time.Time `json:"received_at"`
}
