package claudesessionstore

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

type parsedHistoryTurn struct {
	promptID      string
	record        agentproto.ThreadHistoryTurnRecord
	toolUseItemID map[string]int
}

func readThreadHistory(workspaceRoot, threadID string, runtime RuntimeStateSnapshot) (*agentproto.ThreadHistoryRecord, error) {
	threadID = strings.TrimSpace(threadID)
	filePath, meta, err := findSessionFile(threadID)
	if err != nil {
		return nil, err
	}
	if meta == nil {
		meta = &claudeSessionMeta{ID: threadID, CWD: strings.TrimSpace(workspaceRoot)}
	}
	thread := buildSessionThreadSnapshot(*meta, runtime)
	thread.ThreadID = firstNonEmptyString(thread.ThreadID, threadID)
	if filePath == "" {
		return &agentproto.ThreadHistoryRecord{Thread: thread}, nil
	}
	turns, err := readHistoryTurns(filePath, thread.ThreadID, runtime)
	if err != nil {
		return nil, err
	}
	if len(turns) != 0 && strings.TrimSpace(threadID) == strings.TrimSpace(runtime.SessionID) && strings.TrimSpace(runtime.ActiveTurnID) != "" {
		last := &turns[len(turns)-1]
		last.Status = "running"
		last.TurnID = firstNonEmptyString(runtime.ActiveTurnID, last.TurnID)
		last.CompletedAt = time.Time{}
	}
	return &agentproto.ThreadHistoryRecord{
		Thread: thread,
		Turns:  turns,
	}, nil
}

func findSessionFile(sessionID string) (string, *claudeSessionMeta, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return "", nil, nil
	}
	dirs, _, err := sessionProjectDirs("", true)
	if err != nil {
		return "", nil, err
	}
	for _, dir := range dirs {
		filePath := filepath.Join(dir, sessionID+".jsonl")
		meta, err := readSessionListMeta(filePath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			continue
		}
		metaCopy := meta
		return filePath, &metaCopy, nil
	}
	return "", nil, nil
}

func resolveResumeSession(workspaceRoot, sessionID string) (string, *claudeSessionMeta, error) {
	filePath, meta, err := findSessionFile(sessionID)
	if err != nil || meta == nil {
		return filePath, meta, err
	}
	if !sameWorkspaceCWD(meta.CWD, workspaceRoot) {
		return "", meta, fmt.Errorf("claude session %q belongs to %q, not %q", sessionID, meta.CWD, workspaceRoot)
	}
	return filePath, meta, nil
}

func FindSessionMeta(sessionID string) (*SessionMeta, error) {
	_, meta, err := findSessionFile(sessionID)
	if err != nil || meta == nil {
		return nil, err
	}
	copy := SessionMeta(*meta)
	return &copy, nil
}

func ResolveResumeSession(workspaceRoot, sessionID string) (*SessionMeta, error) {
	_, meta, err := resolveResumeSession(workspaceRoot, sessionID)
	if err != nil || meta == nil {
		return nil, err
	}
	copy := SessionMeta(*meta)
	return &copy, nil
}

