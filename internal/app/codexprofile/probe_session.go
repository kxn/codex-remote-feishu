package codexprofile

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	OAuthProbeCapabilitySetV1       = "codex-app-server-auth-v1"
	ErrorCodexCapabilityUnsupported = "codex_capability_unsupported"
	ErrorCodexBinaryUnavailable     = "codex_binary_unavailable"
	ErrorCodexProbeTimeout          = "codex_probe_timeout"
	ErrorCodexProbeUnavailable      = "codex_probe_unavailable"
	ErrorCodexProbeContractMismatch = "codex_probe_contract_mismatch"
	CodexProfileAPIKeyEnv           = "CODEX_REMOTE_CODEX_PROFILE_API_KEY"
	CodexRuntimeResolvedEnv         = "CODEX_REMOTE_CODEX_PROFILE_RUNTIME_RESOLVED"
	legacyCodexProviderAPIKeyEnv    = "CODEX_REMOTE_CODEX_PROVIDER_API_KEY"
)

var conflictingCodexAuthEnvKeys = []string{
	"OPENAI_API_KEY",
	"CODEX_API_KEY",
	"CODEX_ACCESS_TOKEN",
	"OPENAI_ORGANIZATION",
	"OPENAI_PROJECT",
	"CODEX_REFRESH_TOKEN_URL_OVERRIDE",
	"CODEX_REVOKE_TOKEN_URL_OVERRIDE",
	"CODEX_APP_SERVER_LOGIN_CLIENT_ID",
	legacyCodexProviderAPIKeyEnv,
	CodexProfileAPIKeyEnv,
}

type OAuthProbeObservation struct {
	Result        OAuthProbeResult `json:"result"`
	UserAgent     string           `json:"userAgent,omitempty"`
	CapabilitySet string           `json:"capabilitySet,omitempty"`
}

type ProbeLaunchMaterial struct {
	Args []string
	Env  []string
}

type OAuthProbeError struct {
	Code  string
	Stage string
}

func (e *OAuthProbeError) Error() string {
	if e == nil {
		return ""
	}
	if strings.TrimSpace(e.Stage) == "" {
		return e.Code
	}
	return e.Code + ": " + e.Stage
}

func OAuthProbeErrorCode(err error) string {
	if probeErr, ok := err.(*OAuthProbeError); ok {
		return probeErr.Code
	}
	var wrapped *OAuthProbeError
	if errors.As(err, &wrapped) {
		return wrapped.Code
	}
	return ""
}

func OAuthProbeErrorStage(err error) string {
	var probeErr *OAuthProbeError
	if errors.As(err, &probeErr) {
		return strings.TrimSpace(probeErr.Stage)
	}
	return ""
}

func ConflictingCodexAuthEnvKeys() []string {
	return append([]string{}, conflictingCodexAuthEnvKeys...)
}

func OAuthProbeLaunchMaterial(baseEnv []string) ProbeLaunchMaterial {
	return ProbeLaunchMaterial{
		Args: []string{
			"app-server",
			"-c", `model_provider="openai"`,
			"-c", `openai_base_url=""`,
		},
		Env: removeEnvKeys(baseEnv, conflictingCodexAuthEnvKeys),
	}
}

