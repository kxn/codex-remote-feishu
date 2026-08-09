package acp

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

func (t *Translator) observeMethodFrame(frame map[string]any, method string, result Result) (Result, error) {
	switch method {
	case "session/update":
		params, _ := frame["params"].(map[string]any)
		return t.observeSessionUpdate(params, result)
	case "session/request_permission":
		return t.observePermissionRequest(frame, result)
	case "fs/write_text_file":
		return t.observeWriteTextFile(frame, result)
	default:
		if _, hasID := frame["id"]; hasID {
			response, err := marshalLine(map[string]any{
				"jsonrpc": "2.0",
				"id":      frame["id"],
				"error": map[string]any{
					"code":    -32601,
					"message": "unsupported client method",
				},
			})
			if err != nil {
				return Result{}, err
			}
			result.OutboundToChild = append(result.OutboundToChild, response)
		}
		return result, nil
	}
}

func (t *Translator) observeResponseFrame(frame map[string]any, result Result) (Result, error) {
	requestID := idKey(frame["id"])
	pending, ok := t.pendingRPC[requestID]
	if !ok {
		t.debugf("opencode unknown response id=%s payload=%s", requestID, xutil.CompactJSON(frame))
		return result, nil
	}
	delete(t.pendingRPC, requestID)
	if errorPayload, ok := frame["error"]; ok && errorPayload != nil {
		return t.observeRPCError(pending, errorPayload, result), nil
	}
	payload, _ := frame["result"].(map[string]any)
	switch pending.Kind {
	case "initialize":
		return result, nil
	case "session/new", "session/resume", "session/fork":
		return t.observeSessionReady(pending, payload, result)
	case "session/prompt":
		return t.observePromptResponse(pending, payload, result), nil
	case "session/list":
		return t.observeSessionListResponse(pending, payload, result), nil
	case "session/load":
		return t.observeSessionLoadResponse(pending, payload, result), nil
	case "session/close":
		return result, nil
	default:
		return result, nil
	}
}

func (t *Translator) observeSessionReady(pending pendingRPC, payload map[string]any, result Result) (Result, error) {
	sessionID := strings.TrimSpace(xutil.LookupStringFromAny(payload["sessionId"]))
	if sessionID == "" {
		return result, fmt.Errorf("%s response missing sessionId", pending.Kind)
	}
	command := pending.Command
	session := t.upsertSession(sessionID, t.commandCWD(command), payload)
	t.currentSessionID = sessionID
	events := []agentproto.Event{
		{
			Kind:     agentproto.EventThreadDiscovered,
			ThreadID: sessionID,
			CWD:      session.CWD,
			Name:     session.Title,
		},
		{
			Kind:        agentproto.EventThreadFocused,
			ThreadID:    sessionID,
			CWD:         session.CWD,
			FocusSource: "opencode_acp",
		},
	}
	promptResult, err := t.startPromptForSession(sessionID, command)
	if err != nil {
		return Result{}, err
	}
	result.Events = append(result.Events, events...)
	result.Events = append(result.Events, promptResult.Events...)
	result.OutboundToChild = append(result.OutboundToChild, promptResult.OutboundToChild...)
	return result, nil
}

func (t *Translator) startPromptForSession(sessionID string, command agentproto.Command) (Result, error) {
	turn := t.newTurn(sessionID, command)
	content, err := t.buildPromptContent(command.Prompt.Inputs)
	if err != nil {
		return Result{}, err
	}
	requestID := t.nextRequest("session-prompt")
	t.pendingRPC[requestID] = pendingRPC{Kind: "session/prompt", Command: command, Turn: turn}
	frame, err := marshalLine(map[string]any{
		"jsonrpc": "2.0",
		"id":      requestID,
		"method":  "session/prompt",
		"params": map[string]any{
			"sessionId": sessionID,
			"prompt":    content,
		},
	})
	if err != nil {
		return Result{}, err
	}
	t.activeTurns[sessionID] = turn
	event := agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		CommandID: command.CommandID,
		ThreadID:  sessionID,
		TurnID:    turn.TurnID,
		CWD:       t.commandCWD(command),
		Initiator: turn.Initiator,
	}
	if turn.Traffic != "" {
		event.TrafficClass = turn.Traffic
	}
	return Result{Events: []agentproto.Event{event}, OutboundToChild: [][]byte{frame}}, nil
}

