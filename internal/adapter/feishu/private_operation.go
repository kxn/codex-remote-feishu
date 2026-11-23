package feishu

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type privateOperationWire struct {
	Version         int
	TransportID     string
	Operation       Operation
	Card            json.RawMessage `json:",omitempty"`
	FinalSourceBody string
}

// FreezePrivateOperation preserves the actual card document, including fields
// that plain Operation JSON cannot encode. Replay does not rerun the projector.
func FreezePrivateOperation(op Operation, transportID string) (json.RawMessage, error) {
	if transportID == "" || len(transportID) > 50 {
		return nil, fmt.Errorf("bounded durable transport ID required")
	}
	w := privateOperationWire{Version: 1, TransportID: transportID, Operation: op, FinalSourceBody: op.finalSourceBody}
	if op.Kind == OperationSendCard || op.Kind == OperationUpdateCard {
		card, err := json.Marshal(trimCardPayloadForMessageTransport(renderOperationCard(op, op.effectiveCardEnvelope())))
		if err != nil {
			return nil, err
		}
		w.Card = card
	}
	return json.Marshal(w)
}

func ThawPrivateOperation(raw json.RawMessage) (Operation, error) {
	var w privateOperationWire
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&w); err != nil {
		return Operation{}, err
	}
	if w.Version != 1 || w.TransportID == "" || len(w.TransportID) > 50 {
		return Operation{}, fmt.Errorf("unsupported private operation")
	}
	op := w.Operation
	op.durableTransportID = w.TransportID
	op.finalSourceBody = w.FinalSourceBody
	if op.Kind == OperationSendCard || op.Kind == OperationUpdateCard {
		if err := json.Unmarshal(w.Card, &op.frozenCard); err != nil || len(op.frozenCard) == 0 {
			return Operation{}, fmt.Errorf("private card payload missing")
		}
	} else if len(w.Card) != 0 {
		return Operation{}, fmt.Errorf("non-card operation contains card payload")
	}
	return op, nil
}

// PrivateOperationReceipt contains only the completed platform operation's
// identity and display bookkeeping. It is built after Gateway.Apply succeeds.
type PrivateOperationReceipt struct {
	Version                                                          int
	Kind                                                             OperationKind
	GatewayID, SurfaceSessionID, ChatID, MessageID, ReplyToMessageID string
	ProgressCardStartSeq, ProgressCardEndSeq                         int
	DeliveredAt                                                      time.Time
	EmojiType, ReactionID                                            string
	ReactionAbsent                                                   bool
}

func PrivateReceiptForOperation(op Operation, at time.Time) (PrivateOperationReceipt, error) {
	if at.IsZero() || op.GatewayID == "" || op.SurfaceSessionID == "" || op.ChatID == "" {
		return PrivateOperationReceipt{}, fmt.Errorf("incomplete private operation receipt")
	}
	switch op.Kind {
	case OperationSendText, OperationSendCard, OperationSendImage, OperationUpdateCard, OperationDeleteMessage, OperationAddReaction, OperationRemoveReaction:
		if op.MessageID == "" {
			return PrivateOperationReceipt{}, fmt.Errorf("platform returned no message identity")
		}
	default:
		return PrivateOperationReceipt{}, fmt.Errorf("unknown private operation")
	}
	if (op.Kind == OperationAddReaction || op.Kind == OperationRemoveReaction) && op.ReactionID == "" && !op.ReactionAbsent {
		return PrivateOperationReceipt{}, fmt.Errorf("platform reaction result has no identity or confirmed absence")
	}
	return PrivateOperationReceipt{Version: 1, Kind: op.Kind, GatewayID: op.GatewayID, SurfaceSessionID: op.SurfaceSessionID, ChatID: op.ChatID, MessageID: op.MessageID, ReplyToMessageID: op.ReplyToMessageID, ProgressCardStartSeq: op.ProgressCardStartSeq, ProgressCardEndSeq: op.ProgressCardEndSeq, DeliveredAt: at, EmojiType: op.EmojiType, ReactionID: op.ReactionID, ReactionAbsent: op.ReactionAbsent}, nil
}

type privateTransportContextKey struct{}

