package codexprofile

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/execlaunch"
)

type NativeConfigProbeOptions struct {
	BinaryPath string
	Env        []string
	Version    string
}

type NativeConfigObservation struct {
	ModelProviderID string
	ModelEndpoint   string
	ChatGPTEndpoint string
	ProviderIDs     []string
	ProviderEnvKeys []string
}

func (NativeConfigObservation) MarshalJSON() ([]byte, error) {
	return nil, fmt.Errorf("raw Codex native config observation cannot be serialized")
}

func RunNativeConfigProbeSession(ctx context.Context, reader io.Reader, writer io.Writer, version, cwd string) (NativeConfigObservation, error) {
	frames := nativeConfigProbeFrames(version, cwd)
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 1024*1024)

	if _, err := exchangeOAuthProbeFrame(ctx, scanner, writer, frames[0], "native_initialize"); err != nil {
		return NativeConfigObservation{}, err
	}
	if err := writeOAuthProbeFrame(writer, frames[1]); err != nil {
		return NativeConfigObservation{}, nativeConfigProbeError("initialized_write")
	}
	response, err := exchangeOAuthProbeFrame(ctx, scanner, writer, frames[2], "native_config_read")
	if err != nil {
		return NativeConfigObservation{}, err
	}
	return decodeNativeConfigObservation(response)
}

func RunNativeConfigProbe(ctx context.Context, options NativeConfigProbeOptions) (NativeConfigObservation, error) {
	binaryPath := strings.TrimSpace(options.BinaryPath)
	if binaryPath == "" {
		return NativeConfigObservation{}, nativeConfigProbeError("launch_binary_missing")
	}
	workDir, err := os.MkdirTemp("", "codex-remote-native-probe-*")
	if err != nil {
		return NativeConfigObservation{}, nativeConfigProbeError("launch_workdir")
	}
	defer os.RemoveAll(workDir)

	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := execlaunch.CommandContext(childCtx, binaryPath, "app-server")
	cmd.Dir = workDir
	cmd.Env = append([]string{}, options.Env...)
	cmd.Stderr = io.Discard
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return NativeConfigObservation{}, nativeConfigProbeError("launch_stdin")
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return NativeConfigObservation{}, nativeConfigProbeError("launch_stdout")
	}
	if err := cmd.Start(); err != nil {
		return NativeConfigObservation{}, nativeConfigProbeError("launch_start")
	}

	observation, probeErr := RunNativeConfigProbeSession(childCtx, stdout, stdin, options.Version, workDir)
	_ = stdin.Close()
	cancel()
	_ = cmd.Wait()
	if probeErr != nil {
		return NativeConfigObservation{}, probeErr
	}
	return observation, nil
}

func ProjectNativeConnectionEvidence(observation NativeConfigObservation, connectionGeneration uint64) state.CodexNativeConnectionEvidence {
	if connectionGeneration == 0 {
		connectionGeneration = 1
	}
	return state.CodexNativeConnectionEvidence{
		Revision:             1,
		ConnectionGeneration: connectionGeneration,
		ModelProviderID:      strings.TrimSpace(observation.ModelProviderID),
		ModelEndpointID:      publicEndpointID(observation.ModelEndpoint),
		ChatGPTEndpointID:    publicEndpointID(observation.ChatGPTEndpoint),
	}
}

func nativeConfigProbeFrames(version, cwd string) []OAuthProbeFrame {
	return []OAuthProbeFrame{
		newOAuthProbeRequest("codex-remote-native-initialize", "initialize", map[string]any{
			"clientInfo": map[string]any{
				"name":    "Codex Remote Native Config Probe",
				"title":   "Codex Remote Native Config Probe",
				"version": strings.TrimSpace(version),
			},
			"capabilities": map[string]any{"experimentalApi": true},
		}),
		newOAuthProbeNotification("initialized", map[string]any{}),
		newOAuthProbeRequest("codex-remote-native-config", "config/read", map[string]any{
			"includeLayers": false,
			"cwd":           strings.TrimSpace(cwd),
		}),
	}
}

func decodeNativeConfigObservation(response map[string]any) (NativeConfigObservation, error) {
	configValue, ok := response["config"].(map[string]any)
	if !ok {
		return NativeConfigObservation{}, nativeConfigProbeError("config_missing")
	}
	observation := NativeConfigObservation{
		ModelProviderID: resultString(configValue, "model_provider"),
		ChatGPTEndpoint: resultString(configValue, "chatgpt_base_url"),
	}
	providers, _ := configValue["model_providers"].(map[string]any)
	for providerID := range providers {
		providerID = strings.TrimSpace(providerID)
		if providerID == "" {
			continue
		}
		observation.ProviderIDs = append(observation.ProviderIDs, providerID)
		provider, _ := providers[providerID].(map[string]any)
		if envKey := resultString(provider, "env_key"); envKey != "" {
			observation.ProviderEnvKeys = append(observation.ProviderEnvKeys, envKey)
		}
	}
	sort.Strings(observation.ProviderIDs)
	sort.Strings(observation.ProviderEnvKeys)
	if selected, ok := providers[observation.ModelProviderID].(map[string]any); ok {
		observation.ModelEndpoint = resultString(selected, "base_url")
	}
	return observation, nil
}

func publicEndpointID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err == nil && (strings.EqualFold(parsed.Scheme, "https") || strings.EqualFold(parsed.Scheme, "http")) &&
		parsed.Host != "" && parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == "" {
		return value
	}
	sum := sha256.Sum256([]byte("codex-native-endpoint-v1\x00" + value))
	return "opaque:v1:" + hex.EncodeToString(sum[:])
}

func nativeConfigProbeError(stage string) error {
	return &OAuthProbeError{Code: ErrorOAuthProbeUnknown, Stage: strings.TrimSpace(stage)}
}
