package claude

import (
	"encoding/json"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

// ObserveClient translates a line from the parent's stdin into canonical events.
// For Claude, the parent may send user messages or control requests.
func (t *Translator) ObserveClient(raw []byte) (Result, error) {
	var message map[string]any
	if err := json.Unmarshal(raw, &message); err != nil {
		return Result{}, err
	}

	msgType, _ := message["type"].(string)
	switch msgType {
	case "user":
		t.debugf("observe client user message: thread=%s", t.currentThreadID)
		return Result{Events: []agentproto.Event{{
			Kind:         agentproto.EventLocalInteractionObserved,
			ThreadID:     t.currentThreadID,
			Action:       "turn_start",
			TrafficClass: agentproto.TrafficClassPrimary,
			Initiator:    agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
		}}}, nil
	default:
		return Result{}, nil
	}
}
