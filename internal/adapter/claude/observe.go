package claude

import (
	"sort"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func (t *Translator) observeSystemMessage(message map[string]any) Result {
	subtype := strings.TrimSpace(lookupStringFromAny(message["subtype"]))
	previousModel := strings.TrimSpace(t.model)
	previousCWD := strings.TrimSpace(t.cwd)
	previousPermissionMode := strings.TrimSpace(t.permissionMode)
	switch subtype {
	case "init":
		t.awaitingInit = false
		if sessionID := strings.TrimSpace(lookupStringFromAny(message["session_id"])); sessionID != "" {
			previousSessionID := strings.TrimSpace(t.sessionID)
			t.sessionID = sessionID
			if t.activeTurn != nil && shouldRefreshTurnThreadIDOnInit(t.activeTurn, previousSessionID) {
				t.activeTurn.ThreadID = sessionID
			}
			for _, turn := range t.pendingTurns {
				if turn != nil && shouldRefreshTurnThreadIDOnInit(turn, previousSessionID) {
					turn.ThreadID = sessionID
				}
			}
		}
		t.model = strings.TrimSpace(lookupStringFromAny(message["model"]))
		t.cwd = strings.TrimSpace(lookupStringFromAny(message["cwd"]))
		t.permissionMode = firstNonEmptyString(
			lookupStringFromAny(message["permissionMode"]),
			t.permissionMode,
		)
	case "status":
		if mode := strings.TrimSpace(lookupStringFromAny(message["permissionMode"])); mode != "" {
			t.permissionMode = mode
		}
	}
	events := t.observedPermissionConfigEvents(previousModel, previousCWD, previousPermissionMode)
	return Result{Events: events}
}

func (t *Translator) observedPermissionConfigEvents(previousModel, previousCWD, previousPermissionMode string) []agentproto.Event {
	threadID := strings.TrimSpace(t.sessionID)
	if threadID == "" {
		return nil
	}
	model := strings.TrimSpace(t.model)
	cwd := strings.TrimSpace(t.cwd)
	nativeMode := strings.TrimSpace(t.permissionMode)
	selection := claudePermissionSelectionFromNative(nativeMode)
	changed := model != strings.TrimSpace(previousModel) ||
		cwd != strings.TrimSpace(previousCWD) ||
		nativeMode != strings.TrimSpace(previousPermissionMode)
	if !changed && selection.AccessMode == "" && strings.TrimSpace(selection.PlanMode) == "" {
		return nil
	}
	return []agentproto.Event{{
		Kind:        agentproto.EventConfigObserved,
		ThreadID:    threadID,
		CWD:         cwd,
		Model:       model,
		AccessMode:  selection.AccessMode,
		PlanMode:    selection.PlanMode,
		ConfigScope: "thread",
	}}
}

func shouldRefreshTurnThreadIDOnInit(turn *turnState, previousSessionID string) bool {
	if turn == nil {
		return false
	}
	threadID := strings.TrimSpace(turn.ThreadID)
	if threadID == "" {
		return true
	}
	if turn.Started {
		return false
	}
	return threadID == strings.TrimSpace(previousSessionID)
}

func (t *Translator) observeStreamMessage(message map[string]any) Result {
	event := lookupMap(message, "event")
	switch strings.TrimSpace(lookupStringFromAny(event["type"])) {
	case "message_start":
		return t.observeMessageStart(event)
	case "content_block_start":
		return t.observeContentBlockStart(event)
	case "content_block_delta":
		return t.observeContentBlockDelta(event)
	case "content_block_stop":
		return t.observeContentBlockStop(event)
	default:
		return Result{}
	}
}

func (t *Translator) observeMessageStart(event map[string]any) Result {
	message := lookupMap(event, "message")
	t.currentMessage = &messageState{
		ID:              strings.TrimSpace(lookupStringFromAny(message["id"])),
		ParentToolUseID: strings.TrimSpace(lookupStringFromAny(event["parent_tool_use_id"])),
		Blocks:          map[int]*blockState{},
	}
	events := t.startActiveTurnIfNeeded()
	if t.activeTurn == nil {
		return Result{}
	}
	return Result{Events: events}
}

func (t *Translator) observeContentBlockStart(event map[string]any) Result {
	if t.currentMessage == nil {
		t.currentMessage = &messageState{Blocks: map[int]*blockState{}}
	}
	index := lookupIntFromAny(event["index"])
	block := lookupMap(event, "content_block")
	kind := strings.TrimSpace(lookupStringFromAny(block["type"]))
	state := &blockState{
		Index:     index,
		Kind:      kind,
		ToolUseID: strings.TrimSpace(lookupStringFromAny(block["id"])),
		ToolName:  strings.TrimSpace(lookupStringFromAny(block["name"])),
	}
	var events []agentproto.Event
	if kind == "text" && t.activeTurn != nil {
		state.ItemID = t.nextItemID()
		state.StartedEmitted = true
		events = append(events, agentproto.Event{
			Kind:      agentproto.EventItemStarted,
			CommandID: t.activeTurn.CommandID,
			ThreadID:  t.activeTurn.ThreadID,
			TurnID:    t.activeTurn.TurnID,
			ItemID:    state.ItemID,
			ItemKind:  "agent_message",
		})
	}
	if kind == "thinking" && t.activeTurn != nil {
		state.ItemID = t.nextItemID()
		state.StartedEmitted = true
		state.ReasoningFilter = newThinkingFilterState()
		events = append(events, agentproto.Event{
			Kind:      agentproto.EventItemStarted,
			CommandID: t.activeTurn.CommandID,
			ThreadID:  t.activeTurn.ThreadID,
			TurnID:    t.activeTurn.TurnID,
			ItemID:    state.ItemID,
			ItemKind:  "reasoning_summary",
		})
	}
	if kind == "tool_use" {
		tool := &toolState{
			ToolUseID:       state.ToolUseID,
			ParentToolUseID: t.currentParentToolUseID(),
			ItemID:          t.nextItemID(),
			Name:            state.ToolName,
			Input:           map[string]any{},
			Internal:        isInternalInteractionTool(state.ToolName),
		}
		if t.activeTurn != nil {
			tool.TurnID = t.activeTurn.TurnID
		}
		t.toolStates[tool.ToolUseID] = tool
	}
	t.currentMessage.Blocks[index] = state
	return Result{Events: events}
}

func (t *Translator) observeContentBlockDelta(event map[string]any) Result {
	if t.currentMessage == nil {
		return Result{}
	}
	index := lookupIntFromAny(event["index"])
	state := t.currentMessage.Blocks[index]
	if state == nil {
		return Result{}
	}
	delta := lookupMap(event, "delta")
	switch strings.TrimSpace(lookupStringFromAny(delta["type"])) {
	case "text_delta":
		if t.activeTurn == nil || state.ItemID == "" {
			return Result{}
		}
		text := lookupStringFromAny(delta["text"])
		state.TextBuffer += text
		return Result{
			Events: []agentproto.Event{{
				Kind:      agentproto.EventItemDelta,
				CommandID: t.activeTurn.CommandID,
				ThreadID:  t.activeTurn.ThreadID,
				TurnID:    t.activeTurn.TurnID,
				ItemID:    state.ItemID,
				ItemKind:  "agent_message",
				Delta:     text,
			}},
		}
	case "thinking_delta":
		if t.activeTurn == nil || state.ItemID == "" {
			return Result{}
		}
		thinking := lookupStringFromAny(delta["thinking"])
		if thinking == "" {
			return Result{}
		}
		filtered := filterClaudeThinkingDelta(state.ReasoningFilter, thinking)
		if filtered == "" {
			return Result{}
		}
		state.TextBuffer += filtered
		return Result{
			Events: []agentproto.Event{t.newReasoningSummaryDeltaEvent(state.ItemID, filtered)},
		}
	case "signature_delta":
		return Result{}
	case "input_json_delta":
		state.ToolInputDelta += lookupStringFromAny(delta["partial_json"])
	}
	return Result{}
}

func (t *Translator) observeContentBlockStop(event map[string]any) Result {
	if t.currentMessage == nil || t.activeTurn == nil {
		return Result{}
	}
	index := lookupIntFromAny(event["index"])
	state := t.currentMessage.Blocks[index]
	if state == nil || state.Completed || state.ItemID == "" {
		return Result{}
	}
	if state.Kind == "thinking" {
		state.Completed = true
		if trailing := finalizeClaudeThinkingFilter(state.ReasoningFilter); trailing != "" {
			state.TextBuffer += trailing
			return Result{
				Events: []agentproto.Event{
					t.newReasoningSummaryDeltaEvent(state.ItemID, trailing),
					t.newReasoningSummaryCompletedEvent(state.ItemID),
				},
			}
		}
		return Result{
			Events: []agentproto.Event{t.newReasoningSummaryCompletedEvent(state.ItemID)},
		}
	}
	if state.Kind != "text" {
		return Result{}
	}
	state.Completed = true
	t.activeTurn.LastAssistantText = state.TextBuffer
	t.activeTurn.AgentMessageCompleted = true
	return Result{
		Events: []agentproto.Event{{
			Kind:      agentproto.EventItemCompleted,
			CommandID: t.activeTurn.CommandID,
			ThreadID:  t.activeTurn.ThreadID,
			TurnID:    t.activeTurn.TurnID,
			ItemID:    state.ItemID,
			ItemKind:  "agent_message",
			Status:    "completed",
			Metadata:  map[string]any{"text": state.TextBuffer},
		}},
	}
}

func (t *Translator) observeAssistantMessage(message map[string]any) Result {
	events := t.startActiveTurnIfNeeded()
	if t.activeTurn == nil {
		return Result{Events: events}
	}
	t.syncObservedMessageParent(message)
	content := mapsFromAny(lookupMap(message, "message")["content"])
	if len(content) == 0 {
		return Result{Events: events}
	}
	textBlockCount := 0
	for _, block := range content {
		if strings.TrimSpace(lookupStringFromAny(block["type"])) == "text" {
			textBlockCount++
		}
	}
	textOrdinal := 0
	claimedTextBlocks := map[*blockState]bool{}
	for index, block := range content {
		switch strings.TrimSpace(lookupStringFromAny(block["type"])) {
		case "text":
			text := lookupStringFromAny(block["text"])
			events = append(events, t.completeAssistantText(index, textOrdinal, textBlockCount, text, claimedTextBlocks)...)
			textOrdinal++
		case "tool_use":
			events = append(events, t.finalizeToolUse(block)...)
		}
	}
	return Result{Events: events}
}

func (t *Translator) completeAssistantText(index, textOrdinal, totalTextBlocks int, text string, claimed map[*blockState]bool) []agentproto.Event {
	if t.activeTurn == nil {
		return nil
	}
	state := t.resolveAssistantTextBlock(index, textOrdinal, totalTextBlocks, text, claimed)
	if state == nil {
		state = &blockState{
			Index:  index,
			Kind:   "text",
			ItemID: t.nextItemID(),
		}
	}
	if claimed != nil {
		claimed[state] = true
	}
	return t.finishAssistantTextBlock(state, text)
}

func (t *Translator) finishAssistantTextBlock(state *blockState, text string) []agentproto.Event {
	if t.activeTurn == nil || state == nil {
		return nil
	}
	if state.ItemID == "" {
		state.ItemID = t.nextItemID()
	}
	events := make([]agentproto.Event, 0, 2)
	if !state.StartedEmitted {
		events = append(events, agentproto.Event{
			Kind:      agentproto.EventItemStarted,
			CommandID: t.activeTurn.CommandID,
			ThreadID:  t.activeTurn.ThreadID,
			TurnID:    t.activeTurn.TurnID,
			ItemID:    state.ItemID,
			ItemKind:  "agent_message",
		})
		state.StartedEmitted = true
	}
	if state.Completed {
		if strings.TrimSpace(text) != "" {
			state.TextBuffer = text
		}
		t.activeTurn.LastAssistantText = state.TextBuffer
		t.activeTurn.AgentMessageCompleted = true
		return events
	}
	state.Completed = true
	if strings.TrimSpace(text) != "" {
		state.TextBuffer = text
	}
	t.activeTurn.LastAssistantText = state.TextBuffer
	t.activeTurn.AgentMessageCompleted = true
	events = append(events, agentproto.Event{
		Kind:      agentproto.EventItemCompleted,
		CommandID: t.activeTurn.CommandID,
		ThreadID:  t.activeTurn.ThreadID,
		TurnID:    t.activeTurn.TurnID,
		ItemID:    state.ItemID,
		ItemKind:  "agent_message",
		Status:    "completed",
		Metadata:  map[string]any{"text": state.TextBuffer},
	})
	return events
}

func (t *Translator) resolveAssistantTextBlock(index, textOrdinal, totalTextBlocks int, text string, claimed map[*blockState]bool) *blockState {
	if t.currentMessage == nil {
		return nil
	}
	if state := t.currentMessage.Blocks[index]; state != nil && state.Kind == "text" && !claimed[state] {
		return state
	}
	textBlocks := assistantTextBlocks(t.currentMessage)
	if len(textBlocks) == 0 {
		return nil
	}
	if totalTextBlocks == 1 {
		if state := assistantLastUnclaimedTextBlock(textBlocks, claimed); state != nil {
			return state
		}
	}
	if totalTextBlocks > 1 {
		if state := assistantTextBlockByOrdinal(textBlocks, textOrdinal, claimed); state != nil {
			return state
		}
	}
	if state := assistantLastPendingTextBlock(textBlocks, claimed); state != nil {
		return state
	}
	if state := assistantLastMatchingCompletedTextBlock(textBlocks, text, claimed); state != nil {
		return state
	}
	return nil
}

func assistantTextBlocks(message *messageState) []*blockState {
	if message == nil || len(message.Blocks) == 0 {
		return nil
	}
	blocks := make([]*blockState, 0, len(message.Blocks))
	for _, state := range message.Blocks {
		if state == nil || state.Kind != "text" {
			continue
		}
		blocks = append(blocks, state)
	}
	sort.Slice(blocks, func(i, j int) bool {
		return blocks[i].Index < blocks[j].Index
	})
	return blocks
}

func assistantTextBlockByOrdinal(blocks []*blockState, ordinal int, claimed map[*blockState]bool) *blockState {
	if ordinal < 0 {
		return nil
	}
	seen := 0
	for _, state := range blocks {
		if claimed[state] {
			continue
		}
		if seen == ordinal {
			return state
		}
		seen++
	}
	return nil
}

func assistantLastUnclaimedTextBlock(blocks []*blockState, claimed map[*blockState]bool) *blockState {
	for i := len(blocks) - 1; i >= 0; i-- {
		state := blocks[i]
		if claimed[state] {
			continue
		}
		return state
	}
	return nil
}

func assistantLastPendingTextBlock(blocks []*blockState, claimed map[*blockState]bool) *blockState {
	for i := len(blocks) - 1; i >= 0; i-- {
		state := blocks[i]
		if claimed[state] || state.Completed {
			continue
		}
		return state
	}
	return nil
}

func assistantLastMatchingCompletedTextBlock(blocks []*blockState, text string, claimed map[*blockState]bool) *blockState {
	needle := strings.TrimSpace(text)
	if needle == "" {
		return nil
	}
	for i := len(blocks) - 1; i >= 0; i-- {
		state := blocks[i]
		if claimed[state] || !state.Completed {
			continue
		}
		if strings.TrimSpace(state.TextBuffer) == needle {
			return state
		}
	}
	return nil
}

func (t *Translator) finalizeToolUse(block map[string]any) []agentproto.Event {
	if t.activeTurn == nil {
		return nil
	}
	toolUseID := strings.TrimSpace(lookupStringFromAny(block["id"]))
	tool := t.toolStates[toolUseID]
	if tool == nil {
		tool = &toolState{
			ToolUseID:       toolUseID,
			ParentToolUseID: t.currentParentToolUseID(),
			ItemID:          t.nextItemID(),
			Name:            strings.TrimSpace(lookupStringFromAny(block["name"])),
			Input:           map[string]any{},
			Internal:        isInternalInteractionTool(lookupStringFromAny(block["name"])),
			TurnID:          t.activeTurn.TurnID,
		}
		t.toolStates[toolUseID] = tool
	}
	if strings.TrimSpace(tool.ParentToolUseID) == "" {
		tool.ParentToolUseID = t.currentParentToolUseID()
	}
	tool.Name = firstNonEmptyString(lookupStringFromAny(block["name"]), tool.Name)
	tool.Input = cloneMap(lookupMap(block, "input"))
	if strings.TrimSpace(tool.Name) == "TodoWrite" {
		tool.StartedEmitted = true
		return nil
	}
	if tool.Internal || tool.StartedEmitted || !claudeToolVisibleLifecycle(tool.Name) {
		return nil
	}
	tool.StartedEmitted = true
	itemKind := claudeToolItemKind(tool.Name)
	metadata := claudeToolMetadata(tool.Name, tool.Input)
	event := agentproto.Event{
		Kind:      agentproto.EventItemStarted,
		CommandID: t.activeTurn.CommandID,
		ThreadID:  t.activeTurn.ThreadID,
		TurnID:    t.activeTurn.TurnID,
		ItemID:    tool.ItemID,
		ItemKind:  itemKind,
		Metadata:  metadata,
	}
	if itemKind == "file_change" {
		event.FileChanges = claudeToolFileChanges(metadata)
	}
	return []agentproto.Event{event}
}

func (t *Translator) observeUserMessage(message map[string]any) Result {
	if t.activeTurn == nil {
		return Result{}
	}
	t.syncObservedMessageParent(message)
	blocks := mapsFromAny(lookupMap(message, "message")["content"])
	if toolResult := firstToolResultBlock(blocks); toolResult != nil {
		return t.observeToolResult(message, toolResult)
	}
	if textBlock := firstTextBlock(blocks); textBlock != nil {
		text := strings.TrimSpace(lookupStringFromAny(textBlock["text"]))
		if strings.HasPrefix(text, "[Request interrupted by user") {
			t.activeTurn.InterruptRequested = true
		}
	}
	return Result{}
}

func (t *Translator) observeToolResult(message, block map[string]any) Result {
	toolUseID := strings.TrimSpace(lookupStringFromAny(block["tool_use_id"]))
	tool := t.toolStates[toolUseID]
	if tool != nil && tool.Internal {
		return t.observeInternalToolResult(message, block, tool)
	}
	return t.observeExternalToolResult(message, block, tool)
}

func (t *Translator) observeExternalToolResult(message, block map[string]any, tool *toolState) Result {
	if t.activeTurn == nil {
		return Result{}
	}
	if tool == nil {
		tool = &toolState{
			ToolUseID:       strings.TrimSpace(lookupStringFromAny(block["tool_use_id"])),
			ParentToolUseID: t.currentParentToolUseID(),
			ItemID:          t.nextItemID(),
			Name:            "tool",
			Input:           map[string]any{},
			TurnID:          t.activeTurn.TurnID,
		}
		t.toolStates[tool.ToolUseID] = tool
	}
	if strings.TrimSpace(tool.ParentToolUseID) == "" {
		tool.ParentToolUseID = t.currentParentToolUseID()
	}
	events := make([]agentproto.Event, 0, 2)
	itemKind := claudeToolItemKind(tool.Name)
	startMetadata := claudeToolMetadata(tool.Name, tool.Input)
	if !tool.StartedEmitted && claudeToolVisibleLifecycle(tool.Name) {
		tool.StartedEmitted = true
		event := agentproto.Event{
			Kind:      agentproto.EventItemStarted,
			CommandID: t.activeTurn.CommandID,
			ThreadID:  t.activeTurn.ThreadID,
			TurnID:    t.activeTurn.TurnID,
			ItemID:    tool.ItemID,
			ItemKind:  itemKind,
			Metadata:  startMetadata,
		}
		if itemKind == "file_change" {
			event.FileChanges = claudeToolFileChanges(startMetadata)
		}
		events = append(events, event)
	}
	metadata := claudeToolMetadata(tool.Name, tool.Input)
	if text := stringifyTextContent(block["content"]); strings.TrimSpace(text) != "" {
		metadata["text"] = text
	}
	switch rawToolResult := message["tool_use_result"].(type) {
	case map[string]any:
		for key, value := range rawToolResult {
			metadata[key] = cloneJSONValue(value)
		}
		if itemKind == "file_change" {
			mergeClaudeFileChangeMetadataPayload(metadata, tool.Name, rawToolResult)
		}
	case string:
		if strings.TrimSpace(rawToolResult) != "" {
			metadata["toolUseResult"] = strings.TrimSpace(rawToolResult)
		}
	}
	isError := lookupBoolFromAny(block["is_error"])
	if completionEvent, ok := t.hiddenClaudeToolLifecycleEvent(tool, itemKind, isError, metadata); ok {
		events = append(events, completionEvent)
	} else if claudeToolVisibleLifecycle(tool.Name) && itemKind != "" {
		event := agentproto.Event{
			Kind:      agentproto.EventItemCompleted,
			CommandID: t.activeTurn.CommandID,
			ThreadID:  t.activeTurn.ThreadID,
			TurnID:    t.activeTurn.TurnID,
			ItemID:    tool.ItemID,
			ItemKind:  itemKind,
			Status: map[bool]string{
				true:  "failed",
				false: "completed",
			}[isError],
			Metadata: metadata,
		}
		if itemKind == "file_change" {
			event.FileChanges = claudeToolFileChanges(metadata)
		}
		events = append(events, event)
	}
	tool.Completed = true
	if resolved, ok := t.resolvePendingRequestForToolResult(message, block, tool.ToolUseID); ok {
		events = append(events, resolved)
	}
	return Result{Events: events}
}

func (t *Translator) observeInternalToolResult(message, block map[string]any, tool *toolState) Result {
	if resolved, ok := t.resolvePendingRequestForToolResult(message, block, tool.ToolUseID); ok {
		return Result{
			Events: []agentproto.Event{resolved},
		}
	}
	return Result{}
}

func (t *Translator) observeControlRequest(message map[string]any) Result {
	startEvents := t.startActiveTurnIfNeeded()
	if t.activeTurn == nil {
		return Result{Events: startEvents}
	}
	requestID := strings.TrimSpace(lookupStringFromAny(message["request_id"]))
	request := lookupMap(message, "request")
	if strings.TrimSpace(lookupStringFromAny(request["subtype"])) != "can_use_tool" {
		return Result{Events: startEvents}
	}
	toolName := strings.TrimSpace(lookupStringFromAny(request["tool_name"]))
	toolUseID := strings.TrimSpace(lookupStringFromAny(request["tool_use_id"]))
	input := cloneMap(lookupMap(request, "input"))
	itemID := ""
	if tool := t.toolStates[toolUseID]; tool != nil {
		itemID = tool.ItemID
	}
	var result Result
	switch toolName {
	case "AskUserQuestion":
		result = t.observeAskUserQuestionRequest(requestID, toolName, toolUseID, itemID, input)
	case "ExitPlanMode":
		result = t.observePlanConfirmationRequest(requestID, toolName, toolUseID, input)
	default:
		result = t.observeToolApprovalRequest(requestID, toolName, toolUseID, itemID, request, input)
	}
	if len(startEvents) != 0 {
		result.Events = append(startEvents, result.Events...)
	}
	return result
}

func (t *Translator) observeAskUserQuestionRequest(requestID, toolName, toolUseID, itemID string, input map[string]any) Result {
	questions := make([]agentproto.RequestQuestion, 0)
	pendingQuestions := make([]pendingQuestion, 0)
	for index, record := range mapsFromAny(input["questions"]) {
		question := agentproto.RequestQuestion{
			ID:         sanitizeQuestionID(lookupStringFromAny(record["id"]), index),
			Header:     strings.TrimSpace(lookupStringFromAny(record["header"])),
			Question:   strings.TrimSpace(lookupStringFromAny(record["question"])),
			AllowOther: lookupBoolFromAny(record["allowOther"]),
			Secret:     lookupBoolFromAny(record["secret"]),
		}
		for _, option := range mapsFromAny(record["options"]) {
			question.Options = append(question.Options, agentproto.RequestQuestionOption{
				Label:       strings.TrimSpace(lookupStringFromAny(option["label"])),
				Description: strings.TrimSpace(lookupStringFromAny(option["description"])),
			})
		}
		questions = append(questions, question)
		pendingQuestions = append(pendingQuestions, pendingQuestion{
			ID:       question.ID,
			Header:   question.Header,
			Question: question.Question,
		})
	}
	prompt := &agentproto.RequestPrompt{
		Type:      agentproto.RequestTypeRequestUserInput,
		RawType:   "AskUserQuestion",
		Body:      "Claude 需要你补充答案后才能继续。",
		ItemID:    itemID,
		Questions: questions,
	}
	metadata := map[string]any{
		"requestType":   "request_user_input",
		"requestKind":   "AskUserQuestion",
		"requestMethod": "tool/AskUserQuestion",
		"toolName":      toolName,
		"questions":     buildQuestionMetadata(questions),
	}
	if itemID != "" {
		metadata["itemId"] = itemID
	}
	if sourceContextLabel := t.requestSourceContextLabel(toolUseID); sourceContextLabel != "" {
		metadata["sourceContextLabel"] = sourceContextLabel
	}
	request := &pendingRequest{
		RequestID:    requestID,
		ThreadID:     t.activeTurn.ThreadID,
		TurnID:       t.activeTurn.TurnID,
		RequestType:  agentproto.RequestTypeRequestUserInput,
		SemanticKind: control.RequestSemanticRequestUserInput,
		ToolName:     toolName,
		ToolUseID:    toolUseID,
		Input:        input,
		ItemID:       itemID,
		Questions:    pendingQuestions,
	}
	t.pendingRequests[requestID] = request
	return Result{
		Events: []agentproto.Event{{
			Kind:          agentproto.EventRequestStarted,
			CommandID:     t.activeTurn.CommandID,
			ThreadID:      t.activeTurn.ThreadID,
			TurnID:        t.activeTurn.TurnID,
			RequestID:     requestID,
			RequestPrompt: prompt,
			Metadata:      metadata,
		}},
	}
}

func (t *Translator) observePlanConfirmationRequest(requestID, toolName, toolUseID string, input map[string]any) Result {
	planBody := strings.TrimSpace(lookupStringFromAny(input["plan"]))
	planBodySource := ""
	if planBody != "" {
		planBodySource = "request.input.plan"
	}
	body := planBody
	if body == "" {
		body = "Claude 计划如下，请确认后继续。"
	}
	prompt := &agentproto.RequestPrompt{
		Type:         agentproto.RequestTypeApproval,
		RawType:      "ExitPlanMode",
		Body:         body,
		AcceptLabel:  "批准",
		DeclineLabel: "拒绝",
	}
	metadata := map[string]any{
		"requestType":   "approval",
		"requestKind":   "ExitPlanMode",
		"requestMethod": "tool/ExitPlanMode",
		"toolName":      toolName,
		"body":          body,
	}
	if planBodySource != "" {
		metadata["planBodySource"] = planBodySource
	}
	if sourceContextLabel := t.requestSourceContextLabel(toolUseID); sourceContextLabel != "" {
		metadata["sourceContextLabel"] = sourceContextLabel
	}
	request := &pendingRequest{
		RequestID:          requestID,
		ThreadID:           t.activeTurn.ThreadID,
		TurnID:             t.activeTurn.TurnID,
		RequestType:        agentproto.RequestTypeApproval,
		SemanticKind:       control.RequestSemanticPlanConfirmation,
		ToolName:           toolName,
		ToolUseID:          toolUseID,
		Input:              input,
		PlanBody:           planBody,
		PlanBodySource:     planBodySource,
		InterruptOnDecline: true,
	}
	t.pendingRequests[requestID] = request
	return Result{
		Events: []agentproto.Event{{
			Kind:          agentproto.EventRequestStarted,
			CommandID:     t.activeTurn.CommandID,
			ThreadID:      t.activeTurn.ThreadID,
			TurnID:        t.activeTurn.TurnID,
			RequestID:     requestID,
			RequestPrompt: prompt,
			Metadata:      metadata,
		}},
	}
}

func (t *Translator) observeToolApprovalRequest(requestID, toolName, toolUseID, itemID string, rawRequest, input map[string]any) Result {
	prompt := &agentproto.RequestPrompt{
		Type:         agentproto.RequestTypeApproval,
		RawType:      "can_use_tool",
		Body:         approvalRequestBody(toolName, input),
		ItemID:       itemID,
		AcceptLabel:  "允许一次",
		DeclineLabel: "拒绝",
	}
	metadata := map[string]any{
		"requestType":           "approval",
		"requestKind":           "can_use_tool",
		"requestMethod":         "control_request/can_use_tool",
		"toolName":              toolName,
		"body":                  prompt.Body,
		"permissionSuggestions": encodeMetadataMapList(mapsFromAny(rawRequest["permission_suggestions"])),
	}
	if itemID != "" {
		metadata["itemId"] = itemID
	}
	if blockedPath := strings.TrimSpace(lookupStringFromAny(rawRequest["blocked_path"])); blockedPath != "" {
		metadata["blockedPath"] = blockedPath
	}
	if sourceContextLabel := t.requestSourceContextLabel(toolUseID); sourceContextLabel != "" {
		metadata["sourceContextLabel"] = sourceContextLabel
	}
	request := &pendingRequest{
		RequestID:    requestID,
		ThreadID:     t.activeTurn.ThreadID,
		TurnID:       t.activeTurn.TurnID,
		RequestType:  agentproto.RequestTypeApproval,
		SemanticKind: control.RequestSemanticApprovalCanUseTool,
		ToolName:     toolName,
		ToolUseID:    toolUseID,
		Input:        input,
		ItemID:       itemID,
	}
	t.pendingRequests[requestID] = request
	return Result{
		Events: []agentproto.Event{{
			Kind:          agentproto.EventRequestStarted,
			CommandID:     t.activeTurn.CommandID,
			ThreadID:      t.activeTurn.ThreadID,
			TurnID:        t.activeTurn.TurnID,
			RequestID:     requestID,
			RequestPrompt: prompt,
			Metadata:      metadata,
		}},
	}
}

func (t *Translator) observeControlResponse(message map[string]any) Result {
	response := lookupMap(message, "response")
	requestID := strings.TrimSpace(lookupStringFromAny(response["request_id"]))
	if requestID == "" {
		return Result{}
	}
	pending, ok := t.pendingControlReplies[requestID]
	if !ok {
		return Result{}
	}
	delete(t.pendingControlReplies, requestID)
	var events []agentproto.Event
	if pending.Kind == "set_permission_mode" && strings.TrimSpace(pending.DesiredPermissionMode) != "" {
		previousPermissionMode := strings.TrimSpace(t.permissionMode)
		t.permissionMode = strings.TrimSpace(pending.DesiredPermissionMode)
		events = t.observedPermissionConfigEvents(strings.TrimSpace(t.model), strings.TrimSpace(t.cwd), previousPermissionMode)
	}
	return Result{Events: events, Suppress: true}
}

func (t *Translator) observeResultMessage(message map[string]any) Result {
	events := t.startActiveTurnIfNeeded()
	if t.activeTurn == nil {
		return Result{Events: events}
	}
	if !t.activeTurn.AgentMessageCompleted {
		if text := strings.TrimSpace(lookupStringFromAny(message["result"])); text != "" {
			itemID := t.nextItemID()
			t.activeTurn.AgentMessageCompleted = true
			t.activeTurn.LastAssistantText = text
			events = append(events,
				agentproto.Event{
					Kind:      agentproto.EventItemStarted,
					CommandID: t.activeTurn.CommandID,
					ThreadID:  t.activeTurn.ThreadID,
					TurnID:    t.activeTurn.TurnID,
					ItemID:    itemID,
					ItemKind:  "agent_message",
				},
				agentproto.Event{
					Kind:      agentproto.EventItemCompleted,
					CommandID: t.activeTurn.CommandID,
					ThreadID:  t.activeTurn.ThreadID,
					TurnID:    t.activeTurn.TurnID,
					ItemID:    itemID,
					ItemKind:  "agent_message",
					Status:    "completed",
					Metadata:  map[string]any{"text": text},
				},
			)
		}
	}
	if usage := buildClaudeTokenUsage(message, t.threadUsage[t.activeTurn.ThreadID]); usage != nil {
		t.threadUsage[t.activeTurn.ThreadID] = agentproto.CloneThreadTokenUsage(usage)
		events = append(events, agentproto.Event{
			Kind:       agentproto.EventThreadTokenUsageUpdated,
			CommandID:  t.activeTurn.CommandID,
			ThreadID:   t.activeTurn.ThreadID,
			TurnID:     t.activeTurn.TurnID,
			TokenUsage: usage,
		})
	}
	status, errorMessage, problem := t.resultCompletion(message)
	events = append(events, agentproto.Event{
		Kind:                 agentproto.EventTurnCompleted,
		CommandID:            t.activeTurn.CommandID,
		Initiator:            t.activeTurn.Initiator,
		ThreadID:             t.activeTurn.ThreadID,
		TurnID:               t.activeTurn.TurnID,
		TurnCompletionOrigin: agentproto.TurnCompletionOriginRuntime,
		Status:               status,
		ErrorMessage:         errorMessage,
		Problem:              problem,
	})
	turnID := t.activeTurn.TurnID
	t.activeTurn = nil
	t.currentMessage = nil
	for requestID, request := range t.pendingRequests {
		if request != nil && request.TurnID == turnID {
			delete(t.pendingRequests, requestID)
		}
	}
	for toolUseID, tool := range t.toolStates {
		if tool != nil && tool.TurnID == turnID {
			delete(t.toolStates, toolUseID)
		}
	}
	return Result{Events: events}
}

func (t *Translator) resultCompletion(message map[string]any) (string, string, *agentproto.ErrorInfo) {
	subtype := strings.TrimSpace(lookupStringFromAny(message["subtype"]))
	resultText := strings.TrimSpace(lookupStringFromAny(message["result"]))
	if t.activeTurn != nil && t.activeTurn.InterruptRequested && subtype == "error_during_execution" {
		return "interrupted", "", nil
	}
	if subtype == "success" {
		return "completed", "", nil
	}
	errorMessage := firstNonEmptyString(resultText, "Claude turn failed.")
	return "failed", errorMessage, buildFailureProblem(
		"claude_turn_failed",
		errorMessage,
		compactJSON(message["errors"]),
		t.activeTurn.ThreadID,
		t.activeTurn.TurnID,
	)
}
