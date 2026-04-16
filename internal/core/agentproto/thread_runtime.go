package agentproto

import "strings"

type ThreadRuntimeStatusType string

const (
	ThreadRuntimeStatusTypeNotLoaded   ThreadRuntimeStatusType = "notLoaded"
	ThreadRuntimeStatusTypeIdle        ThreadRuntimeStatusType = "idle"
	ThreadRuntimeStatusTypeSystemError ThreadRuntimeStatusType = "systemError"
	ThreadRuntimeStatusTypeActive      ThreadRuntimeStatusType = "active"
)

type ThreadActiveFlag string

const (
	ThreadActiveFlagWaitingOnApproval  ThreadActiveFlag = "waitingOnApproval"
	ThreadActiveFlagWaitingOnUserInput ThreadActiveFlag = "waitingOnUserInput"
)

type ThreadRuntimeStatus struct {
	Type        ThreadRuntimeStatusType `json:"type,omitempty"`
	ActiveFlags []ThreadActiveFlag      `json:"activeFlags,omitempty"`
}

func NormalizeThreadRuntimeStatusType(value string) ThreadRuntimeStatusType {
	switch strings.TrimSpace(value) {
	case string(ThreadRuntimeStatusTypeNotLoaded), "not_loaded":
		return ThreadRuntimeStatusTypeNotLoaded
	case string(ThreadRuntimeStatusTypeIdle):
		return ThreadRuntimeStatusTypeIdle
	case string(ThreadRuntimeStatusTypeSystemError), "system_error":
		return ThreadRuntimeStatusTypeSystemError
	case string(ThreadRuntimeStatusTypeActive), "running":
		return ThreadRuntimeStatusTypeActive
	default:
		return ""
	}
}

func NormalizeThreadActiveFlag(value string) ThreadActiveFlag {
	switch strings.TrimSpace(value) {
	case string(ThreadActiveFlagWaitingOnApproval), "waiting_on_approval":
		return ThreadActiveFlagWaitingOnApproval
	case string(ThreadActiveFlagWaitingOnUserInput), "waiting_on_user_input":
		return ThreadActiveFlagWaitingOnUserInput
	default:
		return ""
	}
}

func CloneThreadRuntimeStatus(status *ThreadRuntimeStatus) *ThreadRuntimeStatus {
	if status == nil {
		return nil
	}
	cloned := &ThreadRuntimeStatus{Type: status.Type}
	if len(status.ActiveFlags) != 0 {
		cloned.ActiveFlags = append([]ThreadActiveFlag(nil), status.ActiveFlags...)
	}
	return cloned
}

func (s *ThreadRuntimeStatus) IsLoaded() bool {
	if s == nil {
		return false
	}
	switch s.Type {
	case ThreadRuntimeStatusTypeIdle, ThreadRuntimeStatusTypeSystemError, ThreadRuntimeStatusTypeActive:
		return true
	default:
		return false
	}
}

func (s *ThreadRuntimeStatus) HasFlag(flag ThreadActiveFlag) bool {
	if s == nil || flag == "" {
		return false
	}
	for _, current := range s.ActiveFlags {
		if current == flag {
			return true
		}
	}
	return false
}

func (s *ThreadRuntimeStatus) LegacyState() string {
	if s == nil {
		return ""
	}
	switch s.Type {
	case ThreadRuntimeStatusTypeActive:
		return "running"
	case ThreadRuntimeStatusTypeIdle:
		return "idle"
	case ThreadRuntimeStatusTypeSystemError:
		return "system_error"
	case ThreadRuntimeStatusTypeNotLoaded:
		return "not_loaded"
	default:
		return ""
	}
}
