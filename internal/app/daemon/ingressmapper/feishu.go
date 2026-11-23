// Package ingressmapper converts verified platform DTOs into neutral durable
// ownership facts. It never resolves an application or calls a store.
package ingressmapper

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/ingressowner"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
)

func operator(ids platformingress.SenderIDs) (ingressowner.Operator, error) {
	switch {
	case ids.OpenID != "":
		return ingressowner.Operator{IDType: "open_id", ID: ids.OpenID}, nil
	case ids.UserID != "":
		return ingressowner.Operator{IDType: "user_id", ID: ids.UserID}, nil
	case ids.UnionID != "":
		return ingressowner.Operator{IDType: "union_id", ID: ids.UnionID}, nil
	default:
		return ingressowner.Operator{}, fmt.Errorf("platform sender has no typed identity")
	}
}
func base(tenant, app, event, gateway string, generation int64, received time.Time, kind ingressowner.Kind, payload any) (ingressowner.Event, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return ingressowner.Event{}, err
	}
	var facts map[string]json.RawMessage
	if err = json.Unmarshal(raw, &facts); err != nil {
		return ingressowner.Event{}, err
	}
	delete(facts, "gateway_ref")
	delete(facts, "received_at")
	raw, err = json.Marshal(facts)
	if err != nil {
		return ingressowner.Event{}, err
	}
	return ingressowner.Event{Key: ingressowner.Key{Tenant: tenant, AppID: app, EventID: event}, Source: ingressowner.Source{GatewayID: gateway, BindingGeneration: generation, ReceivedAt: received}, Kind: kind, Payload: raw}, nil
}

// Message retains platform creation time, sender IDs, original message content
// and mentions, but removes local gateway and receive-time observations.
func Message(raw platformingress.RawIngressEnvelope, generation int64) (ingressowner.Event, error) {
	if err := raw.Validate(); err != nil {
		return ingressowner.Event{}, err
	}
	actor, err := operator(raw.SenderIDs)
	if err != nil {
		return ingressowner.Event{}, err
	}
	stable := raw
	stable.GatewayRef = ""
	stable.ReceivedAt = time.Time{}
	e, err := base(raw.Tenant, raw.AppID, raw.EventID, raw.GatewayRef, generation, raw.ReceivedAt, ingressowner.Message, stable)
	e.ChatType, e.ChatID, e.MessageID, e.Operator = raw.ChatType, raw.ChatID, raw.MessageID, actor
	return e, err
}
func Card(raw platformingress.HumanInteractionEnvelope, generation int64) (ingressowner.Event, error) {
	actor, err := operator(raw.SenderIDs)
	if err != nil {
		return ingressowner.Event{}, err
	}
	stable := raw
	stable.GatewayRef = ""
	stable.ReceivedAt = time.Time{}
	e, err := base(raw.Tenant, raw.AppID, raw.EventID, raw.GatewayRef, generation, raw.ReceivedAt, ingressowner.Card, stable)
	e.ChatID, e.MessageID, e.Operator = raw.ChatID, raw.MessageID, actor
	var cardKind, cardRef string
	for _, key := range []string{"team_view", "private_view", "team_setup_ingress"} {
		if value, ok := raw.Values[key].(string); ok {
			if cardKind != "" {
				return ingressowner.Event{}, fmt.Errorf("ambiguous card ownership tokens")
			}
			cardKind, cardRef = key, value
		}
	}
	e.CardKind, e.CardRef = cardKind, cardRef
	return e, err
}
func Menu(raw platformingress.MenuIngressEvent, generation int64) (ingressowner.Event, error) {
	actor, err := operator(raw.SenderIDs)
	if err != nil {
		return ingressowner.Event{}, err
	}
	stable := raw
	stable.GatewayRef = ""
	stable.ReceivedAt = time.Time{}
	e, err := base(raw.Tenant, raw.AppID, raw.EventID, raw.GatewayRef, generation, raw.ReceivedAt, ingressowner.Menu, stable)
	e.Operator = actor
	return e, err
}
func Observation(raw platformingress.PlatformIngressEvent, generation int64) (ingressowner.Event, error) {
	var kind ingressowner.Kind
	switch raw.Kind {
	case "reaction.created":
		kind = ingressowner.Reaction
	case "message.recalled":
		kind = ingressowner.Recall
	case "bot.added", "bot.removed", "chat.deleted":
		kind = ingressowner.GroupMember
	default:
		return ingressowner.Event{}, fmt.Errorf("unsupported platform observation")
	}
	var actor ingressowner.Operator
	var err error
	if raw.OperatorType == "user" {
		actor, err = operator(raw.OperatorIDs)
	} else if raw.OperatorType == "app" && raw.OperatorAppID != "" {
		actor = ingressowner.Operator{IDType: "app_id", ID: raw.OperatorAppID}
	}
	if err != nil {
		return ingressowner.Event{}, err
	}
	stable := raw
	stable.GatewayRef = ""
	stable.ReceivedAt = time.Time{}
	e, err := base(raw.Tenant, raw.AppID, raw.EventID, raw.GatewayRef, generation, raw.ReceivedAt, kind, stable)
	e.ChatID, e.MessageID, e.Operator = raw.ChatID, raw.MessageID, actor
	return e, err
}

