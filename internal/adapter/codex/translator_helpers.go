package codex

import (
	"encoding/json"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func chooseAny(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func cloneMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return map[string]any{}
	}
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
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
	return lookupStringFromAny(current)
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
	return cloneMap(current)
}

func lookupStringFromAny(value any) string {
	switch current := value.(type) {
	case string:
		return current
	default:
		return ""
	}
}

func lookupIntFromAny(value any) int {
	switch current := value.(type) {
	case int:
		return current
	case int32:
		return int(current)
	case int64:
		return int(current)
	case float64:
		return int(current)
	default:
		return 0
	}
}

func lookupBoolFromAny(value any) bool {
	current, _ := value.(bool)
	return current
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func extractJSONRPCErrorMessage(message map[string]any) string {
	if message == nil {
		return ""
	}
	errorMap, _ := message["error"].(map[string]any)
	return firstNonEmptyString(
		lookupStringFromAny(errorMap["message"]),
		lookupStringFromAny(message["error"]),
	)
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
	case "fileChange", "file_change":
		return "file_change"
	case "imageGeneration", "image_generation", "imageGenerationCall", "image_generation_call":
		return "image_generation"
	case "mcpToolCall", "mcp_tool_call":
		return "mcp_tool_call"
	case "dynamicToolCall", "dynamic_tool_call":
		return "dynamic_tool_call"
	case "collabAgentToolCall", "collab_agent_tool_call":
		return "collab_agent_tool_call"
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
	case "reasoning":
		if summary := extractStringList(item["summary"]); len(summary) > 0 {
			metadata["summary"] = summary
		}
		if content := extractStringList(item["content"]); len(content) > 0 {
			metadata["content"] = content
		}
	case "image_generation":
		if revisedPrompt := firstNonEmptyString(
			lookupStringFromAny(item["revised_prompt"]),
			lookupStringFromAny(item["revisedPrompt"]),
		); revisedPrompt != "" {
			metadata["revisedPrompt"] = revisedPrompt
		}
		if savedPath := firstNonEmptyString(
			lookupStringFromAny(item["saved_path"]),
			lookupStringFromAny(item["savedPath"]),
		); savedPath != "" {
			metadata["savedPath"] = savedPath
		}
		if imageBase64 := firstNonEmptyString(
			lookupStringFromAny(item["result"]),
			lookupString(item, "result", "data"),
			lookupString(item, "result", "b64_json"),
			lookupString(item, "result", "base64"),
		); imageBase64 != "" {
			metadata["imageBase64"] = imageBase64
		}
	case "dynamic_tool_call":
		if tool := firstNonEmptyString(
			lookupStringFromAny(item["tool"]),
			lookupStringFromAny(item["name"]),
		); tool != "" {
			metadata["tool"] = tool
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
	case "mcp_tool_call":
		if server := firstNonEmptyString(
			lookupStringFromAny(item["server"]),
			lookupString(item, "invocation", "server"),
		); server != "" {
			metadata["server"] = server
		}
		if tool := firstNonEmptyString(
			lookupStringFromAny(item["tool"]),
			lookupStringFromAny(item["name"]),
			lookupString(item, "invocation", "tool"),
		); tool != "" {
			metadata["tool"] = tool
		}
		if errorMessage := firstNonEmptyString(
			lookupString(item, "error", "message"),
			lookupStringFromAny(item["errorMessage"]),
			lookupStringFromAny(item["error_message"]),
			lookupStringFromAny(item["error"]),
		); errorMessage != "" {
			metadata["errorMessage"] = errorMessage
		}
		if durationMs := lookupIntFromAny(item["durationMs"]); durationMs != 0 || item["durationMs"] != nil {
			metadata["durationMs"] = durationMs
		} else if durationMs := lookupIntFromAny(item["duration_ms"]); durationMs != 0 || item["duration_ms"] != nil {
			metadata["durationMs"] = durationMs
		}
	case "command_execution":
		if command := firstNonEmptyString(
			lookupStringFromAny(item["command"]),
			lookupStringFromAny(item["cmd"]),
		); command != "" {
			metadata["command"] = command
		}
		if cwd := firstNonEmptyString(
			lookupStringFromAny(item["cwd"]),
			lookupStringFromAny(item["workdir"]),
			lookupStringFromAny(item["workingDirectory"]),
		); cwd != "" {
			metadata["cwd"] = cwd
		}
		if exitCode := lookupIntFromAny(item["exitCode"]); exitCode != 0 || item["exitCode"] != nil {
			metadata["exitCode"] = exitCode
		} else if exitCode := lookupIntFromAny(item["exit_code"]); exitCode != 0 || item["exit_code"] != nil {
			metadata["exitCode"] = exitCode
		}
	}
	return metadata
}

func extractItemStatus(item map[string]any) string {
	if item == nil {
		return ""
	}
	return firstNonEmptyString(
		lookupStringFromAny(item["status"]),
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
		path := firstNonEmptyString(
			lookupStringFromAny(record["path"]),
			lookupString(record, "file", "path"),
			lookupStringFromAny(record["new_path"]),
		)
		kind, movePath := extractPatchChangeKind(record["kind"])
		if movePath == "" {
			movePath = firstNonEmptyString(
				lookupStringFromAny(record["move_path"]),
				lookupStringFromAny(record["movePath"]),
			)
		}
		diff := firstNonEmptyString(
			lookupStringFromAny(record["diff"]),
			lookupStringFromAny(record["patch"]),
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
			movePath = firstNonEmptyString(
				lookupStringFromAny(typed["move_path"]),
				lookupStringFromAny(typed["movePath"]),
			)
		}
		return kind, movePath
	}
	return "", ""
}

func extractItemText(item map[string]any) string {
	if text := lookupStringFromAny(item["text"]); text != "" {
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
		if text := lookupStringFromAny(current); text != "" {
			out = append(out, text)
		}
	}
	return out
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
		switch normalizeStructuredContentType(lookupStringFromAny(entry["type"])) {
		case "text":
			text := firstNonEmptyString(
				lookupStringFromAny(entry["text"]),
				lookupStringFromAny(entry["value"]),
			)
			if text == "" {
				continue
			}
			items = append(items, map[string]any{
				"type": "text",
				"text": text,
			})
		case "image":
			imageURL := firstNonEmptyString(
				lookupStringFromAny(entry["image_url"]),
				lookupStringFromAny(entry["imageUrl"]),
				lookupStringFromAny(entry["url"]),
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
		switch normalizeStructuredContentType(lookupStringFromAny(entry["type"])) {
		case "text":
			if text := firstNonEmptyString(
				lookupStringFromAny(entry["text"]),
				lookupStringFromAny(entry["value"]),
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
