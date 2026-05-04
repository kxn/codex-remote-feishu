package claude

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func claudeHomeDir() string {
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return home
	}
	if home := strings.TrimSpace(os.Getenv("USERPROFILE")); home != "" {
		return home
	}
	drive := strings.TrimSpace(os.Getenv("HOMEDRIVE"))
	path := strings.TrimSpace(os.Getenv("HOMEPATH"))
	if drive != "" && path != "" {
		return filepath.Clean(drive + path)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

func cloneMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return map[string]any{}
	}
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = cloneJSONValue(value)
	}
	return output
}

func cloneJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		cloned := make(map[string]any, len(typed))
		for key, item := range typed {
			cloned[key] = cloneJSONValue(item)
		}
		return cloned
	case []any:
		cloned := make([]any, 0, len(typed))
		for _, item := range typed {
			cloned = append(cloned, cloneJSONValue(item))
		}
		return cloned
	default:
		return value
	}
}

func lookupMap(value map[string]any, path ...string) map[string]any {
	current := any(value)
	for _, part := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return map[string]any{}
		}
		current = object[part]
	}
	object, _ := current.(map[string]any)
	if object == nil {
		return map[string]any{}
	}
	return object
}

func lookupSliceMaps(value map[string]any, path ...string) []map[string]any {
	current := any(value)
	for _, part := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = object[part]
	}
	return mapsFromAny(current)
}

func mapsFromAny(value any) []map[string]any {
	switch typed := value.(type) {
	case []map[string]any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, cloneMap(item))
		}
		return out
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			object, _ := item.(map[string]any)
			if object != nil {
				out = append(out, cloneMap(object))
			}
		}
		return out
	default:
		return nil
	}
}

func lookupStringFromAny(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return ""
	}
}

func lookupBoolFromAny(value any) bool {
	current, _ := value.(bool)
	return current
}

func lookupIntFromAny(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}

func lookupStringList(value any) []string {
	switch typed := value.(type) {
	case []string:
		return append([]string{}, typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if current := strings.TrimSpace(lookupStringFromAny(item)); current != "" {
				out = append(out, current)
			}
		}
		return out
	default:
		return nil
	}
}

func compactJSON(value any) string {
	if value == nil {
		return ""
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(data)
}

func marshalNDJSON(value any) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func stringifyTextContent(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []any:
		var buffer bytes.Buffer
		for _, item := range typed {
			if buffer.Len() > 0 {
				buffer.WriteString("\n")
			}
			switch entry := item.(type) {
			case string:
				buffer.WriteString(entry)
			case map[string]any:
				if text := strings.TrimSpace(lookupStringFromAny(entry["text"])); text != "" {
					buffer.WriteString(text)
					continue
				}
				if text := strings.TrimSpace(lookupStringFromAny(entry["content"])); text != "" {
					buffer.WriteString(text)
				}
			}
		}
		return buffer.String()
	default:
		return ""
	}
}

func normalizeClaudeSemanticItemKind(raw string) string {
	switch strings.TrimSpace(raw) {
	case "command_execution", "web_search", "dynamic_tool_call", "reasoning_summary", "delegated_task", "file_change":
		return strings.TrimSpace(raw)
	default:
		return ""
	}
}

var claudeThinkingSideChannelTags = []string{
	"claude_background_info",
	"fast_mode_info",
}

func newThinkingFilterState() *thinkingFilterState {
	return &thinkingFilterState{}
}

func filterClaudeThinkingDelta(state *thinkingFilterState, delta string) string {
	if state == nil || delta == "" {
		return delta
	}
	working := state.Pending + delta
	state.Pending = ""
	var out strings.Builder
	for len(working) > 0 {
		if state.Active != "" {
			closeTag := "</" + state.Active + ">"
			index := strings.Index(working, closeTag)
			if index < 0 {
				hold := len(closeTag) - 1
				if hold < 1 {
					hold = 1
				}
				if len(working) <= hold {
					state.Pending = working
					return out.String()
				}
				state.Pending = working[len(working)-hold:]
				return out.String()
			}
			working = working[index+len(closeTag):]
			working = strings.TrimLeft(working, "\r\n")
			state.Active = ""
			continue
		}

		openIndex, openTag, openName := earliestThinkingSideChannelOpenTag(working)
		if openIndex < 0 {
			if split := partialThinkingOpenTagStart(working); split >= 0 {
				out.WriteString(working[:split])
				state.Pending = working[split:]
				return out.String()
			}
			out.WriteString(working)
			return out.String()
		}
		if openIndex > 0 {
			out.WriteString(working[:openIndex])
		}
		working = working[openIndex+len(openTag):]
		state.Active = openName
	}
	return out.String()
}