func RunOAuthProbeSession(ctx context.Context, reader io.Reader, writer io.Writer, version string) (OAuthProbeObservation, error) {
	frames := OAuthProbeFrames(version)
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 1024*1024)

	initialize, err := exchangeOAuthProbeFrame(ctx, scanner, writer, frames[0], "initialize")
	if err != nil {
		return OAuthProbeObservation{}, err
	}
	if err := writeOAuthProbeFrame(writer, frames[1]); err != nil {
		return OAuthProbeObservation{}, &OAuthProbeError{Code: ErrorOAuthProbeUnknown, Stage: "initialized_write"}
	}
	configResponse, err := exchangeOAuthProbeFrame(ctx, scanner, writer, frames[2], "config_read")
	if err != nil {
		return OAuthProbeObservation{}, err
	}
	accountResponse, err := exchangeOAuthProbeFrame(ctx, scanner, writer, frames[3], "account_read")
	if err != nil {
		return OAuthProbeObservation{}, err
	}
	authResponse, err := exchangeOAuthProbeFrame(ctx, scanner, writer, frames[4], "auth_status")
	if err != nil {
		return OAuthProbeObservation{}, err
	}

	evidence, err := decodeOAuthProbeEvidence(configResponse, accountResponse, authResponse)
	if err != nil {
		return OAuthProbeObservation{}, err
	}
	return OAuthProbeObservation{
		Result:        ClassifyOAuthProbe(evidence),
		UserAgent:     resultString(initialize, "userAgent"),
		CapabilitySet: OAuthProbeCapabilitySetV1,
	}, nil
}

func exchangeOAuthProbeFrame(ctx context.Context, scanner *bufio.Scanner, writer io.Writer, frame OAuthProbeFrame, stage string) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, &OAuthProbeError{Code: ErrorOAuthProbeUnknown, Stage: stage + "_canceled"}
	}
	if err := writeOAuthProbeFrame(writer, frame); err != nil {
		return nil, &OAuthProbeError{Code: ErrorOAuthProbeUnknown, Stage: stage + "_write"}
	}
	for scanner.Scan() {
		var response map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &response); err != nil {
			continue
		}
		if fmt.Sprint(response["id"]) != frame.ID {
			continue
		}
		if rpcError, ok := response["error"].(map[string]any); ok {
			code := ErrorOAuthProbeUnknown
			if jsonRPCErrorCode(rpcError) == -32601 {
				code = ErrorCodexCapabilityUnsupported
			}
			return nil, &OAuthProbeError{Code: code, Stage: stage}
		}
		result, ok := response["result"].(map[string]any)
		if !ok {
			return nil, &OAuthProbeError{Code: ErrorOAuthProbeUnknown, Stage: stage + "_invalid_response"}
		}
		return result, nil
	}
	return nil, &OAuthProbeError{Code: ErrorOAuthProbeUnknown, Stage: stage + "_closed"}
}

func jsonRPCErrorCode(value map[string]any) int64 {
	switch code := value["code"].(type) {
	case float64:
		return int64(code)
	case json.Number:
		parsed, _ := code.Int64()
		return parsed
	default:
		return 0
	}
}

func writeOAuthProbeFrame(writer io.Writer, frame OAuthProbeFrame) error {
	raw, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	_, err = writer.Write(append(raw, '\n'))
	return err
}

func decodeOAuthProbeEvidence(configResponse, accountResponse, authResponse map[string]any) (OAuthProbeEvidence, error) {
	evidence := OAuthProbeEvidence{AuthMethod: resultString(authResponse, "authMethod")}
	if configValue, ok := configResponse["config"].(map[string]any); ok {
		evidence.ChatGPTBaseURL = resultString(configValue, "chatgpt_base_url")
	}
	if account, ok := accountResponse["account"].(map[string]any); ok {
		evidence.AccountType = resultString(account, "type")
		evidence.Email = resultString(account, "email")
		evidence.PlanType = resultString(account, "planType")
	}
	if evidence.AccountType == "chatgpt" && evidence.AuthMethod == "" {
		return evidence, &OAuthProbeError{Code: ErrorOAuthProbeUnknown, Stage: "auth_evidence_incomplete"}
	}
	return evidence, nil
}

func resultString(value map[string]any, key string) string {
	result, _ := value[key].(string)
	return strings.TrimSpace(result)
}

func removeEnvKeys(env []string, keys []string) []string {
	out := make([]string, 0, len(env))
	for _, entry := range env {
		key, _, ok := strings.Cut(entry, "=")
		if !ok || !containsFold(keys, key) {
			out = append(out, entry)
		}
	}
	return out
}

func containsFold(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(value, target) {
			return true
		}
	}
	return false
}
