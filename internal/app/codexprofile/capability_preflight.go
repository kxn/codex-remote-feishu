package codexprofile

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/execlaunch"
)

const (
	CodexProfileCapabilitySetV1 = "codex-profile-runtime-v1"

	capabilityProbeProviderID  = "codex_remote_capability_probe"
	capabilityProbeModel       = "codex-remote-capability-model"
	capabilityProbeReviewModel = "codex-remote-capability-review-model"
	capabilityProbeReasoning   = "high"
	capabilityProbeAPIKey      = "codex-remote-capability-probe"
)

type CapabilityPreflightOptions struct {
	BinaryPath string
	Env        []string
	Version    string
}

type CapabilityPreflightObservation struct {
	CapabilitySet string `json:"capabilitySet"`
	UserAgent     string `json:"userAgent,omitempty"`
}

func CapabilityPreflightLaunchMaterial(baseEnv []string, codexHome string) ProbeLaunchMaterial {
	prefix := "model_providers." + capabilityProbeProviderID
	env := removeEnvKeys(baseEnv, ConflictingCodexAuthEnvKeys())
	env = upsertRuntimeEnv(env, "CODEX_HOME", strings.TrimSpace(codexHome))
	env = upsertRuntimeEnv(env, CodexProfileAPIKeyEnv, capabilityProbeAPIKey)
	return ProbeLaunchMaterial{
		Args: []string{
			"app-server",
			"-c", codexOverride("model_provider", capabilityProbeProviderID),
			"-c", codexOverride("model", capabilityProbeModel),
			"-c", codexOverride("review_model", capabilityProbeReviewModel),
			"-c", codexOverride("model_reasoning_effort", capabilityProbeReasoning),
			"-c", "model_context_window=272000",
			"-c", "model_auto_compact_token_limit=244800",
			"-c", codexOverride(prefix+".name", "Codex Remote Capability Probe"),
			"-c", codexOverride(prefix+".base_url", "http://127.0.0.1:9/v1"),
			"-c", codexOverride(prefix+".wire_api", "responses"),
			"-c", codexOverride(prefix+".env_key", CodexProfileAPIKeyEnv),
			"-c", prefix + ".requires_openai_auth=false",
			"-c", prefix + ".supports_websockets=false",
			"-c", codexOverride("cli_auth_credentials_store", "ephemeral"),
		},
		Env: env,
	}
}

func RunCapabilityPreflightSession(ctx context.Context, reader io.Reader, writer io.Writer, version, cwd string) (CapabilityPreflightObservation, error) {
	frames := capabilityPreflightFrames(version, cwd)
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 1024*1024)

	initialize, err := exchangeOAuthProbeFrame(ctx, scanner, writer, frames[0], "capability_initialize")
	if err != nil {
		return CapabilityPreflightObservation{}, capabilityPreflightError("initialize", err)
	}
	if err := writeOAuthProbeFrame(writer, frames[1]); err != nil {
		return CapabilityPreflightObservation{}, capabilityPreflightError("initialized_write", err)
	}
	configResponse, err := exchangeOAuthProbeFrame(ctx, scanner, writer, frames[2], "capability_config_read")
	if err != nil {
		return CapabilityPreflightObservation{}, capabilityPreflightError("config_read", err)
	}
	if err := verifyCapabilityConfig(configResponse); err != nil {
		return CapabilityPreflightObservation{}, capabilityPreflightError("config_contract", err)
	}
	threadResponse, err := exchangeOAuthProbeFrame(ctx, scanner, writer, frames[3], "capability_thread_start")
	if err != nil {
		return CapabilityPreflightObservation{}, capabilityPreflightError("thread_start", err)
	}
	if err := verifyCapabilityThread(threadResponse); err != nil {
		return CapabilityPreflightObservation{}, capabilityPreflightError("thread_contract", err)
	}
	return CapabilityPreflightObservation{
		CapabilitySet: CodexProfileCapabilitySetV1,
		UserAgent:     resultString(initialize, "userAgent"),
	}, nil
}