func (t *Translator) observePromptResponse(pending pendingRPC, payload map[string]any, result Result) Result {
	turn := pending.Turn
	if turn == nil {
		turn = t.activeTurns[xutil.FirstNonEmpty(pending.Command.Target.ThreadID, t.currentSessionID)]
	}
	if turn == nil {
		return result
	}
	if usage, ok := promptUsage(payload["usage"]); ok {
		result.Events = append(result.Events, t.updateUsage(turn.ThreadID, usage))
	}
	result.Events = append(result.Events, t.completeOpenTextItems(turn)...)
	status := statusFromStopReason(xutil.LookupStringFromAny(payload["stopReason"]))
	completed := agentproto.Event{
		Kind:                 agentproto.EventTurnCompleted,
		CommandID:            turn.CommandID,
		ThreadID:             turn.ThreadID,
		TurnID:               turn.TurnID,
		Status:               status,
		TurnCompletionOrigin: agentproto.TurnCompletionOriginRuntime,
		Initiator:            turn.Initiator,
	}
	if turn.Traffic != "" {
		completed.TrafficClass = turn.Traffic
	}
	result.Events = append(result.Events, completed)
	turn.Completed = true
	delete(t.activeTurns, turn.ThreadID)
	return result
}

func (t *Translator) completeOpenTextItems(turn *turnState) []agentproto.Event {
	if turn == nil {
		return nil
	}
	type pendingItem struct {
		key  string
		item *itemState
	}
	var pending []pendingItem
	for key, item := range t.messageItems {
		if item == nil || item.ThreadID != turn.ThreadID || item.TurnID != turn.TurnID || !item.Started || item.Completed {
			continue
		}
		switch item.Kind {
		case "reasoning_summary", "agent_message":
			pending = append(pending, pendingItem{key: key, item: item})
		}
	}
	sort.Slice(pending, func(i, j int) bool {
		left := textItemCompletionPriority(pending[i].item.Kind)
		right := textItemCompletionPriority(pending[j].item.Kind)
		if left != right {
			return left < right
		}
		return pending[i].key < pending[j].key
	})
	events := make([]agentproto.Event, 0, len(pending))
	for _, current := range pending {
		current.item.Completed = true
		events = append(events, t.annotateTurnEvent(turn, agentproto.Event{
			Kind:     agentproto.EventItemCompleted,
			ThreadID: turn.ThreadID,
			TurnID:   turn.TurnID,
			ItemID:   current.item.ItemID,
			ItemKind: current.item.Kind,
			Status:   "completed",
		}))
	}
	return events
}

func textItemCompletionPriority(kind string) int {
	switch kind {
	case "reasoning_summary":
		return 0
	case "agent_message":
		return 1
	default:
		return 2
	}
}

func (t *Translator) observeSessionListResponse(pending pendingRPC, payload map[string]any, result Result) Result {
	rawSessions, _ := payload["sessions"].([]any)
	threads := make([]agentproto.ThreadSnapshotRecord, 0, len(rawSessions))
	for i, raw := range rawSessions {
		item, _ := raw.(map[string]any)
		if item == nil {
			continue
		}
		sessionID := strings.TrimSpace(xutil.LookupStringFromAny(item["sessionId"]))
		if sessionID == "" {
			continue
		}
		cwd := xutil.LookupStringFromAny(item["cwd"])
		title := xutil.LookupStringFromAny(item["title"])
		t.upsertSession(sessionID, cwd, map[string]any{"title": title})
		threads = append(threads, agentproto.ThreadSnapshotRecord{
			ThreadID:  sessionID,
			Name:      title,
			CWD:       cwd,
			Loaded:    sessionID == t.currentSessionID,
			ListOrder: i,
		})
	}
	result.Events = append(result.Events, agentproto.Event{
		Kind:      agentproto.EventThreadsSnapshot,
		CommandID: pending.Command.CommandID,
		Threads:   threads,
	})
	return result
}

