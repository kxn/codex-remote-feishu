package codex

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func (t *Translator) observeProtocolNotice(method string, message map[string]any) Result {
	notice := extractProtocolNotice(method, message)
	if notice == nil {
		return Result{}
	}
	return Result{Events: []agentproto.Event{{
		Kind:           agentproto.EventProtocolNotice,
		ThreadID:       notice.ThreadID,
		TurnID:         notice.TurnID,
		Status:         notice.Severity,
		ErrorMessage:   notice.Summary,
		TrafficClass:   t.trafficClassForTurn(notice.ThreadID, notice.TurnID),
		Initiator:      t.initiatorForTurn(notice.ThreadID, notice.TurnID),
		ProtocolNotice: notice,
		Metadata: map[string]any{
			"method":   notice.Method,
			"kind":     notice.Kind,
			"severity": notice.Severity,
		},
	}}}
}

func extractProtocolNotice(method string, message map[string]any) *agentproto.ProtocolNotice {
	params := lookupMap(message, "params")
	notice := &agentproto.ProtocolNotice{
		Method:   strings.TrimSpace(method),
		Kind:     protocolNoticeKind(method),
		Severity: protocolNoticeSeverity(method),
		ThreadID: firstNonEmptyString(
			lookupString(message, "params", "threadId"),
			lookupString(message, "params", "thread", "id"),
		),
		TurnID: firstNonEmptyString(
			lookupString(message, "params", "turnId"),
			lookupString(message, "params", "turn", "id"),
		),
		Summary: protocolNoticeSummary(method, params),
		Details: lookupStringFromAny(params["details"]),
		Path: firstNonEmptyString(
			lookupStringFromAny(params["path"]),
			lookupString(message, "params", "location", "path"),
		),
		Range: protocolNoticeRange(params),
	}
	return agentproto.NormalizeProtocolNotice(notice)
}

func protocolNoticeKind(method string) string {
	switch strings.TrimSpace(method) {
	case "guardianWarning":
		return "guardian"
	case "deprecationNotice":
		return "deprecation"
	case "configWarning":
		return "config"
	default:
		return "warning"
	}
}

func protocolNoticeSeverity(method string) string {
	switch strings.TrimSpace(method) {
	case "guardianWarning", "configWarning":
		return agentproto.ErrorSeverityWarning
	default:
		return agentproto.ErrorSeverityWarning
	}
}

func protocolNoticeSummary(method string, params map[string]any) string {
	switch strings.TrimSpace(method) {
	case "warning", "guardianWarning":
		return firstNonEmptyString(
			lookupStringFromAny(params["message"]),
			lookupStringFromAny(params["summary"]),
		)
	default:
		return firstNonEmptyString(
			lookupStringFromAny(params["summary"]),
			lookupStringFromAny(params["message"]),
		)
	}
}

func protocolNoticeRange(params map[string]any) string {
	if value := strings.TrimSpace(lookupStringFromAny(params["range"])); value != "" {
		return value
	}
	rangeMap := lookupMap(params, "range")
	if len(rangeMap) == 0 {
		rangeMap = lookupMap(params, "location", "range")
	}
	if len(rangeMap) == 0 {
		return ""
	}
	startLine := lookupIntFromAny(firstNonNil(rangeMap["startLine"], rangeMap["start_line"]))
	startColumn := lookupIntFromAny(firstNonNil(rangeMap["startColumn"], rangeMap["start_column"]))
	endLine := lookupIntFromAny(firstNonNil(rangeMap["endLine"], rangeMap["end_line"]))
	endColumn := lookupIntFromAny(firstNonNil(rangeMap["endColumn"], rangeMap["end_column"]))
	if startLine == 0 && startColumn == 0 && endLine == 0 && endColumn == 0 {
		return ""
	}
	if endLine == 0 && endColumn == 0 {
		return fmt.Sprintf("%d:%d", startLine, startColumn)
	}
	return fmt.Sprintf("%d:%d-%d:%d", startLine, startColumn, endLine, endColumn)
}
