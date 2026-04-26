package codexstate

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

type rolloutLine struct {
	value map[string]any
}

type rolloutSnapshot struct {
	threadID        string
	path            string
	raw             []byte
	digest          string
	mode            os.FileMode
	trailingNewline bool
	lines           []rolloutLine
	latestTurn      *rolloutTurnSnapshot
}

type rolloutTurnSnapshot struct {
	turnID             string
	startIndex         int
	endIndex           int
	reasoningLineIndex []int
	messages           []*rolloutAssistantMessage
}

type rolloutAssistantMessage struct {
	messageKey        string
	phase             string
	text              string
	eventLineIndex    int
	responseLineIndex int
	taskCompleteIndex int
	isFinal           bool
	duplicateDrift    bool
	taskCompleteDrift bool
	responseOnly      bool
	eventOnly         bool
}

func readRolloutSnapshot(path string) (*rolloutSnapshot, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" {
		return nil, fmt.Errorf("%w: missing path", ErrTurnPatchRolloutNotFound)
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrTurnPatchRolloutNotFound, path)
		}
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseRolloutSnapshot(path, raw, info.Mode())
}

func readRolloutThreadID(path string) (string, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" {
		return "", fmt.Errorf("%w: missing path", ErrTurnPatchRolloutNotFound)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("%w: %s", ErrTurnPatchRolloutNotFound, path)
		}
		return "", err
	}
	chunks := bytes.Split(raw, []byte{'\n'})
	for _, chunk := range chunks {
		if len(bytes.TrimSpace(chunk)) == 0 {
			continue
		}
		var decoded map[string]any
		if err := json.Unmarshal(chunk, &decoded); err != nil {
			return "", err
		}
		if topLevelType(decoded) != "session_meta" {
			continue
		}
		return strings.TrimSpace(stringField(payloadMap(decoded), "id")), nil
	}
	return "", fmt.Errorf("%w: missing session_meta thread id", ErrTurnPatchRolloutNotFound)
}

func parseRolloutSnapshot(path string, raw []byte, mode os.FileMode) (*rolloutSnapshot, error) {
	snapshot := &rolloutSnapshot{
		path:            filepath.Clean(strings.TrimSpace(path)),
		raw:             slices.Clone(raw),
		digest:          sha256Hex(raw),
		mode:            mode.Perm(),
		trailingNewline: len(raw) > 0 && raw[len(raw)-1] == '\n',
	}
	chunks := bytes.Split(raw, []byte{'\n'})
	if snapshot.trailingNewline && len(chunks) > 0 && len(chunks[len(chunks)-1]) == 0 {
		chunks = chunks[:len(chunks)-1]
	}
	snapshot.lines = make([]rolloutLine, 0, len(chunks))
	for _, chunk := range chunks {
		if len(bytes.TrimSpace(chunk)) == 0 {
			continue
		}
		var decoded map[string]any
		if err := json.Unmarshal(chunk, &decoded); err != nil {
			return nil, fmt.Errorf("parse rollout line: %w", err)
		}
		snapshot.lines = append(snapshot.lines, rolloutLine{value: decoded})
	}
	snapshot.threadID = strings.TrimSpace(sessionMetaThreadID(snapshot.lines))
	if snapshot.threadID == "" {
		return nil, fmt.Errorf("%w: missing session_meta thread id", ErrTurnPatchRolloutNotFound)
	}
	latestTurn, err := locateLatestCompletedTurn(snapshot.lines)
	if err != nil {
		return nil, err
	}
	snapshot.latestTurn = latestTurn
	return snapshot, nil
}

func sessionMetaThreadID(lines []rolloutLine) string {
	for _, line := range lines {
		if topLevelType(line.value) != "session_meta" {
			continue
		}
		payload := payloadMap(line.value)
		if payload == nil {
			continue
		}
		return strings.TrimSpace(stringField(payload, "id"))
	}
	return ""
}

func locateLatestCompletedTurn(lines []rolloutLine) (*rolloutTurnSnapshot, error) {
	type activeTurn struct {
		turnID string
		start  int
	}
	var current *activeTurn
	var lastCompleted *rolloutTurnSnapshot
	for index, line := range lines {
		switch {
		case isTaskStarted(line.value):
			current = &activeTurn{
				turnID: strings.TrimSpace(stringField(payloadMap(line.value), "turn_id")),
				start:  index,
			}
		case isTaskComplete(line.value):
			payload := payloadMap(line.value)
			turnID := strings.TrimSpace(stringField(payload, "turn_id"))
			if current == nil {
				continue
			}
			if current.turnID != "" && turnID != "" && current.turnID != turnID {
				continue
			}
			lastCompleted = &rolloutTurnSnapshot{
				turnID:     firstNonEmpty(turnID, current.turnID),
				startIndex: current.start,
				endIndex:   index,
			}
			current = nil
		}
	}
	if lastCompleted == nil {
		return nil, ErrTurnPatchLatestTurnNotFound
	}
	messages, reasoning, err := collectTurnMessages(lines, lastCompleted.startIndex, lastCompleted.endIndex)
	if err != nil {
		return nil, err
	}
	if len(messages) == 0 {
		return nil, ErrTurnPatchLatestTurnNotFound
	}
	lastCompleted.messages = messages
	lastCompleted.reasoningLineIndex = reasoning
	return lastCompleted, nil
}