func finalizeClaudeThinkingFilter(state *thinkingFilterState) string {
	if state == nil {
		return ""
	}
	if state.Active != "" {
		state.Pending = ""
		state.Active = ""
		return ""
	}
	trailing := state.Pending
	state.Pending = ""
	return trailing
}

func earliestThinkingSideChannelOpenTag(value string) (int, string, string) {
	bestIndex := -1
	bestTag := ""
	bestName := ""
	for _, name := range claudeThinkingSideChannelTags {
		tag := "<" + name + ">"
		index := strings.Index(value, tag)
		if index < 0 {
			continue
		}
		if bestIndex < 0 || index < bestIndex {
			bestIndex = index
			bestTag = tag
			bestName = name
		}
	}
	return bestIndex, bestTag, bestName
}

func claudeThinkingSideChannelMaxOpenTagLen() int {
	maxLen := 0
	for _, name := range claudeThinkingSideChannelTags {
		if current := len("<" + name + ">"); current > maxLen {
			maxLen = current
		}
	}
	return maxLen
}

func partialThinkingOpenTagStart(value string) int {
	for i := len(value) - 1; i >= 0; i-- {
		if value[i] != '<' {
			continue
		}
		suffix := value[i:]
		for _, name := range claudeThinkingSideChannelTags {
			tag := "<" + name + ">"
			if suffix != tag && strings.HasPrefix(tag, suffix) {
				return i
			}
		}
	}
	return -1
}

func (t *Translator) newReasoningSummaryDeltaEvent(itemID, delta string) agentproto.Event {
	return agentproto.Event{
		Kind:      agentproto.EventItemDelta,
		CommandID: t.activeTurn.CommandID,
		ThreadID:  t.activeTurn.ThreadID,
		TurnID:    t.activeTurn.TurnID,
		ItemID:    itemID,
		ItemKind:  "reasoning_summary",
		Delta:     delta,
	}
}

func (t *Translator) newReasoningSummaryCompletedEvent(itemID string) agentproto.Event {
	return agentproto.Event{
		Kind:      agentproto.EventItemCompleted,
		CommandID: t.activeTurn.CommandID,
		ThreadID:  t.activeTurn.ThreadID,
		TurnID:    t.activeTurn.TurnID,
		ItemID:    itemID,
		ItemKind:  "reasoning_summary",
		Status:    "completed",
	}
}

func claudeToolItemKind(toolName string) string {
	switch strings.TrimSpace(toolName) {
	case "Bash":
		return "command_execution"
	case "WebSearch", "WebFetch", "ToolSearch":
		return "web_search"
	case "TodoWrite":
		return ""
	case "Task":
		return "delegated_task"
	case "TaskOutput", "TaskStop":
		return ""
	case "Edit", "Write", "NotebookEdit":
		return "file_change"
	case "Read", "Glob", "Grep", "Skill":
		return "dynamic_tool_call"
	default:
		return "dynamic_tool_call"
	}
}

func claudeToolVisibleLifecycle(toolName string) bool {
	switch strings.TrimSpace(toolName) {
	case "TaskOutput", "TaskStop":
		return false
	default:
		return strings.TrimSpace(claudeToolItemKind(toolName)) != ""
	}
}

