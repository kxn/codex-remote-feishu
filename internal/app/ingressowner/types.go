// Package ingressowner freezes the application owner of authenticated platform
// events. Application admission and external effects remain application-owned.
package ingressowner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrConflict = errors.New("ingress ownership conflict")
var ErrUnresolved = errors.New("ingress ownership unresolved")
var ErrInvalid = errors.New("invalid ingress identity")

type Application string

const (
	Private Application = "private"
	Team    Application = "team"
)

type Owner struct {
	Application Application `json:"application"`
	ID          string      `json:"id"`
	Epoch       int64       `json:"epoch"`
}

func (o Owner) valid() bool {
	return (o.Application == Private || o.Application == Team) && strings.TrimSpace(o.ID) != "" && o.Epoch > 0
}

type Key struct {
	Tenant  string `json:"tenant"`
	AppID   string `json:"app_id"`
	EventID string `json:"event_id"`
}
type Source struct {
	GatewayID         string    `json:"gateway_id"`
	BindingGeneration int64     `json:"binding_generation"`
	ReceivedAt        time.Time `json:"received_at"`
}
type Operator struct {
	IDType string `json:"id_type"`
	ID     string `json:"id"`
}
type Kind string

const (
	Message     Kind = "message"
	Menu        Kind = "menu"
	Card        Kind = "card"
	Reaction    Kind = "reaction"
	Recall      Kind = "recall"
	GroupMember Kind = "group_member"
)

// Facts are verified SDK facts. Payload is deterministic platform data and must
// exclude receive time, local configuration and gateway binding generations.
type Event struct {
	Key       Key             `json:"key"`
	Source    Source          `json:"source"`
	Kind      Kind            `json:"kind"`
	Payload   json.RawMessage `json:"payload"`
	ChatType  string          `json:"chat_type,omitempty"`
	ChatID    string          `json:"chat_id,omitempty"`
	MessageID string          `json:"message_id,omitempty"`
	CardRef   string          `json:"card_ref,omitempty"`
	CardKind  string          `json:"card_kind,omitempty"`
	Operator  Operator        `json:"operator"`
}