func (t *Translator) observeSessionLoadResponse(pending pendingRPC, payload map[string]any, result Result) Result {
	sessionID := strings.TrimSpace(pending.Command.Target.ThreadID)
	session := t.upsertSession(sessionID, t.commandCWD(pending.Command), payload)
	history := t.historyHydrations[sessionID]
	delete(t.historyHydrations, sessionID)
	var turns []agentproto.ThreadHistoryTurnRecord
	if history != nil {
		turns = history.Turns
	}
	result.Events = append(result.Events, agentproto.Event{
		Kind:      agentproto.EventThreadHistoryRead,
		CommandID: pending.Command.CommandID,
		ThreadID:  sessionID,
		ThreadHistory: &agentproto.ThreadHistoryRecord{
			Thread: agentproto.ThreadSnapshotRecord{
				ThreadID: sessionID,
				CWD:      session.CWD,
				Name:     session.Title,
				Loaded:   sessionID == t.currentSessionID,
			},
			Turns: turns,
		},
	})
	return result
}

func (t *Translator) observeRPCError(pending pendingRPC, errorPayload any, result Result) Result {
	if pending.Kind == "session/load" {
		delete(t.historyHydrations, strings.TrimSpace(pending.Command.Target.ThreadID))
	}
	problem := normalizeOpenCodeRPCError(errorPayload, agentproto.ErrorInfo{
		Layer:     "wrapper",
		Stage:     "observe_server",
		Operation: pending.Kind,
		CommandID: pending.Command.CommandID,
		ThreadID:  pending.Command.Target.ThreadID,
		TurnID:    pending.Command.Target.TurnID,
	})
	if pending.Turn != nil {
		result.Events = append(result.Events, agentproto.Event{
			Kind:                 agentproto.EventTurnCompleted,
			CommandID:            pending.Turn.CommandID,
			ThreadID:             pending.Turn.ThreadID,
			TurnID:               pending.Turn.TurnID,
			Status:               "failed",
			TurnCompletionOrigin: agentproto.TurnCompletionOriginRuntime,
			Problem:              &problem,
		})
		delete(t.activeTurns, pending.Turn.ThreadID)
	}
	result.Events = append(result.Events, agentproto.Event{Kind: agentproto.EventSystemError, Problem: &problem})
	return result
}

func (t *Translator) observeSessionUpdate(params map[string]any, result Result) (Result, error) {
	sessionID := strings.TrimSpace(xutil.LookupStringFromAny(params["sessionId"]))
	update, _ := params["update"].(map[string]any)
	if sessionID == "" || update == nil {
		return result, nil
	}
	if hydration := t.historyHydrations[sessionID]; hydration != nil {
		switch strings.TrimSpace(xutil.LookupStringFromAny(update["sessionUpdate"])) {
		case "session_info_update":
			t.observeSessionInfoUpdate(sessionID, update)
		case "user_message_chunk", "agent_message_chunk", "agent_thought_chunk", "tool_call", "tool_call_update":
			hydration.observeUpdate(update)
		}
		return result, nil
	}
	switch strings.TrimSpace(xutil.LookupStringFromAny(update["sessionUpdate"])) {
	case "agent_message_chunk":
		result.Events = append(result.Events, t.observeTextChunk(sessionID, update, "agent_message")...)
	case "agent_thought_chunk":
		result.Events = append(result.Events, t.observeTextChunk(sessionID, update, "reasoning_summary")...)
	case "tool_call":
		result.Events = append(result.Events, t.observeToolCall(sessionID, update)...)
	case "tool_call_update":
		result.Events = append(result.Events, t.observeToolCallUpdate(sessionID, update)...)
	case "usage_update":
		t.observeUsageUpdate(sessionID, update)
	case "session_info_update":
		t.observeSessionInfoUpdate(sessionID, update)
	case "config_option_update":
		if event, ok := t.observeConfigOptionUpdate(sessionID, update); ok {
			result.Events = append(result.Events, event)
		}
	case "current_mode_update":
		t.observeCurrentModeUpdate(sessionID, update)
	case "available_commands_update":
		t.debugf("opencode available_commands_update session=%s payload=%s", sessionID, xutil.CompactJSON(update))
		return result, nil
	}
	return result, nil
}