func claudeToolMetadata(toolName string, input map[string]any) map[string]any {
	metadata := map[string]any{
		"tool":      strings.TrimSpace(toolName),
		"arguments": cloneMap(input),
	}
	switch claudeToolItemKind(toolName) {
	case "command_execution":
		if command := strings.TrimSpace(lookupStringFromAny(input["command"])); command != "" {
			metadata["command"] = command
		}
		if cwd := strings.TrimSpace(lookupStringFromAny(input["cwd"])); cwd != "" {
			metadata["cwd"] = cwd
		}
	case "web_search":
		mergeClaudeWebToolMetadata(metadata, toolName, input)
	case "delegated_task":
		metadata["subagentType"] = strings.TrimSpace(lookupStringFromAny(input["subagent_type"]))
		metadata["description"] = strings.TrimSpace(lookupStringFromAny(input["description"]))
		if prompt := strings.TrimSpace(lookupStringFromAny(input["prompt"])); prompt != "" {
			metadata["prompt"] = prompt
		}
	case "file_change":
		mergeClaudeFileChangeMetadata(metadata, toolName, input)
	case "dynamic_tool_call":
		metadata["semanticKind"] = claudeDynamicToolSemanticKind(toolName)
		metadata["suppressFinalText"] = true
	}
	return metadata
}

func claudeDynamicToolSemanticKind(toolName string) string {
	switch strings.TrimSpace(toolName) {
	case "Read", "Glob", "Grep":
		return "exploration"
	case "Skill":
		return "skill"
	case "Edit", "Write", "NotebookEdit":
		return "file_change_request"
	default:
		return "generic_tool"
	}
}

func mergeClaudeWebToolMetadata(metadata map[string]any, toolName string, input map[string]any) {
	switch strings.TrimSpace(toolName) {
	case "WebSearch":
		metadata["actionType"] = "search"
		if query := firstNonEmptyString(
			lookupStringFromAny(input["query"]),
			lookupStringFromAny(input["q"]),
		); query != "" {
			metadata["query"] = query
		}
	case "WebFetch":
		metadata["actionType"] = "open_page"
		if url := firstNonEmptyString(
			lookupStringFromAny(input["url"]),
			lookupStringFromAny(input["href"]),
		); url != "" {
			metadata["url"] = url
		}
	case "ToolSearch":
		metadata["actionType"] = "find_in_page"
		if pattern := firstNonEmptyString(
			lookupStringFromAny(input["pattern"]),
			lookupStringFromAny(input["query"]),
			lookupStringFromAny(input["text"]),
		); pattern != "" {
			metadata["pattern"] = pattern
		}
		if url := firstNonEmptyString(
			lookupStringFromAny(input["url"]),
			lookupStringFromAny(input["page_url"]),
		); url != "" {
			metadata["url"] = url
		}
	}
}

func buildClaudeTodoPlanSnapshot(input map[string]any) *agentproto.TurnPlanSnapshot {
	records := mapsFromAny(input["todos"])
	if len(records) == 0 {
		return nil
	}
	snapshot := &agentproto.TurnPlanSnapshot{
		Steps: make([]agentproto.TurnPlanStep, 0, len(records)),
	}
	activeForms := make([]string, 0, len(records))
	for _, record := range records {
		step := strings.TrimSpace(lookupStringFromAny(record["content"]))
		if step == "" {
			step = strings.TrimSpace(lookupStringFromAny(record["activeForm"]))
		}
		if step == "" {
			continue
		}
		status := agentproto.NormalizeTurnPlanStepStatus(lookupStringFromAny(record["status"]))
		if status == "" {
			status = agentproto.TurnPlanStepStatusPending
		}
		snapshot.Steps = append(snapshot.Steps, agentproto.TurnPlanStep{
			Step:   step,
			Status: status,
		})
		if active := strings.TrimSpace(lookupStringFromAny(record["activeForm"])); active != "" && status == agentproto.TurnPlanStepStatusInProgress {
			activeForms = append(activeForms, active)
		}
	}
	if len(snapshot.Steps) == 0 {
		return nil
	}
	if len(activeForms) != 0 {
		snapshot.Explanation = strings.Join(activeForms, "；")
	}
	return snapshot
}

func buildClaudePlanSummary(snapshot *agentproto.TurnPlanSnapshot) string {
	if snapshot == nil {
		return ""
	}
	if explanation := strings.TrimSpace(snapshot.Explanation); explanation != "" {
		return explanation
	}
	for _, step := range snapshot.Steps {
		if step.Status == agentproto.TurnPlanStepStatusInProgress {
			return strings.TrimSpace(step.Step)
		}
	}
	if len(snapshot.Steps) != 0 {
		return strings.TrimSpace(snapshot.Steps[0].Step)
	}
	return ""
}

