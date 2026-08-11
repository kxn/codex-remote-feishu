package state

import (
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"strings"
)

type CodexProfileKind string

const (
	CodexProfileKindNative CodexProfileKind = "native"
	CodexProfileKindOAuth  CodexProfileKind = "oauth"
	CodexProfileKindAPI    CodexProfileKind = "api"

	CodexContextModeDefault   = "codex_default"
	CodexContextModePrice272K = "price_guard_272k"
	CodexContextModeExtended  = "extended_1m"

	ClaudeContextModeDefault  = "claude_default"
	ClaudeContextModeExtended = "extended_1m"

	NativeCodexProfileID = "cp_native"
	OAuthCodexProfileID  = "cp_oauth"

	DefaultOpenCodeProfileID = "op_default"
)

type CodexProfileRef struct {
	ID       string `json:"id"`
	Revision uint64 `json:"revision"`
}

type CodexContextPreferenceRef struct {
	ProfileID string `json:"profileID"`
	Revision  uint64 `json:"revision"`
}

type CodexAdmissionRef struct {
	ProfileRef           CodexProfileRef           `json:"profileRef"`
	ContextPreferenceRef CodexContextPreferenceRef `json:"contextPreferenceRef"`
}

type OpenCodeProfileRef struct {
	ID       string `json:"id"`
	Revision uint64 `json:"revision"`
}

type OpenCodeAdmissionRef struct {
	ProfileRef OpenCodeProfileRef `json:"profileRef"`
}

type OpenCodeProfileSummary struct {
	ID         string `json:"id"`
	Revision   uint64 `json:"revision,omitempty"`
	ETag       string `json:"etag,omitempty"`
	Name       string `json:"name"`
	BaseURL    string `json:"baseURL,omitempty"`
	Model      string `json:"model,omitempty"`
	StatusCode string `json:"statusCode,omitempty"`
	Available  bool   `json:"available"`
	BuiltIn    bool   `json:"builtIn,omitempty"`
	Editable   bool   `json:"editable"`
	Deletable  bool   `json:"deletable"`
	HasAPIKey  bool   `json:"hasAPIKey,omitempty"`
}

func NormalizeCodexAdmissionRef(value *CodexAdmissionRef) *CodexAdmissionRef {
	if value == nil {
		return nil
	}
	profileID := strings.TrimSpace(value.ProfileRef.ID)
	preferenceProfileID := strings.TrimSpace(value.ContextPreferenceRef.ProfileID)
	if profileID == "" || preferenceProfileID != profileID || value.ProfileRef.Revision == 0 || value.ContextPreferenceRef.Revision == 0 {
		return nil
	}
	return &CodexAdmissionRef{
		ProfileRef: CodexProfileRef{
			ID:       profileID,
			Revision: value.ProfileRef.Revision,
		},
		ContextPreferenceRef: CodexContextPreferenceRef{
			ProfileID: preferenceProfileID,
			Revision:  value.ContextPreferenceRef.Revision,
		},
	}
}

func NormalizeOpenCodeAdmissionRef(value *OpenCodeAdmissionRef) *OpenCodeAdmissionRef {
	if value == nil {
		return nil
	}
	profileID := strings.TrimSpace(value.ProfileRef.ID)
	if profileID == "" || value.ProfileRef.Revision == 0 {
		return nil
	}
	return &OpenCodeAdmissionRef{
		ProfileRef: OpenCodeProfileRef{
			ID:       profileID,
			Revision: value.ProfileRef.Revision,
		},
	}
}

func CloneCodexConnectionContract(value *CodexConnectionContract) *CodexConnectionContract {
	if value == nil || strings.TrimSpace(value.ConnectionContractID) == "" {
		return nil
	}
	clone := *value
	clone.ProfileRef.ID = strings.TrimSpace(clone.ProfileRef.ID)
	clone.ConnectionContractID = strings.TrimSpace(clone.ConnectionContractID)
	clone.ModelProviderID = strings.TrimSpace(clone.ModelProviderID)
	clone.ModelEndpointID = strings.TrimSpace(clone.ModelEndpointID)
	clone.ChatGPTEndpointID = strings.TrimSpace(clone.ChatGPTEndpointID)
	clone.CapabilitySet = strings.TrimSpace(clone.CapabilitySet)
	return &clone
}

func CloneCodexThreadPolicy(value *CodexThreadPolicy) *CodexThreadPolicy {
	if value == nil || strings.TrimSpace(value.ThreadPolicyID) == "" {
		return nil
	}
	clone := *value
	clone.ThreadPolicyID = strings.TrimSpace(clone.ThreadPolicyID)
	clone.Model = strings.TrimSpace(clone.Model)
	clone.ReviewModel = strings.TrimSpace(clone.ReviewModel)
	clone.ReasoningEffort = strings.TrimSpace(clone.ReasoningEffort)
	clone.DeveloperInstruction = strings.TrimSpace(clone.DeveloperInstruction)
	return &clone
}

type ProfileContextPreference struct {
	ProfileID string `json:"profileID"`
	Revision  uint64 `json:"revision"`
	ETag      string `json:"etag"`
	Mode      string `json:"mode"`
}

type CodexProfileSummary struct {
	ID                     string                   `json:"id"`
	Revision               uint64                   `json:"revision,omitempty"`
	ETag                   string                   `json:"etag,omitempty"`
	Kind                   CodexProfileKind         `json:"kind"`
	Name                   string                   `json:"name"`
	BaseURL                string                   `json:"baseURL,omitempty"`
	Model                  string                   `json:"model,omitempty"`
	ReviewModel            string                   `json:"reviewModel,omitempty"`
	SubagentModel          string                   `json:"subagentModel,omitempty"`
	Instruction            string                   `json:"instruction,omitempty"`
	ReasoningEffort        string                   `json:"reasoningEffort,omitempty"`
	StatusCode             string                   `json:"statusCode,omitempty"`
	Available              bool                     `json:"available"`
	HasAPIKey              bool                     `json:"hasAPIKey,omitempty"`
	Editable               bool                     `json:"editable"`
	Deletable              bool                     `json:"deletable"`
	ContextEditable        bool                     `json:"contextEditable"`
	ContextPreference      ProfileContextPreference `json:"contextPreference"`
	RequestedContextWindow int64                    `json:"requestedContextWindow,omitempty"`
	EffectiveContextWindow int64                    `json:"effectiveContextWindow,omitempty"`
	ContextStatus          string                   `json:"contextStatus,omitempty"`
}

func CodexProfileDefinitionETag(profileID string, revision uint64) string {
	return profileItemETag("codex-profile-definition", profileID, revision)
}

func CodexContextPreferenceETag(profileID string, revision uint64) string {
	return profileItemETag("codex-context-preference", profileID, revision)
}

func ClaudeContextPreferenceETag(profileID string, revision uint64) string {
	return profileItemETag("claude-context-preference", profileID, revision)
}

func OpenCodeProfileDefinitionETag(profileID string, revision uint64) string {
	return profileItemETag("opencode-profile-definition", profileID, revision)
}

func CodexProfileIDFromLegacyProviderID(providerID string) string {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" || strings.EqualFold(providerID, LegacyDefaultCodexProviderID) {
		return NativeCodexProfileID
	}
	return providerID
}

func profileItemETag(namespace, profileID string, revision uint64) string {
	payload := strings.TrimSpace(namespace) + "\x00" + strings.TrimSpace(profileID) + "\x00" + strconv.FormatUint(revision, 10)
	sum := sha256.Sum256([]byte(payload))
	return `"` + base64.RawURLEncoding.EncodeToString(sum[:18]) + `"`
}
