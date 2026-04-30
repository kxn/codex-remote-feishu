package claude

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

type Result struct {
	Events           []agentproto.Event
	OutboundToClaude [][]byte
	OutboundToParent [][]byte
	Suppress         bool
}

type Translator struct {
	instanceID string
	debugLog   func(string, ...any)

	nextID int

	sessionID      string
	model          string
	cwd            string
	permissionMode string

	activeTurn   *turnState
	pendingTurns []*turnState

	currentMessage *messageState

	toolStates            map[string]*toolState
	pendingRequests       map[string]*pendingRequest
	pendingControlReplies map[string]pendingControlReply
}

type turnState struct {
	CommandID                string
	Initiator                agentproto.Initiator
	ThreadID                 string
	TurnID                   string
	Started                  bool
	InterruptRequested       bool
	LastAssistantText        string
	AgentMessageCompleted    bool
	FallbackAgentMessageUsed bool
}

type messageState struct {
	ID     string
	Blocks map[int]*blockState
}

type blockState struct {
	Index          int
	Kind           string
	ItemID         string
	StartedEmitted bool
	TextBuffer     string
	ToolUseID      string
	ToolName       string
	ToolInputDelta string
	Completed      bool
}

type toolState struct {
	ToolUseID      string
	ItemID         string
	Name           string
	Input          map[string]any
	Internal       bool
	StartedEmitted bool
	Completed      bool
	TurnID         string
}

type pendingQuestion struct {
	ID       string
	Header   string
	Question string
}

type pendingRequest struct {
	RequestID          string
	ThreadID           string
	TurnID             string
	RequestType        agentproto.RequestType
	SemanticKind       string
	ToolName           string
	ToolUseID          string
	Input              map[string]any
	ItemID             string
	PlanBody           string
	PlanBodySource     string
	PlanFilePath       string
	Questions          []pendingQuestion
	InterruptOnDecline bool
	Decision           string
	Response           map[string]any
}

type pendingControlReply struct {
	Kind                  string
	DesiredPermissionMode string
}

func NewTranslator(instanceID string) *Translator {
	return &Translator{
		instanceID:            instanceID,
		toolStates:            map[string]*toolState{},
		pendingRequests:       map[string]*pendingRequest{},
		pendingControlReplies: map[string]pendingControlReply{},
	}
}

func (t *Translator) SetDebugLogger(debugLog func(string, ...any)) {
	t.debugLog = debugLog
}

func (t *Translator) debugf(format string, args ...any) {
	if t.debugLog != nil {
		t.debugLog(format, args...)
	}
}

func (t *Translator) ObserveClient(_ []byte) (Result, error) {
	return Result{}, nil
}

func (t *Translator) ObserveServer(line []byte) (Result, error) {
	var message map[string]any
	if err := json.Unmarshal(line, &message); err != nil {
		return Result{}, err
	}
	switch strings.TrimSpace(lookupStringFromAny(message["type"])) {
	case "system":
		return t.observeSystemMessage(message), nil
	case "stream_event":
		return t.observeStreamMessage(message), nil
	case "assistant":
		return t.observeAssistantMessage(message), nil
	case "user":
		return t.observeUserMessage(message), nil
	case "control_request":
		return t.observeControlRequest(message), nil
	case "control_response":
		return t.observeControlResponse(message), nil
	case "result":
		return t.observeResultMessage(message), nil
	default:
		return Result{}, nil
	}
}

func (t *Translator) BuildChildRestartRestoreFrame(string) ([]byte, string, bool, error) {
	return nil, "", false, nil
}

func (t *Translator) CancelChildRestartRestore(string) {}

func (t *Translator) nextNativeID(prefix string) string {
	t.nextID++
	return fmt.Sprintf("relay-claude-%s-%d", strings.TrimSpace(prefix), t.nextID)
}

func (t *Translator) nextTurnID() string {
	return t.nextNativeID("turn")
}

func (t *Translator) nextItemID() string {
	return t.nextNativeID("item")
}

