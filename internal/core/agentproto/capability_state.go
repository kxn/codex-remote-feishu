package agentproto

import "strings"

type CapabilityStateUpdate struct {
	Method                 string                        `json:"method,omitempty"`
	ThreadID               string                        `json:"threadId,omitempty"`
	SkillsChanged          bool                          `json:"skillsChanged,omitempty"`
	MCPServerStartupStatus *MCPServerStartupStatus       `json:"mcpServerStartupStatus,omitempty"`
	MCPOAuthLoginCompleted *MCPOAuthLoginCompletionState `json:"mcpOAuthLoginCompleted,omitempty"`
	Apps                   []AppStateRecord              `json:"apps,omitempty"`
	Account                *AccountState                 `json:"account,omitempty"`
	RateLimits             map[string]map[string]any     `json:"rateLimits,omitempty"`
	AccountLoginCompleted  *AccountLoginCompletionState  `json:"accountLoginCompleted,omitempty"`
}

type MCPServerStartupStatus struct {
	ThreadID      string `json:"threadId,omitempty"`
	Name          string `json:"name,omitempty"`
	Status        string `json:"status,omitempty"`
	Error         string `json:"error,omitempty"`
	FailureReason string `json:"failureReason,omitempty"`
}

type MCPOAuthLoginCompletionState struct {
	Name     string `json:"name,omitempty"`
	ThreadID string `json:"threadId,omitempty"`
	Success  bool   `json:"success,omitempty"`
	Error    string `json:"error,omitempty"`
}

type AppStateRecord struct {
	ID          string `json:"id,omitempty"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
}

type AccountState struct {
	AuthMode string `json:"authMode,omitempty"`
	PlanType string `json:"planType,omitempty"`
}

type AccountLoginCompletionState struct {
	LoginID string `json:"loginId,omitempty"`
	Success bool   `json:"success,omitempty"`
	Error   string `json:"error,omitempty"`
}

func NormalizeCapabilityStateUpdate(update *CapabilityStateUpdate) *CapabilityStateUpdate {
	if update == nil {
		return nil
	}
	normalized := &CapabilityStateUpdate{
		Method:        strings.TrimSpace(update.Method),
		ThreadID:      strings.TrimSpace(update.ThreadID),
		SkillsChanged: update.SkillsChanged,
		RateLimits:    cloneRateLimits(update.RateLimits),
	}
	if update.MCPServerStartupStatus != nil {
		status := *update.MCPServerStartupStatus
		status.ThreadID = strings.TrimSpace(status.ThreadID)
		status.Name = strings.TrimSpace(status.Name)
		status.Status = strings.TrimSpace(status.Status)
		status.Error = strings.TrimSpace(status.Error)
		status.FailureReason = strings.TrimSpace(status.FailureReason)
		if status.ThreadID == "" {
			status.ThreadID = normalized.ThreadID
		}
		if status.Name != "" || status.Status != "" || status.Error != "" || status.FailureReason != "" {
			normalized.MCPServerStartupStatus = &status
			if normalized.ThreadID == "" {
				normalized.ThreadID = status.ThreadID
			}
		}
	}
	if update.MCPOAuthLoginCompleted != nil {
		completed := *update.MCPOAuthLoginCompleted
		completed.Name = strings.TrimSpace(completed.Name)
		completed.ThreadID = strings.TrimSpace(completed.ThreadID)
		completed.Error = strings.TrimSpace(completed.Error)
		if completed.ThreadID == "" {
			completed.ThreadID = normalized.ThreadID
		}
		if completed.Name != "" || completed.ThreadID != "" || completed.Success || completed.Error != "" {
			normalized.MCPOAuthLoginCompleted = &completed
			if normalized.ThreadID == "" {
				normalized.ThreadID = completed.ThreadID
			}
		}
	}
	for _, app := range update.Apps {
		record := AppStateRecord{
			ID:          strings.TrimSpace(app.ID),
			Name:        strings.TrimSpace(app.Name),
			Description: strings.TrimSpace(app.Description),
		}
		if record.ID == "" && record.Name == "" && record.Description == "" {
			continue
		}
		normalized.Apps = append(normalized.Apps, record)
	}
	if update.Account != nil {
		account := &AccountState{
			AuthMode: strings.TrimSpace(update.Account.AuthMode),
			PlanType: strings.TrimSpace(update.Account.PlanType),
		}
		if account.AuthMode != "" || account.PlanType != "" {
			normalized.Account = account
		}
	}
	if update.AccountLoginCompleted != nil {
		completed := &AccountLoginCompletionState{
			LoginID: strings.TrimSpace(update.AccountLoginCompleted.LoginID),
			Success: update.AccountLoginCompleted.Success,
			Error:   strings.TrimSpace(update.AccountLoginCompleted.Error),
		}
		if completed.LoginID != "" || completed.Success || completed.Error != "" {
			normalized.AccountLoginCompleted = completed
		}
	}
	if normalized.Method == "" {
		return nil
	}
	if !normalized.SkillsChanged && normalized.MCPServerStartupStatus == nil &&
		normalized.MCPOAuthLoginCompleted == nil && len(normalized.Apps) == 0 &&
		normalized.Account == nil && len(normalized.RateLimits) == 0 &&
		normalized.AccountLoginCompleted == nil {
		return nil
	}
	return normalized
}

func CloneCapabilityStateUpdate(update *CapabilityStateUpdate) *CapabilityStateUpdate {
	normalized := NormalizeCapabilityStateUpdate(update)
	if normalized == nil {
		return nil
	}
	cloned := *normalized
	if normalized.MCPServerStartupStatus != nil {
		status := *normalized.MCPServerStartupStatus
		cloned.MCPServerStartupStatus = &status
	}
	if normalized.MCPOAuthLoginCompleted != nil {
		completed := *normalized.MCPOAuthLoginCompleted
		cloned.MCPOAuthLoginCompleted = &completed
	}
	cloned.Apps = append([]AppStateRecord(nil), normalized.Apps...)
	if normalized.Account != nil {
		account := *normalized.Account
		cloned.Account = &account
	}
	cloned.RateLimits = cloneRateLimits(normalized.RateLimits)
	if normalized.AccountLoginCompleted != nil {
		completed := *normalized.AccountLoginCompleted
		cloned.AccountLoginCompleted = &completed
	}
	return &cloned
}

func cloneRateLimits(input map[string]map[string]any) map[string]map[string]any {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]map[string]any, len(input))
	for key, value := range input {
		name := strings.TrimSpace(key)
		if name == "" || len(value) == 0 {
			continue
		}
		record := make(map[string]any, len(value))
		for field, item := range value {
			if strings.TrimSpace(field) != "" {
				record[field] = item
			}
		}
		if len(record) > 0 {
			output[name] = record
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
