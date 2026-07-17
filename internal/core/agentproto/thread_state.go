package agentproto

import "strings"

type ThreadLifecycleAction string

const (
	ThreadLifecycleArchived   ThreadLifecycleAction = "archived"
	ThreadLifecycleUnarchived ThreadLifecycleAction = "unarchived"
	ThreadLifecycleDeleted    ThreadLifecycleAction = "deleted"
	ThreadLifecycleClosed     ThreadLifecycleAction = "closed"
)

type ThreadLifecycleUpdate struct {
	ThreadID string                `json:"threadId,omitempty"`
	Action   ThreadLifecycleAction `json:"action,omitempty"`
}

type ThreadGoalUpdate struct {
	ThreadID        string `json:"threadId,omitempty"`
	TurnID          string `json:"turnId,omitempty"`
	Objective       string `json:"objective,omitempty"`
	Status          string `json:"status,omitempty"`
	TokenBudget     int    `json:"tokenBudget,omitempty"`
	TokensUsed      int    `json:"tokensUsed,omitempty"`
	TimeUsedSeconds int    `json:"timeUsedSeconds,omitempty"`
	Cleared         bool   `json:"cleared,omitempty"`
}

type ThreadSettingsUpdate struct {
	ThreadID        string `json:"threadId,omitempty"`
	Model           string `json:"model,omitempty"`
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
	ApprovalPolicy  string `json:"approvalPolicy,omitempty"`
	Sandbox         string `json:"sandbox,omitempty"`
}

func NormalizeThreadLifecycleAction(value string) ThreadLifecycleAction {
	switch strings.TrimSpace(value) {
	case string(ThreadLifecycleArchived):
		return ThreadLifecycleArchived
	case string(ThreadLifecycleUnarchived):
		return ThreadLifecycleUnarchived
	case string(ThreadLifecycleDeleted):
		return ThreadLifecycleDeleted
	case string(ThreadLifecycleClosed):
		return ThreadLifecycleClosed
	default:
		return ""
	}
}

func NormalizeThreadLifecycleUpdate(update *ThreadLifecycleUpdate) *ThreadLifecycleUpdate {
	if update == nil {
		return nil
	}
	normalized := &ThreadLifecycleUpdate{
		ThreadID: strings.TrimSpace(update.ThreadID),
		Action:   NormalizeThreadLifecycleAction(string(update.Action)),
	}
	if normalized.ThreadID == "" || normalized.Action == "" {
		return nil
	}
	return normalized
}

func CloneThreadLifecycleUpdate(update *ThreadLifecycleUpdate) *ThreadLifecycleUpdate {
	if update == nil {
		return nil
	}
	cloned := *update
	return &cloned
}

func NormalizeThreadGoalUpdate(update *ThreadGoalUpdate) *ThreadGoalUpdate {
	if update == nil {
		return nil
	}
	normalized := &ThreadGoalUpdate{
		ThreadID:        strings.TrimSpace(update.ThreadID),
		TurnID:          strings.TrimSpace(update.TurnID),
		Objective:       strings.TrimSpace(update.Objective),
		Status:          strings.TrimSpace(update.Status),
		TokenBudget:     update.TokenBudget,
		TokensUsed:      update.TokensUsed,
		TimeUsedSeconds: update.TimeUsedSeconds,
		Cleared:         update.Cleared,
	}
	if normalized.ThreadID == "" {
		return nil
	}
	if normalized.TurnID == "" && normalized.Objective == "" && normalized.Status == "" &&
		normalized.TokenBudget == 0 && normalized.TokensUsed == 0 && normalized.TimeUsedSeconds == 0 && !normalized.Cleared {
		return nil
	}
	if normalized.Cleared {
		normalized.Objective = ""
		normalized.Status = ""
		normalized.TokenBudget = 0
		normalized.TokensUsed = 0
		normalized.TimeUsedSeconds = 0
	}
	return normalized
}

func CloneThreadGoalUpdate(update *ThreadGoalUpdate) *ThreadGoalUpdate {
	if update == nil {
		return nil
	}
	cloned := *update
	return &cloned
}

func NormalizeThreadSettingsUpdate(update *ThreadSettingsUpdate) *ThreadSettingsUpdate {
	if update == nil {
		return nil
	}
	normalized := &ThreadSettingsUpdate{
		ThreadID:        strings.TrimSpace(update.ThreadID),
		Model:           strings.TrimSpace(update.Model),
		ReasoningEffort: strings.TrimSpace(update.ReasoningEffort),
		ApprovalPolicy:  strings.TrimSpace(update.ApprovalPolicy),
		Sandbox:         strings.TrimSpace(update.Sandbox),
	}
	if normalized.ThreadID == "" {
		return nil
	}
	if normalized.Model == "" && normalized.ReasoningEffort == "" && normalized.ApprovalPolicy == "" && normalized.Sandbox == "" {
		return nil
	}
	return normalized
}

func CloneThreadSettingsUpdate(update *ThreadSettingsUpdate) *ThreadSettingsUpdate {
	if update == nil {
		return nil
	}
	cloned := *update
	return &cloned
}
