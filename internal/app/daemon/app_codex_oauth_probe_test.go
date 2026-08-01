package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/codexprofile"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/codexoauthstate"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func TestCodexOAuthProbeCoordinatorIsSingleFlight(t *testing.T) {
	app := New(":0", ":0", nil, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		CodexRealBinary: "fake-codex",
		BaseEnv:         []string{"HOME=/tmp/test"},
		Paths:           relayruntime.Paths{StateDir: t.TempDir()},
	})
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	app.runCodexOAuthProbe = func(context.Context, codexprofile.OAuthProbeOptions) (codexprofile.OAuthProbeObservation, error) {
		calls.Add(1)
		close(entered)
		<-release
		return testDetectedOAuthObservation(), nil
	}

	if !app.requestCodexOAuthProbe(context.Background(), false) {
		t.Fatal("first probe request was not started")
	}
	<-entered
	if app.requestCodexOAuthProbe(context.Background(), false) {
		t.Fatal("second concurrent probe request bypassed singleflight")
	}
	close(release)
	waitForCodexOAuthState(t, app, codexprofile.OAuthProbeStatusDetected)
	if calls.Load() != 1 {
		t.Fatalf("probe calls = %d, want 1", calls.Load())
	}
}

func TestCodexOAuthProbeFailureBecomesUnknownWithoutRetry(t *testing.T) {
	app := New(":0", ":0", nil, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		CodexRealBinary: "fake-codex",
		Paths:           relayruntime.Paths{StateDir: t.TempDir()},
	})
	var calls atomic.Int32
	app.runCodexOAuthProbe = func(context.Context, codexprofile.OAuthProbeOptions) (codexprofile.OAuthProbeObservation, error) {
		calls.Add(1)
		return codexprofile.OAuthProbeObservation{}, errors.New("secret diagnostic must not persist")
	}

	if !app.requestCodexOAuthProbe(context.Background(), false) {
		t.Fatal("probe request was not started")
	}
	profile := waitForCodexOAuthState(t, app, codexprofile.OAuthProbeStatusUnknown)
	time.Sleep(50 * time.Millisecond)
	if calls.Load() != 1 {
		t.Fatalf("failed probe retried automatically: calls=%d", calls.Load())
	}
	if profile.LastProbeErrorCode != codexprofile.ErrorOAuthProbeUnknown || strings.Contains(profile.LastProbeErrorCode, "secret") {
		t.Fatalf("unexpected persisted probe error: %#v", profile)
	}
}

func TestCodexOAuthProbeStoreFailureDoesNotAuthorizeLifecycle(t *testing.T) {
	stateDir := t.TempDir()
	app := New(":0", ":0", nil, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		CodexRealBinary: "fake-codex",
		Paths:           relayruntime.Paths{StateDir: stateDir},
	})
	if err := os.Mkdir(codexoauthstate.StatePath(stateDir), 0o700); err != nil {
		t.Fatalf("Mkdir OAuth state path: %v", err)
	}
	app.runCodexOAuthProbe = func(context.Context, codexprofile.OAuthProbeOptions) (codexprofile.OAuthProbeObservation, error) {
		return testDetectedOAuthObservation(), nil
	}

	if !app.requestCodexOAuthProbe(context.Background(), false) {
		t.Fatal("probe request was not started")
	}
	waitForCodexOAuthProbeSettled(t, app)
	assertCodexOAuthLifecycleNotAuthorized(t, app)
}

func TestCodexOAuthProbePreferenceFailureDoesNotAuthorizeLifecycle(t *testing.T) {
	app := New(":0", ":0", nil, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		CodexRealBinary: "fake-codex",
		Paths:           relayruntime.Paths{StateDir: t.TempDir()},
	})
	app.mu.Lock()
	app.profileContextPreferenceState.status = persistedStoreStatusDegraded
	app.profileContextPreferenceState.diagnosticErr = errors.New("test preference failure")
	app.mu.Unlock()
	app.runCodexOAuthProbe = func(context.Context, codexprofile.OAuthProbeOptions) (codexprofile.OAuthProbeObservation, error) {
		return testDetectedOAuthObservation(), nil
	}

	if !app.requestCodexOAuthProbe(context.Background(), false) {
		t.Fatal("probe request was not started")
	}
	waitForCodexOAuthProbeSettled(t, app)
	assertCodexOAuthLifecycleNotAuthorized(t, app)
}

