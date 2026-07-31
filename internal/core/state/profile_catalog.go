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

type ProfileContextPreference struct {
	ProfileID string `json:"profileID"`
	Revision  uint64 `json:"revision"`
	ETag      string `json:"etag"`
	Mode      string `json:"mode"`
}

type CodexProfileSummary struct {
	ID                string                   `json:"id"`
	Revision          uint64                   `json:"revision,omitempty"`
	ETag              string                   `json:"etag,omitempty"`
	Kind              CodexProfileKind         `json:"kind"`
	Name              string                   `json:"name"`
	BaseURL           string                   `json:"baseURL,omitempty"`
	Model             string                   `json:"model,omitempty"`
	ReviewModel       string                   `json:"reviewModel,omitempty"`
	ReasoningEffort   string                   `json:"reasoningEffort,omitempty"`
	StatusCode        string                   `json:"statusCode,omitempty"`
	Available         bool                     `json:"available"`
	HasAPIKey         bool                     `json:"hasAPIKey,omitempty"`
	Editable          bool                     `json:"editable"`
	Deletable         bool                     `json:"deletable"`
	ContextEditable   bool                     `json:"contextEditable"`
	ContextPreference ProfileContextPreference `json:"contextPreference"`
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

func CodexProfileIDFromLegacyProviderID(providerID string) string {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" || strings.EqualFold(providerID, DefaultCodexProviderID) {
		return NativeCodexProfileID
	}
	return providerID
}

func LegacyCodexProviderIDFromProfileID(profileID string) string {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" || profileID == NativeCodexProfileID {
		return DefaultCodexProviderID
	}
	return profileID
}

func profileItemETag(namespace, profileID string, revision uint64) string {
	payload := strings.TrimSpace(namespace) + "\x00" + strings.TrimSpace(profileID) + "\x00" + strconv.FormatUint(revision, 10)
	sum := sha256.Sum256([]byte(payload))
	return `"` + base64.RawURLEncoding.EncodeToString(sum[:18]) + `"`
}