func buildClaudeDelegatedTaskText(metadata map[string]any) string {
	if len(metadata) == 0 {
		return ""
	}
	description := strings.TrimSpace(lookupStringFromAny(metadata["description"]))
	subagentType := strings.TrimSpace(lookupStringFromAny(metadata["subagentType"]))
	switch {
	case description != "" && subagentType != "":
		return fmt.Sprintf("Task (%s): %s", subagentType, description)
	case description != "":
		return "Task: " + description
	case subagentType != "":
		return "Task (" + subagentType + ")"
	default:
		return "Task"
	}
}

func cloneMetadata(metadata map[string]any) map[string]any {
	if len(metadata) == 0 {
		return nil
	}
	out := make(map[string]any, len(metadata))
	for key, value := range metadata {
		out[key] = cloneJSONValue(value)
	}
	return out
}

func sortedMetadataKeys(metadata map[string]any) []string {
	if len(metadata) == 0 {
		return nil
	}
	keys := make([]string, 0, len(metadata))
	for key := range metadata {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sanitizeQuestionID(value string, index int) string {
	value = strings.TrimSpace(value)
	if value != "" {
		return value
	}
	return fmt.Sprintf("question_%d", index+1)
}

func buildQuestionMetadata(questions []agentproto.RequestQuestion) []map[string]any {
	if len(questions) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(questions))
	for _, question := range questions {
		record := map[string]any{
			"id":         strings.TrimSpace(question.ID),
			"header":     strings.TrimSpace(question.Header),
			"question":   strings.TrimSpace(question.Question),
			"allowOther": question.AllowOther,
			"secret":     question.Secret,
		}
		if strings.TrimSpace(question.Placeholder) != "" {
			record["placeholder"] = strings.TrimSpace(question.Placeholder)
		}
		if strings.TrimSpace(question.DefaultValue) != "" {
			record["defaultValue"] = strings.TrimSpace(question.DefaultValue)
		}
		if question.DirectResponse {
			record["directResponse"] = true
		}
		if len(question.Options) != 0 {
			options := make([]map[string]any, 0, len(question.Options))
			for _, option := range question.Options {
				options = append(options, map[string]any{
					"label":       strings.TrimSpace(option.Label),
					"description": strings.TrimSpace(option.Description),
				})
			}
			record["options"] = options
		}
		out = append(out, record)
	}
	return out
}

func buildClaudeTokenUsage(result map[string]any) *agentproto.ThreadTokenUsage {
	usageMap := lookupMap(result, "usage")
	if len(usageMap) == 0 {
		return nil
	}
	inputTokens := lookupIntFromAny(usageMap["input_tokens"])
	cacheReadTokens := lookupIntFromAny(usageMap["cache_read_input_tokens"])
	cacheCreateTokens := lookupIntFromAny(usageMap["cache_creation_input_tokens"])
	outputTokens := lookupIntFromAny(usageMap["output_tokens"])
	total := inputTokens + cacheReadTokens + cacheCreateTokens + outputTokens

	usage := &agentproto.ThreadTokenUsage{
		Total: agentproto.TokenUsageBreakdown{
			InputTokens:       inputTokens,
			CachedInputTokens: cacheReadTokens + cacheCreateTokens,
			OutputTokens:      outputTokens,
			TotalTokens:       total,
		},
		Last: agentproto.TokenUsageBreakdown{
			InputTokens:       inputTokens,
			CachedInputTokens: cacheReadTokens + cacheCreateTokens,
			OutputTokens:      outputTokens,
			TotalTokens:       total,
		},
	}
	bestWindow := 0
	for _, modelUsage := range lookupMap(result, "modelUsage") {
		record, ok := modelUsage.(map[string]any)
		if !ok {
			continue
		}
		if current := lookupIntFromAny(record["contextWindow"]); current > bestWindow {
			bestWindow = current
		}
	}
	if bestWindow > 0 {
		usage.ModelContextWindow = &bestWindow
	}
	return usage
}