func TestMissingOAuthProbeStillInitializesContextPreference(t *testing.T) {
	app := New(":0", ":0", nil, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		CodexRealBinary: "fake-codex",
		Paths:           relayruntime.Paths{StateDir: t.TempDir()},
	})
	app.runCodexOAuthProbe = func(context.Context, codexprofile.OAuthProbeOptions) (codexprofile.OAuthProbeObservation, error) {
		return codexprofile.OAuthProbeObservation{
			Result:        codexprofile.OAuthProbeResult{Status: codexprofile.OAuthProbeStatusMissing},
			CapabilitySet: codexprofile.OAuthProbeCapabilitySetV1,
		}, nil
	}

	if !app.requestCodexOAuthProbe(context.Background(), false) {
		t.Fatal("probe request was not started")
	}
	waitForCodexOAuthState(t, app, codexprofile.OAuthProbeStatusMissing)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		app.mu.Lock()
		store, err := app.profileContextPreferenceStore()
		ok := false
		if err == nil {
			_, ok = store.CodexCurrent(state.OAuthCodexProfileID)
		}
		app.mu.Unlock()
		if err == nil && ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("missing OAuth descriptor did not receive a context preference")
}

func TestCodexOAuthProbeRefreshesMaterializedProfileCatalog(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	cfg := config.DefaultAppConfig()
	record, err := config.PrepareCodexAPIProfileCreate(nil, config.CodexAPIProfileInput{
		Name: "API", BaseURL: "https://api.example/v1", APIKey: "secret", Model: "model", ReasoningEffort: "high",
	})
	if err != nil {
		t.Fatalf("PrepareCodexAPIProfileCreate: %v", err)
	}
	cfg.Codex.Profiles = []config.CodexAPIProfileRecord{record}
	if err := config.WriteAppConfig(configPath, cfg); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}
	app := New(":0", ":0", nil, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		CodexRealBinary: "fake-codex",
		ConfigPath:      configPath,
		Paths:           relayruntime.Paths{StateDir: filepath.Join(root, "state")},
	})
	app.ConfigureAdmin(AdminRuntimeOptions{ConfigPath: configPath})
	app.syncCodexProvidersCatalogFromConfig()
	if _, ok := findServiceCodexProfile(app, state.OAuthCodexProfileID); ok {
		t.Fatal("OAuth profile should not be materialized before the probe")
	}
	app.runCodexOAuthProbe = func(context.Context, codexprofile.OAuthProbeOptions) (codexprofile.OAuthProbeObservation, error) {
		return testDetectedOAuthObservation(), nil
	}

	if !app.requestCodexOAuthProbe(context.Background(), false) {
		t.Fatal("OAuth probe request was not started")
	}
	waitForCodexOAuthState(t, app, codexprofile.OAuthProbeStatusDetected)
	profile, ok := findServiceCodexProfile(app, state.OAuthCodexProfileID)
	if !ok {
		t.Fatalf("OAuth profile was not materialized after probe; profiles=%#v", app.service.CodexProfiles())
	}
	if !profile.Available || profile.Name != "ChatGPT 登录" {
		t.Fatalf("unexpected materialized OAuth profile: %#v", profile)
	}
}

