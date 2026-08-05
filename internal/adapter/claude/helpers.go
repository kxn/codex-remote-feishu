package claude

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/claudeutil"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

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
	return xutil.MapsFromAny(current)
}

func lookupStringList(value any) []string {
	switch typed := value.(type) {
	case []string:
		return append([]string{}, typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if current := strings.TrimSpace(xutil.LookupStringFromAny(item)); current != "" {
				out = append(out, current)
			}
		}
		return out
	default:
		return nil
	}
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
				if text := strings.TrimSpace(xutil.LookupStringFromAny(entry["text"])); text != "" {
					buffer.WriteString(text)
					continue
				}
				if text := strings.TrimSpace(xutil.LookupStringFromAny(entry["content"])); text != "" {
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

func claudeToolVisibleLifecycle(toolName string) bool {
	switch strings.TrimSpace(toolName) {
	case "TaskOutput", "TaskStop":
		return false
	default:
		return strings.TrimSpace(claudeutil.ClaudeToolItemKind(toolName)) != ""
	}
}

func buildClaudeTodoPlanSnapshot(input map[string]any) *agentproto.TurnPlanSnapshot {
	records := xutil.MapsFromAny(input["todos"])
	if len(records) == 0 {
		return nil
	}
	snapshot := &agentproto.TurnPlanSnapshot{
		Steps: make([]agentproto.TurnPlanStep, 0, len(records)),
	}
	activeForms := make([]string, 0, len(records))
	for _, record := range records {
		step := strings.TrimSpace(xutil.LookupStringFromAny(record["content"]))
		if step == "" {
			step = strings.TrimSpace(xutil.LookupStringFromAny(record["activeForm"]))
		}
		if step == "" {
			continue
		}
		status := agentproto.NormalizeTurnPlanStepStatus(xutil.LookupStringFromAny(record["status"]))
		if status == "" {
			status = agentproto.TurnPlanStepStatusPending
		}
		snapshot.Steps = append(snapshot.Steps, agentproto.TurnPlanStep{
			Step:   step,
			Status: status,
		})
		if active := strings.TrimSpace(xutil.LookupStringFromAny(record["activeForm"])); active != "" && status == agentproto.TurnPlanStepStatusInProgress {
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

func buildClaudeDelegatedTaskSourceContextLabel(metadata map[string]any) string {
	subagentType := strings.TrimSpace(xutil.LookupStringFromAny(metadata["subagentType"]))
	switch {
	case subagentType != "":
		return fmt.Sprintf("来自 Task (%s)", subagentType)
	case len(metadata) != 0:
		return "来自 Task"
	default:
		return ""
	}
}

func cloneMetadata(metadata map[string]any) map[string]any {
	if len(metadata) == 0 {
		return nil
	}
	out := make(map[string]any, len(metadata))
	for key, value := range metadata {
		out[key] = xutil.CloneJSONValue(value)
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

func buildClaudeTokenUsage(result map[string]any, previous *agentproto.ThreadTokenUsage) *agentproto.ThreadTokenUsage {
	usageMap := lookupMap(result, "usage")
	if len(usageMap) == 0 {
		return nil
	}
	inputTokens := xutil.LookupIntFromAny(usageMap["input_tokens"])
	cacheReadTokens := xutil.LookupIntFromAny(usageMap["cache_read_input_tokens"])
	cacheCreateTokens := xutil.LookupIntFromAny(usageMap["cache_creation_input_tokens"])
	outputTokens := xutil.LookupIntFromAny(usageMap["output_tokens"])
	totalInputTokens := inputTokens + cacheReadTokens + cacheCreateTokens
	totalTokens := totalInputTokens + outputTokens
	last := agentproto.TokenUsageBreakdown{
		InputTokens:       totalInputTokens,
		CachedInputTokens: cacheReadTokens,
		OutputTokens:      outputTokens,
		TotalTokens:       totalTokens,
	}

	usage := &agentproto.ThreadTokenUsage{
		Total: last,
		Last:  last,
	}
	if previous != nil {
		usage.Total = addTokenUsageBreakdown(previous.Total, last)
		if previous.ModelContextWindow != nil {
			value := *previous.ModelContextWindow
			usage.ModelContextWindow = &value
		}
	}
	bestWindow := 0
	for _, modelUsage := range lookupMap(result, "modelUsage") {
		record, ok := modelUsage.(map[string]any)
		if !ok {
			continue
		}
		if current := xutil.LookupIntFromAny(record["contextWindow"]); current > bestWindow {
			bestWindow = current
		}
	}
	if bestWindow > 0 {
		if usage.ModelContextWindow == nil || bestWindow > *usage.ModelContextWindow {
			usage.ModelContextWindow = &bestWindow
		}
	}
	return usage
}

func addTokenUsageBreakdown(left, right agentproto.TokenUsageBreakdown) agentproto.TokenUsageBreakdown {
	return agentproto.TokenUsageBreakdown{
		InputTokens:           left.InputTokens + right.InputTokens,
		CachedInputTokens:     left.CachedInputTokens + right.CachedInputTokens,
		OutputTokens:          left.OutputTokens + right.OutputTokens,
		ReasoningOutputTokens: left.ReasoningOutputTokens + right.ReasoningOutputTokens,
		TotalTokens:           left.TotalTokens + right.TotalTokens,
	}
}
