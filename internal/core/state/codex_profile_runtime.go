package state

import "time"

const (
	CodexThreadValueExplicit = "explicit"
	CodexThreadValueDefault  = "codex_default"

	CodexReviewModelExplicit   = "explicit"
	CodexReviewModelSameAsMain = "same_as_main"
	CodexReviewModelConfig     = "codex_config"
)

type CodexOAuthProfileState struct {
	ProfileID          string    `json:"profileID"`
	Revision           uint64    `json:"revision"`
	AuthGeneration     uint64    `json:"authGeneration"`
	Status             string    `json:"status"`
	AccountHint        string    `json:"accountHint,omitempty"`
	PlanType           string    `json:"planType,omitempty"`
	LastCheckedAt      time.Time `json:"lastCheckedAt,omitempty"`
	LastProbeErrorCode string    `json:"lastProbeErrorCode,omitempty"`
	AvailabilityCode   string    `json:"availabilityCode,omitempty"`
	CapabilitySet      string    `json:"capabilitySet,omitempty"`
}

type CodexNativeConnectionEvidence struct {
	Revision             uint64 `json:"revision"`
	ConnectionGeneration uint64 `json:"connectionGeneration"`
	ModelProviderID      string `json:"modelProviderID,omitempty"`
	ModelEndpointID      string `json:"modelEndpointID,omitempty"`
	ChatGPTEndpointID    string `json:"chatgptEndpointID,omitempty"`
}

type CodexConnectionContract struct {
	ProfileRef           CodexProfileRef  `json:"profileRef"`
	ConnectionGeneration uint64           `json:"connectionGeneration"`
	ConnectionContractID string           `json:"connectionContractID"`
	Kind                 CodexProfileKind `json:"kind"`
	ModelProviderID      string           `json:"modelProviderID,omitempty"`
	ModelEndpointID      string           `json:"modelEndpointID,omitempty"`
	ChatGPTEndpointID    string           `json:"chatgptEndpointID,omitempty"`
	CapabilitySet        string           `json:"capabilitySet"`
}

type CodexThreadPolicy struct {
	ThreadPolicyID     string `json:"threadPolicyID"`
	ModelMode          string `json:"modelMode"`
	Model              string `json:"model,omitempty"`
	ReviewModelMode    string `json:"reviewModelMode"`
	ReviewModel        string `json:"reviewModel,omitempty"`
	ReasoningMode      string `json:"reasoningMode"`
	ReasoningEffort    string `json:"reasoningEffort,omitempty"`
	ContextMode        string `json:"contextMode"`
	ContextWindow      int64  `json:"contextWindow,omitempty"`
	AutoCompactLimit   int64  `json:"autoCompactLimit,omitempty"`
	PreferenceRevision uint64 `json:"preferenceRevision"`
}