func TestCodexCapabilityPreflightRunsOnceAndFailsClosed(t *testing.T) {
	app := New(":0", ":0", nil, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		CodexRealBinary: "fake-codex",
		Paths:           relayruntime.Paths{StateDir: t.TempDir()},
	})
	var calls atomic.Int32
	app.runCodexCapabilityPreflight = func(context.Context, codexprofile.CapabilityPreflightOptions) (codexprofile.CapabilityPreflightObservation, error) {
		calls.Add(1)
		return codexprofile.CapabilityPreflightObservation{}, &codexprofile.OAuthProbeError{
			Code:  codexprofile.ErrorCodexCapabilityUnsupported,
			Stage: "fixture",
		}
	}

	app.ensureCodexRuntimeCapability(context.Background())
	app.ensureCodexRuntimeCapability(context.Background())
	if calls.Load() != 1 {
		t.Fatalf("capability preflight calls = %d, want 1", calls.Load())
	}
	app.mu.Lock()
	checked := app.codexRuntimeCapability.checked
	capabilitySet := app.codexRuntimeCapability.capabilitySet
	errorCode := app.codexRuntimeCapability.errorCode
	app.mu.Unlock()
	if !checked || capabilitySet != "" || errorCode != codexprofile.ErrorCodexCapabilityUnsupported {
		t.Fatalf("unexpected capability state: checked=%v set=%q error=%q", checked, capabilitySet, errorCode)
	}
}

func TestCodexCapabilityPreflightSuccessFeedsOAuthDescriptor(t *testing.T) {
	app := New(":0", ":0", nil, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		CodexRealBinary: "fake-codex",
		Paths:           relayruntime.Paths{StateDir: t.TempDir()},
	})
	app.runCodexCapabilityPreflight = func(context.Context, codexprofile.CapabilityPreflightOptions) (codexprofile.CapabilityPreflightObservation, error) {
		return codexprofile.CapabilityPreflightObservation{CapabilitySet: codexprofile.CodexProfileCapabilitySetV1}, nil
	}
	app.runCodexOAuthProbe = func(context.Context, codexprofile.OAuthProbeOptions) (codexprofile.OAuthProbeObservation, error) {
		observation := testDetectedOAuthObservation()
		observation.CapabilitySet = "auth-only-evidence"
		return observation, nil
	}

	app.ensureCodexRuntimeCapability(context.Background())
	if !app.requestCodexOAuthProbe(context.Background(), false) {
		t.Fatal("probe request was not started")
	}
	profile := waitForCodexOAuthState(t, app, codexprofile.OAuthProbeStatusDetected)
	if profile.CapabilitySet != codexprofile.CodexProfileCapabilitySetV1 {
		t.Fatalf("OAuth descriptor capability = %q, want runtime preflight set", profile.CapabilitySet)
	}
}

func TestFailedCodexCapabilityPreflightBlocksAllProfileKinds(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	stateDir := filepath.Join(root, "state")
	cfg := config.DefaultAppConfig()
	record, err := config.PrepareCodexAPIProfileCreate(nil, config.CodexAPIProfileInput{
		Name: "API", BaseURL: "https://api.example/v1", APIKey: "secret", Model: "model", ReasoningEffort: "high",
	})
	if err != nil {
		t.Fatalf("PrepareCodexAPIProfileCreate: %v", err)
	}
	cfg.Codex.Profiles = []config.CodexAPIProfileRecord{record}
	if err := config.WriteAppConfig(configPath, cfg); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}

	app := New(":0", ":0", nil, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		CodexRealBinary: "fake-codex",
		ConfigPath:      configPath,
		Paths:           relayruntime.Paths{StateDir: stateDir},
	})
	app.ConfigureAdmin(AdminRuntimeOptions{ConfigPath: configPath})
	app.runCodexCapabilityPreflight = func(context.Context, codexprofile.CapabilityPreflightOptions) (codexprofile.CapabilityPreflightObservation, error) {
		return codexprofile.CapabilityPreflightObservation{}, &codexprofile.OAuthProbeError{Code: codexprofile.ErrorCodexCapabilityUnsupported}
	}
	app.runCodexOAuthProbe = func(context.Context, codexprofile.OAuthProbeOptions) (codexprofile.OAuthProbeObservation, error) {
		return testDetectedOAuthObservation(), nil
	}
	app.ensureCodexRuntimeCapability(context.Background())
	if !app.requestCodexOAuthProbe(context.Background(), false) {
		t.Fatal("OAuth probe request was not started")
	}
	waitForCodexOAuthState(t, app, codexprofile.OAuthProbeStatusDetected)

	for _, profileID := range []string{state.NativeCodexProfileID, record.ID, state.OAuthCodexProfileID} {
		_, _, err := app.applyCodexHeadlessProviderConfig(nil, []string{"app-server"}, agentproto.BackendCodex, profileID)
		if got := codexprofile.RuntimeErrorCode(err); got != codexprofile.ErrorCodexCapabilityUnsupported {
			t.Fatalf("profile %q error = %q, want %q (err=%v)", profileID, got, codexprofile.ErrorCodexCapabilityUnsupported, err)
		}
	}
}