func (t *Translator) observeTextChunk(sessionID string, update map[string]any, kind string) []agentproto.Event {
	turn := t.ensureTurnForSession(sessionID)
	if turn == nil {
		return nil
	}
	content, _ := update["content"].(map[string]any)
	if strings.TrimSpace(xutil.LookupStringFromAny(content["type"])) != "text" {
		return nil
	}
	text := xutil.LookupStringFromAny(content["text"])
	if text == "" {
		return nil
	}
	messageID := xutil.FirstNonEmpty(xutil.LookupStringFromAny(update["messageId"]), "message")
	key := sessionID + "\x00" + kind + "\x00" + messageID
	item := t.messageItems[key]
	if item == nil {
		item = &itemState{
			ItemID:   "opencode-" + kind + "-" + sanitizeID(messageID),
			Kind:     kind,
			ThreadID: sessionID,
			TurnID:   turn.TurnID,
		}
		t.messageItems[key] = item
	}
	events := make([]agentproto.Event, 0, 2)
	if !item.Started {
		item.Started = true
		events = append(events, t.annotateTurnEvent(turn, agentproto.Event{
			Kind:     agentproto.EventItemStarted,
			ThreadID: sessionID,
			TurnID:   turn.TurnID,
			ItemID:   item.ItemID,
			ItemKind: kind,
			Status:   "in_progress",
		}))
	}
	item.Text.WriteString(text)
	if kind == "reasoning_summary" {
		events = append(events, t.annotateTurnEvent(turn, agentproto.Event{
			Kind:     agentproto.EventItemReasoningSummaryPartAdded,
			ThreadID: sessionID,
			TurnID:   turn.TurnID,
			ItemID:   item.ItemID,
			ItemKind: kind,
			Delta:    text,
		}))
	} else {
		events = append(events, t.annotateTurnEvent(turn, agentproto.Event{
			Kind:     agentproto.EventItemDelta,
			ThreadID: sessionID,
			TurnID:   turn.TurnID,
			ItemID:   item.ItemID,
			ItemKind: kind,
			Delta:    text,
		}))
	}
	return events
}

func (t *Translator) observeToolCall(sessionID string, update map[string]any) []agentproto.Event {
	turn := t.ensureTurnForSession(sessionID)
	if turn == nil {
		return nil
	}
	toolID := xutil.LookupStringFromAny(update["toolCallId"])
	if toolID == "" {
		return nil
	}
	kind := toolItemKind(update)
	key := sessionID + "\x00tool\x00" + toolID
	if _, exists := t.messageItems[key]; exists {
		return nil
	}
	item := &itemState{
		ItemID:   "opencode-tool-" + sanitizeID(toolID),
		Kind:     kind,
		ThreadID: sessionID,
		TurnID:   turn.TurnID,
		Started:  kind != "",
		Metadata: opencodeToolMetadata(update, nil),
	}
	t.messageItems[key] = item
	if kind == "" {
		return nil
	}
	return []agentproto.Event{t.annotateTurnEvent(turn, agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: sessionID,
		TurnID:   turn.TurnID,
		ItemID:   item.ItemID,
		ItemKind: kind,
		Name:     xutil.LookupStringFromAny(update["title"]),
		Status:   xutil.FirstNonEmpty(xutil.LookupStringFromAny(update["status"]), "pending"),
		Metadata: xutil.CloneMap(item.Metadata),
	})}
}

