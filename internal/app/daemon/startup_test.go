package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/config"
)

func TestBuildStartupAccessPlanUsesSSHSetupExposure(t *testing.T) {
	cfg := config.DefaultAppConfig()
	currentBinary, realBinary := seedStartupPlanBinaries(t)
	cfg.Wrapper.CodexRealBinary = realBinary
	services := config.ServicesConfig{
		RelayHost:    "127.0.0.1",
		RelayPort:    "9500",
		RelayAPIHost: "127.0.0.1",
		RelayAPIPort: "9501",
	}
	plan := buildStartupAccessPlan(config.LoadedAppConfig{Config: cfg}, services, currentBinary, map[string]string{
		"SSH_CONNECTION": "198.51.100.10 55000 10.0.0.8 22",
	})

	if !plan.SetupRequired {
		t.Fatal("expected setup required")
	}
	if !plan.SSHSession {
		t.Fatal("expected ssh session")
	}
	if plan.AdminBindHost != "0.0.0.0" {
		t.Fatalf("admin bind host = %q, want 0.0.0.0", plan.AdminBindHost)
	}
	if plan.AdminURL != "http://10.0.0.8:9501/admin/" {
		t.Fatalf("admin url = %q", plan.AdminURL)
	}
	if plan.SetupURL != "http://10.0.0.8:9501/setup" {
		t.Fatalf("setup url = %q", plan.SetupURL)
	}
}

func TestBuildStartupAccessPlanUsesLocalhostForLocalSetup(t *testing.T) {
	cfg := config.DefaultAppConfig()
	currentBinary, realBinary := seedStartupPlanBinaries(t)
	cfg.Wrapper.CodexRealBinary = realBinary
	services := config.ServicesConfig{
		RelayHost:    "127.0.0.1",
		RelayPort:    "9500",
		RelayAPIHost: "127.0.0.1",
		RelayAPIPort: "9501",
	}
	plan := buildStartupAccessPlan(config.LoadedAppConfig{Config: cfg}, services, currentBinary, map[string]string{
		"DISPLAY": ":0",
	})

	if !plan.SetupRequired {
		t.Fatal("expected setup required")
	}
	if plan.SSHSession {
		t.Fatal("did not expect ssh session")
	}
	if plan.AdminURL != "http://localhost:9501/admin/" {
		t.Fatalf("admin url = %q", plan.AdminURL)
	}
	if plan.SetupURL != "http://localhost:9501/setup" {
		t.Fatalf("setup url = %q", plan.SetupURL)
	}
}

func TestBuildStartupAccessPlanTreatsVerifiedAppAndRecordedDecisionsAsConfigured(t *testing.T) {
	cfg := config.DefaultAppConfig()
	currentBinary, realBinary := seedStartupPlanBinaries(t)
	cfg.Wrapper.CodexRealBinary = realBinary
	now := time.Now().UTC()
	cfg.Feishu.Apps = []config.FeishuAppConfig{{
		ID:         "main",
		Name:       "Main",
		AppID:      "cli_xxx",
		AppSecret:  "secret_xxx",
		VerifiedAt: &now,
	}}
	cfg.Admin.Onboarding.AutostartDecision = &config.OnboardingDecision{Value: onboardingDecisionDeferred, DecidedAt: &now}
	cfg.Admin.Onboarding.VSCodeDecision = &config.OnboardingDecision{Value: onboardingDecisionVSCodeRemoteOnly, DecidedAt: &now}
	services := config.ServicesConfig{
		RelayHost:    "127.0.0.1",
		RelayPort:    "9500",
		RelayAPIHost: "127.0.0.1",
		RelayAPIPort: "9501",
	}
	plan := buildStartupAccessPlan(config.LoadedAppConfig{Config: cfg}, services, currentBinary, map[string]string{})

	if plan.SetupRequired {
		t.Fatal("did not expect setup required")
	}
	if plan.ConfiguredAppCount != 1 {
		t.Fatalf("configured app count = %d, want 1", plan.ConfiguredAppCount)
	}
	if plan.AdminBindHost != "127.0.0.1" {
		t.Fatalf("admin bind host = %q, want 127.0.0.1", plan.AdminBindHost)
	}
}