func TestCurrentCapabilityFailureOverridesStaleOAuthDescriptor(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	if err := config.WriteAppConfig(configPath, config.DefaultAppConfig()); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}
	app := New(":0", ":0", nil, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		CodexRealBinary: "fake-codex",
		ConfigPath:      configPath,
		Paths:           relayruntime.Paths{StateDir: filepath.Join(root, "state")},
	})
	app.ConfigureAdmin(AdminRuntimeOptions{ConfigPath: configPath})
	app.runCodexOAuthProbe = func(context.Context, codexprofile.OAuthProbeOptions) (codexprofile.OAuthProbeObservation, error) {
		return testDetectedOAuthObservation(), nil
	}
	if !app.requestCodexOAuthProbe(context.Background(), false) {
		t.Fatal("OAuth probe request was not started")
	}
	waitForCodexOAuthState(t, app, codexprofile.OAuthProbeStatusDetected)

	app.mu.Lock()
	app.codexRuntimeCapability = codexRuntimeCapabilityState{
		checked:   true,
		errorCode: codexprofile.ErrorCodexCapabilityUnsupported,
	}
	app.mu.Unlock()
	_, _, err := app.applyCodexHeadlessProviderConfig(nil, []string{"app-server"}, agentproto.BackendCodex, state.OAuthCodexProfileID)
	if got := codexprofile.RuntimeErrorCode(err); got != codexprofile.ErrorCodexCapabilityUnsupported {
		t.Fatalf("error code = %q, want %q (err=%v)", got, codexprofile.ErrorCodexCapabilityUnsupported, err)
	}
}

func TestOAuthProfileLaunchRunsFreshAuthProbeBeforeResolving(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	if err := config.WriteAppConfig(configPath, config.DefaultAppConfig()); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}
	app := New(":0", ":0", nil, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		CodexRealBinary: "fake-codex",
		ConfigPath:      configPath,
		Paths:           relayruntime.Paths{StateDir: filepath.Join(root, "state")},
	})
	app.ConfigureAdmin(AdminRuntimeOptions{ConfigPath: configPath})
	app.runCodexOAuthProbe = func(context.Context, codexprofile.OAuthProbeOptions) (codexprofile.OAuthProbeObservation, error) {
		return testDetectedOAuthObservation(), nil
	}
	if !app.requestCodexOAuthProbe(context.Background(), false) {
		t.Fatal("initial OAuth probe request was not started")
	}
	waitForCodexOAuthState(t, app, codexprofile.OAuthProbeStatusDetected)

	var calls atomic.Int32
	app.runCodexOAuthProbe = func(context.Context, codexprofile.OAuthProbeOptions) (codexprofile.OAuthProbeObservation, error) {
		calls.Add(1)
		return codexprofile.OAuthProbeObservation{
			Result:        codexprofile.OAuthProbeResult{Status: codexprofile.OAuthProbeStatusMissing},
			CapabilitySet: codexprofile.OAuthProbeCapabilitySetV1,
		}, nil
	}

	_, _, err := app.applyCodexHeadlessProviderConfig(nil, []string{"app-server"}, agentproto.BackendCodex, state.OAuthCodexProfileID)
	if got := codexprofile.RuntimeErrorCode(err); got != codexprofile.ErrorOAuthMissing {
		t.Fatalf("launch error = %q, want %q (err=%v)", got, codexprofile.ErrorOAuthMissing, err)
	}
	if calls.Load() != 1 {
		t.Fatalf("fresh launch probe calls = %d, want 1", calls.Load())
	}
}

