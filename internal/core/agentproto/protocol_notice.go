package agentproto

import "strings"

type ProtocolNotice struct {
	Method   string `json:"method,omitempty"`
	Kind     string `json:"kind,omitempty"`
	Severity string `json:"severity,omitempty"`
	ThreadID string `json:"threadId,omitempty"`
	TurnID   string `json:"turnId,omitempty"`
	Summary  string `json:"summary,omitempty"`
	Details  string `json:"details,omitempty"`
	Path     string `json:"path,omitempty"`
	Range    string `json:"range,omitempty"`
}

func NormalizeProtocolNotice(notice *ProtocolNotice) *ProtocolNotice {
	if notice == nil {
		return nil
	}
	normalized := *notice
	normalized.Method = strings.TrimSpace(normalized.Method)
	normalized.Kind = strings.TrimSpace(normalized.Kind)
	normalized.Severity = normalizeSeverity(normalized.Severity)
	normalized.ThreadID = strings.TrimSpace(normalized.ThreadID)
	normalized.TurnID = strings.TrimSpace(normalized.TurnID)
	normalized.Summary = strings.TrimSpace(normalized.Summary)
	normalized.Details = strings.TrimSpace(normalized.Details)
	normalized.Path = strings.TrimSpace(normalized.Path)
	normalized.Range = strings.TrimSpace(normalized.Range)
	if normalized.Method == "" || normalized.Summary == "" {
		return nil
	}
	if normalized.Kind == "" {
		normalized.Kind = "notice"
	}
	if normalized.Severity == "" {
		normalized.Severity = ErrorSeverityWarning
	}
	return &normalized
}

func CloneProtocolNotice(notice *ProtocolNotice) *ProtocolNotice {
	normalized := NormalizeProtocolNotice(notice)
	if normalized == nil {
		return nil
	}
	cloned := *normalized
	return &cloned
}
