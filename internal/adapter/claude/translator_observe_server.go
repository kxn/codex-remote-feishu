package claude

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

// ObserveServer parses a line from the Claude CLI stdout and converts it
// into canonical agentproto events. It may also return OutboundToAgent
// frames to write back to the CLI (e.g. control_response for MCP setup).
func (t *Translator) ObserveServer(raw []byte) (Result, error) {
	var message map[string]any
	if err := json.Unmarshal(raw, &message); err != nil {
		return Result{}, err
	}

	msgType, _ := message["type"].(string)
	switch msgType {
	case "system":
		return t.observeSystem(message)
	case "stream_event":
		return t.observeStreamEvent(message)
	case "assistant":
		return t.observeAssistant(message)
	case "result":
		return t.observeResult(message)
	case "control_request":
		return t.observeControlRequest(message)
	case "control_response":
		return t.observeControlResponse(message)
	case "user":
		// CLI echoing back tool results -- suppress from relay
		return Result{}, nil
	default:
		return Result{}, nil
	}
}

// observeSystem handles system messages (session init).
func (t *Translator) observeSystem(message map[string]any) (Result, error) {
	subtype, _ := message["subtype"].(string)
	if subtype != "init" {
		return Result{}, nil
	}

	t.sessionID = stringField(message, "session_id")
	t.model = stringField(message, "model")
	cwd := stringField(message, "cwd")
	if cwd != "" {
		t.cwd = cwd
	}
	t.initComplete = true

	// Assign thread ID from session if not yet assigned
	if t.currentThreadID == "" {
		t.currentThreadID = fmt.Sprintf("claude-session-%s", t.instanceID)
	}

	t.debugf("observe system init: session=%s model=%s cwd=%s thread=%s", t.sessionID, t.model, t.cwd, t.currentThreadID)

	var events []agentproto.Event

	events = append(events, agentproto.Event{
		Kind:        agentproto.EventThreadDiscovered,
		ThreadID:    t.currentThreadID,
		CWD:         t.cwd,
		Name:        "Claude Session",
		FocusSource: "remote_created_thread",
	})

	if t.model != "" {
		events = append(events, agentproto.Event{
			Kind:    agentproto.EventConfigObserved,
			Model:   t.model,
			CWD:     t.cwd,
			Loaded:  true,
		})
	}

	return Result{Events: events}, nil
}

// observeStreamEvent handles real-time streaming deltas from the Claude CLI.
func (t *Translator) observeStreamEvent(message map[string]any) (Result, error) {
	// stream_event wraps an inner event field
	event, _ := message["event"].(map[string]any)
	if event == nil {
		// Some stream_events have the event type at top level
		event = message
	}

	eventType, _ := event["type"].(string)
	switch eventType {
	case "message_start":
		return t.observeMessageStart(event)
	case "content_block_start":
		return t.observeContentBlockStart(event)
	case "content_block_delta":
		return t.observeContentBlockDelta(event)
	case "content_block_stop":
		return t.observeContentBlockStop(event)
	case "message_delta":
		// Message-level metadata update (e.g. stop_reason) -- no direct mapping needed
		return Result{}, nil
	case "message_stop":
		// Wait for the result message for turn completion
		return Result{}, nil
	default:
		return Result{}, nil
	}
}

func (t *Translator) observeMessageStart(event map[string]any) (Result, error) {
	t.turnID = t.nextTurnID()
	t.turnActive = true
	t.blocks = map[int]*blockState{}

	t.debugf("observe message_start: thread=%s turn=%s", t.currentThreadID, t.turnID)

	return Result{Events: []agentproto.Event{{
		Kind:     agentproto.EventTurnStarted,
		ThreadID: t.currentThreadID,
		TurnID:   t.turnID,
		Status:   "running",
	}}}, nil
}

