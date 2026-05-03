package install

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunServiceInstallUserWritesUnitAndState(t *testing.T) {
	baseDir := t.TempDir()
	stubServiceUserHome(t, baseDir)
	servicePath := "/home/demo/.local/bin:/usr/bin"
	t.Setenv("PATH", servicePath)
	statePath := defaultInstallStatePath(baseDir)
	state := InstallState{
		BaseDir:         baseDir,
		ConfigPath:      filepath.Join(baseDir, ".config", "codex-remote", "config.json"),
		StatePath:       statePath,
		InstalledBinary: seedBinary(t, filepath.Join(baseDir, "bin", "codex-remote"), "binary"),
	}
	ApplyStateMetadata(&state, StateMetadataOptions{StatePath: statePath})
	if err := WriteState(statePath, state); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	originalGOOS := serviceRuntimeGOOS
	originalRunner := systemctlUserRunner
	originalShellLookup := systemdShellEnvLookup
	serviceRuntimeGOOS = "linux"
	defer func() {
		serviceRuntimeGOOS = originalGOOS
		systemctlUserRunner = originalRunner
		systemdShellEnvLookup = originalShellLookup
	}()
	systemdShellEnvLookup = func(env []string, key string) (string, error) {
		return servicePath, nil
	}

	var calls []string
	systemctlUserRunner = func(_ context.Context, args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		return "", nil
	}

	var stdout bytes.Buffer
	if err := RunService([]string{"install-user", "-state-path", statePath}, strings.NewReader(""), &stdout, &bytes.Buffer{}, "vtest"); err != nil {
		t.Fatalf("RunService install-user: %v", err)
	}

	updated, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if updated.ServiceManager != ServiceManagerSystemdUser {
		t.Fatalf("ServiceManager = %q, want %q", updated.ServiceManager, ServiceManagerSystemdUser)
	}
	if strings.TrimSpace(updated.ServiceUnitPath) == "" {
		t.Fatalf("expected service unit path to be written, got %#v", updated)
	}
	unitRaw, err := os.ReadFile(updated.ServiceUnitPath)
	if err != nil {
		t.Fatalf("read unit: %v", err)
	}
	unitText := string(unitRaw)
	if !strings.Contains(unitText, "ExecStart=") || !strings.Contains(unitText, "daemon") {
		t.Fatalf("unit content missing ExecStart daemon: %s", unitText)
	}
	if !strings.Contains(unitText, "Environment=PATH="+systemdEscapeValue(systemdUserServicePATH())) {
		t.Fatalf("unit content missing PATH env: %s", unitText)
	}
	if !strings.Contains(unitText, "XDG_STATE_HOME=") {
		t.Fatalf("unit content missing XDG env: %s", unitText)
	}
	if len(calls) != 1 || calls[0] != "daemon-reload" {
		t.Fatalf("systemctl calls = %#v, want daemon-reload", calls)
	}
	if !strings.Contains(stdout.String(), "service manager: systemd_user") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestApplyStateMetadataRewritesLegacySystemdUserUnitPathToCurrentHome(t *testing.T) {
	baseDir := filepath.Join(string(filepath.Separator), "data", "dl")
	homeDir := t.TempDir()
	stubServiceUserHome(t, homeDir)

	state := InstallState{
		BaseDir:         baseDir,
		ConfigPath:      filepath.Join(baseDir, ".config", "codex-remote", "config.json"),
		StatePath:       filepath.Join(baseDir, ".local", "share", "codex-remote", "install-state.json"),
		ServiceManager:  ServiceManagerSystemdUser,
		ServiceUnitPath: filepath.Join(baseDir, ".config", "systemd", "user", "codex-remote.service"),
	}

	ApplyStateMetadata(&state, StateMetadataOptions{
		StatePath:      state.StatePath,
		BaseDir:        state.BaseDir,
		ServiceManager: state.ServiceManager,
	})

	want := filepath.Join(homeDir, ".config", "systemd", "user", "codex-remote.service")
	if state.ServiceUnitPath != want {
		t.Fatalf("ServiceUnitPath = %q, want %q", state.ServiceUnitPath, want)
	}
}

func TestRunServiceStartRejectsDetachedManager(t *testing.T) {
	baseDir := t.TempDir()
	statePath := defaultInstallStatePath(baseDir)
	state := InstallState{
		BaseDir:         baseDir,
		ConfigPath:      filepath.Join(baseDir, ".config", "codex-remote", "config.json"),
		StatePath:       statePath,
		InstalledBinary: seedBinary(t, filepath.Join(baseDir, "bin", "codex-remote"), "binary"),
	}
	ApplyStateMetadata(&state, StateMetadataOptions{StatePath: statePath})
	if err := WriteState(statePath, state); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	err := RunService([]string{"start", "-state-path", statePath}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, "vtest")
	if err == nil || !strings.Contains(err.Error(), "install-user") {
		t.Fatalf("RunService start error = %v, want install-user guidance", err)
	}
}

func TestRestartInstalledDaemonUsesSystemdUserLifecycle(t *testing.T) {
	baseDir := t.TempDir()
	stubServiceUserHome(t, baseDir)
	statePath := defaultInstallStatePath(baseDir)
	state := InstallState{
		BaseDir:         baseDir,
		ConfigPath:      filepath.Join(baseDir, ".config", "codex-remote", "config.json"),
		StatePath:       statePath,
		ServiceManager:  ServiceManagerSystemdUser,
		InstalledBinary: seedBinary(t, filepath.Join(baseDir, "bin", "codex-remote"), "binary"),
	}
	ApplyStateMetadata(&state, StateMetadataOptions{
		StatePath:      statePath,
		BaseDir:        baseDir,
		ServiceManager: state.ServiceManager,
	})

	originalGOOS := serviceRuntimeGOOS
	originalRunner := systemctlUserRunner
	serviceRuntimeGOOS = "linux"
	defer func() {
		serviceRuntimeGOOS = originalGOOS
		systemctlUserRunner = originalRunner
	}()
	var calls []string
	systemctlUserRunner = func(_ context.Context, args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		return "", nil
	}

	if err := RestartInstalledDaemon(context.Background(), state); err != nil {
		t.Fatalf("RestartInstalledDaemon: %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("systemctl calls = %#v, want daemon-reload + restart", calls)
	}
	if calls[0] != "daemon-reload" || !strings.HasPrefix(calls[1], "restart ") {
		t.Fatalf("systemctl calls = %#v, want daemon-reload then restart", calls)
	}
	updated, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if updated.ServiceManager != ServiceManagerSystemdUser || strings.TrimSpace(updated.ServiceUnitPath) == "" {
		t.Fatalf("expected restarted state to stay systemd_user with unit path, got %#v", updated)
	}
}

func TestRestartInstalledDaemonRejectsDetachedManager(t *testing.T) {
	originalRunner := systemctlUserRunner
	defer func() {
		systemctlUserRunner = originalRunner
	}()
	systemctlUserRunner = func(_ context.Context, args ...string) (string, error) {
		t.Fatalf("detached restart must not call systemctl, got %#v", args)
		return "", nil
	}

	err := RestartInstalledDaemon(context.Background(), InstallState{ServiceManager: ServiceManagerDetached})
	if err == nil || !strings.Contains(err.Error(), "install-user") {
		t.Fatalf("RestartInstalledDaemon error = %v, want install-user guidance", err)
	}
}

func TestRenderSystemdUserUnitEscapesPathsWithoutQuotedAssignments(t *testing.T) {
	originalGOOS := serviceRuntimeGOOS
	originalShellLookup := systemdShellEnvLookup
	serviceRuntimeGOOS = "linux"
	defer func() {
		serviceRuntimeGOOS = originalGOOS
		systemdShellEnvLookup = originalShellLookup
	}()
	t.Setenv("PATH", "/usr/bin")
	systemdShellEnvLookup = func(env []string, key string) (string, error) {
		return "/usr/bin", nil
	}

	state := InstallState{
		BaseDir:         filepath.Join(string(filepath.Separator), "tmp", "codex remote"),
		StatePath:       filepath.Join(string(filepath.Separator), "tmp", "codex remote", ".local", "share", "codex-remote", "install-state.json"),
		ConfigPath:      filepath.Join(string(filepath.Separator), "tmp", "codex remote", ".config", "codex-remote", "config.json"),
		InstalledBinary: filepath.Join(string(filepath.Separator), "tmp", "codex remote", "bin", "codex-remote"),
		ServiceManager:  ServiceManagerSystemdUser,
	}
	ApplyStateMetadata(&state, StateMetadataOptions{
		StatePath:      state.StatePath,
		BaseDir:        state.BaseDir,
		ServiceManager: state.ServiceManager,
	})

	unitText, err := renderSystemdUserUnit(state)
	if err != nil {
		t.Fatalf("renderSystemdUserUnit: %v", err)
	}
	if strings.Contains(unitText, `WorkingDirectory="`) {
		t.Fatalf("unit should not quote WorkingDirectory assignment: %s", unitText)
	}
	if !strings.Contains(unitText, "WorkingDirectory="+systemdEscapeValue(normalizeServicePathValue(state.BaseDir))) {
		t.Fatalf("unit missing escaped WorkingDirectory: %s", unitText)
	}
	if !strings.Contains(unitText, "ExecStart="+systemdEscapeExecWord(normalizeServicePathValue(state.InstalledBinary))+" daemon") {
		t.Fatalf("unit missing escaped ExecStart path: %s", unitText)
	}
}

func TestRenderSystemdUserUnitFallsBackToDefaultPATHWhenEnvironmentEmpty(t *testing.T) {
	originalGOOS := serviceRuntimeGOOS
	originalShellLookup := systemdShellEnvLookup
	serviceRuntimeGOOS = "linux"
	defer func() {
		serviceRuntimeGOOS = originalGOOS
		systemdShellEnvLookup = originalShellLookup
	}()

	t.Setenv("PATH", "")
	systemdShellEnvLookup = func(env []string, key string) (string, error) {
		return "", nil
	}

	state := InstallState{
		BaseDir:         filepath.Join(string(filepath.Separator), "tmp", "codex-remote"),
		StatePath:       filepath.Join(string(filepath.Separator), "tmp", "codex-remote", ".local", "share", "codex-remote", "install-state.json"),
		ConfigPath:      filepath.Join(string(filepath.Separator), "tmp", "codex-remote", ".config", "codex-remote", "config.json"),
		InstalledBinary: filepath.Join(string(filepath.Separator), "tmp", "codex-remote", "bin", "codex-remote"),
		ServiceManager:  ServiceManagerSystemdUser,
	}
	ApplyStateMetadata(&state, StateMetadataOptions{
		StatePath:      state.StatePath,
		BaseDir:        state.BaseDir,
		ServiceManager: state.ServiceManager,
	})

	unitText, err := renderSystemdUserUnit(state)
	if err != nil {
		t.Fatalf("renderSystemdUserUnit: %v", err)
	}
	wantPath := strings.Join(defaultSystemdUserPATH, servicePathListSeparator())
	if !strings.Contains(unitText, "Environment=PATH="+systemdEscapeValue(wantPath)) {
		t.Fatalf("unit missing fallback PATH env: %s", unitText)
	}
}

func TestRenderSystemdUserUnitPrefersInteractiveShellPATH(t *testing.T) {
	originalGOOS := serviceRuntimeGOOS
	originalShellLookup := systemdShellEnvLookup
	serviceRuntimeGOOS = "linux"
	defer func() {
		serviceRuntimeGOOS = originalGOOS
		systemdShellEnvLookup = originalShellLookup
	}()

	t.Setenv("PATH", "/usr/bin")
	shellPath := strings.Join([]string{
		"/home/demo/.nvm/versions/node/v22.18.0/bin",
		"/home/demo/.local/bin",
		"/usr/bin",
	}, servicePathListSeparator())
	systemdShellEnvLookup = func(env []string, key string) (string, error) {
		if key != "PATH" {
			t.Fatalf("lookup key = %q, want PATH", key)
		}
		return shellPath, nil
	}

	state := InstallState{
		BaseDir:         filepath.Join(string(filepath.Separator), "tmp", "codex-remote"),
		StatePath:       filepath.Join(string(filepath.Separator), "tmp", "codex-remote", ".local", "share", "codex-remote", "install-state.json"),
		ConfigPath:      filepath.Join(string(filepath.Separator), "tmp", "codex-remote", ".config", "codex-remote", "config.json"),
		InstalledBinary: filepath.Join(string(filepath.Separator), "tmp", "codex-remote", "bin", "codex-remote"),
		ServiceManager:  ServiceManagerSystemdUser,
	}
	ApplyStateMetadata(&state, StateMetadataOptions{
		StatePath:      state.StatePath,
		BaseDir:        state.BaseDir,
		ServiceManager: state.ServiceManager,
	})

	unitText, err := renderSystemdUserUnit(state)
	if err != nil {
		t.Fatalf("renderSystemdUserUnit: %v", err)
	}
	if !strings.Contains(unitText, "Environment=PATH="+systemdEscapeValue(shellPath)) {
		t.Fatalf("unit missing interactive shell PATH env: %s", unitText)
	}
}

func TestRunServiceStatusUsesWorkspaceBindingWhenStatePathOmitted(t *testing.T) {
	repoRoot := t.TempDir()
	baseDir := t.TempDir()
	statePath := defaultInstallStatePathForInstance(baseDir, "master")
	state := InstallState{
		InstanceID:      "master",
		BaseDir:         baseDir,
		ConfigPath:      filepath.Join(baseDir, ".config", "codex-remote-master", "codex-remote", "config.json"),
		StatePath:       statePath,
		ServiceManager:  ServiceManagerSystemdUser,
		InstalledBinary: seedBinary(t, filepath.Join(baseDir, "bin", "codex-remote"), "binary"),
	}
	ApplyStateMetadata(&state, StateMetadataOptions{
		StatePath:      statePath,
		BaseDir:        baseDir,
		ServiceManager: state.ServiceManager,
	})
	if err := WriteState(statePath, state); err != nil {
		t.Fatalf("WriteState: %v", err)
	}
	if err := writeRepoInstallBinding(repoRoot, repoInstallBinding{
		InstanceID: "master",
		BaseDir:    baseDir,
	}); err != nil {
		t.Fatalf("writeRepoInstallBinding: %v", err)
	}
	t.Setenv(repoRootEnvVar, repoRoot)

	originalRunner := systemctlUserRunner
	defer func() { systemctlUserRunner = originalRunner }()

	var calls []string
	systemctlUserRunner = func(_ context.Context, args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		return "active\n", nil
	}

	var stdout bytes.Buffer
	if err := RunService([]string{"status"}, strings.NewReader(""), &stdout, &bytes.Buffer{}, "vtest"); err != nil {
		t.Fatalf("RunService status: %v", err)
	}
	if len(calls) != 1 || calls[0] != "status --no-pager --full codex-remote-master.service" {
		t.Fatalf("systemctl calls = %#v", calls)
	}
	if !strings.Contains(stdout.String(), "service manager: systemd_user") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}
