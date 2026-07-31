package codexprofile

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunOAuthProbeUsesIsolatedShortLivedProcess(t *testing.T) {
	parentCWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	reportPath := filepath.Join(t.TempDir(), "probe-cwd.txt")
	env := append([]string{}, os.Environ()...)
	env = append(env,
		"GO_WANT_CODEX_OAUTH_PROBE_HELPER=success",
		"CODEX_OAUTH_PROBE_REPORT="+reportPath,
		"CODEX_OAUTH_PROBE_PARENT_CWD="+parentCWD,
		"OPENAI_API_KEY=must-be-cleared",
		"CODEX_ACCESS_TOKEN=must-be-cleared",
		"CODEX_REMOTE_CODEX_PROFILE_API_KEY=must-be-cleared",
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	observation, err := RunOAuthProbe(ctx, OAuthProbeOptions{
		BinaryPath: os.Args[0],
		Env:        env,
		Version:    "test-version",
	})
	if err != nil {
		t.Fatalf("RunOAuthProbe: %v", err)
	}
	if observation.Result.Status != OAuthProbeStatusDetected || observation.CapabilitySet != OAuthProbeCapabilitySetV1 {
		t.Fatalf("unexpected observation: %#v", observation)
	}
	raw, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("read helper report: %v", err)
	}
	probeCWD := strings.TrimSpace(string(raw))
	if probeCWD == "" || probeCWD == parentCWD {
		t.Fatalf("OAuth probe inherited unsafe working directory %q", probeCWD)
	}
	if _, err := os.Stat(probeCWD); !os.IsNotExist(err) {
		t.Fatalf("OAuth probe working directory was not removed: path=%q err=%v", probeCWD, err)
	}
}

func TestRunOAuthProbeTimeoutDoesNotRetry(t *testing.T) {
	env := append([]string{}, os.Environ()...)
	env = append(env, "GO_WANT_CODEX_OAUTH_PROBE_HELPER=hang")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := RunOAuthProbe(ctx, OAuthProbeOptions{
		BinaryPath: os.Args[0],
		Env:        env,
		Version:    "test-version",
	})
	if got := OAuthProbeErrorCode(err); got != ErrorOAuthProbeUnknown {
		t.Fatalf("error code = %q, want %q (err=%v)", got, ErrorOAuthProbeUnknown, err)
	}
}

func TestRunCapabilityPreflightUsesDisposableCodexHome(t *testing.T) {
	reportPath := filepath.Join(t.TempDir(), "codex-home.txt")
	env := append([]string{}, os.Environ()...)
	env = append(env,
		"GO_WANT_CODEX_OAUTH_PROBE_HELPER=capability",
		"CODEX_CAPABILITY_PROBE_REPORT="+reportPath,
		"CODEX_HOME=/production/codex-home",
		"OPENAI_API_KEY=must-be-cleared",
		"CODEX_ACCESS_TOKEN=must-be-cleared",
		CodexProfileAPIKeyEnv+"=must-be-replaced",
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	observation, err := RunCapabilityPreflight(ctx, CapabilityPreflightOptions{
		BinaryPath: os.Args[0],
		Env:        env,
		Version:    "test-version",
	})
	if err != nil {
		t.Fatalf("RunCapabilityPreflight: %v", err)
	}
	if observation.CapabilitySet != CodexProfileCapabilitySetV1 {
		t.Fatalf("unexpected observation: %#v", observation)
	}
	raw, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("read helper report: %v", err)
	}
	codexHome := strings.TrimSpace(string(raw))
	if codexHome == "" || codexHome == "/production/codex-home" {
		t.Fatalf("preflight used unsafe CODEX_HOME %q", codexHome)
	}
	if _, err := os.Stat(codexHome); !os.IsNotExist(err) {
		t.Fatalf("disposable CODEX_HOME was not removed: path=%q err=%v", codexHome, err)
	}
}