func TestPersistedOAuthDescriptorRequiresCurrentLifecycleProbe(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	stateDir := filepath.Join(root, "state")
	if err := config.WriteAppConfig(configPath, config.DefaultAppConfig()); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}
	first := New(":0", ":0", nil, agentproto.ServerIdentity{PID: 101, StartedAt: time.Unix(100, 0)})
	first.SetHeadlessRuntime(HeadlessRuntimeConfig{
		CodexRealBinary: "fake-codex", ConfigPath: configPath, Paths: relayruntime.Paths{StateDir: stateDir},
	})
	first.ConfigureAdmin(AdminRuntimeOptions{ConfigPath: configPath})
	first.runCodexOAuthProbe = func(context.Context, codexprofile.OAuthProbeOptions) (codexprofile.OAuthProbeObservation, error) {
		return testDetectedOAuthObservation(), nil
	}
	if !first.requestCodexOAuthProbe(context.Background(), false) {
		t.Fatal("first lifecycle OAuth probe was not started")
	}
	waitForCodexOAuthState(t, first, codexprofile.OAuthProbeStatusDetected)

	second := New(":0", ":0", nil, agentproto.ServerIdentity{PID: 102, StartedAt: time.Unix(200, 0)})
	second.SetHeadlessRuntime(HeadlessRuntimeConfig{
		CodexRealBinary: "fake-codex", ConfigPath: configPath, Paths: relayruntime.Paths{StateDir: stateDir},
	})
	second.ConfigureAdmin(AdminRuntimeOptions{ConfigPath: configPath})
	_, _, err := second.applyCodexHeadlessProviderConfig(nil, []string{"app-server"}, agentproto.BackendCodex, state.OAuthCodexProfileID)
	if got := codexprofile.RuntimeErrorCode(err); got != codexprofile.ErrorOAuthProbeUnknown {
		t.Fatalf("stale descriptor error = %q, want %q (err=%v)", got, codexprofile.ErrorOAuthProbeUnknown, err)
	}
}

