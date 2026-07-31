package codexprofile

import (
	"encoding/json"
	"net/url"
	"strings"
)

const (
	OfficialChatGPTBaseURL = "https://chatgpt.com/backend-api/"

	ErrorOAuthProbeUnknown          = "oauth_probe_unknown"
	ErrorOAuthDeploymentUnsupported = "oauth_deployment_unsupported"
)

type OAuthProbeStatus string

const (
	OAuthProbeStatusDetected OAuthProbeStatus = "detected"
	OAuthProbeStatusMissing  OAuthProbeStatus = "missing"
	OAuthProbeStatusUnknown  OAuthProbeStatus = "unknown"
)

type OAuthProbeFrame struct {
	ID     string          `json:"id,omitempty"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type OAuthProbeEvidence struct {
	AccountType    string
	AuthMethod     string
	Email          string
	PlanType       string
	ChatGPTBaseURL string
}

type OAuthProbeResult struct {
	Status             OAuthProbeStatus `json:"status"`
	AccountHint        string           `json:"accountHint,omitempty"`
	PlanType           string           `json:"planType,omitempty"`
	LastProbeErrorCode string           `json:"lastProbeErrorCode,omitempty"`
	AvailabilityCode   string           `json:"availabilityCode,omitempty"`
}

func OAuthProbeFrames(version string) []OAuthProbeFrame {
	return []OAuthProbeFrame{
		newOAuthProbeRequest("codex-remote-oauth-initialize", "initialize", map[string]any{
			"clientInfo": map[string]any{
				"name":    "Codex Remote OAuth Probe",
				"title":   "Codex Remote OAuth Probe",
				"version": strings.TrimSpace(version),
			},
			"capabilities": map[string]any{"experimentalApi": true},
		}),
		newOAuthProbeNotification("initialized", map[string]any{}),
		newOAuthProbeRequest("codex-remote-oauth-config", "config/read", map[string]any{
			"includeLayers": false,
		}),
		newOAuthProbeRequest("codex-remote-oauth-account", "account/read", map[string]any{
			"refreshToken": false,
		}),
		newOAuthProbeRequest("codex-remote-oauth-auth-status", "getAuthStatus", map[string]any{
			"includeToken": false,
			"refreshToken": false,
		}),
	}
}

func ClassifyOAuthProbe(evidence OAuthProbeEvidence) OAuthProbeResult {
	accountType := strings.TrimSpace(evidence.AccountType)
	authMethod := strings.TrimSpace(evidence.AuthMethod)
	result := OAuthProbeResult{}

	switch {
	case accountType == "" && authMethod == "":
		result.Status = OAuthProbeStatusMissing
		return result
	case accountType == "chatgpt" && authMethod == "chatgpt":
		result.Status = OAuthProbeStatusDetected
		result.AccountHint = redactEmail(evidence.Email)
		result.PlanType = strings.TrimSpace(evidence.PlanType)
		if !isOfficialChatGPTBaseURL(evidence.ChatGPTBaseURL) {
			result.AvailabilityCode = ErrorOAuthDeploymentUnsupported
		}
		return result
	case authMethod != "" && authMethod != "chatgpt":
		result.Status = OAuthProbeStatusMissing
		return result
	default:
		result.Status = OAuthProbeStatusUnknown
		result.LastProbeErrorCode = ErrorOAuthProbeUnknown
		return result
	}
}

func newOAuthProbeRequest(id, method string, params any) OAuthProbeFrame {
	return OAuthProbeFrame{
		ID:     id,
		Method: method,
		Params: mustMarshalProbeParams(params),
	}
}

func newOAuthProbeNotification(method string, params any) OAuthProbeFrame {
	return OAuthProbeFrame{Method: method, Params: mustMarshalProbeParams(params)}
}

func mustMarshalProbeParams(params any) json.RawMessage {
	raw, err := json.Marshal(params)
	if err != nil {
		panic(err)
	}
	return raw
}

func isOfficialChatGPTBaseURL(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		value = OfficialChatGPTBaseURL
	}
	parsed, err := url.Parse(value)
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") || !strings.EqualFold(parsed.Host, "chatgpt.com") {
		return false
	}
	return strings.TrimRight(parsed.EscapedPath(), "/") == "/backend-api" && parsed.RawQuery == "" && parsed.Fragment == "" && parsed.User == nil
}

func redactEmail(value string) string {
	value = strings.TrimSpace(value)
	local, domain, ok := strings.Cut(value, "@")
	if !ok || local == "" || domain == "" {
		return ""
	}
	return string([]rune(local)[0]) + "***@" + domain
}
