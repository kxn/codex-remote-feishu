package codex

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

func parseThreadSource(source any) *agentproto.ThreadSourceRecord {
	switch typed := source.(type) {
	case string:
		switch strings.TrimSpace(typed) {
		case "cli", "vscode", "exec":
			return &agentproto.ThreadSourceRecord{Kind: agentproto.ThreadSourceKindUser, Name: strings.TrimSpace(typed)}
		case "appServer", "app-server", "app_server", "mcp":
			return &agentproto.ThreadSourceRecord{Kind: agentproto.ThreadSourceKindAppServer, Name: "appServer"}
		case "unknown":
			return &agentproto.ThreadSourceRecord{Kind: agentproto.ThreadSourceKindUnknown, Name: "unknown"}
		default:
			if strings.TrimSpace(typed) == "" {
				return nil
			}
			return &agentproto.ThreadSourceRecord{Kind: agentproto.ThreadSourceKindCustom, Name: strings.TrimSpace(typed)}
		}
	case map[string]any:
		if custom := strings.TrimSpace(xutil.LookupStringFromAny(typed["custom"])); custom != "" {
			return &agentproto.ThreadSourceRecord{Kind: agentproto.ThreadSourceKindCustom, Name: custom}
		}
		if sub := firstNonNil(typed["subAgent"], typed["sub_agent"]); sub != nil {
			return parseSubAgentThreadSource(sub)
		}
	}
	return nil
}

func parseSubAgentThreadSource(source any) *agentproto.ThreadSourceRecord {
	switch typed := source.(type) {
	case string:
		switch strings.TrimSpace(typed) {
		case "review":
			return &agentproto.ThreadSourceRecord{Kind: agentproto.ThreadSourceKindReview, Name: "review"}
		case "compact":
			return &agentproto.ThreadSourceRecord{Kind: agentproto.ThreadSourceKindCompact, Name: "compact"}
		case "memory_consolidation":
			return &agentproto.ThreadSourceRecord{Kind: agentproto.ThreadSourceKindMemoryConsolidation, Name: "memory_consolidation"}
		default:
			if strings.TrimSpace(typed) == "" {
				return nil
			}
			return &agentproto.ThreadSourceRecord{Kind: agentproto.ThreadSourceKindSubAgentOther, Name: strings.TrimSpace(typed)}
		}
	case map[string]any:
		if spawn := lookupMapFromAny(firstNonNil(typed["thread_spawn"], typed["threadSpawn"])); len(spawn) != 0 {
			return &agentproto.ThreadSourceRecord{
				Kind: agentproto.ThreadSourceKindThreadSpawn,
				Name: xutil.FirstNonEmpty(
					xutil.LookupStringFromAny(spawn["agent_role"]),
					xutil.LookupStringFromAny(spawn["agentRole"]),
					xutil.LookupStringFromAny(spawn["agent_nickname"]),
					xutil.LookupStringFromAny(spawn["agentNickname"]),
				),
				ParentThreadID: xutil.FirstNonEmpty(
					xutil.LookupStringFromAny(spawn["parent_thread_id"]),
					xutil.LookupStringFromAny(spawn["parentThreadId"]),
				),
			}
		}
		if other := strings.TrimSpace(xutil.LookupStringFromAny(typed["other"])); other != "" {
			return &agentproto.ThreadSourceRecord{Kind: agentproto.ThreadSourceKindSubAgentOther, Name: other}
		}
	}
	return nil
}
