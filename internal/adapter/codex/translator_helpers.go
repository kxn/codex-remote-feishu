package codex

import (
	"encoding/json"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

func chooseAny(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func setDefault(target map[string]any, key string, value any) {
	if _, exists := target[key]; !exists {
		target[key] = value
	}
}

func isNull(value any) bool {
	return value == nil
}

func lookupString(value map[string]any, path ...string) string {
	var current any = value
	for _, part := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = object[part]
	}
	return xutil.LookupStringFromAny(current)
}

func lookupAny(value map[string]any, path ...string) any {
	var current any = value
	for _, part := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = object[part]
	}
	return current
}

func lookupMap(value map[string]any, path ...string) map[string]any {
	current, _ := lookupAny(value, path...).(map[string]any)
	return current
}

func lookupMapFromAny(value any) map[string]any {
	current, _ := value.(map[string]any)
	if current == nil {
		return map[string]any{}
	}
	return xutil.CloneMap(current)
}

func choose(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func normalizeItemKind(raw string) string {
	switch raw {
	case "agentMessage", "assistant_message", "assistantMessage":
		return "agent_message"
	case "userMessage", "user_message":
		return "user_message"
	case "plan":
		return "plan"
	case "reasoning":
		return "reasoning"
	case "commandExecution", "command_execution":
		return "command_execution"
	case "webSearch", "web_search":
		return "web_search"
	case "fileChange", "file_change":
		return "file_change"
	case "contextCompaction", "context_compaction":
		return "context_compaction"
	case "enteredReviewMode", "entered_review_mode":
		return "entered_review_mode"
	case "exitedReviewMode", "exited_review_mode":
		return "exited_review_mode"
	case "imageGeneration", "image_generation", "imageGenerationCall", "image_generation_call":
		return "image_generation"
	case "mcpToolCall", "mcp_tool_call":
		return "mcp_tool_call"
	case "dynamicToolCall", "dynamic_tool_call":
		return "dynamic_tool_call"
	case "collabToolCall", "collabAgentToolCall", "collab_agent_tool_call":
		return "delegated_task"
	default:
		return raw
	}
}

func extractItemMetadata(itemKind string, item map[string]any) map[string]any {
	metadata := map[string]any{}
	if item == nil {
		return metadata
	}
	if text := extractItemText(item); text != "" {
		metadata["text"] = text
	}
	switch itemKind {
	case "entered_review_mode", "exited_review_mode":
		if review := xutil.FirstNonEmpty(
			xutil.LookupStringFromAny(item["review"]),
			lookupString(item, "result", "review"),
		); review != "" {
			metadata["review"] = review
		}
	case "reasoning":
		if summary := extractStringList(item["summary"]); len(summary) > 0 {
			metadata["summary"] = summary
		}
		if content := extractStringList(item["content"]); len(content) > 0 {
			metadata["content"] = content
		}
	case "image_generation":
		if revisedPrompt := xutil.FirstNonEmpty(
			xutil.LookupStringFromAny(item["revised_prompt"]),
			xutil.LookupStringFromAny(item["revisedPrompt"]),
		); revisedPrompt != "" {
			metadata["revisedPrompt"] = revisedPrompt
		}
		if savedPath := xutil.FirstNonEmpty(
			xutil.LookupStringFromAny(item["saved_path"]),
			xutil.LookupStringFromAny(item["savedPath"]),
		); savedPath != "" {
			metadata["savedPath"] = savedPath
		}
		if imageBase64 := xutil.FirstNonEmpty(
			xutil.LookupStringFromAny(item["result"]),
			lookupString(item, "result", "data"),
			lookupString(item, "result", "b64_json"),
			lookupString(item, "result", "base64"),
		); imageBase64 != "" {
			metadata["imageBase64"] = imageBase64
		}
	case "dynamic_tool_call":
		if tool := xutil.FirstNonEmpty(
			xutil.LookupStringFromAny(item["tool"]),
			xutil.LookupStringFromAny(item["name"]),
		); tool != "" {
			metadata["tool"] = tool
		}
		if arguments := xutil.CloneJSONValue(firstNonNil(
			item["arguments"],
			item["args"],
			lookupAny(item, "invocation", "arguments"),
			lookupAny(item, "input", "arguments"),
			lookupAny(item, "input"),
		)); arguments != nil {
			metadata["arguments"] = arguments
		}
		if success, ok := item["success"].(bool); ok {
			metadata["success"] = success
		}
		if contentItems := extractDynamicToolContentItems(item); len(contentItems) > 0 {
			metadata["contentItems"] = contentItems
		}
		if text, ok := metadata["text"].(string); !ok || strings.TrimSpace(text) == "" {
			if text := extractDynamicToolSummaryText(item); text != "" {
				metadata["text"] = text
			}
		}
	case "delegated_task":
		if subagentType := xutil.FirstNonEmpty(
			xutil.LookupStringFromAny(item["subagentType"]),
			xutil.LookupStringFromAny(item["subagent_type"]),
			xutil.LookupStringFromAny(item["agentType"]),
			xutil.LookupStringFromAny(item["agent_type"]),
			lookupString(item, "input", "subagentType"),
			lookupString(item, "input", "subagent_type"),
			lookupString(item, "input", "agentType"),
			lookupString(item, "input", "agent_type"),
			lookupString(item, "invocation", "subagentType"),
			lookupString(item, "invocation", "subagent_type"),
			lookupString(item, "invocation", "agentType"),
			lookupString(item, "invocation", "agent_type"),
			lookupString(item, "task", "subagentType"),
			lookupString(item, "task", "subagent_type"),
		); subagentType != "" {
			metadata["subagentType"] = subagentType
		}
		if description := xutil.FirstNonEmpty(
			xutil.LookupStringFromAny(item["description"]),
			lookupString(item, "input", "description"),
			lookupString(item, "invocation", "description"),
			lookupString(item, "task", "description"),
		); description != "" {
			metadata["description"] = description
		}
		if prompt := xutil.FirstNonEmpty(
			xutil.LookupStringFromAny(item["prompt"]),
			lookupString(item, "input", "prompt"),
			lookupString(item, "invocation", "prompt"),
			lookupString(item, "task", "prompt"),
		); prompt != "" {
			metadata["prompt"] = prompt
		}
		if text, ok := metadata["text"].(string); !ok || strings.TrimSpace(text) == "" {
			if text := buildDelegatedTaskText(metadata); text != "" {
				metadata["text"] = text
			}
		}
	case "mcp_tool_call":
		if server := xutil.FirstNonEmpty(
			xutil.LookupStringFromAny(item["server"]),
			lookupString(item, "invocation", "server"),
		); server != "" {
			metadata["server"] = server
		}
		if tool := xutil.FirstNonEmpty(
			xutil.LookupStringFromAny(item["tool"]),
			xutil.LookupStringFromAny(item["name"]),
			lookupString(item, "invocation", "tool"),
		); tool != "" {
			metadata["tool"] = tool
		}
		if errorMessage := xutil.FirstNonEmpty(
			lookupString(item, "error", "message"),
			xutil.LookupStringFromAny(item["errorMessage"]),
			xutil.LookupStringFromAny(item["error_message"]),
			xutil.LookupStringFromAny(item["error"]),
		); errorMessage != "" {
			metadata["errorMessage"] = errorMessage
		}
		if arguments := xutil.CloneJSONValue(firstNonNil(
			item["arguments"],
			lookupAny(item, "invocation", "arguments"),
		)); arguments != nil {
			metadata["arguments"] = arguments
		}
		if result := xutil.CloneJSONValue(item["result"]); result != nil {
			metadata["result"] = result
		}
		if result := lookupMap(item, "result"); len(result) != 0 {
			if content := xutil.CloneJSONValue(result["content"]); content != nil {
				metadata["resultContent"] = content
			}
			if structuredContent := xutil.CloneJSONValue(result["structuredContent"]); structuredContent != nil {
				metadata["resultStructuredContent"] = structuredContent
			}
			if meta := lookupMap(result, "_meta"); len(meta) != 0 {
				metadata["resultMeta"] = meta
			}
		}
		if durationMs := xutil.LookupIntFromAny(item["durationMs"]); durationMs != 0 || item["durationMs"] != nil {
			metadata["durationMs"] = durationMs
		} else if durationMs := xutil.LookupIntFromAny(item["duration_ms"]); durationMs != 0 || item["duration_ms"] != nil {
			metadata["durationMs"] = durationMs
		}
	case "command_execution":
		if command := xutil.FirstNonEmpty(
			xutil.LookupStringFromAny(item["command"]),
			xutil.LookupStringFromAny(item["cmd"]),
		); command != "" {
			metadata["command"] = command
		}
		if cwd := xutil.FirstNonEmpty(
			xutil.LookupStringFromAny(item["cwd"]),
			xutil.LookupStringFromAny(item["workdir"]),
			xutil.LookupStringFromAny(item["workingDirectory"]),
		); cwd != "" {
			metadata["cwd"] = cwd
		}
		if exitCode := xutil.LookupIntFromAny(item["exitCode"]); exitCode != 0 || item["exitCode"] != nil {
			metadata["exitCode"] = exitCode
		} else if exitCode := xutil.LookupIntFromAny(item["exit_code"]); exitCode != 0 || item["exit_code"] != nil {
			metadata["exitCode"] = exitCode
		}
	case "web_search":
		if query := xutil.FirstNonEmpty(
			xutil.LookupStringFromAny(item["query"]),
			lookupString(item, "action", "query"),
		); query != "" {
			metadata["query"] = query
		}
		action := lookupMap(item, "action")
		if actionType := normalizeWebSearchActionType(xutil.FirstNonEmpty(
			xutil.LookupStringFromAny(action["type"]),
			xutil.LookupStringFromAny(item["actionType"]),
			xutil.LookupStringFromAny(item["action_type"]),
		)); actionType != "" {
			metadata["actionType"] = actionType
		}
		if queries := extractStringList(firstNonNil(
			action["queries"],
			item["queries"],
		)); len(queries) > 0 {
			metadata["queries"] = queries
		}
		if url := xutil.FirstNonEmpty(
			xutil.LookupStringFromAny(action["url"]),
			xutil.LookupStringFromAny(item["url"]),
		); url != "" {
			metadata["url"] = url
		}
		if pattern := xutil.FirstNonEmpty(
			xutil.LookupStringFromAny(action["pattern"]),
			xutil.LookupStringFromAny(item["pattern"]),
		); pattern != "" {
			metadata["pattern"] = pattern
		}
	}
	return metadata
}

func extractCommandExecutionExploration(itemKind string, item map[string]any) *agentproto.ExplorationActions {
	if itemKind != "command_execution" || item == nil {
		return nil
	}
	source, exists := item["commandActions"]
	if !exists {
		return nil
	}
	rawActions, ok := source.([]any)
	if !ok || len(rawActions) == 0 {
		return &agentproto.ExplorationActions{}
	}
	actions := make([]agentproto.ExplorationAction, 0, len(rawActions))
	for _, raw := range rawActions {
		action, ok := raw.(map[string]any)
		if !ok {
			return &agentproto.ExplorationActions{}
		}
		mapped, ok := mapCommandExecutionExplorationAction(action)
		if !ok {
			return &agentproto.ExplorationActions{}
		}
		actions = append(actions, mapped)
	}
	return &agentproto.ExplorationActions{Actions: actions}
}

func mapCommandExecutionExplorationAction(action map[string]any) (agentproto.ExplorationAction, bool) {
	actionType := strings.TrimSpace(xutil.LookupStringFromAny(action["type"]))
	switch actionType {
	case "read":
		path := strings.TrimSpace(xutil.LookupStringFromAny(action["path"]))
		name := strings.TrimSpace(xutil.LookupStringFromAny(action["name"]))
		item := choose(path, name)
		if item == "" {
			return agentproto.ExplorationAction{}, false
		}
		return agentproto.ExplorationAction{Kind: agentproto.ExplorationActionRead, Items: []string{item}}, true
	case "search":
		query := strings.TrimSpace(xutil.LookupStringFromAny(action["query"]))
		if query == "" {
			return agentproto.ExplorationAction{}, false
		}
		return agentproto.ExplorationAction{
			Kind:      agentproto.ExplorationActionSearch,
			Summary:   query,
			Secondary: strings.TrimSpace(xutil.LookupStringFromAny(action["path"])),
		}, true
	case "listFiles":
		path := strings.TrimSpace(xutil.LookupStringFromAny(action["path"]))
		summary := choose(path, strings.TrimSpace(xutil.LookupStringFromAny(action["command"])))
		if summary == "" {
			return agentproto.ExplorationAction{}, false
		}
		return agentproto.ExplorationAction{Kind: agentproto.ExplorationActionList, Summary: summary}, true
	default:
		return agentproto.ExplorationAction{}, false
	}
}

func extractItemStatus(item map[string]any) string {
	if item == nil {
		return ""
	}
	return xutil.FirstNonEmpty(
		xutil.LookupStringFromAny(item["status"]),
		lookupString(item, "item", "status"),
	)
}

func extractFileChangeRecords(itemKind string, item map[string]any) []agentproto.FileChangeRecord {
	if itemKind != "file_change" || item == nil {
		return nil
	}
	source := item["changes"]
	if source == nil {
		source = item["fileChanges"]
	}
	if source == nil {
		source = lookupAny(item, "fileChange", "changes")
	}
	if source == nil {
		return nil
	}
	var rawChanges []any
	switch typed := source.(type) {
	case []any:
		rawChanges = typed
	case []map[string]any:
		rawChanges = make([]any, 0, len(typed))
		for _, current := range typed {
			rawChanges = append(rawChanges, current)
		}
	default:
		return nil
	}
	records := make([]agentproto.FileChangeRecord, 0, len(rawChanges))
	for _, raw := range rawChanges {
		record, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		path := xutil.FirstNonEmpty(
			xutil.LookupStringFromAny(record["path"]),
			lookupString(record, "file", "path"),
			xutil.LookupStringFromAny(record["new_path"]),
		)
		kind, movePath := extractPatchChangeKind(record["kind"])
		if movePath == "" {
			movePath = xutil.FirstNonEmpty(
				xutil.LookupStringFromAny(record["move_path"]),
				xutil.LookupStringFromAny(record["movePath"]),
			)
		}
		diff := xutil.FirstNonEmpty(
			xutil.LookupStringFromAny(record["diff"]),
			xutil.LookupStringFromAny(record["patch"]),
		)
		if path == "" && movePath == "" && diff == "" && kind == "" {
			continue
		}
		records = append(records, agentproto.FileChangeRecord{
			Path:     path,
			Kind:     kind,
			MovePath: movePath,
			Diff:     diff,
		})
	}
	if len(records) == 0 {
		return nil
	}
	return records
}

func extractPatchChangeKind(value any) (agentproto.FileChangeKind, string) {
	switch typed := value.(type) {
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "add":
			return agentproto.FileChangeAdd, ""
		case "delete":
			return agentproto.FileChangeDelete, ""
		case "update":
			return agentproto.FileChangeUpdate, ""
		}
	case map[string]any:
		kind, movePath := extractPatchChangeKind(typed["type"])
		if movePath == "" {
			movePath = xutil.FirstNonEmpty(
				xutil.LookupStringFromAny(typed["move_path"]),
				xutil.LookupStringFromAny(typed["movePath"]),
			)
		}
		return kind, movePath
	}
	return "", ""
}

