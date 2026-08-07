package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/codexprofile"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func TestDaemonStartsCodexHeadlessWithCustomProviderLaunchOverrides(t *testing.T) {
	cfg := config.DefaultAppConfig()
	cfg.Codex.Providers = []config.CodexProviderConfig{{
		ID:              "team-proxy",
		Name:            "Team Proxy",
		BaseURL:         "https://proxy.example/v1",
		APIKey:          "provider-secret",
		Model:           "gpt-5.5",
		ReasoningEffort: "xhigh",
	}}
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := config.WriteAppConfig(configPath, cfg); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}

	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		BinaryPath:      "/tmp/codex-remote",
		CodexRealBinary: "fake-codex",
		ConfigPath:      configPath,
		BaseEnv:         []string{"PATH=/usr/bin", "OPENAI_API_KEY=global-secret", "CUSTOM_API_KEY=native-secret"},
		LaunchArgs:      []string{"app-server"},
		Paths: relayruntime.Paths{
			LogsDir:  t.TempDir(),
			StateDir: t.TempDir(),
		},
	})
	app.ConfigureAdmin(AdminRuntimeOptions{
		ConfigPath:      configPath,
		Services:        defaultFeishuServices(),
		AdminListenHost: "127.0.0.1",
		AdminListenPort: "9501",
		AdminURL:        "http://localhost:9501/admin/",
		SetupURL:        "http://localhost:9501/setup",
	})
	app.runCodexNativeConfigProbe = func(context.Context, codexprofile.NativeConfigProbeOptions) (codexprofile.NativeConfigObservation, error) {
		return codexprofile.NativeConfigObservation{ProviderEnvKeys: []string{"CUSTOM_API_KEY"}}, nil
	}
	app.ensureCodexNativeConnectionEvidence(context.Background())
	app.service.MaterializeSurfaceResumeWithCodexProvider("surface-1", "", "chat-1", "user-1", "normal", agentproto.BackendCodex, "team-proxy", "", "", "")

	var captured relayruntime.HeadlessLaunchOptions
	app.startHeadless = func(opts relayruntime.HeadlessLaunchOptions) (int, error) {
		captured = opts
		return 4324, nil
	}

	command := control.DaemonCommand{
		Kind:             control.DaemonCommandStartHeadless,
		SurfaceSessionID: "surface-1",
		InstanceID:       "inst-codex-provider",
		ThreadCWD:        evalSymlinkForTest(t, t.TempDir()),
		Backend:          agentproto.BackendCodex,
		CodexProviderID:  "team-proxy",
	}
	authorizePendingHeadlessForTest(t, app, command)
	app.startManagedHeadless(command)

	if !containsEnvEntry(captured.Env, "CODEX_REMOTE_INSTANCE_BACKEND=codex") {
		t.Fatalf("expected codex backend env, got %#v", captured.Env)
	}
	if !containsEnvEntry(captured.Env, config.CodexRuntimeProviderIDEnv+"=team-proxy") {
		t.Fatalf("expected runtime provider id env, got %#v", captured.Env)
	}
	if !containsEnvEntry(captured.Env, codexprofile.CodexProfileAPIKeyEnv+"=provider-secret") {
		t.Fatalf("expected provider api key env, got %#v", captured.Env)
	}
	if containsEnvEntry(captured.Env, "OPENAI_API_KEY=") || containsEnvEntry(captured.Env, config.CodexProviderAPIKeyEnv+"=") {
		t.Fatalf("expected conflicting auth env to be cleared, got %#v", captured.Env)
	}
	if containsEnvEntry(captured.Env, "CUSTOM_API_KEY=native-secret") {
		t.Fatalf("expected native provider env key to be cleared, got %#v", captured.Env)
	}
	args := strings.Join(captured.Args, "\n")
	for _, want := range []string{
		`model_provider="codex_remote_profile_`,
		`.name="Codex Remote API"`,
		`.base_url="https://proxy.example/v1"`,
		`.wire_api="responses"`,
		`.env_key="CODEX_REMOTE_CODEX_PROFILE_API_KEY"`,
		`.requires_openai_auth=false`,
		`.supports_websockets=false`,
		`cli_auth_credentials_store="ephemeral"`,
	} {
		if !strings.Contains(args, want) {
			t.Fatalf("expected launch args to contain %q, got %#v", want, captured.Args)
		}
	}
	for _, forbidden := range []string{`model="gpt-5.5"`, `model_reasoning_effort="xhigh"`, `review_model=`} {
		if strings.Contains(args, forbidden) {
			t.Fatalf("launch args must not contain thread policy override %q: %#v", forbidden, captured.Args)
		}
	}
}