func (e Event) validate() error {
	if strings.TrimSpace(e.Key.Tenant) == "" || strings.TrimSpace(e.Key.AppID) == "" || strings.TrimSpace(e.Key.EventID) == "" || strings.TrimSpace(e.Source.GatewayID) == "" || e.Source.BindingGeneration < 1 || e.Source.ReceivedAt.IsZero() || !json.Valid(e.Payload) || string(e.Payload) == "null" {
		return ErrInvalid
	}
	switch e.Kind {
	case Message:
		if (e.ChatType != "p2p" && e.ChatType != "group") || e.ChatID == "" || e.MessageID == "" || e.Operator.IDType == "" || e.Operator.ID == "" {
			return ErrInvalid
		}
	case Menu:
		if e.Operator.IDType == "" || e.Operator.ID == "" || e.ChatID != "" || e.ChatType != "" {
			return ErrInvalid
		}
	case Card, Reaction, Recall:
		if e.MessageID == "" || e.ChatType != "" || (e.Kind != Recall && (e.Operator.IDType == "" || e.Operator.ID == "")) {
			return ErrInvalid
		}
	case GroupMember:
		if e.ChatID == "" || e.ChatType != "" {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

type PlatformMessage struct {
	Tenant            string `json:"tenant"`
	AppID             string `json:"app_id"`
	GatewayID         string `json:"gateway_id"`
	BindingGeneration int64  `json:"binding_generation"`
	ChatID            string `json:"chat_id"`
	MessageID         string `json:"message_id"`
	Owner             Owner  `json:"owner"`
	Version           int64  `json:"version"`
	Lifecycle         string `json:"lifecycle"`
	CardRef           string `json:"card_ref,omitempty"`
	CardKind          string `json:"card_kind,omitempty"`
}

func (m PlatformMessage) validate() error {
	if m.Tenant == "" || m.AppID == "" || m.GatewayID == "" || m.BindingGeneration < 1 || m.ChatID == "" || m.MessageID == "" || !m.Owner.valid() || m.Version < 1 || m.Lifecycle == "" {
		return ErrInvalid
	}
	if (m.CardRef == "") != (m.CardKind == "") || m.CardKind != "" && !(((m.CardKind == "team_view" || m.CardKind == "team_setup_ingress") && m.Owner.Application == Team) || (m.CardKind == "private_view" && m.Owner.Application == Private)) {
		return ErrInvalid
	}
	return nil
}

type State string

const (
	Unresolved State = "unresolved"
	Pending    State = "pending"
	Delivering State = "delivering"
	Accepted   State = "accepted"
	Expired    State = "expired"
	Historical State = "historical"
)

type RouteSnapshot struct {
	Message *PlatformMessage `json:"message,omitempty"`
	Private *PrivateBinding  `json:"private,omitempty"`
}

type Record struct {
	Sequence       int64             `json:"sequence"`
	Event          Event             `json:"event"`
	Route          *RouteSnapshot    `json:"route,omitempty"`
	Owner          *Owner            `json:"owner,omitempty"`
	DeliveryTarget *Owner            `json:"delivery_target,omitempty"`
	State          State             `json:"state"`
	Response       json.RawMessage   `json:"response,omitempty"`
	Receipt        *AdmissionReceipt `json:"receipt,omitempty"`
	Revision       int64             `json:"revision"`
}

// Resolver authenticates identity and selects only a permitted owner. For menu
// it must use an explicit verified P2P binding. For card/observation it receives
// the immutable platform message binding and must validate actor and lifecycle.
// ErrUnresolved retains a durable event without assigning an owner.
type Resolver interface {
	Resolve(context.Context, Event, *PlatformMessage) (Owner, error)
}
type ReceiptVerifier interface {
	VerifyPlatformReceipt(context.Context, PlatformMessage, []byte) error
}

// AdmissionReceipt is returned only after the application has committed its
// event-key deduplicated admission. It is not a platform delivery receipt.
// CallbackReplacement describes a committed synchronous response offered for
// an already confirmed private message. It is not a platform send receipt.
type CallbackReplacement struct {
	TargetMessageID string `json:"target_message_id"`
	Token           string `json:"token"`
	ResponseHash    string `json:"response_hash"`
}

type AdmissionReceipt struct {
	Replacement *CallbackReplacement `json:"replacement,omitempty"`
	Key         Key                  `json:"key"`
	Owner       Owner                `json:"owner"`
	AdmissionID string               `json:"admission_id"`
	PayloadHash string               `json:"payload_hash"`
	Response    json.RawMessage      `json:"response,omitempty"`
}
type Admitter interface {
	Admit(context.Context, Record) (AdmissionReceipt, error)
}

// AdmissionHash freezes the platform facts, first source, resolved owner and
// route, excluding local scheduling state and record revision.
func AdmissionHash(v Record) (string, error) {
	if v.Owner == nil {
		return "", ErrInvalid
	}
	raw, err := json.Marshal(struct {
		Event          Event          `json:"event"`
		Owner          Owner          `json:"owner"`
		Route          *RouteSnapshot `json:"route,omitempty"`
		DeliveryTarget *Owner         `json:"delivery_target,omitempty"`
	}{v.Event, *v.Owner, v.Route, v.DeliveryTarget})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func checkResolved(e Event, o Owner, m *PlatformMessage) error {
	if !o.valid() {
		return ErrInvalid
	}
	if e.Kind == Menu && o.Application != Private {
		return fmt.Errorf("%w: menu must be private", ErrInvalid)
	}
	if e.Kind == GroupMember && o.Application != Team {
		return fmt.Errorf("%w: group membership must be team", ErrInvalid)
	}
	if e.Kind == Message && e.ChatType == "group" && o.Application != Team {
		return fmt.Errorf("%w: chat type and application disagree", ErrInvalid)
	}
	if m != nil && o != m.Owner {
		return ErrConflict
	}
	return nil
}

// PrivateBinding records an independently verified P2P observation or confirmed
// creation, not a menu event. Typed IDs are scoped to the Bot identity.
type PrivateBinding struct {
	Tenant            string   `json:"tenant"`
	AppID             string   `json:"app_id"`
	GatewayID         string   `json:"gateway_id"`
	BindingGeneration int64    `json:"binding_generation"`
	Operator          Operator `json:"operator"`
	ChatID            string   `json:"chat_id"`
	Owner             Owner    `json:"owner"`
}

func (b PrivateBinding) validate() error {
	if b.Tenant == "" || b.AppID == "" || b.GatewayID == "" || b.BindingGeneration < 1 || b.Operator.IDType == "" || b.Operator.ID == "" || b.ChatID == "" || !b.Owner.valid() || b.Owner.Application != Private {
		return ErrInvalid
	}
	return nil
}

type BindingVerifier interface {
	VerifyPrivateBinding(context.Context, PrivateBinding, []byte) error
}