func (t *Translator) observeToolCallUpdate(sessionID string, update map[string]any) []agentproto.Event {
	turn := t.ensureTurnForSession(sessionID)
	if turn == nil {
		return nil
	}
	toolID := xutil.LookupStringFromAny(update["toolCallId"])
	if toolID == "" {
		return nil
	}
	key := sessionID + "\x00tool\x00" + toolID
	item := t.messageItems[key]
	if item == nil {
		item = &itemState{
			ItemID:   "opencode-tool-" + sanitizeID(toolID),
			Kind:     toolItemKind(update),
			ThreadID: sessionID,
			TurnID:   turn.TurnID,
			Metadata: opencodeToolMetadata(update, nil),
		}
		t.messageItems[key] = item
	}
	if item.Kind == "" {
		item.Kind = toolItemKind(update)
	}
	item.Metadata = opencodeToolMetadata(update, item.Metadata)
	if item.Kind == "" {
		if event, ok := t.todoPlanEvent(turn, sessionID, item, update); ok {
			return []agentproto.Event{event}
		}
		return nil
	}
	events := make([]agentproto.Event, 0, 2)
	if !item.Started {
		item.Started = true
		events = append(events, t.annotateTurnEvent(turn, agentproto.Event{
			Kind:     agentproto.EventItemStarted,
			ThreadID: sessionID,
			TurnID:   turn.TurnID,
			ItemID:   item.ItemID,
			ItemKind: item.Kind,
			Name:     xutil.LookupStringFromAny(update["title"]),
			Status:   xutil.FirstNonEmpty(xutil.LookupStringFromAny(update["status"]), "in_progress"),
			Metadata: xutil.CloneMap(item.Metadata),
		}))
	}
	status := xutil.LookupStringFromAny(update["status"])
	if status == "completed" || status == "failed" {
		if item.Completed {
			return events
		}
		item.Completed = true
		events = append(events, t.annotateTurnEvent(turn, agentproto.Event{
			Kind:     agentproto.EventItemCompleted,
			ThreadID: sessionID,
			TurnID:   turn.TurnID,
			ItemID:   item.ItemID,
			ItemKind: item.Kind,
			Status:   status,
			Metadata: opencodeToolCompletionMetadata(item.Metadata, update),
		}))
		return events
	}
	content := historyContentText(update["content"])
	if content == "" {
		content = xutil.CompactJSON(update["content"])
	}
	if content == "" {
		return events
	}
	deltaKind := item.Kind
	metadata := xutil.CloneMap(item.Metadata)
	if deltaKind == "command_execution" {
		deltaKind = "command_execution_output"
		metadata = nil
	}
	events = append(events, t.annotateTurnEvent(turn, agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: sessionID,
		TurnID:   turn.TurnID,
		ItemID:   item.ItemID,
		ItemKind: deltaKind,
		Delta:    content,
		Metadata: metadata,
	}))
	return events
}

func (t *Translator) observePermissionRequest(frame map[string]any, result Result) (Result, error) {
	requestID := idKey(frame["id"])
	params, _ := frame["params"].(map[string]any)
	sessionID := strings.TrimSpace(xutil.LookupStringFromAny(params["sessionId"]))
	toolCall, _ := params["toolCall"].(map[string]any)
	toolID := xutil.LookupStringFromAny(toolCall["toolCallId"])
	options := parsePermissionOptions(params["options"])
	turn := t.ensureTurnForSession(sessionID)
	pending := pendingPermission{
		NativeID: frame["id"],
		ThreadID: sessionID,
		ItemID:   toolID,
		Options:  options,
	}
	if turn != nil {
		pending.TurnID = turn.TurnID
	}
	t.pendingPermissions[requestID] = pending
	promptOptions := make([]agentproto.RequestOption, 0, len(options))
	for _, option := range options {
		promptOptions = append(promptOptions, agentproto.RequestOption{
			OptionID: option.ID,
			Label:    option.Label,
			Style:    permissionOptionStyle(option),
		})
	}
	result.Events = append(result.Events, agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  sessionID,
		TurnID:    pending.TurnID,
		RequestID: requestID,
		Status:    "pending",
		RequestPrompt: &agentproto.RequestPrompt{
			Type:         agentproto.RequestTypeApproval,
			RawType:      "session/request_permission",
			Title:        xutil.FirstNonEmpty(xutil.LookupStringFromAny(toolCall["title"]), "OpenCode permission request"),
			ItemID:       toolID,
			AcceptLabel:  "Allow",
			DeclineLabel: "Reject",
			Options:      promptOptions,
			Permissions: &agentproto.PermissionsRequestPrompt{
				Permissions: []map[string]any{xutil.CloneMap(toolCall)},
			},
		},
	})
	return result, nil
}

