package claude

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/translator"
)

// Compile-time check that *Translator satisfies the translator.Translator interface.
var _ translator.Translator = (*Translator)(nil)

// Result is an alias for the canonical translator.Result type.
type Result = translator.Result

// permissionRequest tracks a pending can_use_tool control request.
type permissionRequest struct {
	RequestID string
	ThreadID  string
	TurnID    string
}

// Translator converts between the Claude CLI SDK protocol (NDJSON) and
// the canonical agentproto events/commands used by the relay system.
//
// Unlike Codex, Claude CLI has no thread concept. A single process corresponds
// to one session (one thread). Thread switching is not supported in v1.
type Translator struct {
	instanceID      string
	debugLog        func(string, ...any)
	nextID          int
	sessionID       string
	currentThreadID string
	cwd             string
	model           string
	initComplete    bool
	pendingInitID   string

	// Turn tracking (Claude has at most one turn active per session)
	turnID     string
	turnActive bool
	turnNumber int

	// Streaming state for content blocks
	activeBlockIndex int
	activeBlockType  string // "text", "thinking", "tool_use"
	activeItemID     string
	activeToolName   string
	activeToolUseID  string

	// Permission tracking
	pendingPermissions map[string]permissionRequest
}

// NewTranslator creates a new Claude translator for the given instance.
func NewTranslator(instanceID string) *Translator {
	return &Translator{
		instanceID:         instanceID,
		pendingPermissions: map[string]permissionRequest{},
	}
}

// SetDebugLogger configures debug logging.
func (t *Translator) SetDebugLogger(debugLog func(string, ...any)) {
	t.debugLog = debugLog
}

func (t *Translator) debugf(format string, args ...any) {
	if t.debugLog != nil {
		t.debugLog(format, args...)
	}
}

func (t *Translator) nextRequest(prefix string) string {
	value := fmt.Sprintf("relay-%s-%d", prefix, t.nextID)
	t.nextID++
	return value
}

func (t *Translator) nextTurnID() string {
	t.turnNumber++
	return fmt.Sprintf("claude-turn-%d", t.turnNumber)
}

func (t *Translator) nextItemID(prefix string) string {
	return fmt.Sprintf("%s-%s-%d", prefix, t.turnID, t.nextID)
}

// buildUserMessage constructs a Claude user message from agentproto inputs.
func (t *Translator) buildUserMessage(inputs []agentproto.Input) map[string]any {
	// Build content - if only text, use simple string; otherwise use content blocks
	if len(inputs) == 1 && inputs[0].Type == agentproto.InputText {
		return map[string]any{
			"type": "user",
			"message": map[string]any{
				"role":    "user",
				"content": inputs[0].Text,
			},
		}
	}
	var blocks []map[string]any
	for _, input := range inputs {
		switch input.Type {
		case agentproto.InputText:
			blocks = append(blocks, map[string]any{
				"type": "text",
				"text": input.Text,
			})
		case agentproto.InputLocalImage, agentproto.InputRemoteImage:
			url := input.URL
			if input.Type == agentproto.InputLocalImage {
				url = "file://" + input.Path
			}
			blocks = append(blocks, map[string]any{
				"type": "image",
				"source": map[string]any{
					"type":       "url",
					"url":        url,
					"media_type": strings.TrimSpace(input.MIMEType),
				},
			})
		}
	}
	return map[string]any{
		"type": "user",
		"message": map[string]any{
			"role":    "user",
			"content": blocks,
		},
	}
}

func marshalNDJSON(v any) ([]byte, error) {
	bytes, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return append(bytes, '\n'), nil
}
