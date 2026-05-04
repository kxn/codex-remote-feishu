package codex

import (
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func parseThreadList(result any) []agentproto.ThreadSnapshotRecord {
	var raw []any
	switch value := result.(type) {
	case map[string]any:
		switch current := value["threads"].(type) {
		case []any:
			raw = current
		}
		if len(raw) == 0 {
			switch current := value["data"].(type) {
			case []any:
				raw = current
			}
		}
	case []any:
		raw = value
	}
	output := make([]agentproto.ThreadSnapshotRecord, 0, len(raw))
	for index, current := range raw {
		switch item := current.(type) {
		case string:
			output = append(output, agentproto.ThreadSnapshotRecord{ThreadID: item, Loaded: true, ListOrder: index + 1})
		case map[string]any:
			record := parseThreadRecord(item)
			record.Loaded = true
			record.ListOrder = index + 1
			if record.ThreadID != "" {
				output = append(output, record)
			}
		}
	}
	return output
}

func parseThreadRecord(result any) agentproto.ThreadSnapshotRecord {
	var object map[string]any
	switch value := result.(type) {
	case map[string]any:
		if thread, ok := value["thread"].(map[string]any); ok {
			object = thread
		} else {
			object = value
		}
	default:
		return agentproto.ThreadSnapshotRecord{}
	}
	runtimeStatus := parseThreadRuntimeStatus(firstNonNil(
		object["status"],
		object["threadStatus"],
		object["runtimeStatus"],
		object["state"],
	))
	stateValue := strings.TrimSpace(lookupStringFromAny(object["state"]))
	if stateValue == "" && runtimeStatus != nil {
		stateValue = runtimeStatus.LegacyState()
	}
	loaded := lookupBoolFromAny(object["loaded"])
	if runtimeStatus != nil {
		loaded = runtimeStatus.IsLoaded()
	}
	return agentproto.ThreadSnapshotRecord{
		ThreadID: choose(
			lookupStringFromAny(object["id"]),
			lookupStringFromAny(object["threadId"]),
		),
		ForkedFromID: choose(
			lookupStringFromAny(object["forkedFromId"]),
			lookupStringFromAny(object["forked_from_id"]),
		),
		Source: parseThreadSource(firstNonNil(object["source"], object["sessionSource"])),
		Name: choose(
			lookupStringFromAny(object["name"]),
			lookupStringFromAny(object["title"]),
		),
		Preview: choose(
			lookupStringFromAny(object["preview"]),
			lookupStringFromAny(object["summary"]),
		),
		CWD: choose(
			lookupStringFromAny(object["cwd"]),
			lookupStringFromAny(object["path"]),
		),
		Model: choose(
			lookupString(object, "latestCollaborationMode", "settings", "model"),
			lookupString(object, "collaborationMode", "settings", "model"),
			lookupStringFromAny(object["model"]),
		),
		ReasoningEffort: choose(
			lookupString(object, "latestCollaborationMode", "settings", "reasoning_effort"),
			lookupString(object, "collaborationMode", "settings", "reasoning_effort"),
			lookupString(object, "config", "model_reasoning_effort"),
			lookupString(object, "config", "reasoning_effort"),
			lookupStringFromAny(object["effort"]),
		),
		PlanMode: choose(
			normalizeObservedPlanMode(lookupString(object, "latestCollaborationMode", "mode")),
			normalizeObservedPlanMode(lookupString(object, "collaborationMode", "mode")),
		),
		Loaded:   loaded,
		Archived: lookupBoolFromAny(object["archived"]),
		State:    stateValue,
		ListOrder: lookupIntFromAny(chooseAny(
			object["listOrder"],
			object["list_order"],
		)),
		RuntimeStatus: runtimeStatus,
	}
}

func parseThreadRuntimeStatus(source any) *agentproto.ThreadRuntimeStatus {
	switch typed := source.(type) {
	case string:
		statusType := agentproto.NormalizeThreadRuntimeStatusType(typed)
		if statusType == "" {
			return nil
		}
		return &agentproto.ThreadRuntimeStatus{Type: statusType}
	case map[string]any:
		statusType := agentproto.NormalizeThreadRuntimeStatusType(firstNonEmptyString(
			lookupStringFromAny(typed["type"]),
			lookupStringFromAny(typed["status"]),
			lookupStringFromAny(typed["state"]),
		))
		if statusType == "" {
			return nil
		}
		status := &agentproto.ThreadRuntimeStatus{Type: statusType}
		if statusType == agentproto.ThreadRuntimeStatusTypeActive {
			status.ActiveFlags = parseThreadActiveFlags(typed["activeFlags"])
		}
		return status
	default:
		return nil
	}
}

func parseThreadActiveFlags(source any) []agentproto.ThreadActiveFlag {
	raw := contentArrayValues(source)
	if len(raw) == 0 {
		return nil
	}
	flags := make([]agentproto.ThreadActiveFlag, 0, len(raw))
	seen := map[agentproto.ThreadActiveFlag]bool{}
	for _, current := range raw {
		flag := agentproto.NormalizeThreadActiveFlag(lookupStringFromAny(current))
		if flag == "" || seen[flag] {
			continue
		}
		seen[flag] = true
		flags = append(flags, flag)
	}
	if len(flags) == 0 {
		return nil
	}
	return flags
}

func parseThreadHistoryRecord(result any) agentproto.ThreadHistoryRecord {
	record := agentproto.ThreadHistoryRecord{}
	if object, ok := result.(map[string]any); ok {
		record.Thread = parseThreadRecord(object)
		turnSource := object["turns"]
		if turnSource == nil {
			if thread, ok := object["thread"].(map[string]any); ok {
				if record.Thread.ThreadID == "" {
					record.Thread = parseThreadRecord(thread)
				}
				turnSource = thread["turns"]
			}
		}
		record.Turns = parseThreadHistoryTurns(turnSource)
	}
	return record
}

func parseThreadHistoryTurns(source any) []agentproto.ThreadHistoryTurnRecord {
	raw := contentArrayValues(source)
	if len(raw) == 0 {
		return nil
	}
	out := make([]agentproto.ThreadHistoryTurnRecord, 0, len(raw))
	for _, current := range raw {
		turnMap, _ := current.(map[string]any)
		if turnMap == nil {
			continue
		}
		record := agentproto.ThreadHistoryTurnRecord{
			TurnID: choose(
				lookupStringFromAny(turnMap["id"]),
				lookupStringFromAny(turnMap["turnId"]),
			),
			Status: choose(
				lookupStringFromAny(turnMap["status"]),
				lookupStringFromAny(turnMap["state"]),
			),
			StartedAt:    parseProtocolTime(firstNonNil(turnMap["startedAt"], turnMap["started_at"], turnMap["createdAt"], turnMap["created_at"])),
			CompletedAt:  parseProtocolTime(firstNonNil(turnMap["completedAt"], turnMap["completed_at"], turnMap["finishedAt"], turnMap["finished_at"])),
			ErrorMessage: choose(lookupStringFromAny(turnMap["errorMessage"]), lookupString(turnMap, "error", "message")),
			RequestID: choose(
				canonicalRequestID(turnMap["requestId"]),
				canonicalRequestID(turnMap["request_id"]),
			),
			Items: parseThreadHistoryItems(turnMap["items"]),
		}
		if record.TurnID == "" {
			continue
		}
		out = append(out, record)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseThreadHistoryItems(source any) []agentproto.ThreadHistoryItemRecord {
	raw := contentArrayValues(source)
	if len(raw) == 0 {
		return nil
	}
	out := make([]agentproto.ThreadHistoryItemRecord, 0, len(raw))
	for _, current := range raw {
		itemMap, _ := current.(map[string]any)
		if itemMap == nil {
			continue
		}
		itemKind := normalizeItemKind(choose(
			lookupStringFromAny(itemMap["type"]),
			lookupString(itemMap, "item", "type"),
		))
		record := agentproto.ThreadHistoryItemRecord{
			ItemID: choose(
				lookupStringFromAny(itemMap["id"]),
				lookupStringFromAny(itemMap["itemId"]),
			),
			Kind:   itemKind,
			Status: extractItemStatus(itemMap),
			Text:   extractItemText(itemMap),
		}
		if itemKind == "command_execution" {
			if metadata := extractItemMetadata(itemKind, itemMap); len(metadata) != 0 {
				if command, _ := metadata["command"].(string); strings.TrimSpace(command) != "" {
					record.Command = strings.TrimSpace(command)
				}
				if cwd, _ := metadata["cwd"].(string); strings.TrimSpace(cwd) != "" {
					record.CWD = strings.TrimSpace(cwd)
				}
				if exitCode, ok := metadata["exitCode"].(int); ok {
					value := exitCode
					record.ExitCode = &value
				}
			}
		} else if itemKind == "delegated_task" {
			if metadata := extractItemMetadata(itemKind, itemMap); len(metadata) != 0 {
				record.Metadata = metadata
				if strings.TrimSpace(record.Text) == "" {
					record.Text = buildDelegatedTaskText(metadata)
				}
			}
		}
		if record.ItemID == "" && record.Kind == "" && record.Text == "" && record.Command == "" {
			continue
		}
		out = append(out, record)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseProtocolTime(value any) time.Time {
	text := strings.TrimSpace(lookupStringFromAny(value))
	if text == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, text); err == nil {
			return parsed
		}
	}
	return time.Time{}
}
