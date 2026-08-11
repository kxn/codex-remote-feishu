package daemon

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/codexprofile"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func TestCodexNativeConfigProbeRunsOnceAndReservesConfiguredProviderIDs(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	record, err := config.PrepareCodexAPIProfileCreate(nil, config.CodexAPIProfileInput{
		Name: "API", BaseURL: "https://api.example/v1", APIKey: "secret", Model: "model", ReasoningEffort: "high",
	})
	if err != nil {
		t.Fatalf("PrepareCodexAPIProfileCreate: %v", err)
	}
	cfg := config.DefaultAppConfig()
	cfg.Codex.Profiles = []config.CodexAPIProfileRecord{record}
	if err := config.WriteAppConfig(configPath, cfg); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}
	current, ok := config.CurrentCodexAPIProfile(record)
	if !ok {
		t.Fatal("current API Profile missing")
	}
	baseline, err := (codexprofile.RuntimeResolver{
		APIProfiles: cfg.Codex.Profiles,
		Preference: func(ref state.CodexContextPreferenceRef) (state.ProfileContextPreference, bool) {
			return state.ProfileContextPreference{ProfileID: ref.ProfileID, Revision: ref.Revision, Mode: state.CodexContextModeDefault}, true
		},
		CapabilitySet: codexprofile.CodexProfileCapabilitySetV1,
	}).Resolve(state.CodexAdmissionRef{
		ProfileRef:           state.CodexProfileRef{ID: record.ID, Revision: current.Revision},
		ContextPreferenceRef: state.CodexContextPreferenceRef{ProfileID: record.ID, Revision: 1},
	})
	if err != nil {
		t.Fatalf("baseline Resolve: %v", err)
	}
	collidingProviderID := baseline.Connection.ModelProviderID

	app := New(":0", ":0", nil, agentproto.ServerIdentity{PID: 101, StartedAt: time.Unix(100, 0)})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		CodexRealBinary: "/tmp/codex",
		ConfigPath:      configPath,
		BaseEnv:         []string{"PATH=/usr/bin", "OPENAI_API_KEY=native-key"},
		Paths:           relayruntime.Paths{StateDir: filepath.Join(root, "state")},
	})
	app.ConfigureAdmin(AdminRuntimeOptions{ConfigPath: configPath})
	var calls atomic.Int32
	app.runCodexNativeConfigProbe = func(context.Context, codexprofile.NativeConfigProbeOptions) (codexprofile.NativeConfigObservation, error) {
		calls.Add(1)
		return codexprofile.NativeConfigObservation{
			ModelProviderID: "native-provider",
			ModelEndpoint:   "https://native.example/v1",
			ProviderIDs:     []string{collidingProviderID},
		}, nil
	}

	app.ensureCodexNativeConnectionEvidence(context.Background())
	app.ensureCodexNativeConnectionEvidence(context.Background())
	if calls.Load() != 1 {
		t.Fatalf("native config probe calls = %d, want 1", calls.Load())
	}
	_, args, err := app.applyCodexHeadlessProfileConfig(nil, []string{"app-server"}, agentproto.BackendCodex, record.ID)
	if err != nil {
		t.Fatalf("applyCodexHeadlessProfileConfig: %v", err)
	}
	if strings.Contains(strings.Join(args, "\n"), `model_provider="`+collidingProviderID+`"`) {
		t.Fatalf("API launch reused native provider ID %q: %#v", collidingProviderID, args)
	}
	if app.codexNativeConnection.evidence.ModelEndpointID != "https://native.example/v1" {
		t.Fatalf("native evidence = %#v", app.codexNativeConnection.evidence)
	}
}

func TestCodexNativeConfigProbeFailureUsesConservativeLifecycleEvidence(t *testing.T) {
	newApp := func(pid int) *App {
		app := New(":0", ":0", nil, agentproto.ServerIdentity{PID: pid, StartedAt: time.Unix(int64(pid), 0)})
		app.SetHeadlessRuntime(HeadlessRuntimeConfig{
			CodexRealBinary: "/tmp/codex",
			BaseEnv:         []string{"OPENAI_API_KEY=native-key"},
			Paths:           relayruntime.Paths{StateDir: t.TempDir()},
		})
		app.runCodexNativeConfigProbe = func(context.Context, codexprofile.NativeConfigProbeOptions) (codexprofile.NativeConfigObservation, error) {
			return codexprofile.NativeConfigObservation{}, errors.New("probe failed")
		}
		app.ensureCodexNativeConnectionEvidence(context.Background())
		return app
	}
	first := newApp(101)
	second := newApp(102)
	firstGeneration := first.codexNativeConnection.evidence.ConnectionGeneration
	secondGeneration := second.codexNativeConnection.evidence.ConnectionGeneration
	if firstGeneration == 0 || secondGeneration == 0 || firstGeneration == secondGeneration {
		t.Fatalf("lifecycle generations = %d, %d", firstGeneration, secondGeneration)
	}

	env, args, err := first.applyCodexHeadlessProfileConfig(
		[]string{"OPENAI_API_KEY=native-key"},
		[]string{"app-server"},
		agentproto.BackendCodex,
		config.CodexNativeProfileID,
	)
	if err != nil {
		t.Fatalf("native launch after probe failure: %v", err)
	}
	if !containsEnvEntry(env, "OPENAI_API_KEY=native-key") || strings.Join(args, "\n") != "app-server" {
		t.Fatalf("native launch was modified after probe failure: env=%#v args=%#v", env, args)
	}
}