func (t *Translator) observeWriteTextFile(frame map[string]any, result Result) (Result, error) {
	params, _ := frame["params"].(map[string]any)
	sessionID := strings.TrimSpace(xutil.LookupStringFromAny(params["sessionId"]))
	rawPath := strings.TrimSpace(xutil.LookupStringFromAny(params["path"]))
	content := xutil.LookupStringFromAny(params["content"])
	if !t.hasWriteApproval(sessionID) {
		return t.rejectClientRequest(frame, result, -32000, "fs/write_text_file requires an approved OpenCode permission request")
	}
	workspace := t.sessionWorkspace(sessionID)
	targetPath, relPath, err := resolveWorkspaceWritePath(workspace, rawPath)
	if err != nil {
		return t.rejectClientRequest(frame, result, -32000, err.Error())
	}
	oldContent, readErr := os.ReadFile(targetPath)
	if readErr != nil && !os.IsNotExist(readErr) {
		return t.rejectClientRequest(frame, result, -32000, readErr.Error())
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return t.rejectClientRequest(frame, result, -32000, err.Error())
	}
	if err := os.WriteFile(targetPath, []byte(content), 0o644); err != nil {
		return t.rejectClientRequest(frame, result, -32000, err.Error())
	}
	t.consumeWriteApproval(sessionID)
	changeKind := agentproto.FileChangeUpdate
	if os.IsNotExist(readErr) {
		changeKind = agentproto.FileChangeAdd
	}
	turn := t.ensureTurnForSession(sessionID)
	event := agentproto.Event{
		Kind:     agentproto.EventItemFileChangePatchUpdated,
		ThreadID: sessionID,
		ItemID:   "opencode-write-" + sanitizeID(relPath),
		ItemKind: "file_change",
		FileChanges: []agentproto.FileChangeRecord{{
			Path: relPath,
			Kind: changeKind,
			Diff: simpleTextDiff(relPath, string(oldContent), content),
		}},
	}
	if turn != nil {
		event.TurnID = turn.TurnID
		event = t.annotateTurnEvent(turn, event)
	}
	response, err := marshalLine(map[string]any{
		"jsonrpc": "2.0",
		"id":      frame["id"],
		"result":  map[string]any{},
	})
	if err != nil {
		return Result{}, err
	}
	result.OutboundToChild = append(result.OutboundToChild, response)
	result.Events = append(result.Events, event)
	return result, nil
}

func (t *Translator) hasWriteApproval(sessionID string) bool {
	approval := t.writeApprovals[sessionID]
	return approval.Always || approval.Remaining > 0
}

func (t *Translator) consumeWriteApproval(sessionID string) {
	approval := t.writeApprovals[sessionID]
	if approval.Always {
		return
	}
	if approval.Remaining <= 1 {
		delete(t.writeApprovals, sessionID)
		return
	}
	approval.Remaining--
	t.writeApprovals[sessionID] = approval
}

func (t *Translator) rejectClientRequest(frame map[string]any, result Result, code int, message string) (Result, error) {
	response, err := marshalLine(map[string]any{
		"jsonrpc": "2.0",
		"id":      frame["id"],
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	})
	if err != nil {
		return Result{}, err
	}
	result.OutboundToChild = append(result.OutboundToChild, response)
	return result, nil
}