func TestRunCapabilityPreflightAgainstRealCodex(t *testing.T) {
	binaryPath := strings.TrimSpace(os.Getenv("CODEX_TEST_REAL_BINARY"))
	if binaryPath == "" {
		t.Skip("CODEX_TEST_REAL_BINARY is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	observation, err := RunCapabilityPreflight(ctx, CapabilityPreflightOptions{
		BinaryPath: binaryPath,
		Env:        os.Environ(),
		Version:    "integration-test",
	})
	if err != nil {
		t.Fatalf("RunCapabilityPreflight: %v", err)
	}
	if observation.CapabilitySet != CodexProfileCapabilitySetV1 || strings.TrimSpace(observation.UserAgent) == "" {
		t.Fatalf("unexpected real Codex observation: %#v", observation)
	}
}

func TestRunNativeConfigProbeAgainstRealCodex(t *testing.T) {
	binaryPath := strings.TrimSpace(os.Getenv("CODEX_TEST_REAL_BINARY"))
	if binaryPath == "" {
		t.Skip("CODEX_TEST_REAL_BINARY is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	observation, err := RunNativeConfigProbe(ctx, NativeConfigProbeOptions{
		BinaryPath: binaryPath,
		Env:        os.Environ(),
		Version:    "integration-test",
	})
	if err != nil {
		t.Fatalf("RunNativeConfigProbe: %v", err)
	}
	if observation.ModelProviderID == "" || !containsFold(observation.ProviderIDs, observation.ModelProviderID) {
		t.Fatalf("native probe did not return a selected configured provider")
	}
}

func TestRunNativeConfigProbePreservesNativeAuthAndUsesNeutralCWD(t *testing.T) {
	reportPath := filepath.Join(t.TempDir(), "native-cwd.txt")
	env := append([]string{}, os.Environ()...)
	env = append(env,
		"GO_WANT_CODEX_OAUTH_PROBE_HELPER=native",
		"CODEX_NATIVE_PROBE_REPORT="+reportPath,
		"CODEX_HOME=/production/codex-home",
		"OPENAI_API_KEY=native-api-key",
		"CODEX_ACCESS_TOKEN=native-access-token",
	)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	observation, err := RunNativeConfigProbe(ctx, NativeConfigProbeOptions{
		BinaryPath: os.Args[0],
		Env:        env,
		Version:    "test-version",
	})
	if err != nil {
		t.Fatalf("RunNativeConfigProbe: %v", err)
	}
	if observation.ModelProviderID != "native-provider" {
		t.Fatalf("unexpected observation: %#v", observation)
	}
	raw, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("read helper report: %v", err)
	}
	probeCWD := strings.TrimSpace(string(raw))
	if probeCWD == "" || probeCWD == "/production/codex-home" {
		t.Fatalf("native probe cwd = %q", probeCWD)
	}
	if _, err := os.Stat(probeCWD); !os.IsNotExist(err) {
		t.Fatalf("native probe working directory was not removed: path=%q err=%v", probeCWD, err)
	}
}

func TestMain(m *testing.M) {
	mode := os.Getenv("GO_WANT_CODEX_OAUTH_PROBE_HELPER")
	if mode != "" {
		os.Exit(runCodexOAuthProbeHelper(mode))
	}
	os.Exit(m.Run())
}

func runCodexOAuthProbeHelper(mode string) int {
	if mode == "hang" {
		time.Sleep(10 * time.Minute)
	}
	if mode == "capability" {
		return runCodexCapabilityProbeHelper()
	}
	if mode == "native" {
		return runCodexNativeConfigProbeHelper()
	}
	if mode != "success" {
		return 2
	}
	if os.Getenv("OPENAI_API_KEY") != "" || os.Getenv("CODEX_ACCESS_TOKEN") != "" || os.Getenv(CodexProfileAPIKeyEnv) != "" {
		return 3
	}
	if parentCWD := strings.TrimSpace(os.Getenv("CODEX_OAUTH_PROBE_PARENT_CWD")); parentCWD != "" {
		cwd, err := os.Getwd()
		if err != nil || cwd == parentCWD {
			return 8
		}
		if reportPath := strings.TrimSpace(os.Getenv("CODEX_OAUTH_PROBE_REPORT")); reportPath != "" {
			if err := os.WriteFile(reportPath, []byte(cwd), 0o600); err != nil {
				return 9
			}
		}
	}
	if !sameProbeHelperArgs(os.Args[1:], OAuthProbeLaunchMaterial(nil).Args) {
		return 4
	}

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var frame map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &frame); err != nil {
			return 5
		}
		id, hasID := frame["id"].(string)
		if !hasID {
			continue
		}
		method, _ := frame["method"].(string)
		result := map[string]any{}
		switch method {
		case "initialize":
			result["userAgent"] = "codex-helper/1"
		case "config/read":
			result["config"] = map[string]any{"chatgpt_base_url": OfficialChatGPTBaseURL}
		case "account/read":
			result["account"] = map[string]any{"type": "chatgpt", "email": "helper@example.com", "planType": "plus"}
		case "getAuthStatus":
			result["authMethod"] = "chatgpt"
		default:
			return 6
		}
		if err := writeProbeTestFrame(os.Stdout, map[string]any{"id": id, "result": result}); err != nil {
			return 7
		}
	}
	return 0
}