func extractItemText(item map[string]any) string {
	if text := xutil.LookupStringFromAny(item["text"]); text != "" {
		return text
	}
	return extractTextFromContentArray(
		firstNonNil(
			item["content"],
			item["contentItems"],
			item["content_items"],
			item["output"],
			lookupAny(item, "result", "content"),
			lookupAny(item, "result", "contentItems"),
			lookupAny(item, "result", "content_items"),
			lookupAny(item, "result", "output"),
		),
	)
}

func extractStringList(value any) []string {
	raw, _ := value.([]any)
	if len(raw) == 0 {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, current := range raw {
		if text := xutil.LookupStringFromAny(current); text != "" {
			out = append(out, text)
		}
	}
	return out
}

func normalizeWebSearchActionType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "search":
		return "search"
	case "openpage", "open_page":
		return "open_page"
	case "findinpage", "find_in_page":
		return "find_in_page"
	case "other":
		return "other"
	default:
		return ""
	}
}

func extractDynamicToolContentItems(item map[string]any) []map[string]any {
	source := firstNonNil(
		item["contentItems"],
		item["content_items"],
		item["content"],
		item["output"],
		lookupAny(item, "result", "contentItems"),
		lookupAny(item, "result", "content_items"),
		lookupAny(item, "result", "content"),
		lookupAny(item, "result", "output"),
	)
	if source == nil {
		return nil
	}
	rawEntries := contentArrayValues(source)
	if len(rawEntries) == 0 {
		return nil
	}
	items := make([]map[string]any, 0, len(rawEntries))
	for _, current := range rawEntries {
		entry, _ := current.(map[string]any)
		if entry == nil {
			continue
		}
		switch normalizeStructuredContentType(xutil.LookupStringFromAny(entry["type"])) {
		case "text":
			text := xutil.FirstNonEmpty(
				xutil.LookupStringFromAny(entry["text"]),
				xutil.LookupStringFromAny(entry["value"]),
			)
			if text == "" {
				continue
			}
			items = append(items, map[string]any{
				"type": "text",
				"text": text,
			})
		case "image":
			imageURL := xutil.FirstNonEmpty(
				xutil.LookupStringFromAny(entry["image_url"]),
				xutil.LookupStringFromAny(entry["imageUrl"]),
				xutil.LookupStringFromAny(entry["url"]),
			)
			if imageURL == "" {
				continue
			}
			record := map[string]any{
				"type": "image",
				"url":  imageURL,
			}
			if looksLikeDataURL(imageURL) {
				record["imageBase64"] = imageURL
			}
			items = append(items, record)
		}
	}
	return items
}