// StampPrivateView adds the issued ownership token to every callback in the
// already-frozen card. It preserves the exact existing action/form values.
func StampPrivateView(raw json.RawMessage, token string) (json.RawMessage, bool, error) {
	if strings.TrimSpace(token) == "" || token != strings.TrimSpace(token) {
		return nil, false, fmt.Errorf("private view token required")
	}
	var wire privateOperationWire
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&wire); err != nil {
		return nil, false, err
	}
	if wire.Version != 1 {
		return nil, false, fmt.Errorf("invalid private operation")
	}
	if wire.Operation.Kind != OperationSendCard && wire.Operation.Kind != OperationUpdateCard {
		return raw, false, nil
	}
	var card map[string]any
	if err := json.Unmarshal(wire.Card, &card); err != nil {
		return nil, false, err
	}
	count := 0
	var visit func(any) error
	visit = func(value any) error {
		switch node := value.(type) {
		case map[string]any:
			if list, ok := node["behaviors"].([]any); ok {
				for _, item := range list {
					behavior, _ := item.(map[string]any)
					if v := callbackValueFromBehavior(behavior); v != nil {
						if old, exists := v["private_view"]; exists && old != token {
							return fmt.Errorf("conflicting private view")
						}
						v["private_view"] = token
						count++
					}
				}
			}
			if v := legacyButtonCallbackValue(node); v != nil {
				if old, exists := v["private_view"]; exists && old != token {
					return fmt.Errorf("conflicting private view")
				}
				v["private_view"] = token
				count++
			}
			for _, child := range node {
				if err := visit(child); err != nil {
					return err
				}
			}
		case []any:
			for _, child := range node {
				if err := visit(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := visit(card); err != nil {
		return nil, false, err
	}
	if count == 0 {
		return raw, false, nil
	}
	var err error
	wire.Card, err = json.Marshal(card)
	if err != nil {
		return nil, false, err
	}
	output, err := json.Marshal(wire)
	return output, true, err
}

// UnstampPrivateView recovers the immutable pre-token operation for receipt
// verification and requires every callback to carry the same issued token.
func UnstampPrivateView(raw json.RawMessage) (json.RawMessage, string, error) {
	var wire privateOperationWire
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&wire); err != nil {
		return nil, "", err
	}
	if wire.Version != 1 {
		return nil, "", fmt.Errorf("invalid private operation")
	}
	if wire.Operation.Kind != OperationSendCard && wire.Operation.Kind != OperationUpdateCard {
		return raw, "", nil
	}
	var card map[string]any
	if err := json.Unmarshal(wire.Card, &card); err != nil {
		return nil, "", err
	}
	token := ""
	callbacks := 0
	check := func(v map[string]any) error {
		rawValue, exists := v["private_view"]
		if !exists {
			return fmt.Errorf("unstamped callback")
		}
		s, ok := rawValue.(string)
		if !ok || s == "" || token != "" && token != s {
			return fmt.Errorf("inconsistent private view")
		}
		token = s
		delete(v, "private_view")
		callbacks++
		return nil
	}
	var visit func(any) error
	visit = func(value any) error {
		switch node := value.(type) {
		case map[string]any:
			if list, ok := node["behaviors"].([]any); ok {
				for _, item := range list {
					behavior, _ := item.(map[string]any)
					if v := callbackValueFromBehavior(behavior); v != nil {
						if err := check(v); err != nil {
							return err
						}
					}
				}
			}
			if v := legacyButtonCallbackValue(node); v != nil {
				if err := check(v); err != nil {
					return err
				}
			}
			for _, child := range node {
				if err := visit(child); err != nil {
					return err
				}
			}
		case []any:
			for _, child := range node {
				if err := visit(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := visit(card); err != nil {
		return nil, "", err
	}
	if callbacks == 0 {
		return raw, "", nil
	}
	var err error
	wire.Card, err = json.Marshal(card)
	if err != nil {
		return nil, "", err
	}
	original, err := json.Marshal(wire)
	return original, token, err
}

// PrivateOperationCard returns the exact frozen transport card for independent
// platform observation; callers must still verify message and bot identity.
func PrivateOperationCard(raw json.RawMessage) (json.RawMessage, error) {
	var wire privateOperationWire
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&wire); err != nil {
		return nil, err
	}
	if wire.Version != 1 || (wire.Operation.Kind != OperationSendCard && wire.Operation.Kind != OperationUpdateCard) || !json.Valid(wire.Card) {
		return nil, fmt.Errorf("private card operation required")
	}
	return append(json.RawMessage(nil), wire.Card...), nil
}