func TestBuildStartupAccessPlanTreatsVerifiedLegacyAppAsConfiguredWithoutMachineDecisions(t *testing.T) {
	cfg := config.DefaultAppConfig()
	currentBinary, realBinary := seedStartupPlanBinaries(t)
	cfg.Wrapper.CodexRealBinary = realBinary
	now := time.Now().UTC()
	cfg.Feishu.Apps = []config.FeishuAppConfig{{
		ID:         "main",
		Name:       "Main",
		AppID:      "cli_xxx",
		AppSecret:  "secret_xxx",
		VerifiedAt: &now,
	}}
	services := config.ServicesConfig{
		RelayHost:    "127.0.0.1",
		RelayPort:    "9500",
		RelayAPIHost: "127.0.0.1",
		RelayAPIPort: "9501",
	}
	plan := buildStartupAccessPlan(config.LoadedAppConfig{Config: cfg}, services, currentBinary, map[string]string{})

	if plan.SetupRequired {
		t.Fatal("did not expect setup required for legacy verified config")
	}
}

func TestBuildStartupAccessPlanTreatsClaudeOnlyRuntimeAsConfigured(t *testing.T) {
	cfg := config.DefaultAppConfig()
	currentBinary, _ := seedStartupPlanBinaries(t)
	claudePath := filepath.Join(t.TempDir(), executableName("claude"))
	if err := os.WriteFile(claudePath, []byte("claude"), 0o755); err != nil {
		t.Fatalf("write claude binary: %v", err)
	}
	t.Setenv(config.ClaudeBinaryEnv, claudePath)

	now := time.Now().UTC()
	cfg.Feishu.Apps = []config.FeishuAppConfig{{
		ID:         "main",
		Name:       "Main",
		AppID:      "cli_xxx",
		AppSecret:  "secret_xxx",
		VerifiedAt: &now,
	}}
	cfg.Admin.Onboarding.AutostartDecision = &config.OnboardingDecision{Value: onboardingDecisionDeferred, DecidedAt: &now}
	cfg.Admin.Onboarding.VSCodeDecision = &config.OnboardingDecision{Value: onboardingDecisionVSCodeRemoteOnly, DecidedAt: &now}
	cfg.Wrapper.CodexRealBinary = ""

	services := config.ServicesConfig{
		RelayHost:    "127.0.0.1",
		RelayPort:    "9500",
		RelayAPIHost: "127.0.0.1",
		RelayAPIPort: "9501",
	}
	plan := buildStartupAccessPlan(config.LoadedAppConfig{Config: cfg}, services, currentBinary, map[string]string{})

	if plan.SetupRequired {
		t.Fatal("did not expect setup required when Claude runtime is available")
	}
}

func seedStartupPlanBinaries(t *testing.T) (string, string) {
	t.Helper()

	dir := t.TempDir()
	currentBinary := filepath.Join(dir, executableName("codex-remote"))
	if err := os.WriteFile(currentBinary, []byte("wrapper"), 0o755); err != nil {
		t.Fatalf("write current binary: %v", err)
	}
	realBinary := filepath.Join(dir, executableName("codex-real"))
	if err := os.WriteFile(realBinary, []byte("real"), 0o755); err != nil {
		t.Fatalf("write real binary: %v", err)
	}
	return currentBinary, realBinary
}

func TestMaybeOpenSetupBrowserHonorsModeAndFlag(t *testing.T) {
	original := browserOpener
	defer func() { browserOpener = original }()

	called := 0
	browserOpener = func(url string, env map[string]string) error {
		called++
		if url != "http://localhost:9501/setup" {
			t.Fatalf("browser url = %q", url)
		}
		return nil
	}

	err := maybeOpenSetupBrowser(startupAccessPlan{
		SetupRequired:   true,
		AutoOpenBrowser: true,
		SetupURL:        "http://localhost:9501/setup",
	}, map[string]string{})
	if err != nil {
		t.Fatalf("maybeOpenSetupBrowser: %v", err)
	}
	if called != 1 {
		t.Fatalf("browser called = %d, want 1", called)
	}

	if err := maybeOpenSetupBrowser(startupAccessPlan{
		SetupRequired:   true,
		AutoOpenBrowser: false,
		SetupURL:        "http://localhost:9501/setup",
	}, map[string]string{}); err != nil {
		t.Fatalf("maybeOpenSetupBrowser(disabled): %v", err)
	}
	if called != 1 {
		t.Fatalf("browser called after disabled = %d, want 1", called)
	}

	browserOpener = func(string, map[string]string) error {
		return errors.New("unexpected")
	}
	if err := maybeOpenSetupBrowser(startupAccessPlan{
		SetupRequired:   true,
		AutoOpenBrowser: true,
		SSHSession:      true,
		SetupURL:        "http://localhost:9501/setup",
	}, map[string]string{}); err != nil {
		t.Fatalf("maybeOpenSetupBrowser(ssh): %v", err)
	}
}