func (t *Translator) observeUsageUpdate(sessionID string, update map[string]any) {
	used := xutil.LookupIntFromAny(update["used"])
	size := xutil.LookupIntFromAny(update["size"])
	if used == 0 && size == 0 {
		return
	}
	t.debugf("opencode usage_update session=%s used=%d size=%d payload=%s", sessionID, used, size, xutil.CompactJSON(update))
}

func (t *Translator) observeSessionInfoUpdate(sessionID string, update map[string]any) {
	session := t.sessions[sessionID]
	session.ID = sessionID
	if info, _ := update["sessionInfo"].(map[string]any); info != nil {
		session.Title = xutil.FirstNonEmpty(xutil.LookupStringFromAny(info["title"]), session.Title)
		session.CWD = xutil.FirstNonEmpty(xutil.LookupStringFromAny(info["cwd"]), session.CWD)
	}
	t.sessions[sessionID] = session
}

func (t *Translator) observeConfigOptionUpdate(sessionID string, update map[string]any) (agentproto.Event, bool) {
	option, _ := update["configOption"].(map[string]any)
	if option == nil {
		option, _ = update["option"].(map[string]any)
	}
	if option == nil {
		t.debugf("opencode config_option_update session=%s payload=%s", sessionID, xutil.CompactJSON(update))
		return agentproto.Event{}, false
	}
	session := t.sessions[sessionID]
	session.ID = sessionID
	session.CWD = xutil.FirstNonEmpty(session.CWD, t.workspaceRoot)
	session.ConfigOptions = upsertConfigOption(session.ConfigOptions, option)
	session.ModelOptions, session.CurrentModel, session.CurrentMode = parseConfigOptions(session.ConfigOptions)
	t.sessions[sessionID] = session
	t.debugf("opencode config_option_update session=%s option=%s", sessionID, xutil.CompactJSON(option))
	if strings.TrimSpace(xutil.LookupStringFromAny(option["id"])) != "model" || strings.TrimSpace(session.CurrentModel) == "" {
		return agentproto.Event{}, false
	}
	settings := agentproto.NormalizeThreadSettingsUpdate(&agentproto.ThreadSettingsUpdate{
		ThreadID: sessionID,
		Model:    session.CurrentModel,
	})
	if settings == nil {
		return agentproto.Event{}, false
	}
	return agentproto.Event{
		Kind:           agentproto.EventThreadSettingsUpdated,
		ThreadID:       sessionID,
		ThreadSettings: settings,
	}, true
}

func (t *Translator) observeCurrentModeUpdate(sessionID string, update map[string]any) {
	mode := strings.TrimSpace(xutil.FirstNonEmpty(
		xutil.LookupStringFromAny(update["currentMode"]),
		xutil.LookupStringFromAny(update["mode"]),
		xutil.LookupStringFromAny(update["value"]),
	))
	if mode != "" {
		session := t.sessions[sessionID]
		session.ID = sessionID
		session.CWD = xutil.FirstNonEmpty(session.CWD, t.workspaceRoot)
		session.CurrentMode = mode
		t.sessions[sessionID] = session
	}
	t.debugf("opencode current_mode_update session=%s payload=%s", sessionID, xutil.CompactJSON(update))
}

func (t *Translator) updateUsage(sessionID string, last agentproto.TokenUsageBreakdown) agentproto.Event {
	usage := t.threadUsage[sessionID]
	if usage == nil {
		usage = &agentproto.ThreadTokenUsage{}
	}
	usage.Last = last
	usage.Total.InputTokens += last.InputTokens
	usage.Total.CachedInputTokens += last.CachedInputTokens
	usage.Total.OutputTokens += last.OutputTokens
	usage.Total.ReasoningOutputTokens += last.ReasoningOutputTokens
	usage.Total.TotalTokens += last.TotalTokens
	t.threadUsage[sessionID] = usage
	return agentproto.Event{
		Kind:       agentproto.EventThreadTokenUsageUpdated,
		ThreadID:   sessionID,
		TokenUsage: agentproto.CloneThreadTokenUsage(usage),
	}
}
