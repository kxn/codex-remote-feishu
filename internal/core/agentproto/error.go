package agentproto

import (
	"errors"
	"strings"
)

const (
	ErrorSeverityWarning = "warning"
	ErrorSeverityError   = "error"
)

type ErrorInfo struct {
	Severity         string `json:"severity,omitempty"`
	Code             string `json:"code,omitempty"`
	Layer            string `json:"layer,omitempty"`
	Stage            string `json:"stage,omitempty"`
	Operation        string `json:"operation,omitempty"`
	Message          string `json:"message,omitempty"`
	Details          string `json:"details,omitempty"`
	SurfaceSessionID string `json:"surfaceSessionId,omitempty"`
	CommandID        string `json:"commandId,omitempty"`
	ThreadID         string `json:"threadId,omitempty"`
	TurnID           string `json:"turnId,omitempty"`
	ItemID           string `json:"itemId,omitempty"`
	RequestID        string `json:"requestId,omitempty"`
	Retryable        bool   `json:"retryable,omitempty"`
}

func (e ErrorInfo) Error() string {
	return firstNonEmptyErrorString(e.Message, e.Details, e.Code, "relay error")
}

func (e ErrorInfo) WithDefaults(defaults ErrorInfo) ErrorInfo {
	if strings.TrimSpace(e.Severity) == "" {
		e.Severity = defaults.Severity
	}
	if strings.TrimSpace(e.Code) == "" {
		e.Code = defaults.Code
	}
	if strings.TrimSpace(e.Layer) == "" {
		e.Layer = defaults.Layer
	}
	if strings.TrimSpace(e.Stage) == "" {
		e.Stage = defaults.Stage
	}
	if strings.TrimSpace(e.Operation) == "" {
		e.Operation = defaults.Operation
	}
	if strings.TrimSpace(e.Message) == "" {
		e.Message = defaults.Message
	}
	if strings.TrimSpace(e.Details) == "" {
		e.Details = defaults.Details
	}
	if strings.TrimSpace(e.SurfaceSessionID) == "" {
		e.SurfaceSessionID = defaults.SurfaceSessionID
	}
	if strings.TrimSpace(e.CommandID) == "" {
		e.CommandID = defaults.CommandID
	}
	if strings.TrimSpace(e.ThreadID) == "" {
		e.ThreadID = defaults.ThreadID
	}
	if strings.TrimSpace(e.TurnID) == "" {
		e.TurnID = defaults.TurnID
	}
	if strings.TrimSpace(e.ItemID) == "" {
		e.ItemID = defaults.ItemID
	}
	if strings.TrimSpace(e.RequestID) == "" {
		e.RequestID = defaults.RequestID
	}
	if !e.Retryable && defaults.Retryable {
		e.Retryable = true
	}
	return e.Normalize()
}

func (e ErrorInfo) Normalize() ErrorInfo {
	e.Severity = normalizeSeverity(e.Severity)
	e.Code = strings.TrimSpace(e.Code)
	e.Layer = strings.TrimSpace(e.Layer)
	e.Stage = strings.TrimSpace(e.Stage)
	e.Operation = strings.TrimSpace(e.Operation)
	e.Message = strings.TrimSpace(e.Message)
	e.Details = strings.TrimSpace(e.Details)
	e.SurfaceSessionID = strings.TrimSpace(e.SurfaceSessionID)
	e.CommandID = strings.TrimSpace(e.CommandID)
	e.ThreadID = strings.TrimSpace(e.ThreadID)
	e.TurnID = strings.TrimSpace(e.TurnID)
	e.ItemID = strings.TrimSpace(e.ItemID)
	e.RequestID = strings.TrimSpace(e.RequestID)
	if e.Message == "" {
		e.Message = firstNonEmptyErrorString(e.Details, e.Code, "relay error")
	}
	return e
}

func ErrorInfoFromError(err error, defaults ErrorInfo) ErrorInfo {
	if err == nil {
		return defaults.Normalize()
	}

	var info ErrorInfo
	if errors.As(err, &info) {
		return info.WithDefaults(defaults)
	}

	var infoPtr *ErrorInfo
	if errors.As(err, &infoPtr) && infoPtr != nil {
		return infoPtr.WithDefaults(defaults)
	}

	info = defaults
	if strings.TrimSpace(info.Message) == "" {
		info.Message = err.Error()
	}
	if strings.TrimSpace(info.Details) == "" {
		info.Details = err.Error()
	}
	return info.Normalize()
}

func NewSystemErrorEvent(problem ErrorInfo) Event {
	problem = problem.Normalize()
	return Event{
		Kind:         EventSystemError,
		ThreadID:     problem.ThreadID,
		TurnID:       problem.TurnID,
		ItemID:       problem.ItemID,
		RequestID:    problem.RequestID,
		Status:       problem.Severity,
		ErrorMessage: problem.Message,
		Problem:      &problem,
	}
}

func normalizeSeverity(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", ErrorSeverityError:
		return ErrorSeverityError
	case ErrorSeverityWarning:
		return ErrorSeverityWarning
	default:
		return ErrorSeverityError
	}
}

func firstNonEmptyErrorString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