// Restore functions reconstruct the first authenticated DTO, including local
// receive time and gateway source. They reject inconsistent stored envelopes.
func RestoreMessage(e ingressowner.Event) (platformingress.RawIngressEnvelope, error) {
	var raw platformingress.RawIngressEnvelope
	if e.Kind != ingressowner.Message {
		return raw, fmt.Errorf("wrong event kind")
	}
	if err := json.Unmarshal(e.Payload, &raw); err != nil {
		return raw, err
	}
	raw.GatewayRef = e.Source.GatewayID
	raw.ReceivedAt = e.Source.ReceivedAt
	check, err := Message(raw, e.Source.BindingGeneration)
	if err != nil {
		return raw, err
	}
	if !sameFacts(check, e) {
		return raw, fmt.Errorf("stored message facts mismatch")
	}
	return raw, nil
}
func RestoreCard(e ingressowner.Event) (platformingress.HumanInteractionEnvelope, error) {
	var raw platformingress.HumanInteractionEnvelope
	if e.Kind != ingressowner.Card {
		return raw, fmt.Errorf("wrong event kind")
	}
	if err := json.Unmarshal(e.Payload, &raw); err != nil {
		return raw, err
	}
	raw.GatewayRef = e.Source.GatewayID
	raw.ReceivedAt = e.Source.ReceivedAt
	check, err := Card(raw, e.Source.BindingGeneration)
	if err != nil {
		return raw, err
	}
	if !sameFacts(check, e) {
		return raw, fmt.Errorf("stored card facts mismatch")
	}
	return raw, nil
}
func RestoreMenu(e ingressowner.Event) (platformingress.MenuIngressEvent, error) {
	var raw platformingress.MenuIngressEvent
	if e.Kind != ingressowner.Menu {
		return raw, fmt.Errorf("wrong event kind")
	}
	if err := json.Unmarshal(e.Payload, &raw); err != nil {
		return raw, err
	}
	raw.GatewayRef = e.Source.GatewayID
	raw.ReceivedAt = e.Source.ReceivedAt
	check, err := Menu(raw, e.Source.BindingGeneration)
	if err != nil {
		return raw, err
	}
	if !sameFacts(check, e) {
		return raw, fmt.Errorf("stored menu facts mismatch")
	}
	return raw, nil
}
func RestoreObservation(e ingressowner.Event) (platformingress.PlatformIngressEvent, error) {
	var raw platformingress.PlatformIngressEvent
	if e.Kind != ingressowner.Reaction && e.Kind != ingressowner.Recall && e.Kind != ingressowner.GroupMember {
		return raw, fmt.Errorf("wrong event kind")
	}
	if err := json.Unmarshal(e.Payload, &raw); err != nil {
		return raw, err
	}
	raw.GatewayRef = e.Source.GatewayID
	raw.ReceivedAt = e.Source.ReceivedAt
	check, err := Observation(raw, e.Source.BindingGeneration)
	if err != nil {
		return raw, err
	}
	if !sameFacts(check, e) {
		return raw, fmt.Errorf("stored observation facts mismatch")
	}
	return raw, nil
}
func sameFacts(a, b ingressowner.Event) bool {
	return a.Key == b.Key && a.Source == b.Source && a.Kind == b.Kind && a.ChatType == b.ChatType && a.ChatID == b.ChatID && a.MessageID == b.MessageID && a.Operator == b.Operator && a.CardKind == b.CardKind && a.CardRef == b.CardRef && string(a.Payload) == string(b.Payload)
}