func (t *Translator) observeContentBlockStart(event map[string]any) (Result, error) {
	index := intField(event, "index")
	block, _ := event["content_block"].(map[string]any)
	blockType, _ := block["type"].(string)
	itemID := fmt.Sprintf("item-%s-%d", t.turnID, index)

	bs := &blockState{
		blockType: blockType,
		itemID:    itemID,
	}

	var itemKind string
	switch blockType {
	case "text":
		itemKind = "agent_message"
	case "thinking":
		itemKind = "reasoning_content"
	case "tool_use":
		itemKind = "command_execution"
		bs.toolName = stringField(block, "name")
		bs.toolUseID = stringField(block, "id")
	default:
		itemKind = blockType
	}

	t.blocks[index] = bs

	t.debugf("observe content_block_start: index=%d type=%s itemKind=%s tool=%s",
		index, blockType, itemKind, bs.toolName)

	metadata := map[string]any{"blockIndex": index}
	if bs.toolName != "" {
		metadata["toolName"] = bs.toolName
		metadata["toolUseId"] = bs.toolUseID
	}

	return Result{Events: []agentproto.Event{{
		Kind:     agentproto.EventItemStarted,
		ThreadID: t.currentThreadID,
		TurnID:   t.turnID,
		ItemID:   itemID,
		ItemKind: itemKind,
		Metadata: metadata,
	}}}, nil
}

func (t *Translator) observeContentBlockDelta(event map[string]any) (Result, error) {
	index := intField(event, "index")
	bs, exists := t.blocks[index]
	if !exists {
		return Result{}, nil
	}

	delta, _ := event["delta"].(map[string]any)
	if delta == nil {
		return Result{}, nil
	}

	deltaType, _ := delta["type"].(string)
	var itemKind, text string

	switch deltaType {
	case "text_delta":
		itemKind = "agent_message"
		text, _ = delta["text"].(string)
	case "thinking_delta":
		itemKind = "reasoning_content"
		text, _ = delta["thinking"].(string)
	case "input_json_delta":
		itemKind = "command_execution"
		text, _ = delta["partial_json"].(string)
	default:
		// signature_delta and other unknown types -- silently skip
		return Result{}, nil
	}

	if text == "" {
		return Result{}, nil
	}

	return Result{Events: []agentproto.Event{{
		Kind:     agentproto.EventItemDelta,
		ThreadID: t.currentThreadID,
		TurnID:   t.turnID,
		ItemID:   bs.itemID,
		ItemKind: itemKind,
		Delta:    text,
	}}}, nil
}

func (t *Translator) observeContentBlockStop(event map[string]any) (Result, error) {
	index := intField(event, "index")
	bs, exists := t.blocks[index]
	if !exists {
		return Result{}, nil
	}

	itemKind := "agent_message"
	switch bs.blockType {
	case "thinking":
		itemKind = "reasoning_content"
	case "tool_use":
		itemKind = "command_execution"
	case "text":
		itemKind = "agent_message"
	}

	t.debugf("observe content_block_stop: index=%d type=%s", index, bs.blockType)

	delete(t.blocks, index)

	return Result{Events: []agentproto.Event{{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: t.currentThreadID,
		TurnID:   t.turnID,
		ItemID:   bs.itemID,
		ItemKind: itemKind,
	}}}, nil
}

// observeAssistant handles complete assistant messages.
func (t *Translator) observeAssistant(message map[string]any) (Result, error) {
	// The assistant message contains the full content blocks.
	// If we've been tracking stream events, these are already emitted.
	// We only need this for cases where streaming didn't provide all info.
	return Result{}, nil
}

// observeResult handles turn completion messages.
func (t *Translator) observeResult(message map[string]any) (Result, error) {
	isError, _ := message["is_error"].(bool)
	status := "completed"
	errorMsg := ""
	if isError {
		status = "failed"
		errorMsg, _ = message["result"].(string)
	}

	turnID := t.turnID
	t.turnActive = false

	t.debugf("observe result: thread=%s turn=%s status=%s isError=%t", t.currentThreadID, turnID, status, isError)

	return Result{Events: []agentproto.Event{{
		Kind:         agentproto.EventTurnCompleted,
		ThreadID:     t.currentThreadID,
		TurnID:       turnID,
		Status:       status,
		ErrorMessage: errorMsg,
	}}}, nil
}

