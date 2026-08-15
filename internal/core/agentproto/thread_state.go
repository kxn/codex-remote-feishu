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
	ThreadID         string `json:"threadId,omitempty"`
	TurnID           string `json:"turnId,omitempty"`
	Objective        string `json:"objective,omitempty"`
	Status           string `json:"status,omitempty"`
	TokenBudget      *int64 `json:"tokenBudget,omitempty"`
	TokensUsed       int64  `json:"tokensUsed,omitempty"`
	TimeUsedSeconds  int64  `json:"timeUsedSeconds,omitempty"`
	CreatedAt        int64  `json:"createdAt,omitempty"`
	UpdatedAt        int64  `json:"updatedAt,omitempty"`
	Cleared          bool   `json:"cleared,omitempty"`
	ExternalMutation bool   `json:"externalMutation,omitempty"`
}

type ThreadSettingsUpdate struct {
	ThreadID        string `json:"threadId,omitempty"`
	ModelProviderID string `json:"modelProviderId,omitempty"`
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
		ThreadID:         strings.TrimSpace(update.ThreadID),
		TurnID:           strings.TrimSpace(update.TurnID),
		Objective:        strings.TrimSpace(update.Objective),
		Status:           strings.TrimSpace(update.Status),
		TokenBudget:      cloneInt64Ptr(update.TokenBudget),
		TokensUsed:       update.TokensUsed,
		TimeUsedSeconds:  update.TimeUsedSeconds,
		CreatedAt:        update.CreatedAt,
		UpdatedAt:        update.UpdatedAt,
		Cleared:          update.Cleared,
		ExternalMutation: update.ExternalMutation,
	}
	if normalized.ThreadID == "" {
		return nil
	}
	if normalized.TurnID == "" && normalized.Objective == "" && normalized.Status == "" &&
		normalized.TokenBudget == nil && normalized.TokensUsed == 0 && normalized.TimeUsedSeconds == 0 &&
		normalized.CreatedAt == 0 && normalized.UpdatedAt == 0 && !normalized.Cleared && !normalized.ExternalMutation {
		return nil
	}
	if normalized.Cleared {
		normalized.Objective = ""
		normalized.Status = ""
		normalized.TokenBudget = nil
		normalized.TokensUsed = 0
		normalized.TimeUsedSeconds = 0
		normalized.CreatedAt = 0
		normalized.UpdatedAt = 0
	}
	return normalized
}

func CloneThreadGoalUpdate(update *ThreadGoalUpdate) *ThreadGoalUpdate {
	if update == nil {
		return nil
	}
	cloned := *update
	cloned.TokenBudget = cloneInt64Ptr(update.TokenBudget)
	return &cloned
}

func cloneInt64Ptr(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func NormalizeThreadSettingsUpdate(update *ThreadSettingsUpdate) *ThreadSettingsUpdate {
	if update == nil {
		return nil
	}
	normalized := &ThreadSettingsUpdate{
		ThreadID:        strings.TrimSpace(update.ThreadID),
		ModelProviderID: strings.TrimSpace(update.ModelProviderID),
		Model:           strings.TrimSpace(update.Model),
		ReasoningEffort: strings.TrimSpace(update.ReasoningEffort),
		ApprovalPolicy:  strings.TrimSpace(update.ApprovalPolicy),
		Sandbox:         strings.TrimSpace(update.Sandbox),
	}
	if normalized.ThreadID == "" {
		return nil
	}
	if normalized.ModelProviderID == "" && normalized.Model == "" && normalized.ReasoningEffort == "" && normalized.ApprovalPolicy == "" && normalized.Sandbox == "" {
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
