package claudesessionstore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func isInternalInteractionTool(toolName string) bool {
	switch strings.TrimSpace(toolName) {
	case "AskUserQuestion", "ExitPlanMode":
		return true
	default:
		return false
	}
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

func mergeClaudeFileChangeMetadata(metadata map[string]any, toolName string, input map[string]any) {
	if metadata == nil {
		return
	}
	metadata["semanticKind"] = "file_change_request"
	metadata["suppressFinalText"] = true
	mergeClaudeFileChangeMetadataPayload(metadata, toolName, input)
}

func mergeClaudeFileChangeMetadataPayload(metadata map[string]any, toolName string, payload map[string]any) {
	if metadata == nil || len(payload) == 0 {
		return
	}
	toolName = strings.TrimSpace(toolName)
	if toolName != "" {
		metadata["tool"] = toolName
	}
	if path := firstNonEmptyString(
		lookupStringFromAny(payload["filePath"]),
		lookupStringFromAny(payload["file_path"]),
		lookupStringFromAny(payload["path"]),
		lookupStringFromAny(payload["notebook_path"]),
	); path != "" {
		metadata["filePath"] = path
	}
	if oldString := firstNonEmptyString(
		lookupStringFromAny(payload["oldString"]),
		lookupStringFromAny(payload["old_string"]),
		lookupStringFromAny(payload["originalFile"]),
	); oldString != "" {
		metadata["oldString"] = oldString
	}
	if newString := firstNonEmptyString(
		lookupStringFromAny(payload["newString"]),
		lookupStringFromAny(payload["new_string"]),
		lookupStringFromAny(payload["content"]),
		lookupStringFromAny(payload["new_source"]),
	); newString != "" {
		metadata["newString"] = newString
	}
	if replaceAll, ok := claudeLookupBool(payload, "replaceAll", "replace_all"); ok {
		metadata["replaceAll"] = replaceAll
	}
	if changeType := strings.TrimSpace(lookupStringFromAny(payload["type"])); changeType != "" {
		metadata["changeType"] = changeType
	}
	if editMode := strings.TrimSpace(lookupStringFromAny(payload["edit_mode"])); editMode != "" {
		metadata["editMode"] = editMode
	}
	if cellID := strings.TrimSpace(lookupStringFromAny(payload["cell_id"])); cellID != "" {
		metadata["cellID"] = cellID
	}
	if cellType := strings.TrimSpace(lookupStringFromAny(payload["cell_type"])); cellType != "" {
		metadata["cellType"] = cellType
	}
	if records := mapsFromAny(payload["structuredPatch"]); len(records) != 0 {
		metadata["structuredPatchRecords"] = records
	}
	if textPatch := strings.TrimSpace(lookupStringFromAny(payload["structuredPatch"])); textPatch != "" {
		metadata["structuredPatch"] = textPatch
	}
}

func claudeLookupBool(values map[string]any, keys ...string) (bool, bool) {
	for _, key := range keys {
		value, ok := values[key]
		if !ok {
			continue
		}
		current, ok := value.(bool)
		if ok {
			return current, true
		}
	}
	return false, false
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

func localSessionPlaneError(command agentproto.Command, code, message string, err error) agentproto.ErrorInfo {
	return agentproto.ErrorInfo{
		Code:             code,
		Layer:            "wrapper",
		Stage:            "local_session_plane",
		Operation:        string(command.Kind),
		Message:          message,
		Details:          strings.TrimSpace(err.Error()),
		SurfaceSessionID: command.Origin.Surface,
		CommandID:        command.CommandID,
		ThreadID:         command.Target.ThreadID,
		TurnID:           command.Target.TurnID,
	}
}