// observeControlRequest handles control requests from the CLI.
// These may be tool permission checks or MCP setup during initialization.
func (t *Translator) observeControlRequest(message map[string]any) (Result, error) {
	requestID := stringField(message, "request_id")
	request, _ := message["request"].(map[string]any)
	if request == nil {
		return Result{}, nil
	}

	subtype, _ := request["subtype"].(string)
	switch subtype {
	case "can_use_tool":
		return t.observeCanUseTool(requestID, request)
	case "mcp_message":
		return t.observeMCPMessage(requestID, request)
	default:
		t.debugf("observe control_request: unknown subtype=%s request=%s", subtype, requestID)
		return Result{}, nil
	}
}

// observeCanUseTool translates a tool permission request to an EventRequestStarted.
func (t *Translator) observeCanUseTool(requestID string, request map[string]any) (Result, error) {
	toolName, _ := request["tool_name"].(string)
	toolInput, _ := request["input"].(map[string]any)

	// Generate a canonical request ID for the relay
	canonicalID := fmt.Sprintf("perm-%s", requestID)

	t.pendingPermissions[canonicalID] = permissionRequest{
		RequestID: requestID,
		ThreadID:  t.currentThreadID,
		TurnID:    t.turnID,
	}

	// Build metadata for the orchestrator
	title := fmt.Sprintf("Allow %s?", toolName)
	body := ""
	if toolInput != nil {
		if cmd, ok := toolInput["command"].(string); ok {
			body = cmd
		} else {
			raw, _ := json.Marshal(toolInput)
			body = string(raw)
		}
	}

	metadata := map[string]any{
		"type":  "approval",
		"title": title,
		"body":  body,
		"options": []map[string]any{
			{"id": "accept", "label": "Allow", "style": "primary"},
			{"id": "decline", "label": "Deny", "style": "default"},
		},
		"toolName": toolName,
	}

	t.debugf("observe can_use_tool: request=%s tool=%s", requestID, toolName)

	return Result{Events: []agentproto.Event{{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  t.currentThreadID,
		TurnID:    t.turnID,
		RequestID: canonicalID,
		Status:    "pending",
		Metadata:  metadata,
	}}}, nil
}

// observeMCPMessage handles MCP setup messages during initialization.
// These are handled internally by responding immediately.
func (t *Translator) observeMCPMessage(requestID string, request map[string]any) (Result, error) {
	serverName, _ := request["server_name"].(string)
	mcpMsg, _ := request["message"].(map[string]any)
	method, _ := mcpMsg["method"].(string)
	rpcID := mcpMsg["id"]

	t.debugf("observe mcp_message: request=%s server=%s method=%s", requestID, serverName, method)

	// For MCP setup during init, respond with appropriate defaults
	var mcpResult any
	switch method {
	case "initialize":
		mcpResult = map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"serverInfo": map[string]any{
				"name":    "codex-remote",
				"version": "1.0",
			},
		}
	case "notifications/initialized":
		mcpResult = map[string]any{}
	case "tools/list":
		mcpResult = map[string]any{
			"tools": []any{},
		}
	default:
		mcpResult = map[string]any{}
	}

	response := map[string]any{
		"type": "control_response",
		"response": map[string]any{
			"subtype":    "success",
			"request_id": requestID,
			"response": map[string]any{
				"mcp_response": map[string]any{
					"jsonrpc": "2.0",
					"id":      rpcID,
					"result":  mcpResult,
				},
			},
		},
	}

	bytes, err := marshalNDJSON(response)
	if err != nil {
		return Result{}, err
	}

	return Result{OutboundToAgent: [][]byte{bytes}}, nil
}

// observeControlResponse handles control responses from the CLI.
func (t *Translator) observeControlResponse(message map[string]any) (Result, error) {
	response, _ := message["response"].(map[string]any)
	if response == nil {
		return Result{}, nil
	}
	requestID := stringField(response, "request_id")

	// Check if this is the init response
	if requestID == t.pendingInitID {
		t.debugf("observe control_response: init complete request=%s", requestID)
		t.pendingInitID = ""
		return Result{}, nil
	}

	return Result{}, nil
}

func stringField(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return strings.TrimSpace(v)
}

func intField(m map[string]any, key string) int {
	v, _ := m[key].(float64)
	return int(v)
}
