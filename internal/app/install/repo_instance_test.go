package install

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/testutil"
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
	stubServiceUserHome(t, baseDir)
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
	fallbackBaseDir := t.TempDir()
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

	selection, err := resolveInstallInstanceSelection("", "", fallbackBaseDir, "linux")
	if err != nil {
		t.Fatalf("resolveInstallInstanceSelection() error = %v", err)
	}
	if selection.InstanceID != defaultInstanceID {
		t.Fatalf("InstanceID = %q, want stable", selection.InstanceID)
	}
	if selection.BaseDir != fallbackBaseDir {
		t.Fatalf("BaseDir = %q, want %q", selection.BaseDir, fallbackBaseDir)
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

	selection, err := resolveInstallInstanceSelection("master", "/data/dl", t.TempDir(), "linux")
	if err != nil {
		t.Fatalf("resolveInstallInstanceSelection() error = %v", err)
	}
	if !testutil.SamePath(selection.RepoRoot, repoRoot) {
		t.Fatalf("RepoRoot = %q, want %q", selection.RepoRoot, repoRoot)
	}
	if selection.BaseDir != "/data/dl" {
		t.Fatalf("BaseDir = %q, want /data/dl", selection.BaseDir)
	}
	if selection.ServiceName != "codex-remote-master.service" {
		t.Fatalf("ServiceName = %q", selection.ServiceName)
	}
	if !selection.WriteBinding {
		t.Fatalf("expected explicit instance to persist binding, got %#v", selection)
	}
}

func TestResolveInstallInstanceSelectionUsesRepoBinding(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv(repoRootEnvVar, repoRoot)
	if err := writeRepoInstallBinding(repoRoot, repoInstallBinding{
		InstanceID: "master",
		BaseDir:    "/data/dl",
	}); err != nil {
		t.Fatalf("writeRepoInstallBinding() error = %v", err)
	}

	selection, err := resolveInstallInstanceSelection("", "", t.TempDir(), "linux")
	if err != nil {
		t.Fatalf("resolveInstallInstanceSelection() error = %v", err)
	}
	if selection.InstanceID != "master" {
		t.Fatalf("InstanceID = %q, want master", selection.InstanceID)
	}
	if selection.BaseDir != "/data/dl" {
		t.Fatalf("BaseDir = %q, want /data/dl", selection.BaseDir)
	}
	if selection.StatePath != filepath.Join("/data/dl", ".local", "share", "codex-remote-master", "codex-remote", "install-state.json") {
		t.Fatalf("StatePath = %q", selection.StatePath)
	}
	if selection.WriteBinding || selection.ClearBinding {
		t.Fatalf("unexpected binding action: %#v", selection)
	}
}

func TestResolveInstallInstanceSelectionUsesLegacyRepoBindingAndDetectsAncestorBaseDir(t *testing.T) {
	baseDir := t.TempDir()
	repoRoot := filepath.Join(baseDir, "workspace")
	if err := os.MkdirAll(filepath.Join(repoRoot, ".codex-remote"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(repoInstallInstancePath(repoRoot), []byte("master\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	statePath := defaultInstallStatePathForInstance(baseDir, "master")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatalf("MkdirAll(state dir) error = %v", err)
	}
	if err := os.WriteFile(statePath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(state) error = %v", err)
	}
	t.Setenv(repoRootEnvVar, repoRoot)

	selection, err := resolveInstallInstanceSelection("", "", t.TempDir(), "linux")
	if err != nil {
		t.Fatalf("resolveInstallInstanceSelection() error = %v", err)
	}
	if selection.InstanceID != "master" {
		t.Fatalf("InstanceID = %q, want master", selection.InstanceID)
	}
	if selection.BaseDir != baseDir {
		t.Fatalf("BaseDir = %q, want %q", selection.BaseDir, baseDir)
	}
}

func TestResolveInstallInstanceSelectionFallsBackToAncestorStableBaseDir(t *testing.T) {
	baseDir := t.TempDir()
	repoRoot := filepath.Join(baseDir, "workspace")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	statePath := defaultInstallStatePathForInstance(baseDir, defaultInstanceID)
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatalf("MkdirAll(state dir) error = %v", err)
	}
	if err := os.WriteFile(statePath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(state) error = %v", err)
	}
	t.Setenv(repoRootEnvVar, repoRoot)

	selection, err := resolveInstallInstanceSelection("", "", t.TempDir(), "linux")
	if err != nil {
		t.Fatalf("resolveInstallInstanceSelection() error = %v", err)
	}
	if selection.InstanceID != defaultInstanceID {
		t.Fatalf("InstanceID = %q, want stable", selection.InstanceID)
	}
	if selection.BaseDir != baseDir {
		t.Fatalf("BaseDir = %q, want %q", selection.BaseDir, baseDir)
	}
	if selection.WriteBinding || selection.ClearBinding {
		t.Fatalf("unexpected binding action: %#v", selection)
	}
}

func TestResolveInstallInstanceSelectionExplicitStableClearsRepoBinding(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv(repoRootEnvVar, repoRoot)
	if err := writeRepoInstallBinding(repoRoot, repoInstallBinding{
		InstanceID: "master",
		BaseDir:    "/data/dl",
	}); err != nil {
		t.Fatalf("writeRepoInstallBinding() error = %v", err)
	}

	selection, err := resolveInstallInstanceSelection("stable", "", t.TempDir(), "linux")
	if err != nil {
		t.Fatalf("resolveInstallInstanceSelection() error = %v", err)
	}
	if !selection.ClearBinding {
		t.Fatalf("expected clear binding action, got %#v", selection)
	}
	if err := persistInstallInstanceSelection(selection); err != nil {
		t.Fatalf("persistInstallInstanceSelection() error = %v", err)
	}
	if _, ok, err := readRepoInstallBinding(repoRoot); err != nil {
		t.Fatalf("readRepoInstallBinding() error = %v", err)
	} else if ok {
		t.Fatal("expected repo-local binding to be removed")
	}
	if _, err := os.Stat(repoInstallInstancePath(repoRoot)); !os.IsNotExist(err) {
		t.Fatalf("expected legacy binding to be removed, stat err = %v", err)
	}
}

func TestResolveInstallInstanceSelectionExplicitStableWithBaseDirWritesBinding(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv(repoRootEnvVar, repoRoot)

	selection, err := resolveInstallInstanceSelection("stable", "/data/dl", t.TempDir(), "linux")
	if err != nil {
		t.Fatalf("resolveInstallInstanceSelection() error = %v", err)
	}
	if !selection.WriteBinding {
		t.Fatalf("expected binding write action, got %#v", selection)
	}
	if err := persistInstallInstanceSelection(selection); err != nil {
		t.Fatalf("persistInstallInstanceSelection() error = %v", err)
	}
	binding, ok, err := readRepoInstallBinding(repoRoot)
	if err != nil {
		t.Fatalf("readRepoInstallBinding() error = %v", err)
	}
	if !ok {
		t.Fatal("expected binding to be written")
	}
	if binding.InstanceID != defaultInstanceID {
		t.Fatalf("InstanceID = %q, want stable", binding.InstanceID)
	}
	if binding.BaseDir != "/data/dl" {
		t.Fatalf("BaseDir = %q, want /data/dl", binding.BaseDir)
	}
	if binding.LogPath == "" {
		t.Fatalf("expected derived log path in binding, got %#v", binding)
	}
}
