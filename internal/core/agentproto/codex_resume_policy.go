package agentproto

import "strings"

type CodexResumeMode string

const (
	CodexResumeApplyTargetProfile     CodexResumeMode = "apply_target_profile"
	CodexResumePreserveThreadSettings CodexResumeMode = "preserve_thread_settings"

	CodexThreadValueExplicit          = "explicit"
	CodexThreadValueDefault           = "codex_default"
	CodexThreadValuePreservedObserved = "preserved_observed"

	CodexReviewModelExplicit   = "explicit"
	CodexReviewModelSameAsMain = "same_as_main"
	CodexReviewModelConfig     = "codex_config"

	CodexContextPreferenceRequested = "context_preference_requested"
	CodexContextPreferenceClamped   = "context_preference_clamped"
)

type CodexResumePolicy struct {
	Mode                 CodexResumeMode `json:"mode,omitempty"`
	ConnectionContractID string          `json:"connectionContractId,omitempty"`
	ThreadPolicyID       string          `json:"threadPolicyId,omitempty"`
	ModelProviderID      string          `json:"modelProviderId,omitempty"`
	ModelMode            string          `json:"modelMode,omitempty"`
	Model                string          `json:"model,omitempty"`
	ReviewModelMode      string          `json:"reviewModelMode,omitempty"`
	ReviewModel          string          `json:"reviewModel,omitempty"`
	ReasoningMode        string          `json:"reasoningMode,omitempty"`
	ReasoningEffort      string          `json:"reasoningEffort,omitempty"`
	ContextMode          string          `json:"contextMode,omitempty"`
	ContextWindow        int64           `json:"contextWindow,omitempty"`
	AutoCompactLimit     int64           `json:"autoCompactLimit,omitempty"`
}

type CodexEffectiveThreadContract struct {
	ResumeMode             CodexResumeMode `json:"resumeMode,omitempty"`
	ConnectionContractID   string          `json:"connectionContractId,omitempty"`
	ThreadPolicyID         string          `json:"threadPolicyId,omitempty"`
	ModelProviderID        string          `json:"modelProviderId,omitempty"`
	ModelMode              string          `json:"modelMode,omitempty"`
	Model                  string          `json:"model,omitempty"`
	ReviewModelMode        string          `json:"reviewModelMode,omitempty"`
	ReviewModel            string          `json:"reviewModel,omitempty"`
	ReasoningMode          string          `json:"reasoningMode,omitempty"`
	ReasoningEffort        string          `json:"reasoningEffort,omitempty"`
	ContextMode            string          `json:"contextMode,omitempty"`
	RequestedContextWindow int64           `json:"requestedContextWindow,omitempty"`
	RequestedAutoCompact   int64           `json:"requestedAutoCompact,omitempty"`
	EffectiveContextWindow int64           `json:"effectiveContextWindow,omitempty"`
	ContextStatus          string          `json:"contextStatus,omitempty"`
}

func NormalizeCodexResumePolicy(policy *CodexResumePolicy) *CodexResumePolicy {
	if policy == nil {
		return nil
	}
	clone := *policy
	clone.ConnectionContractID = strings.TrimSpace(clone.ConnectionContractID)
	clone.ThreadPolicyID = strings.TrimSpace(clone.ThreadPolicyID)
	clone.ModelProviderID = strings.TrimSpace(clone.ModelProviderID)
	clone.ModelMode = strings.TrimSpace(clone.ModelMode)
	clone.Model = strings.TrimSpace(clone.Model)
	clone.ReviewModelMode = strings.TrimSpace(clone.ReviewModelMode)
	clone.ReviewModel = strings.TrimSpace(clone.ReviewModel)
	clone.ReasoningMode = strings.TrimSpace(clone.ReasoningMode)
	clone.ReasoningEffort = strings.TrimSpace(clone.ReasoningEffort)
	clone.ContextMode = strings.TrimSpace(clone.ContextMode)
	switch clone.Mode {
	case CodexResumeApplyTargetProfile, CodexResumePreserveThreadSettings:
	default:
		clone.Mode = CodexResumeApplyTargetProfile
	}
	if clone.ModelProviderID == "" {
		return nil
	}
	return &clone
}

func CloneCodexResumePolicy(policy *CodexResumePolicy) *CodexResumePolicy {
	return NormalizeCodexResumePolicy(policy)
}

func CodexEffectiveThreadContractFromPolicy(policy *CodexResumePolicy) *CodexEffectiveThreadContract {
	policy = NormalizeCodexResumePolicy(policy)
	if policy == nil {
		return nil
	}
	return &CodexEffectiveThreadContract{
		ResumeMode:             policy.Mode,
		ConnectionContractID:   policy.ConnectionContractID,
		ThreadPolicyID:         policy.ThreadPolicyID,
		ModelProviderID:        policy.ModelProviderID,
		ModelMode:              policy.ModelMode,
		Model:                  policy.Model,
		ReviewModelMode:        policy.ReviewModelMode,
		ReviewModel:            policy.ReviewModel,
		ReasoningMode:          policy.ReasoningMode,
		ReasoningEffort:        policy.ReasoningEffort,
		ContextMode:            policy.ContextMode,
		RequestedContextWindow: policy.ContextWindow,
		RequestedAutoCompact:   policy.AutoCompactLimit,
	}
}

func CloneCodexEffectiveThreadContract(contract *CodexEffectiveThreadContract) *CodexEffectiveThreadContract {
	if contract == nil {
		return nil
	}
	clone := *contract
	return &clone
}