func TestDaemonStartsDeepSeekCodexHeadlessWithManagedModelCatalog(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	stateDir := filepath.Join(root, "state")
	record, err := config.PrepareCodexAPIProfileCreate(nil, config.CodexAPIProfileInput{
		Name: "DeepSeek", BaseURL: "https://api.deepseek.com/", APIKey: "deepseek-secret",
		Model: "deepseek-v4-flash", ReasoningEffort: "high",
	})
	if err != nil {
		t.Fatalf("PrepareCodexAPIProfileCreate: %v", err)
	}
	cfg := config.DefaultAppConfig()
	cfg.Codex.Profiles = []config.CodexAPIProfileRecord{record}
	if err := config.WriteAppConfig(configPath, cfg); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}

	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		BinaryPath:      "/tmp/codex-remote",
		CodexRealBinary: "fake-codex",
		ConfigPath:      configPath,
		BaseEnv:         []string{"PATH=/usr/bin", "OPENAI_API_KEY=global-secret"},
		LaunchArgs:      []string{"app-server"},
		Paths: relayruntime.Paths{
			LogsDir:  filepath.Join(root, "logs"),
			StateDir: stateDir,
		},
	})
	app.ConfigureAdmin(AdminRuntimeOptions{ConfigPath: configPath})
	app.runCodexNativeConfigProbe = func(context.Context, codexprofile.NativeConfigProbeOptions) (codexprofile.NativeConfigObservation, error) {
		return codexprofile.NativeConfigObservation{}, nil
	}
	app.ensureCodexNativeConnectionEvidence(context.Background())
	app.service.MaterializeSurfaceResumeWithCodexProvider("surface-1", "", "chat-1", "user-1", "normal", agentproto.BackendCodex, record.ID, "", "", "")

	var captured relayruntime.HeadlessLaunchOptions
	app.startHeadless = func(opts relayruntime.HeadlessLaunchOptions) (int, error) {
		captured = opts
		return 4330, nil
	}
	command := control.DaemonCommand{
		Kind:             control.DaemonCommandStartHeadless,
		SurfaceSessionID: "surface-1",
		InstanceID:       "inst-deepseek-profile",
		ThreadCWD:        root,
		Backend:          agentproto.BackendCodex,
		CodexProviderID:  record.ID,
	}
	authorizePendingHeadlessForTest(t, app, command)
	app.startManagedHeadless(command)

	catalogPath := codexConfigOverrideValue(captured.Args, "model_catalog_json")
	if catalogPath == "" {
		t.Fatalf("expected DeepSeek launch to include model_catalog_json, args=%#v", captured.Args)
	}
	relCatalogPath, err := filepath.Rel(stateDir, catalogPath)
	if err != nil {
		t.Fatalf("expected managed catalog under state dir %q, got %q: %v", stateDir, catalogPath, err)
	}
	if relCatalogPath == "." || relCatalogPath == ".." ||
		strings.HasPrefix(relCatalogPath, ".."+string(os.PathSeparator)) ||
		filepath.IsAbs(relCatalogPath) {
		t.Fatalf("expected managed catalog under state dir %q, got %q", stateDir, catalogPath)
	}
	raw, err := os.ReadFile(catalogPath)
	if err != nil {
		t.Fatalf("expected managed DeepSeek catalog file: %v", err)
	}
	content := string(raw)
	for _, want := range []string{`"slug": "deepseek-v4-flash"`, `"slug": "deepseek-v4-pro"`, `"default_reasoning_level": "high"`, `"effort": "max"`} {
		if !strings.Contains(content, want) {
			t.Fatalf("managed DeepSeek catalog missing %q: %s", want, content)
		}
	}
	if strings.Contains(content, "deepseek-secret") {
		t.Fatal("managed DeepSeek catalog leaked API key")
	}
}

