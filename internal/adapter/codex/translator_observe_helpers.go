package codex

import (
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
	"strings"
)

func threadRefreshNeedsRead(record agentproto.ThreadSnapshotRecord) bool {
	if strings.TrimSpace(record.CWD) == "" {
		return true
	}
	if strings.TrimSpace(record.Name) == "" && strings.TrimSpace(record.Preview) == "" {
		return true
	}
	if record.RuntimeStatus == nil && strings.TrimSpace(record.State) == "" {
		return true
	}
	return false
}

func parseCodexProblemEvent(message map[string]any) *agentproto.ErrorInfo {
	errPayload := lookupMap(message, "params", "error")
	if len(errPayload) == 0 {
		return nil
	}
	messageText := strings.TrimSpace(xutil.LookupStringFromAny(errPayload["message"]))
	detailsText := strings.TrimSpace(xutil.LookupStringFromAny(errPayload["additionalDetails"]))
	retryable := lookupBool(message, "params", "willRetry")
	if retryable && detailsText != "" && strings.HasPrefix(strings.ToLower(messageText), "reconnecting") {
		messageText = detailsText
	}
	problem := agentproto.ErrorInfo{
		Code:      xutil.FirstNonEmpty(codexErrorCode(errPayload["codexErrorInfo"]), "codex_runtime_error"),
		Layer:     "codex",
		Stage:     "runtime_error",
		Message:   xutil.FirstNonEmpty(messageText, detailsText),
		Details:   xutil.FirstNonEmpty(detailsText, messageText),
		ThreadID:  lookupString(message, "params", "threadId"),
		TurnID:    lookupString(message, "params", "turnId"),
		Retryable: retryable,
	}
	normalized := problem.Normalize()
	return &normalized
}

func codexErrorCode(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case map[string]any:
		if len(typed) != 1 {
			return ""
		}
		for key := range typed {
			return strings.TrimSpace(key)
		}
	}
	return ""
}

func extractTurnPlanSnapshot(message map[string]any) *agentproto.TurnPlanSnapshot {
	params := lookupMap(message, "params")
	if len(params) == 0 {
		return nil
	}
	snapshot := &agentproto.TurnPlanSnapshot{
		Explanation: strings.TrimSpace(xutil.LookupStringFromAny(params["explanation"])),
	}
	rawPlan, _ := params["plan"].([]any)
	for _, raw := range rawPlan {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		step := strings.TrimSpace(xutil.LookupStringFromAny(entry["step"]))
		status := agentproto.NormalizeTurnPlanStepStatus(xutil.LookupStringFromAny(entry["status"]))
		if step == "" && status == "" {
			continue
		}
		snapshot.Steps = append(snapshot.Steps, agentproto.TurnPlanStep{
			Step:   step,
			Status: status,
		})
	}
	if snapshot.Explanation == "" && len(snapshot.Steps) == 0 {
		return nil
	}
	return snapshot
}

func lookupBool(message map[string]any, path ...string) bool {
	current := any(message)
	for _, segment := range path {
		m, ok := current.(map[string]any)
		if !ok {
			return false
		}
		next, exists := m[segment]
		if !exists {
			return false
		}
		current = next
	}
	value, ok := current.(bool)
	return ok && value
}
