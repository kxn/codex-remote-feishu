package acp

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

func upsertConfigOption(options []map[string]any, option map[string]any) []map[string]any {
	id := strings.TrimSpace(xutil.LookupStringFromAny(option["id"]))
	if id == "" {
		return options
	}
	cloned := xutil.CloneMap(option)
	for i := range options {
		if strings.TrimSpace(xutil.LookupStringFromAny(options[i]["id"])) == id {
			options[i] = cloned
			return options
		}
	}
	return append(options, cloned)
}

func permissionOptionStyle(option permissionOption) string {
	if strings.Contains(option.Kind, "reject") || strings.EqualFold(option.ID, "reject") {
		return "danger"
	}
	return "primary"
}

func resolvePermissionOptionID(response map[string]any) string {
	if id := strings.TrimSpace(xutil.LookupStringFromAny(response["optionId"])); id != "" {
		return id
	}
	if decision := strings.TrimSpace(xutil.LookupStringFromAny(response["decision"])); decision != "" {
		switch decision {
		case "accept", "approved", "allow", "once":
			return "once"
		case "decline", "reject", "denied":
			return "reject"
		default:
			return decision
		}
	}
	if approved, ok := response["approved"].(bool); ok {
		if approved {
			return "once"
		}
		return "reject"
	}
	return ""
}

func permissionApprovalGrant(optionID string, options []permissionOption) string {
	normalizedID := strings.ToLower(strings.TrimSpace(optionID))
	switch normalizedID {
	case "once":
		return "once"
	case "always":
		return "always"
	case "reject":
		return ""
	}
	for _, option := range options {
		if strings.TrimSpace(option.ID) != strings.TrimSpace(optionID) {
			continue
		}
		kind := strings.ToLower(strings.TrimSpace(option.Kind))
		id := strings.ToLower(strings.TrimSpace(option.ID))
		switch {
		case strings.Contains(kind, "reject") || strings.Contains(kind, "deny"):
			return ""
		case strings.Contains(kind, "allow") && (strings.Contains(kind, "always") || strings.Contains(kind, "session")):
			return "always"
		case strings.Contains(kind, "allow"):
			return "once"
		case strings.Contains(id, "always") || strings.Contains(id, "session"):
			return "always"
		case strings.Contains(id, "allow") || strings.Contains(id, "approve") || strings.Contains(id, "accept"):
			return "once"
		}
	}
	return ""
}

func toolItemKind(update map[string]any) string {
	switch normalizeToolKind(xutil.LookupStringFromAny(update["kind"])) {
	case "bash", "execute", "shell", "terminal":
		return "command_execution"
	case "edit", "write", "apply_patch", "patch":
		return "file_change"
	case "fetch", "web_fetch", "websearch", "web_search":
		return "web_search"
	case "task":
		return "delegated_task"
	case "todowrite", "todo_write", "todo":
		return ""
	case "mcp", "mcp_tool", "mcp_tool_call":
		return "mcp_tool_call"
	default:
		if rawInput, _ := update["rawInput"].(map[string]any); rawInput != nil {
			if xutil.LookupStringFromAny(rawInput["server"]) != "" && xutil.LookupStringFromAny(rawInput["tool"]) != "" {
				return "mcp_tool_call"
			}
		}
		return "dynamic_tool_call"
	}
}

