package claude

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

// TranslateCommand converts a canonical agentproto command into native
// Claude CLI protocol frames to write to the agent's stdin.
func (t *Translator) TranslateCommand(command agentproto.Command) ([][]byte, error) {
	switch command.Kind {
	case agentproto.CommandPromptSend:
		return t.translatePromptSend(command)
	case agentproto.CommandTurnInterrupt:
		return t.translateTurnInterrupt(command)
	case agentproto.CommandTurnSteer:
		// Claude CLI does not support appending input mid-turn.
		return nil, fmt.Errorf("turn/steer is not supported by Claude CLI")
	case agentproto.CommandThreadsRefresh:
		// Claude has no native thread listing. This is handled by returning
		// synthetic events -- no outbound frames needed.
		return nil, nil
	case agentproto.CommandRequestRespond:
		return t.translateRequestRespond(command)
	default:
		return nil, nil
	}
}

func (t *Translator) translatePromptSend(command agentproto.Command) ([][]byte, error) {
	targetThread := command.Target.ThreadID

	// If targeting a specific thread that differs from our current one, reject.
	// Claude CLI is single-session; thread switching requires process restart.
	if targetThread != "" && t.currentThreadID != "" && targetThread != t.currentThreadID {
		t.debugf("translate prompt: rejecting thread switch from %s to %s", t.currentThreadID, targetThread)
		return nil, fmt.Errorf("Claude instance does not support thread switching (current=%s, target=%s)", t.currentThreadID, targetThread)
	}

	// Assign a thread ID if this is the first prompt
	if t.currentThreadID == "" {
		if targetThread != "" {
			t.currentThreadID = targetThread
		} else {
			t.currentThreadID = fmt.Sprintf("claude-session-%s", t.instanceID)
		}
	}

	msg := t.buildUserMessage(command.Prompt.Inputs)
	bytes, err := marshalNDJSON(msg)
	if err != nil {
		return nil, err
	}

	t.debugf(
		"translate prompt: thread=%s surface=%s inputs=%d",
		t.currentThreadID,
		firstNonEmpty(command.Origin.Surface, command.Origin.ChatID),
		len(command.Prompt.Inputs),
	)

	return [][]byte{bytes}, nil
}

func (t *Translator) translateTurnInterrupt(_ agentproto.Command) ([][]byte, error) {
	payload := map[string]any{
		"type":       "control_request",
		"request_id": t.nextRequest("interrupt"),
		"request": map[string]any{
			"subtype": "interrupt",
		},
	}
	bytes, err := marshalNDJSON(payload)
	if err != nil {
		return nil, err
	}
	t.debugf("translate interrupt: thread=%s turn=%s", t.currentThreadID, t.turnID)
	return [][]byte{bytes}, nil
}

func (t *Translator) translateRequestRespond(command agentproto.Command) ([][]byte, error) {
	requestID := command.Request.RequestID
	if requestID == "" {
		return nil, nil
	}

	pending, exists := t.pendingPermissions[requestID]
	if !exists {
		t.debugf("translate request respond: unknown request %s", requestID)
		return nil, nil
	}
	delete(t.pendingPermissions, requestID)

	responseType, _ := command.Request.Response["type"].(string)
	var behavior string
	switch responseType {
	case "approval":
		if decision, _ := command.Request.Response["decision"].(string); strings.TrimSpace(decision) != "" {
			if decision == "accept" {
				behavior = "allow"
			} else {
				behavior = "deny"
			}
		} else {
			approved, _ := command.Request.Response["approved"].(bool)
			if approved {
				behavior = "allow"
			} else {
				behavior = "deny"
			}
		}
	default:
		approved, _ := command.Request.Response["approved"].(bool)
		if approved {
			behavior = "allow"
		} else {
			behavior = "deny"
		}
	}

	response := map[string]any{
		"type": "control_response",
		"response": map[string]any{
			"subtype":    "success",
			"request_id": pending.RequestID,
			"response": map[string]any{
				"behavior":     behavior,
				"updatedInput": map[string]any{},
			},
		},
	}

	bytes, err := marshalNDJSON(response)
	if err != nil {
		return nil, err
	}
	t.debugf("translate request respond: request=%s behavior=%s", requestID, behavior)
	return [][]byte{bytes}, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