func extractDynamicToolSummaryText(item map[string]any) string {
	if text := extractTextFromContentArray(
		firstNonNil(
			item["contentItems"],
			item["content_items"],
			item["content"],
			item["output"],
			lookupAny(item, "result", "contentItems"),
			lookupAny(item, "result", "content_items"),
			lookupAny(item, "result", "content"),
			lookupAny(item, "result", "output"),
		),
	); text != "" {
		return text
	}
	value := firstNonNil(item["output"], item["result"])
	if value == nil {
		return ""
	}
	if rendered := compactStructuredValue(value); rendered != "" {
		return rendered
	}
	return ""
}

func buildDelegatedTaskText(metadata map[string]any) string {
	if len(metadata) == 0 {
		return ""
	}
	description := strings.TrimSpace(xutil.LookupStringFromAny(metadata["description"]))
	subagentType := strings.TrimSpace(xutil.LookupStringFromAny(metadata["subagentType"]))
	switch {
	case description != "" && subagentType != "":
		return "Task (" + subagentType + "): " + description
	case description != "":
		return "Task: " + description
	case subagentType != "":
		return "Task (" + subagentType + ")"
	default:
		return "Task"
	}
}

func extractTextFromContentArray(source any) string {
	rawEntries := contentArrayValues(source)
	if len(rawEntries) == 0 {
		return ""
	}
	parts := make([]string, 0, len(rawEntries))
	for _, current := range rawEntries {
		entry, _ := current.(map[string]any)
		if entry == nil {
			continue
		}
		switch normalizeStructuredContentType(xutil.LookupStringFromAny(entry["type"])) {
		case "text":
			if text := xutil.FirstNonEmpty(
				xutil.LookupStringFromAny(entry["text"]),
				xutil.LookupStringFromAny(entry["value"]),
			); text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, "\n")
}

func contentArrayValues(source any) []any {
	switch typed := source.(type) {
	case []any:
		return typed
	case []map[string]any:
		out := make([]any, 0, len(typed))
		for _, current := range typed {
			out = append(out, current)
		}
		return out
	default:
		return nil
	}
}

func normalizeStructuredContentType(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.ReplaceAll(normalized, "_", "")
	switch normalized {
	case "text", "inputtext":
		return "text"
	case "image", "inputimage":
		return "image"
	default:
		return normalized
	}
}

func looksLikeDataURL(value string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(value)), "data:")
}

func compactStructuredValue(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case map[string]any, []any:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(encoded))
	default:
		return ""
	}
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}