func TestDetectedOAuthProfileLaunchUsesIsolatedBuiltInOpenAI(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	stateDir := filepath.Join(root, "state")
	if err := config.WriteAppConfig(configPath, config.DefaultAppConfig()); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}
	app := New(":0", ":0", nil, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		BinaryPath:      "/tmp/codex-remote",
		CodexRealBinary: "fake-codex",
		ConfigPath:      configPath,
		BaseEnv: []string{
			"HOME=/tmp/test",
			"OPENAI_API_KEY=global-secret",
			"CODEX_ACCESS_TOKEN=external-token",
			"CUSTOM_API_KEY=native-secret",
			codexprofile.CodexProfileAPIKeyEnv + "=other-profile-secret",
		},
		LaunchArgs: []string{"app-server"},
		Paths:      relayruntime.Paths{StateDir: stateDir, LogsDir: filepath.Join(root, "logs")},
	})
	app.ConfigureAdmin(AdminRuntimeOptions{ConfigPath: configPath})
	app.runCodexNativeConfigProbe = func(context.Context, codexprofile.NativeConfigProbeOptions) (codexprofile.NativeConfigObservation, error) {
		return codexprofile.NativeConfigObservation{ProviderEnvKeys: []string{"CUSTOM_API_KEY"}}, nil
	}
	app.ensureCodexNativeConnectionEvidence(context.Background())
	app.runCodexOAuthProbe = func(context.Context, codexprofile.OAuthProbeOptions) (codexprofile.OAuthProbeObservation, error) {
		return testDetectedOAuthObservation(), nil
	}
	if !app.requestCodexOAuthProbe(context.Background(), false) {
		t.Fatal("probe request was not started")
	}
	profile := waitForCodexOAuthState(t, app, codexprofile.OAuthProbeStatusDetected)
	if profile.Revision == 0 {
		t.Fatalf("detected profile missing revision: %#v", profile)
	}

	var captured relayruntime.HeadlessLaunchOptions
	app.startHeadless = func(options relayruntime.HeadlessLaunchOptions) (int, error) {
		captured = options
		return 4321, nil
	}
	app.startManagedHeadless(control.DaemonCommand{
		Kind: control.DaemonCommandStartHeadless, InstanceID: "inst-oauth", ThreadCWD: root,
		Backend: agentproto.BackendCodex, CodexProviderID: state.OAuthCodexProfileID,
	})
	if captured.BinaryPath == "" {
		t.Fatal("detected OAuth profile did not reach launcher")
	}
	args := strings.Join(captured.Args, "\n")
	if !strings.Contains(args, `model_provider="openai"`) || !strings.Contains(args, `openai_base_url=""`) {
		t.Fatalf("unexpected OAuth launch args: %#v", captured.Args)
	}
	for _, key := range codexprofile.ConflictingCodexAuthEnvKeys() {
		if containsEnvEntry(captured.Env, key+"=") {
			t.Fatalf("OAuth launch retained conflicting env %s: %#v", key, captured.Env)
		}
	}
	if containsEnvEntry(captured.Env, "CUSTOM_API_KEY=native-secret") {
		t.Fatalf("OAuth launch retained native provider env key: %#v", captured.Env)
	}
}