func TestDaemonStartsCodexHeadlessWithCanonicalProfileID(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	stateDir := filepath.Join(root, "state")
	cfg := config.DefaultAppConfig()
	record, err := config.PrepareCodexAPIProfileCreate(nil, config.CodexAPIProfileInput{
		Name: "Canonical Profile", BaseURL: "https://canonical.example/v1", APIKey: "canonical-secret",
		Model: "gpt-5.5", ReasoningEffort: "high",
	})
	if err != nil {
		t.Fatalf("PrepareCodexAPIProfileCreate: %v", err)
	}
	if !strings.HasPrefix(record.ID, "cp_") {
		t.Fatalf("generated profile ID = %q, want canonical cp_ prefix", record.ID)
	}
	cfg.Codex.Profiles = []config.CodexAPIProfileRecord{record}
	if err := config.WriteAppConfig(configPath, cfg); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}

	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		BinaryPath: "/tmp/codex-remote", BaseEnv: []string{"PATH=/usr/bin"}, LaunchArgs: []string{"app-server"},
		Paths: relayruntime.Paths{LogsDir: t.TempDir(), StateDir: stateDir},
	})
	app.ConfigureAdmin(AdminRuntimeOptions{ConfigPath: configPath})
	var captured relayruntime.HeadlessLaunchOptions
	app.startHeadless = func(opts relayruntime.HeadlessLaunchOptions) (int, error) {
		captured = opts
		return 4326, nil
	}

	app.startManagedHeadless(control.DaemonCommand{
		Kind: control.DaemonCommandStartHeadless, InstanceID: "inst-canonical-profile", ThreadCWD: root,
		Backend: agentproto.BackendCodex, CodexProviderID: record.ID,
	})
	if !containsEnvEntry(captured.Env, config.CodexRuntimeProviderIDEnv+"="+record.ID) {
		t.Fatalf("canonical profile did not reach launcher: env=%#v args=%#v", captured.Env, captured.Args)
	}
	args := strings.Join(captured.Args, "\n")
	if !strings.Contains(args, `model_provider="codex_remote_profile_`) ||
		strings.Contains(args, `model_provider="`+record.ID+`"`) ||
		!strings.Contains(args, `.base_url="https://canonical.example/v1"`) {
		t.Fatalf("canonical profile was not projected to an internal provider: %#v", captured.Args)
	}
}

func codexConfigOverrideValue(args []string, key string) string {
	prefix := key + "="
	for index := 0; index+1 < len(args); index++ {
		if args[index] != "-c" || !strings.HasPrefix(args[index+1], prefix) {
			continue
		}
		value := strings.TrimPrefix(args[index+1], prefix)
		unquoted, err := strconv.Unquote(value)
		if err != nil {
			return ""
		}
		return unquoted
	}
	return ""
}

func TestCodexConfigOverrideValueUnquotesEscapedWindowsPath(t *testing.T) {
	want := `C:\Users\runneradmin\AppData\Local\Temp\state\codex-model-catalogs\deepseek-models-v1.json`
	got := codexConfigOverrideValue([]string{"app-server", "-c", "model_catalog_json=" + strconv.Quote(want)}, "model_catalog_json")
	if got != want {
		t.Fatalf("override value = %q, want %q", got, want)
	}
}