func opencodeToolMetadata(update map[string]any, previous map[string]any) map[string]any {
	metadata := xutil.CloneMap(previous)
	toolID := strings.TrimSpace(xutil.LookupStringFromAny(update["toolCallId"]))
	if toolID != "" {
		metadata["toolCallId"] = toolID
	}
	kind := strings.TrimSpace(xutil.LookupStringFromAny(update["kind"]))
	if kind != "" {
		metadata["kind"] = kind
	}
	effectiveKind := xutil.FirstNonEmpty(kind, metadataString(metadata, "kind"))
	rawInput := opencodeRawInput(update, metadata)
	if rawInput != nil {
		metadata["rawInput"] = xutil.CloneMap(rawInput)
		metadata["arguments"] = xutil.CloneMap(rawInput)
	}
	itemKind := toolItemKind(map[string]any{"kind": effectiveKind, "rawInput": rawInput})
	if itemKind == "" && effectiveKind == "" {
		itemKind = toolItemKind(update)
	}
	toolName := opencodeToolDisplayName(effectiveKind, rawInput)
	if toolName != "" {
		metadata["tool"] = toolName
	}
	switch itemKind {
	case "command_execution":
		if command := firstMapString(rawInput, "cmd", "command"); command != "" {
			metadata["command"] = command
		}
		if cwd := firstMapString(rawInput, "cwd"); cwd != "" {
			metadata["cwd"] = cwd
		}
	case "file_change":
		metadata["semanticKind"] = "file_change_request"
		if path := firstMapString(rawInput, "path", "filePath", "file_path"); path != "" {
			metadata["filePath"] = path
		}
	case "delegated_task":
		if description := firstMapString(rawInput, "description", "summary"); description != "" {
			metadata["description"] = description
		}
		if subagentType := firstMapString(rawInput, "subagentType", "subagent_type"); subagentType != "" {
			metadata["subagentType"] = subagentType
		}
	case "mcp_tool_call":
		if server := firstMapString(rawInput, "server", "serverName", "mcpServer"); server != "" {
			metadata["server"] = server
		}
		if tool := firstMapString(rawInput, "tool", "toolName", "name"); tool != "" {
			metadata["tool"] = tool
		}
	case "dynamic_tool_call":
		if isExplorationToolKind(kind) {
			metadata["semanticKind"] = "exploration"
		} else {
			metadata["semanticKind"] = "generic_tool"
		}
	}
	return metadata
}

func opencodeToolCompletionMetadata(metadata map[string]any, update map[string]any) map[string]any {
	out := opencodeToolMetadata(update, metadata)
	if rawOutput := xutil.CloneJSONValue(update["rawOutput"]); rawOutput != nil {
		out["rawOutput"] = rawOutput
		if output, _ := rawOutput.(map[string]any); output != nil {
			if text := firstMapStringPreserve(output, "output", "stdout", "text"); text != "" {
				out["text"] = text
			}
			if errorMessage := firstMapString(output, "error", "errorMessage", "message"); errorMessage != "" {
				out["errorMessage"] = errorMessage
			}
			if _, ok := output["exitCode"]; ok {
				out["exitCode"] = xutil.LookupIntFromAny(output["exitCode"])
			}
		}
	}
	if errorMessage := firstMapString(update, "error", "errorMessage", "message"); errorMessage != "" {
		out["errorMessage"] = errorMessage
	}
	return out
}

func opencodeRawInput(update map[string]any, metadata map[string]any) map[string]any {
	if rawInput, _ := update["rawInput"].(map[string]any); rawInput != nil {
		return xutil.CloneMap(rawInput)
	}
	if rawInput, _ := metadata["rawInput"].(map[string]any); rawInput != nil {
		return xutil.CloneMap(rawInput)
	}
	if rawInput, _ := metadata["arguments"].(map[string]any); rawInput != nil {
		return xutil.CloneMap(rawInput)
	}
	return nil
}

func opencodeToolDisplayName(kind string, rawInput map[string]any) string {
	kind = strings.TrimSpace(kind)
	if tool := firstMapString(rawInput, "tool", "toolName", "name"); tool != "" && strings.HasPrefix(normalizeToolKind(kind), "mcp") {
		return tool
	}
	if kind != "" {
		return kind
	}
	return firstMapString(rawInput, "tool", "toolName", "name")
}

func normalizeToolKind(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.ReplaceAll(normalized, "-", "_")
	return normalized
}

func isExplorationToolKind(kind string) bool {
	switch normalizeToolKind(kind) {
	case "read", "grep", "glob", "list", "ls", "search":
		return true
	default:
		return false
	}
}

func firstMapString(values map[string]any, keys ...string) string {
	if values == nil {
		return ""
	}
	for _, key := range keys {
		if text := strings.TrimSpace(xutil.LookupStringFromAny(values[key])); text != "" {
			return text
		}
	}
	return ""
}

func firstMapStringPreserve(values map[string]any, keys ...string) string {
	if values == nil {
		return ""
	}
	for _, key := range keys {
		if text := xutil.LookupStringFromAny(values[key]); text != "" {
			return text
		}
	}
	return ""
}