func RunCapabilityPreflight(ctx context.Context, options CapabilityPreflightOptions) (CapabilityPreflightObservation, error) {
	binaryPath := strings.TrimSpace(options.BinaryPath)
	if binaryPath == "" {
		return CapabilityPreflightObservation{}, capabilityPreflightError("launch_binary_missing", nil)
	}
	codexHome, err := os.MkdirTemp("", "codex-remote-capability-*")
	if err != nil {
		return CapabilityPreflightObservation{}, capabilityPreflightError("create_codex_home", err)
	}
	defer os.RemoveAll(codexHome)

	material := CapabilityPreflightLaunchMaterial(options.Env, codexHome)
	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := execlaunch.CommandContext(childCtx, binaryPath, material.Args...)
	cmd.Dir = codexHome
	cmd.Env = material.Env
	cmd.Stderr = io.Discard
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return CapabilityPreflightObservation{}, capabilityPreflightError("launch_stdin", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return CapabilityPreflightObservation{}, capabilityPreflightError("launch_stdout", err)
	}
	if err := cmd.Start(); err != nil {
		return CapabilityPreflightObservation{}, capabilityPreflightError("launch_start", err)
	}

	observation, probeErr := RunCapabilityPreflightSession(childCtx, stdout, stdin, options.Version, codexHome)
	_ = stdin.Close()
	cancel()
	_ = cmd.Wait()
	if probeErr != nil {
		return CapabilityPreflightObservation{}, probeErr
	}
	return observation, nil
}

func capabilityPreflightFrames(version, cwd string) []OAuthProbeFrame {
	return []OAuthProbeFrame{
		newOAuthProbeRequest("codex-remote-capability-initialize", "initialize", map[string]any{
			"clientInfo": map[string]any{
				"name":    "Codex Remote Capability Probe",
				"title":   "Codex Remote Capability Probe",
				"version": strings.TrimSpace(version),
			},
			"capabilities": map[string]any{"experimentalApi": true},
		}),
		newOAuthProbeNotification("initialized", map[string]any{}),
		newOAuthProbeRequest("codex-remote-capability-config", "config/read", map[string]any{
			"includeLayers": false,
			"cwd":           strings.TrimSpace(cwd),
		}),
		newOAuthProbeRequest("codex-remote-capability-thread", "thread/start", map[string]any{
			"model":         capabilityProbeModel,
			"modelProvider": capabilityProbeProviderID,
			"cwd":           strings.TrimSpace(cwd),
			"ephemeral":     true,
			"config": map[string]any{
				"model_reasoning_effort":         capabilityProbeReasoning,
				"review_model":                   capabilityProbeReviewModel,
				"model_context_window":           272000,
				"model_auto_compact_token_limit": 244800,
			},
		}),
	}
}

func capabilityProbeConfigFixture() map[string]any {
	return map[string]any{
		"model_provider":                 capabilityProbeProviderID,
		"model":                          capabilityProbeModel,
		"review_model":                   capabilityProbeReviewModel,
		"model_reasoning_effort":         capabilityProbeReasoning,
		"model_context_window":           272000,
		"model_auto_compact_token_limit": 244800,
	}
}

func verifyCapabilityConfig(response map[string]any) error {
	configValue, ok := response["config"].(map[string]any)
	if !ok {
		return fmt.Errorf("config/read omitted config")
	}
	for key, expected := range capabilityProbeConfigFixture() {
		actual, ok := configValue[key]
		if !ok || fmt.Sprint(actual) != fmt.Sprint(expected) {
			return fmt.Errorf("config/read mismatch for %s", key)
		}
	}
	return nil
}

func verifyCapabilityThread(response map[string]any) error {
	expected := map[string]string{
		"modelProvider":   capabilityProbeProviderID,
		"model":           capabilityProbeModel,
		"reasoningEffort": capabilityProbeReasoning,
	}
	for key, value := range expected {
		if resultString(response, key) != value {
			return fmt.Errorf("thread/start mismatch for %s", key)
		}
	}
	return nil
}

func capabilityPreflightError(stage string, _ error) error {
	return &OAuthProbeError{Code: ErrorCodexCapabilityUnsupported, Stage: strings.TrimSpace(stage)}
}
