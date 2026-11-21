package workstore

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"slices"
	"time"
)

type SelectionNotice struct {
	Card              json.RawMessage     `json:"card,omitempty"`
	IngressID         string              `json:"ingress_id"`
	Gateway           work.IngressGateway `json:"gateway"`
	Tenant            string              `json:"tenant"`
	ChatID            string              `json:"chat_id"`
	UUID              string              `json:"uuid"`
	Text              string              `json:"text"`
	CandidateRoleIDs  []string            `json:"candidate_role_ids"`
	State             work.OperationState `json:"state"`
	ExternalMessageID string              `json:"external_message_id,omitempty"`
	LastError         string              `json:"last_error,omitempty"`
	Observation       json.RawMessage     `json:"observation,omitempty"`
	Revision          int64               `json:"revision"`
	CreatedAt         time.Time           `json:"created_at"`
	UpdatedAt         time.Time           `json:"updated_at"`
}
type BeginSelectionNoticeRequest struct {
	Card             json.RawMessage
	IngressID        string
	Gateway          work.IngressGateway
	Text             string
	CandidateRoleIDs []string
	Now              time.Time
}
type SelectionNoticeOutcome struct {
	State             work.OperationState
	ExternalMessageID string
	ChatID            string
	LastError         string
	Now               time.Time
}

func (t *Tx) GetSelectionNotice(id string) (SelectionNotice, error) {
	var notice SelectionNotice
	var payload string
	err := t.tx.QueryRowContext(t.ctx, "SELECT payload FROM ingress_selection_notices WHERE ingress_id=?", id).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return notice, ErrNotFound
	}
	if err != nil {
		return notice, err
	}
	err = json.Unmarshal([]byte(payload), &notice)
	return notice, err
}
func (t *Tx) TryBeginSelectionNotice(input BeginSelectionNoticeRequest) (notice SelectionNotice, send bool, err error) {
	err = t.atomic(func() error {
		record, e := t.GetIngress(input.IngressID)
		if e != nil {
			return e
		}
		if record.Envelope.ChatType != "group" || record.Envelope.SenderType != "user" || record.Route.Kind != work.ScopeBootstrap || record.State != work.IngressPending {
			return fmt.Errorf("selection notice requires unresolved real human group establishment")
		}
		if record.Gateway != input.Gateway || input.Text == "" || input.Now.IsZero() {
			return fmt.Errorf("selection notice identity or body invalid")
		}
		old, e := t.GetSelectionNotice(input.IngressID)
		if e == nil {
			if !bytes.Equal(old.Card, input.Card) || old.Gateway != input.Gateway || old.Text != input.Text || !slices.Equal(old.CandidateRoleIDs, input.CandidateRoleIDs) {
				return ErrIdempotencyConflict
			}
			notice = old
			return nil
		}
		if !errors.Is(e, ErrNotFound) {
			return e
		}
		binding, e := t.GetRoleBotBinding(input.Gateway.RoleID)
		if e != nil {
			return e
		}
		if binding.AppID != input.Gateway.AppID || binding.GatewayRef != input.Gateway.GatewayRef || binding.IdentityGeneration != input.Gateway.BotGeneration {
			return fmt.Errorf("selection notice gateway identity changed")
		}
		sum := sha256.Sum256([]byte("selection:" + record.ID))
		if len(input.Card) > 28*1024 || len(input.Card) > 0 && !json.Valid(input.Card) {
			return fmt.Errorf("invalid selection card")
		}
		notice = SelectionNotice{Card: append(json.RawMessage(nil), input.Card...), IngressID: record.ID, Gateway: input.Gateway, Tenant: record.Envelope.Tenant, ChatID: record.Envelope.ChatID, UUID: hex.EncodeToString(sum[:20]), Text: input.Text, CandidateRoleIDs: append([]string{}, input.CandidateRoleIDs...), State: work.OperationSending, Revision: 1, CreatedAt: input.Now, UpdatedAt: input.Now}
		_, e = t.tx.ExecContext(t.ctx, "INSERT INTO ingress_selection_notices(ingress_id,revision,payload) VALUES(?,?,?)", notice.IngressID, notice.Revision, mustJSON(notice))
		send = e == nil
		return e
	})
	return
}
func (t *Tx) FinishSelectionNotice(id string, expected int64, outcome SelectionNoticeOutcome) error {
	notice, err := t.GetSelectionNotice(id)
	if err != nil {
		return err
	}
	if notice.Revision != expected || notice.State != work.OperationSending {
		return ErrConflict
	}
	if outcome.State != work.OperationConfirmed && outcome.State != work.OperationUnknown {
		return fmt.Errorf("selection result must confirm delivery or preserve uncertainty")
	}
	if outcome.Now.IsZero() {
		return fmt.Errorf("selection result time required")
	}
	if outcome.State == work.OperationConfirmed && (outcome.ExternalMessageID == "" || outcome.ChatID != notice.ChatID) {
		return fmt.Errorf("selection receipt does not match original group")
	}
	return t.finishSelectionNotice(notice, outcome)
}

// ObserveSelectionNotice is used only after the bound gateway read the exact
// message and verified the original card and callback. No CLI calls it.
func (t *Tx) ObserveSelectionNotice(id string, expected int64, outcome SelectionNoticeOutcome, observation json.RawMessage) error {
	notice, err := t.GetSelectionNotice(id)
	if err != nil {
		return err
	}
	if notice.State == work.OperationConfirmed && notice.ExternalMessageID == outcome.ExternalMessageID && notice.ChatID == outcome.ChatID {
		return nil
	}
	if notice.Revision != expected || notice.State != work.OperationUnknown && notice.State != work.OperationSending {
		return ErrConflict
	}
	if outcome.State != work.OperationConfirmed || outcome.ExternalMessageID == "" || outcome.ChatID != notice.ChatID || outcome.Now.IsZero() ||
		notice.ExternalMessageID != "" && notice.ExternalMessageID != outcome.ExternalMessageID || !json.Valid(observation) {
		return ErrScopeDenied
	}
	notice.Observation = observation
	return t.finishSelectionNotice(notice, outcome)
}

func (t *Tx) finishSelectionNotice(notice SelectionNotice, outcome SelectionNoticeOutcome) error {
	expected := notice.Revision
	notice.State = outcome.State
	notice.ExternalMessageID = outcome.ExternalMessageID
	notice.LastError = outcome.LastError
	notice.Revision++
	notice.UpdatedAt = outcome.Now
	result, err := t.tx.ExecContext(t.ctx, "UPDATE ingress_selection_notices SET revision=?,payload=? WHERE ingress_id=? AND revision=?", notice.Revision, mustJSON(notice), notice.IngressID, expected)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrConflict
	}
	return nil
}