func metadataString(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	return strings.TrimSpace(xutil.LookupStringFromAny(values[key]))
}

func (t *Translator) todoPlanEvent(turn *turnState, sessionID string, item *itemState, update map[string]any) (agentproto.Event, bool) {
	status := strings.TrimSpace(xutil.LookupStringFromAny(update["status"]))
	if status != "completed" && status != "failed" {
		return agentproto.Event{}, false
	}
	if item != nil && item.Completed {
		return agentproto.Event{}, false
	}
	if item != nil {
		item.Completed = true
	}
	metadata := map[string]any{}
	if item != nil {
		metadata = item.Metadata
	}
	metadata = opencodeToolMetadata(update, metadata)
	snapshot := buildOpenCodeTodoPlanSnapshot(opencodeRawInput(update, metadata))
	if snapshot == nil {
		return agentproto.Event{}, false
	}
	event := agentproto.Event{
		Kind:         agentproto.EventTurnPlanUpdated,
		ThreadID:     sessionID,
		TurnID:       turn.TurnID,
		PlanSnapshot: snapshot,
	}
	return t.annotateTurnEvent(turn, event), true
}

func buildOpenCodeTodoPlanSnapshot(input map[string]any) *agentproto.TurnPlanSnapshot {
	records := xutil.MapsFromAny(input["todos"])
	if len(records) == 0 {
		records = xutil.MapsFromAny(input["items"])
	}
	if len(records) == 0 {
		return nil
	}
	snapshot := &agentproto.TurnPlanSnapshot{Steps: make([]agentproto.TurnPlanStep, 0, len(records))}
	activeForms := make([]string, 0, len(records))
	for _, record := range records {
		step := strings.TrimSpace(xutil.FirstNonEmpty(
			xutil.LookupStringFromAny(record["content"]),
			xutil.LookupStringFromAny(record["step"]),
			xutil.LookupStringFromAny(record["title"]),
			xutil.LookupStringFromAny(record["activeForm"]),
		))
		if step == "" {
			continue
		}
		status := agentproto.NormalizeTurnPlanStepStatus(xutil.LookupStringFromAny(record["status"]))
		if status == "" {
			status = agentproto.TurnPlanStepStatusPending
		}
		snapshot.Steps = append(snapshot.Steps, agentproto.TurnPlanStep{Step: step, Status: status})
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

func normalizeOpenCodeRPCError(errorPayload any, defaults agentproto.ErrorInfo) agentproto.ErrorInfo {
	payload, _ := errorPayload.(map[string]any)
	message := strings.TrimSpace(xutil.LookupStringFromAny(payload["message"]))
	if message == "" {
		message = "OpenCode ACP request failed."
	}
	lower := strings.ToLower(message)
	code := "opencode_acp_request_failed"
	switch {
	case strings.Contains(lower, "session") && (strings.Contains(lower, "not found") || strings.Contains(lower, "missing")):
		code = "opencode_session_not_found"
	case strings.Contains(lower, "invalid model") || strings.Contains(lower, "unknown model") || strings.Contains(lower, "model") && strings.Contains(lower, "not found"):
		code = "opencode_invalid_model"
	case strings.Contains(lower, "mcp"):
		code = "opencode_mcp_failure"
	case strings.Contains(lower, "auth") || strings.Contains(lower, "api key") || strings.Contains(lower, "oauth") || strings.Contains(lower, "login"):
		code = "opencode_auth_required"
	case strings.Contains(lower, "permission denied") || strings.Contains(lower, "denied by policy") || strings.Contains(lower, "not allowed"):
		code = "opencode_permission_denied"
	}
	return agentproto.ErrorInfo{
		Severity:  agentproto.ErrorSeverityError,
		Code:      code,
		Layer:     defaults.Layer,
		Stage:     defaults.Stage,
		Operation: defaults.Operation,
		Message:   message,
		Details:   xutil.CompactJSON(errorPayload),
		CommandID: defaults.CommandID,
		ThreadID:  defaults.ThreadID,
		TurnID:    defaults.TurnID,
	}.Normalize()
}