func (t *Translator) ensureActiveTurn() *turnState {
	if t.activeTurn == nil && len(t.pendingTurns) != 0 {
		t.activeTurn = t.pendingTurns[0]
		t.pendingTurns = append([]*turnState(nil), t.pendingTurns[1:]...)
	}
	return t.activeTurn
}

func (t *Translator) startActiveTurnIfNeeded() []agentproto.Event {
	turn := t.ensureActiveTurn()
	if turn == nil || turn.Started {
		return nil
	}
	turn.Started = true
	return []agentproto.Event{{
		Kind:      agentproto.EventTurnStarted,
		CommandID: turn.CommandID,
		Initiator: turn.Initiator,
		ThreadID:  turn.ThreadID,
		TurnID:    turn.TurnID,
		CWD:       t.cwd,
		Model:     t.model,
	}}
}

func (t *Translator) canonicalThreadID(fallback string) string {
	if strings.TrimSpace(t.sessionID) != "" {
		return strings.TrimSpace(t.sessionID)
	}
	if strings.TrimSpace(fallback) != "" {
		return strings.TrimSpace(fallback)
	}
	return t.nextNativeID("thread")
}

func buildFailureProblem(code, message, details, threadID, turnID string) *agentproto.ErrorInfo {
	if strings.TrimSpace(message) == "" {
		return nil
	}
	problem := agentproto.ErrorInfo{
		Code:      strings.TrimSpace(code),
		Layer:     "wrapper",
		Stage:     "observe_claude_stdout",
		Operation: "claude.result",
		Message:   strings.TrimSpace(message),
		Details:   strings.TrimSpace(details),
		ThreadID:  strings.TrimSpace(threadID),
		TurnID:    strings.TrimSpace(turnID),
	}
	return &problem
}

func resolveRequestDecision(response map[string]any) string {
	if len(response) == 0 {
		return ""
	}
	switch strings.TrimSpace(lookupStringFromAny(response["decision"])) {
	case "accept", "acceptForSession", "decline", "cancel":
		return strings.TrimSpace(lookupStringFromAny(response["decision"]))
	default:
		return ""
	}
}

func isInternalInteractionTool(toolName string) bool {
	switch strings.TrimSpace(toolName) {
	case "AskUserQuestion", "ExitPlanMode":
		return true
	default:
		return false
	}
}

func firstToolResultBlock(blocks []map[string]any) map[string]any {
	for _, block := range blocks {
		if strings.TrimSpace(lookupStringFromAny(block["type"])) == "tool_result" {
			return block
		}
	}
	return nil
}

func firstTextBlock(blocks []map[string]any) map[string]any {
	for _, block := range blocks {
		if strings.TrimSpace(lookupStringFromAny(block["type"])) == "text" {
			return block
		}
	}
	return nil
}

func toolUseSummary(toolName string, input map[string]any) string {
	if command := strings.TrimSpace(lookupStringFromAny(input["command"])); command != "" {
		return command
	}
	if description := strings.TrimSpace(lookupStringFromAny(input["description"])); description != "" {
		return description
	}
	if len(input) != 0 {
		return compactJSON(input)
	}
	if strings.TrimSpace(toolName) != "" {
		return toolName
	}
	return ""
}

func approvalRequestBody(toolName string, input map[string]any) string {
	summary := strings.TrimSpace(toolUseSummary(toolName, input))
	if summary == "" {
		return "Claude 请求调用工具后继续执行。"
	}
	if toolName == "Bash" {
		return "Claude 请求执行以下命令：\n" + summary
	}
	return "Claude 请求调用工具：\n" + summary
}

func findRequestByToolUseID(pending map[string]*pendingRequest, toolUseID string) *pendingRequest {
	toolUseID = strings.TrimSpace(toolUseID)
	if toolUseID == "" {
		return nil
	}
	for _, request := range pending {
		if request != nil && strings.TrimSpace(request.ToolUseID) == toolUseID {
			return request
		}
	}
	return nil
}
