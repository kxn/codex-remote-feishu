package privatechat

import (
	"context"
	"encoding/json"
)

// CardResponse is the exact platform response and its authenticated card target.
// It is only available to a caller after the input transaction has committed.
type CardResponse struct {
	Version         int             `json:"version"`
	TargetMessageID string          `json:"target_message_id"`
	ViewToken       string          `json:"view_token,omitempty"`
	Response        json.RawMessage `json:"response"`
}

func validateCardResponse(c CardResponse) error {
	if c.Version != 1 || c.TargetMessageID == "" || !json.Valid(c.Response) || string(c.Response) == "null" {
		return ErrJournalState
	}
	return nil
}

// ReadCommittedCardResponse returns only the exact response committed with the
// authenticated ingress. No platform delivery acknowledgement is assumed.
func (p *Processor) ReadCommittedCardResponse(ctx context.Context, o Owner, generation, seq int64) (CardResponse, error) {
	var c CardResponse
	if !validOwner(o) || generation < 1 || seq < 1 {
		return c, ErrJournalState
	}
	var body []byte
	err := p.journal.db.QueryRowContext(ctx, `SELECT r.body FROM private_callback_response r JOIN ingress i ON i.seq=r.seq WHERE i.seq=? AND i.tenant=? AND i.app_id=? AND i.gateway=? AND i.surface=? AND i.chat=? AND i.user_id=? AND i.generation=?`, seq, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation).Scan(&body)
	if err != nil {
		return c, err
	}
	if err = json.Unmarshal(body, &c); err != nil {
		return c, err
	}
	return c, validateCardResponse(c)
}