func readHistoryTurns(filePath, sessionID string, runtime RuntimeStateSnapshot) ([]agentproto.ThreadHistoryTurnRecord, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	turns := make([]agentproto.ThreadHistoryTurnRecord, 0, 32)
	var current *parsedHistoryTurn
	syntheticCount := 0

	flushCurrent := func() {
		if current == nil {
			return
		}
		if strings.TrimSpace(current.record.TurnID) == "" {
			syntheticCount++
			current.record.TurnID = fmt.Sprintf("claude-history-turn-%d", syntheticCount)
		}
		if strings.TrimSpace(current.record.Status) == "" {
			current.record.Status = "completed"
		}
		turns = append(turns, current.record)
		current = nil
	}

	for scanner.Scan() {
		entry, ok := parseSessionLine(scanner.Text())
		if !ok {
			continue
		}
		if lookupBoolFromAny(entry["isSidechain"]) {
			continue
		}
		recordType := strings.TrimSpace(lookupStringFromAny(entry["type"]))
		promptID := strings.TrimSpace(lookupStringFromAny(entry["promptId"]))
		if historyStartsTurn(entry) && (current == nil || promptID != "" && promptID != current.promptID) {
			flushCurrent()
			current = newParsedHistoryTurn(promptID)
		}
		if current == nil {
			if promptID == "" {
				continue
			}
			current = newParsedHistoryTurn(promptID)
		}
		if current.promptID == "" && promptID != "" {
			current.promptID = promptID
			current.record.TurnID = promptID
		}
		current.observeTimestamp(parseHistoryTimestamp(entry))
		switch recordType {
		case "user":
			appendHistoryUserEntry(current, entry)
		case "assistant":
			appendHistoryAssistantEntry(current, entry)
		case "max_turns_reached":
			current.record.Status = "failed"
			current.record.ErrorMessage = "Claude 达到最大 turn 限制"
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	flushCurrent()
	if len(turns) != 0 && strings.TrimSpace(runtime.SessionID) == strings.TrimSpace(sessionID) && strings.TrimSpace(runtime.ActiveTurnID) != "" {
		last := &turns[len(turns)-1]
		last.Status = "running"
		last.TurnID = firstNonEmptyString(runtime.ActiveTurnID, last.TurnID)
		last.CompletedAt = time.Time{}
	}
	return turns, nil
}

func newParsedHistoryTurn(promptID string) *parsedHistoryTurn {
	promptID = strings.TrimSpace(promptID)
	return &parsedHistoryTurn{
		promptID: promptID,
		record: agentproto.ThreadHistoryTurnRecord{
			TurnID: promptID,
		},
		toolUseItemID: map[string]int{},
	}
}

func (t *parsedHistoryTurn) observeTimestamp(ts time.Time) {
	if t == nil || ts.IsZero() {
		return
	}
	if t.record.StartedAt.IsZero() {
		t.record.StartedAt = ts
	}
	if ts.After(t.record.CompletedAt) {
		t.record.CompletedAt = ts
	}
}

func (t *parsedHistoryTurn) appendItem(item agentproto.ThreadHistoryItemRecord) int {
	if t == nil {
		return -1
	}
	if strings.TrimSpace(item.ItemID) == "" {
		item.ItemID = fmt.Sprintf("%s-item-%d", firstNonEmptyString(t.record.TurnID, t.promptID, "claude-history"), len(t.record.Items)+1)
	}
	t.record.Items = append(t.record.Items, item)
	return len(t.record.Items) - 1
}

func appendHistoryUserEntry(turn *parsedHistoryTurn, entry map[string]any) {
	if turn == nil {
		return
	}
	message, _ := entry["message"].(map[string]any)
	content := message["content"]
	if text := strings.TrimSpace(sessionMessageText(content)); text != "" {
		turn.appendItem(agentproto.ThreadHistoryItemRecord{
			Kind: "user_message",
			Text: text,
		})
	}
	for _, block := range mapsFromAny(content) {
		if strings.TrimSpace(lookupStringFromAny(block["type"])) != "tool_result" {
			continue
		}
		toolUseID := strings.TrimSpace(lookupStringFromAny(block["tool_use_id"]))
		exitCode := historyExitCode(block, entry["tool_use_result"])
		if itemIndex, ok := turn.toolUseItemID[toolUseID]; ok && itemIndex >= 0 && itemIndex < len(turn.record.Items) {
			turn.record.Items[itemIndex].ExitCode = exitCode
		}
	}
}

func appendHistoryAssistantEntry(turn *parsedHistoryTurn, entry map[string]any) {
	if turn == nil {
		return
	}
	message, _ := entry["message"].(map[string]any)
	blocks := mapsFromAny(message["content"])
	textParts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		switch strings.TrimSpace(lookupStringFromAny(block["type"])) {
		case "text":
			text := strings.TrimSpace(lookupStringFromAny(block["text"]))
			if text != "" {
				textParts = append(textParts, text)
			}
		case "thinking":
			continue
		case "tool_use":
			toolName := strings.TrimSpace(lookupStringFromAny(block["name"]))
			if isInternalInteractionTool(toolName) {
				continue
			}
			input, _ := block["input"].(map[string]any)
			item, ok := claudeHistoryToolItem(toolName, input)
			if !ok {
				continue
			}
			itemIndex := turn.appendItem(item)
			toolUseID := strings.TrimSpace(lookupStringFromAny(block["id"]))
			if toolUseID != "" {
				turn.toolUseItemID[toolUseID] = itemIndex
			}
		}
	}
	if len(textParts) != 0 {
		turn.appendItem(agentproto.ThreadHistoryItemRecord{
			Kind: "agent_message",
			Text: strings.Join(textParts, "\n"),
		})
	}
}

func claudeHistoryToolItem(toolName string, input map[string]any) (agentproto.ThreadHistoryItemRecord, bool) {
	itemKind := claudeToolItemKind(toolName)
	metadata := claudeToolMetadata(toolName, input)
	switch itemKind {
	case "command_execution":
		command := strings.TrimSpace(lookupStringFromAny(metadata["command"]))
		if command == "" {
			command = strings.TrimSpace(toolUseSummary(toolName, input))
		}
		if command == "" {
			return agentproto.ThreadHistoryItemRecord{}, false
		}
		return agentproto.ThreadHistoryItemRecord{
			Kind:     "command_execution",
			Command:  command,
			Text:     command,
			Metadata: metadata,
		}, true
	case "web_search":
		text := strings.TrimSpace(webHistoryText(metadata))
		if text == "" {
			text = strings.TrimSpace(toolUseSummary(toolName, input))
		}
		if text == "" {
			return agentproto.ThreadHistoryItemRecord{}, false
		}
		return agentproto.ThreadHistoryItemRecord{
			Kind:     "web_search",
			Text:     text,
			Metadata: metadata,
		}, true
	case "delegated_task":
		text := buildClaudeDelegatedTaskText(metadata)
		return agentproto.ThreadHistoryItemRecord{
			Kind:     "delegated_task",
			Text:     text,
			Metadata: metadata,
		}, true
	case "file_change":
		text := buildClaudeFileChangeHistoryText(metadata)
		if text == "" {
			text = strings.TrimSpace(toolUseSummary(toolName, input))
		}
		if text == "" {
			return agentproto.ThreadHistoryItemRecord{}, false
		}
		return agentproto.ThreadHistoryItemRecord{
			Kind:     "file_change",
			Text:     text,
			Metadata: metadata,
		}, true
	case "dynamic_tool_call":
		text := strings.TrimSpace(toolUseSummary(toolName, input))
		if text == "" {
			text = strings.TrimSpace(lookupStringFromAny(metadata["tool"]))
		}
		if text == "" {
			return agentproto.ThreadHistoryItemRecord{}, false
		}
		return agentproto.ThreadHistoryItemRecord{
			Kind:     "dynamic_tool_call",
			Text:     text,
			Metadata: metadata,
		}, true
	default:
		return agentproto.ThreadHistoryItemRecord{}, false
	}
}

func buildClaudeFileChangeHistoryText(metadata map[string]any) string {
	if len(metadata) == 0 {
		return ""
	}
	path := strings.TrimSpace(lookupStringFromAny(metadata["filePath"]))
	if path == "" {
		return ""
	}
	return path
}

func webHistoryText(metadata map[string]any) string {
	switch strings.TrimSpace(lookupStringFromAny(metadata["actionType"])) {
	case "search":
		return strings.TrimSpace(lookupStringFromAny(metadata["query"]))
	case "open_page":
		return strings.TrimSpace(lookupStringFromAny(metadata["url"]))
	case "find_in_page":
		pattern := strings.TrimSpace(lookupStringFromAny(metadata["pattern"]))
		url := strings.TrimSpace(lookupStringFromAny(metadata["url"]))
		switch {
		case pattern != "" && url != "":
			return pattern + " @ " + url
		case pattern != "":
			return pattern
		default:
			return url
		}
	default:
		return ""
	}
}

func historyStartsTurn(entry map[string]any) bool {
	if strings.TrimSpace(lookupStringFromAny(entry["type"])) != "user" {
		return false
	}
	message, _ := entry["message"].(map[string]any)
	return historyHasPromptContent(message["content"])
}

func historyHasPromptContent(content any) bool {
	switch value := content.(type) {
	case string:
		return strings.TrimSpace(value) != ""
	case []any:
		for _, blockValue := range value {
			block, _ := blockValue.(map[string]any)
			switch strings.TrimSpace(lookupStringFromAny(block["type"])) {
			case "text", "image":
				return true
			}
		}
	}
	return false
}

func parseHistoryTimestamp(entry map[string]any) time.Time {
	raw := strings.TrimSpace(lookupStringFromAny(entry["timestamp"]))
	if raw == "" {
		return time.Time{}
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999Z07:00",
		"2006-01-02 15:04:05Z07:00",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func historyExitCode(values ...any) *int {
	for _, value := range values {
		switch typed := value.(type) {
		case map[string]any:
			if exitCode := historyExitCode(typed["exitCode"], typed["exit_code"]); exitCode != nil {
				return exitCode
			}
		case int:
			code := typed
			return &code
		case int64:
			code := int(typed)
			return &code
		case float64:
			code := int(typed)
			return &code
		}
	}
	return nil
}