func TestDaemonStartsCodexHeadlessWithFrozenAdmissionRevision(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	stateDir := filepath.Join(root, "state")
	record, err := config.PrepareCodexAPIProfileCreate(nil, config.CodexAPIProfileInput{
		Name: "Team Proxy", BaseURL: "https://old.example/v1", APIKey: "old-secret",
		Model: "gpt-5.5", ReasoningEffort: "high",
	})
	if err != nil {
		t.Fatalf("PrepareCodexAPIProfileCreate: %v", err)
	}
	record, changed, err := config.PrepareCodexAPIProfileUpdate(record, config.CodexAPIProfileInput{
		Name: "Team Proxy", BaseURL: "https://new.example/v1", APIKey: "new-secret",
		Model: "gpt-5.5", ReasoningEffort: "medium",
	})
	if err != nil || !changed {
		t.Fatalf("PrepareCodexAPIProfileUpdate changed=%v err=%v", changed, err)
	}
	cfg := config.DefaultAppConfig()
	cfg.Codex.Profiles = []config.CodexAPIProfileRecord{record}
	if err := config.WriteAppConfig(configPath, cfg); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}

	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		BinaryPath: "/tmp/codex-remote", BaseEnv: []string{"PATH=/usr/bin"}, LaunchArgs: []string{"app-server"},
		Paths: relayruntime.Paths{LogsDir: t.TempDir(), StateDir: stateDir},
	})
	app.ConfigureAdmin(AdminRuntimeOptions{ConfigPath: configPath})
	var captured relayruntime.HeadlessLaunchOptions
	app.startHeadless = func(opts relayruntime.HeadlessLaunchOptions) (int, error) {
		captured = opts
		return 4327, nil
	}

	app.startManagedHeadless(control.DaemonCommand{
		Kind:            control.DaemonCommandStartHeadless,
		InstanceID:      "inst-frozen-profile",
		ThreadCWD:       root,
		Backend:         agentproto.BackendCodex,
		CodexProviderID: record.ID,
		CodexAdmissionRef: &state.CodexAdmissionRef{
			ProfileRef:           state.CodexProfileRef{ID: record.ID, Revision: 1},
			ContextPreferenceRef: state.CodexContextPreferenceRef{ProfileID: record.ID, Revision: 1},
		},
	})

	if !containsEnvEntry(captured.Env, codexprofile.CodexProfileAPIKeyEnv+"=old-secret") {
		t.Fatalf("expected frozen old API key env, got %#v", captured.Env)
	}
	args := strings.Join(captured.Args, "\n")
	for _, want := range []string{`.base_url="https://old.example/v1"`} {
		if !strings.Contains(args, want) {
			t.Fatalf("expected frozen launch args to contain %q, got %#v", want, captured.Args)
		}
	}
	for _, forbidden := range []string{`model="gpt-5.5"`, `model_reasoning_effort="high"`, `review_model=`} {
		if strings.Contains(args, forbidden) {
			t.Fatalf("frozen launch args must not contain thread policy override %q: %#v", forbidden, captured.Args)
		}
	}
	if strings.Contains(args, "https://new.example/v1") || containsEnvEntry(captured.Env, codexprofile.CodexProfileAPIKeyEnv+"=new-secret") {
		t.Fatalf("launch drifted to current profile revision: env=%#v args=%#v", captured.Env, captured.Args)
	}
}

func TestDaemonStartsCodexHeadlessWithDefaultProviderKeepsLaunchArgsClean(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := config.WriteAppConfig(configPath, config.DefaultAppConfig()); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}

	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		BinaryPath: "/tmp/codex-remote",
		ConfigPath: configPath,
		BaseEnv:    []string{"PATH=/usr/bin"},
		LaunchArgs: []string{"app-server"},
		Paths: relayruntime.Paths{
			LogsDir:  t.TempDir(),
			StateDir: t.TempDir(),
		},
	})
	app.ConfigureAdmin(AdminRuntimeOptions{
		ConfigPath:      configPath,
		Services:        defaultFeishuServices(),
		AdminListenHost: "127.0.0.1",
		AdminListenPort: "9501",
		AdminURL:        "http://localhost:9501/admin/",
		SetupURL:        "http://localhost:9501/setup",
	})

	var captured relayruntime.HeadlessLaunchOptions
	app.startHeadless = func(opts relayruntime.HeadlessLaunchOptions) (int, error) {
		captured = opts
		return 4325, nil
	}

	app.startManagedHeadless(control.DaemonCommand{
		Kind:            control.DaemonCommandStartHeadless,
		InstanceID:      "inst-codex-default",
		ThreadCWD:       evalSymlinkForTest(t, t.TempDir()),
		Backend:         agentproto.BackendCodex,
		CodexProviderID: config.CodexDefaultProviderID,
	})

	if strings.Contains(strings.Join(captured.Args, "\n"), "model_provider=") {
		t.Fatalf("expected built-in default provider to avoid provider overrides, got %#v", captured.Args)
	}
	if containsEnvEntry(captured.Env, codexprofile.CodexProfileAPIKeyEnv+"=") {
		t.Fatalf("expected built-in default provider to avoid provider key env, got %#v", captured.Env)
	}
}

