package shim

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestModeManager(t *testing.T) {
	if got := ModeManaged.Manager(); got != SidecarManager {
		t.Fatalf("ModeManaged.Manager() = %q, want %q", got, SidecarManager)
	}
	if got := ModeUpgrade.Manager(); got != UpgradeSidecarManager {
		t.Fatalf("ModeUpgrade.Manager() = %q, want %q", got, UpgradeSidecarManager)
	}
}

func TestModeForManager(t *testing.T) {
	cases := map[string]Mode{
		SidecarManager:        ModeManaged,
		UpgradeSidecarManager: ModeUpgrade,
		"codex-remote-other":  ModeManaged,
		"":                    ModeManaged,
	}
	for input, want := range cases {
		if got := ModeForManager(input); got != want {
			t.Fatalf("ModeForManager(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestRealBinaryPath(t *testing.T) {
	tests := map[string]string{
		"/tmp/codex":     filepath.Join(string(filepath.Separator), "tmp", "codex.real"),
		`C:\tmp\codex`:   `C:\tmp\codex.real`,
		"/tmp/codex.exe": filepath.Join(string(filepath.Separator), "tmp", "codex.real.exe"),
	}
	for input, want := range tests {
		if got := RealBinaryPath(input); got != want {
			t.Fatalf("RealBinaryPath(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSidecarPath(t *testing.T) {
	tests := map[string]string{
		"/tmp/codex":     filepath.Join(string(filepath.Separator), "tmp", "codex.remote.json"),
		`C:\tmp\codex`:   `C:\tmp\codex.remote.json`,
		"/tmp/codex.exe": filepath.Join(string(filepath.Separator), "tmp", "codex.remote.json"),
	}
	for input, want := range tests {
		if got := SidecarPath(input); got != want {
			t.Fatalf("SidecarPath(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestShimDerivedPathsCanonicalizeWindowsExtendedEntrypoint(t *testing.T) {
	if got := RealBinaryPath(`\\?\C:\repo\bin\codex.exe`); got != `C:\repo\bin\codex.real.exe` {
		t.Fatalf("RealBinaryPath() = %q, want native extended-prefix-free path", got)
	}
	if got := SidecarPath(`//?/C:/repo/bin/codex.exe`); got != `C:\repo\bin\codex.remote.json` {
		t.Fatalf("SidecarPath() = %q, want native extended-prefix-free path", got)
	}
}

func TestSamePathCanonicalizesWindowsExtendedForms(t *testing.T) {
	pairs := [][2]string{
		{`\\?\C:\repo\codex.exe`, `C:\repo\codex.exe`},
		{`//?/C:/repo/codex.exe`, `C:\repo\codex.exe`},
		{`\\?\UNC\server\share\codex.exe`, `\\server\share\codex.exe`},
		{`C:\Repo\codex.exe`, `c:/repo/codex.exe`},
	}
	for _, pair := range pairs {
		if !SamePath(pair[0], pair[1]) {
			t.Fatalf("SamePath(%q, %q) = false, want true", pair[0], pair[1])
		}
	}
}

func TestNormalizeSidecarCanonicalizesWindowsExtendedPaths(t *testing.T) {
	got := NormalizeSidecar(Sidecar{
		InstallStatePath: `//?/C:/repo/state/install-state.json`,
		ConfigPath:       `\\?\C:\repo\config\config.json`,
	}, ModeManaged)
	if got.InstallStatePath != `C:\repo\state\install-state.json` {
		t.Fatalf("InstallStatePath = %q, want native extended-prefix-free path", got.InstallStatePath)
	}
	if got.ConfigPath != `C:\repo\config\config.json` {
		t.Fatalf("ConfigPath = %q, want native extended-prefix-free path", got.ConfigPath)
	}
}

func TestWriteAndReadSidecarManaged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "codex.remote.json")
	want := Sidecar{
		InstallStatePath: filepath.Join(dir, "install-state.json"),
		ConfigPath:       filepath.Join(dir, "config.json"),
		InstanceID:       "stable",
	}
	if err := WriteSidecar(path, want, ModeManaged); err != nil {
		t.Fatalf("WriteSidecar: %v", err)
	}
	got, err := ReadSidecar(path)
	if err != nil {
		t.Fatalf("ReadSidecar: %v", err)
	}
	if got.SchemaVersion != SidecarSchemaVersion {
		t.Fatalf("SchemaVersion = %d", got.SchemaVersion)
	}
	if got.Manager != SidecarManager {
		t.Fatalf("Manager = %q", got.Manager)
	}
	if got.InstallStatePath != want.InstallStatePath {
		t.Fatalf("InstallStatePath = %q, want %q", got.InstallStatePath, want.InstallStatePath)
	}
	if got.ConfigPath != want.ConfigPath {
		t.Fatalf("ConfigPath = %q, want %q", got.ConfigPath, want.ConfigPath)
	}
	if got.InstanceID != want.InstanceID {
		t.Fatalf("InstanceID = %q, want %q", got.InstanceID, want.InstanceID)
	}
}

func TestWriteAndReadSidecarUpgrade(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "codex-remote-upgrade-shim-1.remote.json")
	want := Sidecar{
		InstallStatePath: filepath.Join(dir, "install-state.json"),
		InstanceID:       "instance-1",
	}
	if err := WriteSidecar(path, want, ModeUpgrade); err != nil {
		t.Fatalf("WriteSidecar: %v", err)
	}
	got, err := ReadSidecar(path)
	if err != nil {
		t.Fatalf("ReadSidecar: %v", err)
	}
	if got.Manager != UpgradeSidecarManager {
		t.Fatalf("Manager = %q", got.Manager)
	}
	if got.InstallStatePath != want.InstallStatePath {
		t.Fatalf("InstallStatePath = %q, want %q", got.InstallStatePath, want.InstallStatePath)
	}
	if got.ConfigPath != "" {
		t.Fatalf("ConfigPath = %q, want empty", got.ConfigPath)
	}
}

func TestWriteSidecarRejectsMissingBinding(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "codex.remote.json")
	if err := WriteSidecar(path, Sidecar{InstallStatePath: filepath.Join(dir, "install-state.json")}, ModeManaged); err == nil {
		t.Fatal("expected missing config path to fail for managed mode")
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("expected no sidecar file, stat err=%v", statErr)
	}
}

func TestWriteSidecarUpgradeAcceptsStateOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "upgrade.remote.json")
	if err := WriteSidecar(path, Sidecar{InstallStatePath: filepath.Join(dir, "install-state.json")}, ModeUpgrade); err != nil {
		t.Fatalf("WriteSidecar upgrade: %v", err)
	}
	if !SidecarValid(Sidecar{InstallStatePath: filepath.Join(dir, "install-state.json")}, ModeUpgrade) {
		t.Fatal("expected upgrade sidecar with install-state only to be valid")
	}
	if SidecarValid(Sidecar{}, ModeUpgrade) {
		t.Fatal("expected empty upgrade sidecar to be invalid")
	}
}

func TestReadSidecarLegacyManaged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.remote.json")
	raw := `{"schemaVersion":1,"manager":"codex-remote","installStatePath":` + strconv.Quote(filepath.Join(dir, "install-state.json")) + `,"configPath":` + strconv.Quote(filepath.Join(dir, "config.json")) + `}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := ReadSidecar(path)
	if err != nil {
		t.Fatalf("ReadSidecar: %v", err)
	}
	if got.Manager != SidecarManager {
		t.Fatalf("Manager = %q", got.Manager)
	}
	if got.ConfigPath == "" {
		t.Fatal("expected legacy config path to survive read")
	}
}
