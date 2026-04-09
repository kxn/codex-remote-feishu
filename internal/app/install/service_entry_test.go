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

func TestRenderSystemdUserUnitEscapesPathsWithoutQuotedAssignments(t *testing.T) {
	originalGOOS := serviceRuntimeGOOS
	serviceRuntimeGOOS = "linux"
	defer func() {
		serviceRuntimeGOOS = originalGOOS
	}()

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
	if !strings.Contains(unitText, `WorkingDirectory=/tmp/codex\\x20remote`) {
		t.Fatalf("unit missing escaped WorkingDirectory: %s", unitText)
	}
	if !strings.Contains(unitText, `ExecStart=/tmp/codex\\x20remote/bin/codex-remote daemon`) {
		t.Fatalf("unit missing escaped ExecStart path: %s", unitText)
	}
}