func TestDaemonRejectsIncompleteMigratedCodexProfileBeforeLaunch(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	stateDir := filepath.Join(root, "state")
	cfg := config.DefaultAppConfig()
	cfg.Codex.Providers = []config.CodexProviderConfig{{
		ID: "incomplete", Name: "Incomplete", BaseURL: "https://proxy.example/v1", APIKey: "secret",
	}}
	if err := config.WriteAppConfig(configPath, cfg); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}

	app := New(":0", ":0", nil, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		BinaryPath: "/bin/false",
		Paths:      relayruntime.Paths{StateDir: stateDir},
	})
	app.ConfigureAdmin(AdminRuntimeOptions{ConfigPath: configPath})
	launched := false
	app.startHeadless = func(relayruntime.HeadlessLaunchOptions) (int, error) {
		launched = true
		return 1, nil
	}

	app.handleDaemonCommand(control.DaemonCommand{
		Kind:            control.DaemonCommandStartHeadless,
		InstanceID:      "headless-incomplete",
		Backend:         agentproto.BackendCodex,
		CodexProviderID: "incomplete",
	})
	if launched {
		t.Fatal("incomplete migrated Codex Profile reached the launcher")
	}
}

func TestCodexHeadlessLaunchProblemPreservesStableRuntimeReason(t *testing.T) {
	problem := codexHeadlessLaunchProblem(
		&codexprofile.RuntimeError{Code: codexprofile.ErrorProfileSecretMissing},
		agentproto.ErrorInfo{
			Code:      "codex_provider_prepare_failed",
			Message:   "Codex Provider 准备失败。",
			Retryable: true,
		},
	)
	if problem.Code != codexprofile.ErrorProfileSecretMissing {
		t.Fatalf("problem code = %q, want %q", problem.Code, codexprofile.ErrorProfileSecretMissing)
	}
	if problem.Retryable {
		t.Fatal("deterministic Profile failure must not be marked retryable")
	}
	if !strings.Contains(problem.Message, "API Key") {
		t.Fatalf("problem message = %q, want actionable API Key reason", problem.Message)
	}
}

func TestCodexHeadlessLaunchProblemClassifiesProbeFailures(t *testing.T) {
	tests := []struct {
		code        string
		stage       string
		wantMessage string
	}{
		{code: codexprofile.ErrorCodexBinaryUnavailable, wantMessage: "找不到"},
		{code: codexprofile.ErrorCodexProbeTimeout, wantMessage: "超时"},
		{code: codexprofile.ErrorCodexProbeUnavailable, stage: "capability_initialize", wantMessage: "暂时无法"},
		{code: codexprofile.ErrorCodexProbeContractMismatch, wantMessage: "契约"},
	}
	for _, test := range tests {
		problem := codexHeadlessLaunchProblem(
			&codexprofile.RuntimeError{Code: test.code, Stage: test.stage},
			agentproto.ErrorInfo{Code: "codex_provider_prepare_failed", Message: "Codex Provider 准备失败。", Retryable: true},
		)
		if problem.Code != test.code {
			t.Fatalf("problem code = %q, want %q", problem.Code, test.code)
		}
		if problem.Retryable {
			t.Fatalf("probe failure %q must not be marked retryable", test.code)
		}
		if !strings.Contains(problem.Message, test.wantMessage) {
			t.Fatalf("problem message for %q = %q, want contains %q", test.code, problem.Message, test.wantMessage)
		}
		if problem.Details != test.stage {
			t.Fatalf("problem details for %q = %q, want stage %q", test.code, problem.Details, test.stage)
		}
	}
}
