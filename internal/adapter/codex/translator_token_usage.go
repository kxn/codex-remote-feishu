package codex

import (
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

func extractThreadTokenUsageNotification(message map[string]any) (string, string, *agentproto.ThreadTokenUsage) {
	params := lookupMap(message, "params")
	if len(params) == 0 {
		return "", "", nil
	}
	threadID := xutil.LookupStringFromAny(params["threadId"])
	turnID := xutil.LookupStringFromAny(params["turnId"])
	usageMap := lookupMapFromAny(params["tokenUsage"])
	if len(usageMap) == 0 {
		return threadID, turnID, nil
	}
	usage := &agentproto.ThreadTokenUsage{
		Total: extractTokenUsageBreakdown(lookupMapFromAny(usageMap["total"])),
		Last:  extractTokenUsageBreakdown(lookupMapFromAny(usageMap["last"])),
	}
	if windowRaw := usageMap["modelContextWindow"]; windowRaw != nil {
		value := xutil.LookupIntFromAny(windowRaw)
		usage.ModelContextWindow = &value
	}
	return threadID, turnID, usage
}

func extractTokenUsageBreakdown(value map[string]any) agentproto.TokenUsageBreakdown {
	if len(value) == 0 {
		return agentproto.TokenUsageBreakdown{}
	}
	return agentproto.TokenUsageBreakdown{
		InputTokens:           xutil.LookupIntFromAny(value["inputTokens"]),
		CachedInputTokens:     xutil.LookupIntFromAny(value["cachedInputTokens"]),
		OutputTokens:          xutil.LookupIntFromAny(value["outputTokens"]),
		ReasoningOutputTokens: xutil.LookupIntFromAny(value["reasoningOutputTokens"]),
		TotalTokens:           xutil.LookupIntFromAny(value["totalTokens"]),
	}
}
