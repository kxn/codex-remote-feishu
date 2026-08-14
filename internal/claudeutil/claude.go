// Package claudeutil holds claude-session helper functions shared by the
// claude adapter and the claude session store. Keeping them in one package
// prevents the two callers from drifting apart again.
package claudeutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

// ClaudeHomeDir resolves the user's home directory the same way on Unix and
// Windows, preferring explicit HOME/USERPROFILE over os.UserHomeDir.
func ClaudeHomeDir() string {
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

// IsInternalInteractionTool reports whether the tool is an internal interaction
// tool (question/plan) rather than a real tool call.
func IsInternalInteractionTool(toolName string) bool {
	switch strings.TrimSpace(toolName) {
	case "AskUserQuestion", "ExitPlanMode":
		return true
	default:
		return false
	}
}

// ToolUseSummary produces a short human-readable summary of a tool use.
func ToolUseSummary(toolName string, input map[string]any) string {
	if command := strings.TrimSpace(xutil.LookupStringFromAny(input["command"])); command != "" {
		return command
	}
	if description := strings.TrimSpace(xutil.LookupStringFromAny(input["description"])); description != "" {
		return description
	}
	if len(input) != 0 {
		return xutil.CompactJSON(input)
	}
	if strings.TrimSpace(toolName) != "" {
		return toolName
	}
	return ""
}

// ClaudeToolItemKind classifies a claude tool name into a semantic kind.
func ClaudeToolItemKind(toolName string) string {
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

// ClaudeToolMetadata builds the metadata map describing a claude tool use.
func ClaudeToolMetadata(toolName string, input map[string]any) map[string]any {
	metadata := map[string]any{
		"tool":      strings.TrimSpace(toolName),
		"arguments": xutil.CloneMap(input),
	}
	switch ClaudeToolItemKind(toolName) {
	case "command_execution":
		if command := strings.TrimSpace(xutil.LookupStringFromAny(input["command"])); command != "" {
			metadata["command"] = command
		}
		if cwd := strings.TrimSpace(xutil.LookupStringFromAny(input["cwd"])); cwd != "" {
			metadata["cwd"] = cwd
		}
	case "web_search":
		MergeClaudeWebToolMetadata(metadata, toolName, input)
	case "delegated_task":
		metadata["subagentType"] = strings.TrimSpace(xutil.LookupStringFromAny(input["subagent_type"]))
		metadata["description"] = strings.TrimSpace(xutil.LookupStringFromAny(input["description"]))
		if prompt := strings.TrimSpace(xutil.LookupStringFromAny(input["prompt"])); prompt != "" {
			metadata["prompt"] = prompt
		}
	case "file_change":
		MergeClaudeFileChangeMetadata(metadata, toolName, input)
	case "dynamic_tool_call":
		metadata["semanticKind"] = ClaudeDynamicToolSemanticKind(toolName)
		metadata["suppressFinalText"] = true
	}
	return metadata
}

// ClaudeDynamicToolSemanticKind maps a dynamic tool name to a semantic kind.
func ClaudeDynamicToolSemanticKind(toolName string) string {
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

// ClaudeExplorationActions maps Claude-native exploration tools to the shared
// structured exploration carrier.
func ClaudeExplorationActions(toolName string, input map[string]any) *agentproto.ExplorationActions {
	var action agentproto.ExplorationAction
	switch strings.TrimSpace(toolName) {
	case "Read":
		action.Kind = agentproto.ExplorationActionRead
		if path := strings.TrimSpace(xutil.LookupStringFromAny(input["file_path"])); path != "" {
			action.Items = []string{path}
		}
	case "Glob":
		action.Kind = agentproto.ExplorationActionList
		action.Summary = strings.TrimSpace(xutil.FirstNonEmpty(
			xutil.LookupStringFromAny(input["path"]),
			xutil.LookupStringFromAny(input["pattern"]),
		))
	case "Grep":
		action.Kind = agentproto.ExplorationActionSearch
		action.Summary = strings.TrimSpace(xutil.FirstNonEmpty(
			xutil.LookupStringFromAny(input["pattern"]),
			xutil.LookupStringFromAny(input["query"]),
		))
		action.Secondary = strings.TrimSpace(xutil.LookupStringFromAny(input["path"]))
	default:
		return nil
	}
	return &agentproto.ExplorationActions{Actions: []agentproto.ExplorationAction{action}}
}

// MergeClaudeWebToolMetadata fills web_search action metadata for a claude tool.
func MergeClaudeWebToolMetadata(metadata map[string]any, toolName string, input map[string]any) {
	switch strings.TrimSpace(toolName) {
	case "WebSearch":
		metadata["actionType"] = "search"
		if query := xutil.FirstNonEmpty(
			xutil.LookupStringFromAny(input["query"]),
			xutil.LookupStringFromAny(input["q"]),
		); query != "" {
			metadata["query"] = query
		}
	case "WebFetch":
		metadata["actionType"] = "open_page"
		if url := xutil.FirstNonEmpty(
			xutil.LookupStringFromAny(input["url"]),
			xutil.LookupStringFromAny(input["href"]),
		); url != "" {
			metadata["url"] = url
		}
	case "ToolSearch":
		metadata["actionType"] = "find_in_page"
		if pattern := xutil.FirstNonEmpty(
			xutil.LookupStringFromAny(input["pattern"]),
			xutil.LookupStringFromAny(input["query"]),
			xutil.LookupStringFromAny(input["text"]),
		); pattern != "" {
			metadata["pattern"] = pattern
		}
		if url := xutil.FirstNonEmpty(
			xutil.LookupStringFromAny(input["url"]),
			xutil.LookupStringFromAny(input["page_url"]),
		); url != "" {
			metadata["url"] = url
		}
	}
}

// MergeClaudeFileChangeMetadata fills file_change metadata for a claude tool.
func MergeClaudeFileChangeMetadata(metadata map[string]any, toolName string, input map[string]any) {
	if metadata == nil {
		return
	}
	metadata["semanticKind"] = "file_change_request"
	metadata["suppressFinalText"] = true
	MergeClaudeFileChangeMetadataPayload(metadata, toolName, input)
}

// MergeClaudeFileChangeMetadataPayload fills file-change payload details.
func MergeClaudeFileChangeMetadataPayload(metadata map[string]any, toolName string, payload map[string]any) {
	if metadata == nil || len(payload) == 0 {
		return
	}
	toolName = strings.TrimSpace(toolName)
	if toolName != "" {
		metadata["tool"] = toolName
	}
	if path := xutil.FirstNonEmpty(
		xutil.LookupStringFromAny(payload["filePath"]),
		xutil.LookupStringFromAny(payload["file_path"]),
		xutil.LookupStringFromAny(payload["path"]),
		xutil.LookupStringFromAny(payload["notebook_path"]),
	); path != "" {
		metadata["filePath"] = path
	}
	if oldString := xutil.FirstNonEmpty(
		xutil.LookupStringFromAny(payload["oldString"]),
		xutil.LookupStringFromAny(payload["old_string"]),
		xutil.LookupStringFromAny(payload["originalFile"]),
	); oldString != "" {
		metadata["oldString"] = oldString
	}
	if newString := xutil.FirstNonEmpty(
		xutil.LookupStringFromAny(payload["newString"]),
		xutil.LookupStringFromAny(payload["new_string"]),
		xutil.LookupStringFromAny(payload["content"]),
		xutil.LookupStringFromAny(payload["new_source"]),
	); newString != "" {
		metadata["newString"] = newString
	}
	if replaceAll, ok := ClaudeLookupBool(payload, "replaceAll", "replace_all"); ok {
		metadata["replaceAll"] = replaceAll
	}
	if changeType := strings.TrimSpace(xutil.LookupStringFromAny(payload["type"])); changeType != "" {
		metadata["changeType"] = changeType
	}
	if editMode := strings.TrimSpace(xutil.LookupStringFromAny(payload["edit_mode"])); editMode != "" {
		metadata["editMode"] = editMode
	}
	if cellID := strings.TrimSpace(xutil.LookupStringFromAny(payload["cell_id"])); cellID != "" {
		metadata["cellID"] = cellID
	}
	if cellType := strings.TrimSpace(xutil.LookupStringFromAny(payload["cell_type"])); cellType != "" {
		metadata["cellType"] = cellType
	}
	if records := xutil.MapsFromAny(payload["structuredPatch"]); len(records) != 0 {
		metadata["structuredPatchRecords"] = records
	}
	if textPatch := strings.TrimSpace(xutil.LookupStringFromAny(payload["structuredPatch"])); textPatch != "" {
		metadata["structuredPatch"] = textPatch
	}
}

// ClaudeLookupBool looks up the first present bool key and reports it.
func ClaudeLookupBool(values map[string]any, keys ...string) (bool, bool) {
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

// BuildClaudeDelegatedTaskText renders a delegated-task summary string.
func BuildClaudeDelegatedTaskText(metadata map[string]any) string {
	if len(metadata) == 0 {
		return ""
	}
	description := strings.TrimSpace(xutil.LookupStringFromAny(metadata["description"]))
	subagentType := strings.TrimSpace(xutil.LookupStringFromAny(metadata["subagentType"]))
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