func TestCodexNativeConfigProbeFailureDoesNotBlockAPIProfileLaunch(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	stateDir := filepath.Join(root, "state")
	record, err := config.PrepareCodexAPIProfileCreate(nil, config.CodexAPIProfileInput{
		Name: "API", BaseURL: "https://api.example/v1", APIKey: "secret", Model: "model", ReasoningEffort: "high",
	})
	if err != nil {
		t.Fatalf("PrepareCodexAPIProfileCreate: %v", err)
	}
	cfg := config.DefaultAppConfig()
	cfg.Codex.Profiles = []config.CodexAPIProfileRecord{record}
	if err := config.WriteAppConfig(configPath, cfg); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}
	app := New(":0", ":0", nil, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		CodexRealBinary: "/tmp/codex",
		ConfigPath:      configPath,
		BaseEnv:         []string{"CUSTOM_API_KEY=native-secret"},
		Paths:           relayruntime.Paths{StateDir: stateDir},
	})
	app.ConfigureAdmin(AdminRuntimeOptions{ConfigPath: configPath})
	app.runCodexNativeConfigProbe = func(context.Context, codexprofile.NativeConfigProbeOptions) (codexprofile.NativeConfigObservation, error) {
		return codexprofile.NativeConfigObservation{}, errors.New("native config unavailable")
	}

	app.ensureCodexNativeConnectionEvidence(context.Background())
	env, args, err := app.applyCodexHeadlessProfileConfig(
		[]string{"CUSTOM_API_KEY=native-secret"},
		[]string{"app-server"},
		agentproto.BackendCodex,
		record.ID,
	)
	if err != nil {
		t.Fatalf("API launch blocked by native probe failure: %v", err)
	}
	if !strings.Contains(strings.Join(args, "\n"), `model_provider="codex_remote_profile_`) {
		t.Fatalf("API launch material missing isolated provider: args=%#v", args)
	}
	if !containsEnvEntry(env, "CUSTOM_API_KEY=native-secret") {
		t.Fatalf("expected native provider env key to be preserved without probe evidence: %#v", env)
	}
}

func TestCodexNativeProbeRetriesAfterFailureTTL(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	cfg := config.DefaultAppConfig()
	if err := config.WriteAppConfig(configPath, cfg); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}
	app := New(":0", ":0", nil, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		CodexRealBinary: "/tmp/codex",
		ConfigPath:      configPath,
		BaseEnv:         []string{"HOME=/tmp/test"},
		Paths:           relayruntime.Paths{StateDir: filepath.Join(root, "state")},
	})
	app.ConfigureAdmin(AdminRuntimeOptions{ConfigPath: configPath})
	calls := 0
	app.runCodexNativeConfigProbe = func(context.Context, codexprofile.NativeConfigProbeOptions) (codexprofile.NativeConfigObservation, error) {
		calls++
		if calls == 1 {
			return codexprofile.NativeConfigObservation{}, errors.New("native config unavailable")
		}
		return codexprofile.NativeConfigObservation{ProviderEnvKeys: []string{"CUSTOM_API_KEY"}}, nil
	}

	app.ensureCodexNativeConnectionEvidence(context.Background())
	if _, _, _, failed := app.effectiveCodexNativeConnectionLocked(); !failed {
		t.Fatal("expected first native probe failure to be recorded")
	}
	if calls != 1 {
		t.Fatalf("expected one probe attempt, got %d", calls)
	}

	app.mu.Lock()
	app.codexNativeConnection.failedAt = time.Now().Add(-codexProbeRetryInterval - time.Second)
	app.mu.Unlock()
	app.maybeRetryCodexNativeProbeIfDue(context.Background())

	if _, _, _, failed := app.effectiveCodexNativeConnectionLocked(); failed {
		t.Fatal("expected retry to clear native probe failure")
	}
	if calls != 2 {
		t.Fatalf("expected retry after TTL, calls=%d", calls)
	}
}
