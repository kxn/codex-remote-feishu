package install

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseServiceManagerRejectsSystemdUserOutsideLinux(t *testing.T) {
	_, err := ParseServiceManager(string(ServiceManagerSystemdUser), "darwin")
	if err == nil || !strings.Contains(err.Error(), "only supported on linux") {
		t.Fatalf("ParseServiceManager non-linux error = %v", err)
	}
}

func TestApplyStateMetadataInfersLinuxServicePaths(t *testing.T) {
	baseDir := filepath.Join(string(filepath.Separator), "tmp", "codex-remote-home")
	state := InstallState{
		ConfigPath:      filepath.Join(baseDir, ".config", "codex-remote", "config.json"),
		StatePath:       filepath.Join(baseDir, ".local", "share", "codex-remote", "install-state.json"),
		ServiceManager:  ServiceManagerSystemdUser,
		InstalledBinary: filepath.Join(baseDir, ".local", "bin", "codex-remote"),
	}

	ApplyStateMetadata(&state, StateMetadataOptions{
		StatePath:      state.StatePath,
		ServiceManager: state.ServiceManager,
	})

	if state.BaseDir != baseDir {
		t.Fatalf("BaseDir = %q, want %q", state.BaseDir, baseDir)
	}
	if state.ServiceUnitPath != filepath.Join(baseDir, ".config", "systemd", "user", "codex-remote.service") {
		t.Fatalf("ServiceUnitPath = %q", state.ServiceUnitPath)
	}
}

func TestApplyStateMetadataInfersDebugInstancePaths(t *testing.T) {
	baseDir := filepath.Join(string(filepath.Separator), "tmp", "codex-remote-home")
	state := InstallState{
		ConfigPath:      filepath.Join(baseDir, ".config", "codex-remote-debug", "codex-remote", "config.json"),
		StatePath:       filepath.Join(baseDir, ".local", "share", "codex-remote-debug", "codex-remote", "install-state.json"),
		ServiceManager:  ServiceManagerSystemdUser,
		InstalledBinary: filepath.Join(baseDir, ".local", "share", "codex-remote-debug", "bin", "codex-remote"),
	}

	ApplyStateMetadata(&state, StateMetadataOptions{
		StatePath:      state.StatePath,
		ServiceManager: state.ServiceManager,
	})

	if state.BaseDir != baseDir {
		t.Fatalf("BaseDir = %q, want %q", state.BaseDir, baseDir)
	}
	if state.InstanceID != debugInstanceID {
		t.Fatalf("InstanceID = %q, want %q", state.InstanceID, debugInstanceID)
	}
	if state.ServiceUnitPath != filepath.Join(baseDir, ".config", "systemd", "user", "codex-remote-debug.service") {
		t.Fatalf("ServiceUnitPath = %q", state.ServiceUnitPath)
	}
}

func TestRunServiceStatusReportsDetachedManagerWithoutSystemd(t *testing.T) {
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

	var stdout bytes.Buffer
	if err := RunService([]string{"status", "-state-path", statePath}, strings.NewReader(""), &stdout, &bytes.Buffer{}, "vtest"); err != nil {
		t.Fatalf("RunService status: %v", err)
	}
	if !strings.Contains(stdout.String(), "service manager: detached") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "not configured") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}