func TestOAuthProfileFreshProbeDoesNotLaunchAfterPendingCancel(t *testing.T) {
	app, command := newOAuthLaunchAuthorizationTestApp(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	app.runCodexOAuthProbe = func(context.Context, codexprofile.OAuthProbeOptions) (codexprofile.OAuthProbeObservation, error) {
		close(entered)
		<-release
		return testDetectedOAuthObservation(), nil
	}
	var launches atomic.Int32
	app.startHeadless = func(relayruntime.HeadlessLaunchOptions) (int, error) {
		launches.Add(1)
		return 4321, nil
	}

	done := make(chan []eventcontract.Event, 1)
	go func() {
		done <- app.startManagedHeadless(command)
	}()
	<-entered
	app.mu.Lock()
	if surface := app.service.Surface(command.SurfaceSessionID); surface != nil {
		surface.PendingHeadless = nil
	}
	app.mu.Unlock()
	close(release)
	events := <-done

	if launches.Load() != 0 {
		t.Fatalf("fresh probe launched after pending cancellation")
	}
	if len(events) != 0 {
		t.Fatalf("expected canceled launch to stay silent, got %#v", events)
	}
}

func TestOAuthProfileLaunchDoesNotProbeWhenPendingAlreadyCanceled(t *testing.T) {
	app, command := newOAuthLaunchAuthorizationTestApp(t)
	app.mu.Lock()
	if surface := app.service.Surface(command.SurfaceSessionID); surface != nil {
		surface.PendingHeadless = nil
	}
	app.mu.Unlock()
	var probeCalls atomic.Int32
	var launches atomic.Int32
	app.runCodexOAuthProbe = func(context.Context, codexprofile.OAuthProbeOptions) (codexprofile.OAuthProbeObservation, error) {
		probeCalls.Add(1)
		return testDetectedOAuthObservation(), nil
	}
	app.startHeadless = func(relayruntime.HeadlessLaunchOptions) (int, error) {
		launches.Add(1)
		return 4321, nil
	}

	events := app.startManagedHeadless(command)

	if probeCalls.Load() != 0 {
		t.Fatalf("canceled pending launch still ran OAuth probe")
	}
	if launches.Load() != 0 {
		t.Fatalf("canceled pending launch reached headless launcher")
	}
	if len(events) != 0 {
		t.Fatalf("expected stale start command to stay silent, got %#v", events)
	}
}

func TestOAuthProfileFreshProbeDoesNotLaunchAfterShutdown(t *testing.T) {
	app, command := newOAuthLaunchAuthorizationTestApp(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	app.runCodexOAuthProbe = func(context.Context, codexprofile.OAuthProbeOptions) (codexprofile.OAuthProbeObservation, error) {
		close(entered)
		<-release
		return testDetectedOAuthObservation(), nil
	}
	var launches atomic.Int32
	app.startHeadless = func(relayruntime.HeadlessLaunchOptions) (int, error) {
		launches.Add(1)
		return 4321, nil
	}

	done := make(chan []eventcontract.Event, 1)
	go func() {
		done <- app.startManagedHeadless(command)
	}()
	<-entered
	_ = app.beginShutdownNotices()
	close(release)
	events := <-done

	if launches.Load() != 0 {
		t.Fatalf("fresh probe launched after shutdown")
	}
	if len(events) != 0 {
		t.Fatalf("expected shutdown launch abort to stay silent, got %#v", events)
	}
}

func TestOAuthProfileFreshProbeDoesNotStartDuplicateLaunches(t *testing.T) {
	app, command := newOAuthLaunchAuthorizationTestApp(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	var probeCalls atomic.Int32
	app.runCodexOAuthProbe = func(context.Context, codexprofile.OAuthProbeOptions) (codexprofile.OAuthProbeObservation, error) {
		if probeCalls.Add(1) == 1 {
			close(entered)
		}
		<-release
		return testDetectedOAuthObservation(), nil
	}
	var launches atomic.Int32
	app.startHeadless = func(relayruntime.HeadlessLaunchOptions) (int, error) {
		launches.Add(1)
		return 4321, nil
	}

	firstDone := make(chan []eventcontract.Event, 1)
	secondDone := make(chan []eventcontract.Event, 1)
	go func() {
		firstDone <- app.startManagedHeadless(command)
	}()
	<-entered
	go func() {
		secondDone <- app.startManagedHeadless(command)
	}()
	time.Sleep(20 * time.Millisecond)
	close(release)
	<-firstDone
	<-secondDone

	if probeCalls.Load() != 1 {
		t.Fatalf("duplicate launches should share one fresh probe, calls=%d", probeCalls.Load())
	}
	if launches.Load() != 1 {
		t.Fatalf("expected exactly one process launch, got %d", launches.Load())
	}
}

func TestOAuthProfileCapabilityProbeErrorReachesLaunchFailure(t *testing.T) {
	app, command := newOAuthLaunchAuthorizationTestApp(t)
	app.runCodexOAuthProbe = func(context.Context, codexprofile.OAuthProbeOptions) (codexprofile.OAuthProbeObservation, error) {
		return codexprofile.OAuthProbeObservation{}, &codexprofile.OAuthProbeError{
			Code:  codexprofile.ErrorCodexCapabilityUnsupported,
			Stage: "get_auth_status",
		}
	}
	var launches atomic.Int32
	app.startHeadless = func(relayruntime.HeadlessLaunchOptions) (int, error) {
		launches.Add(1)
		return 4321, nil
	}

	events := app.startManagedHeadless(command)

	if launches.Load() != 0 {
		t.Fatalf("capability probe error reached headless launcher")
	}
	if len(events) == 0 || events[0].Notice == nil || events[0].Notice.Code != codexprofile.ErrorCodexCapabilityUnsupported {
		t.Fatalf("expected capability launch failure notice, got %#v", events)
	}
}

func testDetectedOAuthObservation() codexprofile.OAuthProbeObservation {
	return codexprofile.OAuthProbeObservation{
		Result: codexprofile.OAuthProbeResult{
			Status:      codexprofile.OAuthProbeStatusDetected,
			AccountHint: "u***@example.com",
			PlanType:    "plus",
		},
		CapabilitySet: codexprofile.OAuthProbeCapabilitySetV1,
	}
}

func newOAuthLaunchAuthorizationTestApp(t *testing.T) (*App, control.DaemonCommand) {
	t.Helper()
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	stateDir := filepath.Join(root, "state")
	if err := config.WriteAppConfig(configPath, config.DefaultAppConfig()); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}
	app := New(":0", ":0", nil, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		BinaryPath:      "/tmp/codex-remote",
		CodexRealBinary: "fake-codex",
		ConfigPath:      configPath,
		BaseEnv:         []string{"HOME=/tmp/test"},
		LaunchArgs:      []string{"app-server"},
		Paths:           relayruntime.Paths{StateDir: stateDir, LogsDir: filepath.Join(root, "logs")},
	})
	app.ConfigureAdmin(AdminRuntimeOptions{ConfigPath: configPath})
	app.runCodexNativeConfigProbe = func(context.Context, codexprofile.NativeConfigProbeOptions) (codexprofile.NativeConfigObservation, error) {
		return codexprofile.NativeConfigObservation{}, nil
	}
	app.ensureCodexNativeConnectionEvidence(context.Background())
	app.service.MaterializeSurfaceResumeWithCodexProvider("surface-oauth", root, "chat-1", "user-1", "normal", agentproto.BackendCodex, state.OAuthCodexProfileID, "", "", "")
	surface := app.service.Surface("surface-oauth")
	if surface == nil {
		t.Fatal("expected surface")
	}
	surface.PendingHeadless = &state.HeadlessLaunchRecord{
		InstanceID:      "inst-oauth",
		WorkspaceKey:    root,
		ThreadCWD:       root,
		Backend:         agentproto.BackendCodex,
		CodexProviderID: state.OAuthCodexProfileID,
		Status:          state.HeadlessLaunchStarting,
		Purpose:         state.HeadlessLaunchPurposeThreadRestore,
	}
	return app, control.DaemonCommand{
		Kind:             control.DaemonCommandStartHeadless,
		SurfaceSessionID: "surface-oauth",
		InstanceID:       "inst-oauth",
		WorkspaceKey:     root,
		ThreadCWD:        root,
		Backend:          agentproto.BackendCodex,
		CodexProviderID:  state.OAuthCodexProfileID,
	}
}

func findServiceCodexProfile(app *App, profileID string) (state.CodexProfileSummary, bool) {
	for _, profile := range app.service.CodexProfiles() {
		if profile.ID == profileID {
			return profile, true
		}
	}
	return state.CodexProfileSummary{}, false
}

func waitForCodexOAuthState(t *testing.T, app *App, want codexprofile.OAuthProbeStatus) state.CodexOAuthProfileState {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		app.mu.Lock()
		profile, ok := app.codexOAuthProfileState.current()
		app.mu.Unlock()
		if ok && profile.Status == string(want) {
			return profile
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for Codex OAuth status %q", want)
	return state.CodexOAuthProfileState{}
}

func waitForCodexOAuthProbeSettled(t *testing.T, app *App) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		app.mu.Lock()
		inFlight := app.codexOAuthProfileState.probeInFlight
		app.mu.Unlock()
		if !inFlight {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for Codex OAuth probe to settle")
}

func assertCodexOAuthLifecycleNotAuthorized(t *testing.T, app *App) {
	t.Helper()
	app.mu.Lock()
	defer app.mu.Unlock()
	if app.codexOAuthProfileState.err == nil {
		t.Fatal("probe failure did not retain a runtime error")
	}
	if app.codexOAuthProfileState.probeCompleted {
		t.Fatal("failed probe path authorized the current lifecycle")
	}
	if _, ok := app.codexOAuthProfileState.current(); ok {
		t.Fatal("failed probe path exposed an OAuth descriptor to the resolver")
	}
}
