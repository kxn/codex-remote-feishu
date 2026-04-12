package install

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseInstanceIDAcceptsCustomInstance(t *testing.T) {
	got, err := parseInstanceID("Repo-123_ab")
	if err != nil {
		t.Fatalf("parseInstanceID() error = %v", err)
	}
	if got != "repo-123_ab" {
		t.Fatalf("parseInstanceID() = %q, want repo-123_ab", got)
	}
}

func TestApplyStateMetadataInfersCustomInstancePaths(t *testing.T) {
	baseDir := filepath.Join(string(filepath.Separator), "tmp", "codex-remote-home")
	state := InstallState{
		ConfigPath:      filepath.Join(baseDir, ".config", "codex-remote-repo-1234", "codex-remote", "config.json"),
		StatePath:       filepath.Join(baseDir, ".local", "share", "codex-remote-repo-1234", "codex-remote", "install-state.json"),
		ServiceManager:  ServiceManagerSystemdUser,
		InstalledBinary: filepath.Join(baseDir, ".local", "share", "codex-remote-repo-1234", "bin", "codex-remote"),
	}

	ApplyStateMetadata(&state, StateMetadataOptions{
		StatePath:      state.StatePath,
		ServiceManager: state.ServiceManager,
	})

	if state.BaseDir != baseDir {
		t.Fatalf("BaseDir = %q, want %q", state.BaseDir, baseDir)
	}
	if state.InstanceID != "repo-1234" {
		t.Fatalf("InstanceID = %q, want repo-1234", state.InstanceID)
	}
	if state.ServiceUnitPath != filepath.Join(baseDir, ".config", "systemd", "user", "codex-remote-repo-1234.service") {
		t.Fatalf("ServiceUnitPath = %q", state.ServiceUnitPath)
	}
}

func TestResolveInstallInstanceSelectionDefaultsToStableOutsideRepo(t *testing.T) {
	t.Setenv(repoRootEnvVar, "")
	wd := t.TempDir()
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	if err := os.Chdir(wd); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}
	defer func() {
		if err := os.Chdir(originalWD); err != nil {
			t.Fatalf("restore wd error = %v", err)
		}
	}()
	originalPortAvailable := instancePortAvailableFunc
	instancePortAvailableFunc = func(int) bool { return true }
	defer func() { instancePortAvailableFunc = originalPortAvailable }()

	selection, err := resolveInstallInstanceSelection("", t.TempDir())
	if err != nil {
		t.Fatalf("resolveInstallInstanceSelection() error = %v", err)
	}
	if selection.InstanceID != defaultInstanceID {
		t.Fatalf("InstanceID = %q, want stable", selection.InstanceID)
	}
	if selection.WriteBinding || selection.ClearBinding {
		t.Fatalf("unexpected binding action: %#v", selection)
	}
}

func TestResolveInstallInstanceSelectionDetectsRepoRootFromWorkingDirectory(t *testing.T) {
	repoRoot := t.TempDir()
	workDir := filepath.Join(repoRoot, "internal", "app", "install")
	if err := os.MkdirAll(filepath.Join(repoRoot, ".git"), 0o755); err != nil {
		t.Fatalf("MkdirAll(.git) error = %v", err)
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(workDir) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "go.mod"), []byte(projectModuleDeclaration+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(go.mod) error = %v", err)
	}

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	if err := os.Chdir(workDir); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}
	defer func() {
		if err := os.Chdir(originalWD); err != nil {
			t.Fatalf("restore wd error = %v", err)
		}
	}()
	t.Setenv(repoRootEnvVar, "")

	selection, err := resolveInstallInstanceSelection("repo-1234", t.TempDir())
	if err != nil {
		t.Fatalf("resolveInstallInstanceSelection() error = %v", err)
	}
	if selection.RepoRoot != repoRoot {
		t.Fatalf("RepoRoot = %q, want %q", selection.RepoRoot, repoRoot)
	}
	if !selection.WriteBinding {
		t.Fatalf("expected explicit custom instance to persist binding, got %#v", selection)
	}
}

func TestResolveInstallInstanceSelectionUsesRepoBinding(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv(repoRootEnvVar, repoRoot)
	if err := writeRepoInstallInstance(repoRoot, "repo-1234"); err != nil {
		t.Fatalf("writeRepoInstallInstance() error = %v", err)
	}

	selection, err := resolveInstallInstanceSelection("", t.TempDir())
	if err != nil {
		t.Fatalf("resolveInstallInstanceSelection() error = %v", err)
	}
	if selection.InstanceID != "repo-1234" {
		t.Fatalf("InstanceID = %q, want repo-1234", selection.InstanceID)
	}
	if selection.WriteBinding || selection.ClearBinding {
		t.Fatalf("unexpected binding action: %#v", selection)
	}
}

func TestResolveInstallInstanceSelectionCreatesDedicatedRepoInstanceWhenStableExists(t *testing.T) {
	repoRoot := t.TempDir()
	baseDir := t.TempDir()
	t.Setenv(repoRootEnvVar, repoRoot)

	statePath := defaultInstallStatePathForInstance(baseDir, defaultInstanceID)
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(statePath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	originalPortAvailable := instancePortAvailableFunc
	instancePortAvailableFunc = func(int) bool { return true }
	defer func() { instancePortAvailableFunc = originalPortAvailable }()

	selection, err := resolveInstallInstanceSelection("", baseDir)
	if err != nil {
		t.Fatalf("resolveInstallInstanceSelection() error = %v", err)
	}
	if selection.InstanceID != deriveRepoInstanceID(repoRoot) {
		t.Fatalf("InstanceID = %q, want %q", selection.InstanceID, deriveRepoInstanceID(repoRoot))
	}
	if !selection.WriteBinding {
		t.Fatalf("expected binding write action, got %#v", selection)
	}
	if err := persistInstallInstanceSelection(selection); err != nil {
		t.Fatalf("persistInstallInstanceSelection() error = %v", err)
	}
	bound, ok, err := readRepoInstallInstance(repoRoot)
	if err != nil {
		t.Fatalf("readRepoInstallInstance() error = %v", err)
	}
	if !ok || bound != selection.InstanceID {
		t.Fatalf("bound = %q ok=%t, want %q true", bound, ok, selection.InstanceID)
	}
}

func TestResolveInstallInstanceSelectionExplicitStableClearsRepoBinding(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv(repoRootEnvVar, repoRoot)
	if err := writeRepoInstallInstance(repoRoot, "repo-1234"); err != nil {
		t.Fatalf("writeRepoInstallInstance() error = %v", err)
	}

	selection, err := resolveInstallInstanceSelection("stable", t.TempDir())
	if err != nil {
		t.Fatalf("resolveInstallInstanceSelection() error = %v", err)
	}
	if !selection.ClearBinding {
		t.Fatalf("expected clear binding action, got %#v", selection)
	}
	if err := persistInstallInstanceSelection(selection); err != nil {
		t.Fatalf("persistInstallInstanceSelection() error = %v", err)
	}
	if _, ok, err := readRepoInstallInstance(repoRoot); err != nil {
		t.Fatalf("readRepoInstallInstance() error = %v", err)
	} else if ok {
		t.Fatal("expected repo-local binding to be removed")
	}
}