func collectTurnMessages(lines []rolloutLine, start, end int) ([]*rolloutAssistantMessage, []int, error) {
	var (
		messages   []*rolloutAssistantMessage
		reasoning  []int
		pendingEvt *rolloutAssistantMessage
	)
	for index := start; index <= end && index < len(lines); index++ {
		line := lines[index]
		switch {
		case isAgentReasoning(line.value), isResponseReasoning(line.value):
			reasoning = append(reasoning, index)
		case isAgentMessage(line.value):
			payload := payloadMap(line.value)
			msg := &rolloutAssistantMessage{
				phase:             strings.TrimSpace(stringField(payload, "phase")),
				text:              stringField(payload, "message"),
				eventLineIndex:    index,
				responseLineIndex: -1,
				taskCompleteIndex: -1,
				eventOnly:         true,
			}
			messages = append(messages, msg)
			pendingEvt = msg
		case isAssistantResponseMessage(line.value):
			payload := payloadMap(line.value)
			text, err := assistantResponseText(payload)
			if err != nil {
				return nil, nil, err
			}
			phase := strings.TrimSpace(stringField(payload, "phase"))
			if pendingEvt != nil && pendingEvt.responseLineIndex < 0 && pendingEvt.eventLineIndex >= 0 {
				pendingEvt.responseLineIndex = index
				pendingEvt.responseOnly = false
				pendingEvt.eventOnly = false
				if pendingEvt.text != text || pendingEvt.phase != phase {
					pendingEvt.duplicateDrift = true
				}
				pendingEvt = nil
				continue
			}
			messages = append(messages, &rolloutAssistantMessage{
				phase:             phase,
				text:              text,
				eventLineIndex:    -1,
				responseLineIndex: index,
				taskCompleteIndex: -1,
				responseOnly:      true,
			})
		case isTaskComplete(line.value):
			payload := payloadMap(line.value)
			lastText := stringField(payload, "last_agent_message")
			if lastText == "" || len(messages) == 0 {
				continue
			}
			last := messages[len(messages)-1]
			last.isFinal = true
			if last.text != lastText {
				last.taskCompleteDrift = true
				continue
			}
			last.taskCompleteIndex = index
		}
	}
	for i, message := range messages {
		message.messageKey = fmt.Sprintf("msg-%d", i+1)
		if message.duplicateDrift || message.taskCompleteDrift {
			return nil, nil, ErrTurnPatchDuplicateDrift
		}
	}
	return messages, reasoning, nil
}

func assistantResponseText(payload map[string]any) (string, error) {
	content, _ := payload["content"].([]any)
	if len(content) == 0 {
		return "", fmt.Errorf("%w: assistant response content is empty", ErrTurnPatchDuplicateDrift)
	}
	var parts []string
	for _, item := range content {
		entry, _ := item.(map[string]any)
		if entry == nil {
			return "", fmt.Errorf("%w: assistant response content item is invalid", ErrTurnPatchDuplicateDrift)
		}
		if strings.TrimSpace(stringField(entry, "type")) != "output_text" {
			return "", fmt.Errorf("%w: unsupported assistant response item type", ErrTurnPatchDuplicateDrift)
		}
		parts = append(parts, stringField(entry, "text"))
	}
	return strings.Join(parts, ""), nil
}

func rewriteAssistantResponseText(payload map[string]any, newText string) error {
	content, _ := payload["content"].([]any)
	if len(content) == 0 {
		return fmt.Errorf("%w: assistant response content is empty", ErrTurnPatchDuplicateDrift)
	}
	for idx, item := range content {
		entry, _ := item.(map[string]any)
		if entry == nil {
			return fmt.Errorf("%w: assistant response content item is invalid", ErrTurnPatchDuplicateDrift)
		}
		if strings.TrimSpace(stringField(entry, "type")) != "output_text" {
			return fmt.Errorf("%w: unsupported assistant response item type", ErrTurnPatchDuplicateDrift)
		}
		if idx == 0 {
			entry["text"] = newText
		} else {
			entry["text"] = ""
		}
	}
	payload["content"] = content
	return nil
}