func runCodexNativeConfigProbeHelper() int {
	if os.Getenv("CODEX_HOME") != "/production/codex-home" || os.Getenv("OPENAI_API_KEY") != "native-api-key" || os.Getenv("CODEX_ACCESS_TOKEN") != "native-access-token" {
		return 30
	}
	if !sameProbeHelperArgs(os.Args[1:], []string{"app-server"}) {
		return 31
	}
	cwd, err := os.Getwd()
	if err != nil || cwd == "/production/codex-home" {
		return 32
	}
	if reportPath := strings.TrimSpace(os.Getenv("CODEX_NATIVE_PROBE_REPORT")); reportPath != "" {
		if err := os.WriteFile(reportPath, []byte(cwd), 0o600); err != nil {
			return 33
		}
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var frame map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &frame); err != nil {
			return 34
		}
		id, hasID := frame["id"].(string)
		if !hasID {
			continue
		}
		method, _ := frame["method"].(string)
		result := map[string]any{}
		switch method {
		case "initialize":
			result["userAgent"] = "codex-helper/1"
		case "config/read":
			result["config"] = map[string]any{
				"model_provider": "native-provider",
				"model_providers": map[string]any{
					"native-provider": map[string]any{"base_url": "https://native.example/v1"},
				},
			}
		default:
			return 35
		}
		if err := writeProbeTestFrame(os.Stdout, map[string]any{"id": id, "result": result}); err != nil {
			return 36
		}
	}
	return 0
}

func runCodexCapabilityProbeHelper() int {
	if os.Getenv("OPENAI_API_KEY") != "" || os.Getenv("CODEX_ACCESS_TOKEN") != "" || os.Getenv(CodexProfileAPIKeyEnv) != capabilityProbeAPIKey {
		return 20
	}
	codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME"))
	if codexHome == "" || codexHome == "/production/codex-home" {
		return 21
	}
	if reportPath := strings.TrimSpace(os.Getenv("CODEX_CAPABILITY_PROBE_REPORT")); reportPath != "" {
		if err := os.WriteFile(reportPath, []byte(codexHome), 0o600); err != nil {
			return 22
		}
	}
	if !sameProbeHelperArgs(os.Args[1:], CapabilityPreflightLaunchMaterial(nil, codexHome).Args) {
		return 23
	}

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var frame map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &frame); err != nil {
			return 24
		}
		id, hasID := frame["id"].(string)
		if !hasID {
			continue
		}
		method, _ := frame["method"].(string)
		result := map[string]any{}
		switch method {
		case "initialize":
			result["userAgent"] = "codex-helper/1"
		case "config/read":
			result["config"] = capabilityProbeConfigFixture()
		case "thread/start":
			result = map[string]any{
				"modelProvider":   capabilityProbeProviderID,
				"model":           capabilityProbeModel,
				"reasoningEffort": capabilityProbeReasoning,
				"cwd":             codexHome,
			}
		default:
			return 25
		}
		if err := writeProbeTestFrame(os.Stdout, map[string]any{"id": id, "result": result}); err != nil {
			return 26
		}
	}
	return 0
}

func sameProbeHelperArgs(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if strings.TrimSpace(got[index]) != strings.TrimSpace(want[index]) {
			return false
		}
	}
	return true
}
