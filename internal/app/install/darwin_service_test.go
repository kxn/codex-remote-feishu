package install

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func withDarwinGOOS(t *testing.T) func() {
	originalGOOS := serviceRuntimeGOOS
	serviceRuntimeGOOS = "darwin"
	return func() { serviceRuntimeGOOS = originalGOOS }
}

func withMockLaunchctl(t *testing.T, fn func(ctx context.Context, args ...string) (string, error)) func() {
	originalRunner := launchctlUserRunner
	launchctlUserRunner = fn
	return func() { launchctlUserRunner = originalRunner }
}

func TestParseServiceManagerRejectsLaunchdUserOutsideDarwin(t *testing.T) {
	tests := []string{"linux", "windows"}
	for _, goos := range tests {
		t.Run(goos, func(t *testing.T) {
			_, err := ParseServiceManager("launchd_user", goos)
			if err == nil {
				t.Fatalf("expected error for GOOS=%s", goos)
			}
			if !strings.Contains(err.Error(), "only supported on darwin") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestParseServiceManagerAcceptsLaunchdUserOnDarwin(t *testing.T) {
	mgr, err := ParseServiceManager("launchd_user", "darwin")
	if err != nil {
		t.Fatalf("ParseServiceManager: %v", err)
	}
	if mgr != ServiceManagerLaunchdUser {
		t.Fatalf("got %q, want %q", mgr, ServiceManagerLaunchdUser)
	}
}

func TestLaunchdLabelForInstance(t *testing.T) {
	tests := []struct {
		instanceID string
		want       string
	}{
		{"", "com.codex-remote.service"},
		{"stable", "com.codex-remote.service"},
		{"debug", "com.codex-remote-debug.service"},
	}
	for _, tc := range tests {
		got := launchdLabelForInstance(tc.instanceID)
		if got != tc.want {
			t.Fatalf("launchdLabelForInstance(%q) = %q, want %q", tc.instanceID, got, tc.want)
		}
	}
}

func TestApplyStateMetadataInfersDarwinLaunchdPaths(t *testing.T) {
	defer withDarwinGOOS(t)()
	baseDir := t.TempDir()
	stubServiceUserHome(t, baseDir)

	state := InstallState{
		InstanceID:     "stable",
		BaseDir:        baseDir,
		StatePath:      defaultInstallStatePath(baseDir),
		ServiceManager: ServiceManagerLaunchdUser,
	}
	ApplyStateMetadata(&state, StateMetadataOptions{
		InstanceID:     state.InstanceID,
		StatePath:      state.StatePath,
		BaseDir:        state.BaseDir,
		ServiceManager: ServiceManagerLaunchdUser,
	})

	if state.ServiceUnitPath != filepath.Join(baseDir, "Library", "LaunchAgents", "com.codex-remote.service.plist") {
		t.Fatalf("ServiceUnitPath = %q, want LaunchAgents plist path", state.ServiceUnitPath)
	}
}

func TestApplyStateMetadataInfersDebugInstanceLaunchdPaths(t *testing.T) {
	defer withDarwinGOOS(t)()
	baseDir := t.TempDir()
	stubServiceUserHome(t, baseDir)

	state := InstallState{
		InstanceID:     "debug",
		BaseDir:        baseDir,
		StatePath:      defaultInstallStatePathForInstance(baseDir, "debug"),
		ServiceManager: ServiceManagerLaunchdUser,
	}
	ApplyStateMetadata(&state, StateMetadataOptions{
		InstanceID:     state.InstanceID,
		StatePath:      state.StatePath,
		BaseDir:        state.BaseDir,
		ServiceManager: ServiceManagerLaunchdUser,
	})

	want := filepath.Join(baseDir, "Library", "LaunchAgents", "com.codex-remote-debug.service.plist")
	if state.ServiceUnitPath != want {
		t.Fatalf("ServiceUnitPath = %q, want %q", state.ServiceUnitPath, want)
	}
}

func TestRenderLaunchdUserPlistContainsKeyElements(t *testing.T) {
	defer withDarwinGOOS(t)()
	baseDir := t.TempDir()
	stubServiceUserHome(t, baseDir)
	binaryPath := seedBinary(t, filepath.Join(baseDir, "bin", "codex-remote"), "binary")
	t.Setenv("PATH", "/usr/bin:/bin")

	state := InstallState{
		InstanceID:      "stable",
		BaseDir:         baseDir,
		StatePath:       defaultInstallStatePath(baseDir),
		InstalledBinary: binaryPath,
	}
	ApplyStateMetadata(&state, StateMetadataOptions{
		InstanceID:     state.InstanceID,
		StatePath:      state.StatePath,
		BaseDir:        state.BaseDir,
		ServiceManager: ServiceManagerLaunchdUser,
	})

	plist, err := renderLaunchdUserPlist(state)
	if err != nil {
		t.Fatalf("renderLaunchdUserPlist: %v", err)
	}

	mustContain := []string{
		"<key>Label</key>",
		"<string>com.codex-remote.service</string>",
		"<key>ProgramArguments</key>",
		"<string>daemon</string>",
		"<key>KeepAlive</key>",
		"<key>SuccessfulExit</key>",
		"<false/>",
		"<key>RunAtLoad</key>",
		"<true/>",
		"<key>EnvironmentVariables</key>",
		"<key>XDG_CONFIG_HOME</key>",
		"<key>XDG_DATA_HOME</key>",
		"<key>XDG_STATE_HOME</key>",
		"<key>PATH</key>",
		"<key>StandardOutPath</key>",
		"<key>StandardErrorPath</key>",
	}
	for _, s := range mustContain {
		if !strings.Contains(plist, s) {
			t.Fatalf("plist missing expected key/text: %q\nplist:\n%s", s, plist)
		}
	}
}

func TestRenderLaunchdUserPlistEscapesXMLSpecialChars(t *testing.T) {
	defer withDarwinGOOS(t)()
	baseDir := t.TempDir()
	stubServiceUserHome(t, baseDir)
	binaryPath := seedBinary(t, filepath.Join(baseDir, "my & <path>", "codex-remote"), "binary")
	t.Setenv("PATH", "/usr/bin")

	state := InstallState{
		InstanceID:      "stable",
		BaseDir:         baseDir,
		StatePath:       defaultInstallStatePath(baseDir),
		InstalledBinary: binaryPath,
	}
	ApplyStateMetadata(&state, StateMetadataOptions{
		InstanceID:     state.InstanceID,
		StatePath:      state.StatePath,
		BaseDir:        state.BaseDir,
		ServiceManager: ServiceManagerLaunchdUser,
	})

	plist, err := renderLaunchdUserPlist(state)
	if err != nil {
		t.Fatalf("renderLaunchdUserPlist: %v", err)
	}

	if strings.Contains(plist, "& ") && !strings.Contains(plist, "&amp;") {
		t.Fatalf("plain & not escaped in plist: %s", plist)
	}
	if strings.Contains(plist, "my & <path>") {
		t.Fatalf("special chars not escaped: %s", plist)
	}
}

func TestRenderLaunchdUserPlistRejectsMissingBinary(t *testing.T) {
	defer withDarwinGOOS(t)()
	state := InstallState{
		InstanceID: "stable",
		BaseDir:    t.TempDir(),
	}
	_, err := renderLaunchdUserPlist(state)
	if err == nil {
		t.Fatal("expected error for missing binary path")
	}
	if !strings.Contains(err.Error(), "binary path") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInstallLaunchdUserPlistWritesPlistAndBootstraps(t *testing.T) {
	defer withDarwinGOOS(t)()
	baseDir := t.TempDir()
	stubServiceUserHome(t, baseDir)
	binaryPath := seedBinary(t, filepath.Join(baseDir, "bin", "codex-remote"), "binary")
	t.Setenv("PATH", "/usr/bin")

	state := InstallState{
		InstanceID:      "stable",
		BaseDir:         baseDir,
		StatePath:       defaultInstallStatePath(baseDir),
		InstalledBinary: binaryPath,
	}
	ApplyStateMetadata(&state, StateMetadataOptions{
		InstanceID:     state.InstanceID,
		StatePath:      state.StatePath,
		BaseDir:        state.BaseDir,
		ServiceManager: ServiceManagerLaunchdUser,
	})

	var calls []string
	defer withMockLaunchctl(t, func(_ context.Context, args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		return "", nil
	})()

	result, err := installLaunchdUserPlist(context.Background(), state)
	if err != nil {
		t.Fatalf("installLaunchdUserPlist: %v", err)
	}

	if result.ServiceUnitPath == "" {
		t.Fatal("expected ServiceUnitPath to be set")
	}

	// Verify plist file was written
	if _, err := os.Stat(result.ServiceUnitPath); err != nil {
		t.Fatalf("plist file not written: %v", err)
	}
	plistContent, err := os.ReadFile(result.ServiceUnitPath)
	if err != nil {
		t.Fatalf("read plist: %v", err)
	}
	if !strings.Contains(string(plistContent), "<key>Label</key>") {
		t.Fatalf("plist content missing Label key: %s", string(plistContent))
	}

	// Verify bootstrap was called
	joined := strings.Join(calls, "; ")
	if !strings.Contains(joined, "bootstrap") {
		t.Fatalf("expected bootstrap call, got: %v", calls)
	}
}

func TestUninstallLaunchdUserPlistBootsOutAndRemovesPlist(t *testing.T) {
	defer withDarwinGOOS(t)()
	baseDir := t.TempDir()
	stubServiceUserHome(t, baseDir)
	binaryPath := seedBinary(t, filepath.Join(baseDir, "bin", "codex-remote"), "binary")
	t.Setenv("PATH", "/usr/bin")

	state := InstallState{
		InstanceID:      "stable",
		BaseDir:         baseDir,
		StatePath:       defaultInstallStatePath(baseDir),
		InstalledBinary: binaryPath,
	}
	ApplyStateMetadata(&state, StateMetadataOptions{
		InstanceID:     state.InstanceID,
		StatePath:      state.StatePath,
		BaseDir:        state.BaseDir,
		ServiceManager: ServiceManagerLaunchdUser,
	})

	// Manually create plist file (no bootstrap via mock)
	plistPath := state.ServiceUnitPath
	if err := os.MkdirAll(filepath.Dir(plistPath), 0o755); err != nil {
		t.Fatalf("mkdir plist dir: %v", err)
	}
	if err := os.WriteFile(plistPath, []byte("<plist/>"), 0o644); err != nil {
		t.Fatalf("write plist: %v", err)
	}

	var uninstallCalls []string
	defer withMockLaunchctl(t, func(_ context.Context, args ...string) (string, error) {
		uninstallCalls = append(uninstallCalls, strings.Join(args, " "))
		return "", nil
	})()

	if err := uninstallLaunchdUserPlist(context.Background(), state); err != nil {
		t.Fatalf("uninstallLaunchdUserPlist: %v", err)
	}

	joined := strings.Join(uninstallCalls, "; ")
	if !strings.Contains(joined, "bootout") {
		t.Fatalf("expected bootout call, got: %v", uninstallCalls)
	}

	// Verify plist was removed
	if _, err := os.Stat(state.ServiceUnitPath); !os.IsNotExist(err) {
		t.Fatalf("plist should be removed, got stat error: %v", err)
	}
}

func TestRunServiceInstallUserDarwinWritesLaunchdPlist(t *testing.T) {
	defer withDarwinGOOS(t)()
	baseDir := t.TempDir()
	stubServiceUserHome(t, baseDir)
	binaryPath := seedBinary(t, filepath.Join(baseDir, "bin", "codex-remote"), "binary")
	t.Setenv("PATH", "/usr/bin")

	statePath := defaultInstallStatePath(baseDir)
	state := InstallState{
		BaseDir:         baseDir,
		ConfigPath:      filepath.Join(baseDir, ".config", "codex-remote", "config.json"),
		StatePath:       statePath,
		InstalledBinary: binaryPath,
	}
	ApplyStateMetadata(&state, StateMetadataOptions{StatePath: statePath})
	if err := WriteState(statePath, state); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	var calls []string
	defer withMockLaunchctl(t, func(_ context.Context, args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		return "", nil
	})()

	var stdout bytes.Buffer
	if err := RunService([]string{"install-user", "-state-path", statePath}, strings.NewReader(""), &stdout, &bytes.Buffer{}, "vtest"); err != nil {
		t.Fatalf("RunService install-user: %v", err)
	}

	updated, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if updated.ServiceManager != ServiceManagerLaunchdUser {
		t.Fatalf("ServiceManager = %q, want %q", updated.ServiceManager, ServiceManagerLaunchdUser)
	}
	wantPath := filepath.Join(baseDir, "Library", "LaunchAgents", "com.codex-remote.service.plist")
	if updated.ServiceUnitPath != wantPath {
		t.Fatalf("ServiceUnitPath = %q, want %q", updated.ServiceUnitPath, wantPath)
	}

	// Verify plist exists and contains label
	plistRaw, err := os.ReadFile(updated.ServiceUnitPath)
	if err != nil {
		t.Fatalf("read plist: %v", err)
	}
	if !strings.Contains(string(plistRaw), "<key>Label</key>") {
		t.Fatalf("plist missing Label key: %s", string(plistRaw))
	}
}

func TestLaunchdUserStartCallsKickstart(t *testing.T) {
	defer withDarwinGOOS(t)()
	baseDir := t.TempDir()
	stubServiceUserHome(t, baseDir)
	t.Setenv("PATH", "/usr/bin")

	state := InstallState{
		InstanceID: "stable",
		BaseDir:    baseDir,
	}
	ApplyStateMetadata(&state, StateMetadataOptions{
		InstanceID:     state.InstanceID,
		BaseDir:        state.BaseDir,
		ServiceManager: ServiceManagerLaunchdUser,
	})

	var calls []string
	defer withMockLaunchctl(t, func(_ context.Context, args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		return "", nil
	})()

	if err := launchdUserStart(context.Background(), state); err != nil {
		t.Fatalf("launchdUserStart: %v", err)
	}

	joined := strings.Join(calls, "; ")
	if !strings.Contains(joined, "kickstart") {
		t.Fatalf("expected kickstart call, got: %v", calls)
	}
}

func TestLaunchdUserRestartCallsKickstartWithK(t *testing.T) {
	defer withDarwinGOOS(t)()
	baseDir := t.TempDir()
	stubServiceUserHome(t, baseDir)
	t.Setenv("PATH", "/usr/bin")

	state := InstallState{
		InstanceID: "stable",
		BaseDir:    baseDir,
	}
	ApplyStateMetadata(&state, StateMetadataOptions{
		InstanceID:     state.InstanceID,
		BaseDir:        state.BaseDir,
		ServiceManager: ServiceManagerLaunchdUser,
	})

	var calls []string
	defer withMockLaunchctl(t, func(_ context.Context, args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		return "", nil
	})()

	if err := launchdUserRestart(context.Background(), state); err != nil {
		t.Fatalf("launchdUserRestart: %v", err)
	}

	joined := strings.Join(calls, "; ")
	if !strings.Contains(joined, "kickstart") || !strings.Contains(joined, "-k") {
		t.Fatalf("expected kickstart -k call, got: %v", calls)
	}
}

func TestLaunchdUserIsRunningReturnsTrueWhenRunning(t *testing.T) {
	defer withDarwinGOOS(t)()
	baseDir := t.TempDir()
	stubServiceUserHome(t, baseDir)

	state := InstallState{
		InstanceID: "stable",
		BaseDir:    baseDir,
	}
	ApplyStateMetadata(&state, StateMetadataOptions{
		InstanceID:     state.InstanceID,
		BaseDir:        state.BaseDir,
		ServiceManager: ServiceManagerLaunchdUser,
	})

	defer withMockLaunchctl(t, func(_ context.Context, args ...string) (string, error) {
		output := "state = running\npid = 12345\n"
		return output, nil
	})()

	running, err := launchdUserIsRunning(context.Background(), state)
	if err != nil {
		t.Fatalf("launchdUserIsRunning: %v", err)
	}
	if !running {
		t.Fatal("expected running=true")
	}
}

func TestLaunchdUserIsRunningReturnsFalseWhenNotFound(t *testing.T) {
	defer withDarwinGOOS(t)()
	baseDir := t.TempDir()
	stubServiceUserHome(t, baseDir)

	state := InstallState{
		InstanceID: "stable",
		BaseDir:    baseDir,
	}
	ApplyStateMetadata(&state, StateMetadataOptions{
		InstanceID:     state.InstanceID,
		BaseDir:        state.BaseDir,
		ServiceManager: ServiceManagerLaunchdUser,
	})

	defer withMockLaunchctl(t, func(_ context.Context, args ...string) (string, error) {
		return "", fmt.Errorf("Could not find domain for label")
	})()

	running, err := launchdUserIsRunning(context.Background(), state)
	if err != nil {
		t.Fatalf("launchdUserIsRunning: %v", err)
	}
	if running {
		t.Fatal("expected running=false for not-found domain")
	}
}

func TestDetectAutostartDarwinReportsSupported(t *testing.T) {
	defer withDarwinGOOS(t)()
	baseDir := t.TempDir()
	stubServiceUserHome(t, baseDir)

	var calls []string
	defer withMockLaunchctl(t, func(_ context.Context, args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		// "print" returns not-found for empty state
		return "", fmt.Errorf("Could not find domain for label")
	})()

	status, err := DetectAutostart("")
	if err != nil {
		t.Fatalf("DetectAutostart: %v", err)
	}
	if status.Platform != "darwin" {
		t.Fatalf("Platform = %q, want darwin", status.Platform)
	}
	if !status.Supported {
		t.Fatalf("expected supported status on darwin, got %#v", status)
	}
	if status.Manager != ServiceManagerLaunchdUser {
		t.Fatalf("Manager = %q, want %q", status.Manager, ServiceManagerLaunchdUser)
	}
}

func TestDetectAutostartDarwinShowsEnabledWhenPlistConfigured(t *testing.T) {
	defer withDarwinGOOS(t)()
	baseDir := t.TempDir()
	stubServiceUserHome(t, baseDir)
	binaryPath := seedBinary(t, filepath.Join(baseDir, "bin", "codex-remote"), "binary")
	t.Setenv("PATH", "/usr/bin")

	// Create a configured state
	statePath := defaultInstallStatePath(baseDir)
	state := InstallState{
		InstanceID:      "stable",
		BaseDir:         baseDir,
		StatePath:       statePath,
		ConfigPath:      filepath.Join(baseDir, ".config", "codex-remote", "config.json"),
		InstalledBinary: binaryPath,
	}
	ApplyStateMetadata(&state, StateMetadataOptions{
		InstanceID:     state.InstanceID,
		StatePath:      state.StatePath,
		BaseDir:        state.BaseDir,
		ServiceManager: ServiceManagerLaunchdUser,
	})
	state.ServiceManager = ServiceManagerLaunchdUser
	state.ServiceUnitPath = filepath.Join(baseDir, "Library", "LaunchAgents", "com.codex-remote.service.plist")

	// Write the plist file
	if err := os.MkdirAll(filepath.Dir(state.ServiceUnitPath), 0o755); err != nil {
		t.Fatalf("mkdir plist dir: %v", err)
	}
	if err := os.WriteFile(state.ServiceUnitPath, []byte("<plist/>"), 0o644); err != nil {
		t.Fatalf("write plist: %v", err)
	}
	if err := WriteState(statePath, state); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	defer withMockLaunchctl(t, func(_ context.Context, args ...string) (string, error) {
		output := "state = running\npid = 12345\n"
		return output, nil
	})()

	status, err := DetectAutostart(statePath)
	if err != nil {
		t.Fatalf("DetectAutostart: %v", err)
	}
	if !status.Configured {
		t.Fatal("expected Configured=true")
	}
	if !status.Enabled {
		t.Fatal("expected Enabled=true when plist exists and service is running")
	}
	if status.Status != "enabled" {
		t.Fatalf("Status = %q, want enabled", status.Status)
	}
}

func TestXmlEscape(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello", "hello"},
		{"&", "&amp;"},
		{"<", "&lt;"},
		{">", "&gt;"},
		{`"`, "&quot;"},
		{"'", "&apos;"},
		{"<foo>&</foo>", "&lt;foo&gt;&amp;&lt;/foo&gt;"},
	}
	for _, tc := range tests {
		got := xmlEscape(tc.input)
		if got != tc.want {
			t.Fatalf("xmlEscape(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