func applyRolloutReplacements(snapshot *rolloutSnapshot, replacements []TurnPatchReplacement) (updatedRaw []byte, replacedCount int, removedReasoning int, err error) {
	if snapshot == nil || snapshot.latestTurn == nil {
		return nil, 0, 0, ErrTurnPatchLatestTurnNotFound
	}
	if len(replacements) == 0 {
		return nil, 0, 0, fmt.Errorf("%w: no replacements", ErrTurnPatchReplacementRequired)
	}
	byKey := map[string]string{}
	for _, replacement := range replacements {
		key := strings.TrimSpace(replacement.MessageKey)
		newText := replacement.NewText
		if key == "" {
			return nil, 0, 0, fmt.Errorf("%w: missing message key", ErrTurnPatchReplacementNotFound)
		}
		if strings.TrimSpace(newText) == "" {
			return nil, 0, 0, fmt.Errorf("%w: %s", ErrTurnPatchReplacementRequired, key)
		}
		byKey[key] = newText
	}
	for _, message := range snapshot.latestTurn.messages {
		newText, ok := byKey[message.messageKey]
		if !ok {
			continue
		}
		if err := applySingleMessageReplacement(snapshot.lines, message, newText); err != nil {
			return nil, 0, 0, err
		}
		replacedCount++
	}
	if replacedCount != len(byKey) {
		return nil, 0, 0, ErrTurnPatchReplacementNotFound
	}
	filtered := removeRolloutLineIndexes(snapshot.lines, snapshot.latestTurn.reasoningLineIndex)
	updatedRaw, err = marshalRolloutLines(filtered, snapshot.trailingNewline)
	if err != nil {
		return nil, 0, 0, err
	}
	return updatedRaw, replacedCount, len(snapshot.latestTurn.reasoningLineIndex), nil
}

func applySingleMessageReplacement(lines []rolloutLine, message *rolloutAssistantMessage, newText string) error {
	if message == nil {
		return ErrTurnPatchReplacementNotFound
	}
	if message.eventLineIndex >= 0 {
		payload := payloadMap(lines[message.eventLineIndex].value)
		current := strings.TrimSpace(stringField(payload, "message"))
		if current != message.text {
			return ErrTurnPatchDuplicateDrift
		}
		payload["message"] = newText
	}
	if message.responseLineIndex >= 0 {
		payload := payloadMap(lines[message.responseLineIndex].value)
		current, err := assistantResponseText(payload)
		if err != nil {
			return err
		}
		if current != message.text {
			return ErrTurnPatchDuplicateDrift
		}
		if err := rewriteAssistantResponseText(payload, newText); err != nil {
			return err
		}
	}
	if message.taskCompleteIndex >= 0 {
		payload := payloadMap(lines[message.taskCompleteIndex].value)
		current := strings.TrimSpace(stringField(payload, "last_agent_message"))
		if current != message.text {
			return ErrTurnPatchDuplicateDrift
		}
		payload["last_agent_message"] = newText
	}
	message.text = newText
	return nil
}

func removeRolloutLineIndexes(lines []rolloutLine, indexes []int) []rolloutLine {
	if len(indexes) == 0 {
		return slices.Clone(lines)
	}
	skip := map[int]struct{}{}
	for _, index := range indexes {
		skip[index] = struct{}{}
	}
	filtered := make([]rolloutLine, 0, len(lines)-len(skip))
	for index, line := range lines {
		if _, ok := skip[index]; ok {
			continue
		}
		filtered = append(filtered, line)
	}
	return filtered
}

func marshalRolloutLines(lines []rolloutLine, trailingNewline bool) ([]byte, error) {
	var buf bytes.Buffer
	for index, line := range lines {
		raw, err := json.Marshal(line.value)
		if err != nil {
			return nil, err
		}
		buf.Write(raw)
		if trailingNewline || index+1 < len(lines) {
			buf.WriteByte('\n')
		}
	}
	return buf.Bytes(), nil
}

func topLevelType(line map[string]any) string {
	return strings.TrimSpace(stringField(line, "type"))
}

func payloadMap(line map[string]any) map[string]any {
	payload, _ := line["payload"].(map[string]any)
	return payload
}

func stringField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	value, _ := m[key].(string)
	return value
}

func isTaskStarted(line map[string]any) bool {
	return topLevelType(line) == "event_msg" && strings.TrimSpace(stringField(payloadMap(line), "type")) == "task_started"
}

func isTaskComplete(line map[string]any) bool {
	return topLevelType(line) == "event_msg" && strings.TrimSpace(stringField(payloadMap(line), "type")) == "task_complete"
}

func isAgentReasoning(line map[string]any) bool {
	return topLevelType(line) == "event_msg" && strings.TrimSpace(stringField(payloadMap(line), "type")) == "agent_reasoning"
}

func isResponseReasoning(line map[string]any) bool {
	return topLevelType(line) == "response_item" && strings.TrimSpace(stringField(payloadMap(line), "type")) == "reasoning"
}

func isAgentMessage(line map[string]any) bool {
	return topLevelType(line) == "event_msg" && strings.TrimSpace(stringField(payloadMap(line), "type")) == "agent_message"
}

func isAssistantResponseMessage(line map[string]any) bool {
	payload := payloadMap(line)
	return topLevelType(line) == "response_item" &&
		strings.TrimSpace(stringField(payload, "type")) == "message" &&
		strings.TrimSpace(stringField(payload, "role")) == "assistant"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func sha256Hex(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
