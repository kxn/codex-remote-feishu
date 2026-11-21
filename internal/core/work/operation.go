package work

import (
	"fmt"
	"time"
)

type OperationState string

const (
	OperationPrepared  OperationState = "prepared"
	OperationSending   OperationState = "sending"
	OperationConfirmed OperationState = "confirmed"
	OperationRetryable OperationState = "retryable"
	OperationTerminal  OperationState = "terminal"
	OperationUnknown   OperationState = "unknown"
)

type Operation struct {
	RequestRef       string          `json:"request_ref,omitempty"`
	ID               string          `json:"id"`
	ScopeID          string          `json:"scope_id"`
	Actor            Actor           `json:"actor"`
	ContextID        string          `json:"context_id,omitempty"`
	Kind             string          `json:"kind"`
	TargetID         string          `json:"target_id"`
	TargetGeneration int64           `json:"target_generation"`
	IdempotencyKey   string          `json:"idempotency_key"`
	RequestHash      string          `json:"request_hash"`
	State            OperationState  `json:"state"`
	Steps            []OperationStep `json:"steps"`
	ExternalReceipt  string          `json:"external_receipt,omitempty"`
	LastError        string          `json:"last_error,omitempty"`
	NextAttemptAt    *time.Time      `json:"next_attempt_at,omitempty"`
	Revision         int64           `json:"revision"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
}
type OperationStep struct {
	ID      string         `json:"id"`
	State   OperationState `json:"state"`
	Receipt string         `json:"receipt,omitempty"`
}

func (v Operation) Validate() error {
	if err := required(v.ID, v.ScopeID, v.Kind, v.TargetID, v.IdempotencyKey, v.RequestHash); err != nil {
		return err
	}
	if err := v.Actor.Validate(); err != nil {
		return err
	}
	if err := positive("target generation", v.TargetGeneration); err != nil {
		return err
	}
	if err := positive("operation revision", v.Revision); err != nil {
		return err
	}
	if err := ValidateOperationTransition(v.State, v.State); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, step := range v.Steps {
		if err := required(step.ID); err != nil {
			return err
		}
		if seen[step.ID] {
			return fmt.Errorf("duplicate operation step")
		}
		seen[step.ID] = true
		if err := ValidateOperationTransition(step.State, step.State); err != nil {
			return err
		}
		if step.State == OperationConfirmed && step.Receipt == "" {
			return fmt.Errorf("confirmed step needs receipt")
		}
	}
	if v.State == OperationConfirmed && v.ExternalReceipt == "" {
		return fmt.Errorf("confirmed operation requires external receipt")
	}
	return nil
}

// ValidateReplay permits replay only for exactly the same actor, scope,
// operation kind and request hash. It does not retry an unknown remote effect.
func ValidateReplay(existing, incoming Operation) error {
	if existing.Actor != incoming.Actor || existing.ScopeID != incoming.ScopeID || existing.Kind != incoming.Kind || existing.IdempotencyKey != incoming.IdempotencyKey {
		return fmt.Errorf("idempotency namespace mismatch")
	}
	if existing.RequestRef != incoming.RequestRef || existing.RequestHash != incoming.RequestHash || existing.TargetID != incoming.TargetID || existing.TargetGeneration != incoming.TargetGeneration {
		return fmt.Errorf("idempotency key reused for different request")
	}
	return nil
}

type OutboxState string

const (
	OutboxPending   OutboxState = "pending"
	OutboxSending   OutboxState = "sending"
	OutboxSent      OutboxState = "sent"
	OutboxRetryable OutboxState = "retryable"
	OutboxUnknown   OutboxState = "unknown"
	OutboxTerminal  OutboxState = "terminal"
)

type Outbox struct {
	ID                string      `json:"id"`
	ScopeID           string      `json:"scope_id"`
	OperationID       string      `json:"operation_id"`
	Target            string      `json:"target"`
	EventID           string      `json:"event_id"`
	PayloadRef        string      `json:"payload_ref"`
	State             OutboxState `json:"state"`
	ExternalMessageID string      `json:"external_message_id,omitempty"`
	NextAttemptAt     *time.Time  `json:"next_attempt_at,omitempty"`
	Revision          int64       `json:"revision"`
}

func (v Outbox) Validate() error {
	if err := required(v.ID, v.ScopeID, v.OperationID, v.Target, v.EventID, v.PayloadRef); err != nil {
		return err
	}
	if !oneOf(v.State, OutboxPending, OutboxSending, OutboxSent, OutboxRetryable, OutboxUnknown, OutboxTerminal) {
		return fmt.Errorf("invalid outbox state")
	}
	if v.State == OutboxSent && v.ExternalMessageID == "" {
		return fmt.Errorf("sent outbox requires receipt")
	}
	return positive("outbox revision", v.Revision)
}
