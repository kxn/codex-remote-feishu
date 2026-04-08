package install

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareRollbackCandidateSnapshotsBinaryAndConfig(t *testing.T) {
	baseDir := t.TempDir()
	binaryPath := filepath.Join(baseDir, "codex-remote")
	configPath := filepath.Join(baseDir, "config", "config.json")
	statePath := filepath.Join(baseDir, "state", "install-state.json")

	if err := os.WriteFile(binaryPath, []byte("binary-v1"), 0o755); err != nil {
		t.Fatalf("WriteFile binary: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("MkdirAll config dir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("{\"version\":\"old\"}\n"), 0o600); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	candidate, err := PrepareRollbackCandidate(InstallState{
		StatePath:          statePath,
		CurrentBinaryPath:  binaryPath,
		CurrentVersion:     "v1.0.0",
		InstallSource:      InstallSourceRelease,
		ConfigPath:         configPath,
		WrapperConfigPath:  configPath,
		ServicesConfigPath: configPath,
	}, "v1.1.0")
	if err != nil {
		t.Fatalf("PrepareRollbackCandidate: %v", err)
	}

	if candidate.BinaryPath == "" {
		t.Fatal("expected binary backup path")
	}
	binaryRaw, err := os.ReadFile(candidate.BinaryPath)
	if err != nil {
		t.Fatalf("ReadFile binary backup: %v", err)
	}
	if string(binaryRaw) != "binary-v1" {
		t.Fatalf("binary backup = %q, want %q", string(binaryRaw), "binary-v1")
	}

	if len(candidate.ConfigSnapshots) != 1 {
		t.Fatalf("config snapshot count = %d, want 1", len(candidate.ConfigSnapshots))
	}
	snapshot := candidate.ConfigSnapshots[0]
	if snapshot.Path != configPath {
		t.Fatalf("snapshot path = %q, want %q", snapshot.Path, configPath)
	}
	if !snapshot.Existed {
		t.Fatal("expected config snapshot to record existing file")
	}
	configRaw, err := os.ReadFile(snapshot.BackupPath)
	if err != nil {
		t.Fatalf("ReadFile config backup: %v", err)
	}
	if string(configRaw) != "{\"version\":\"old\"}\n" {
		t.Fatalf("config backup = %q, want %q", string(configRaw), "{\"version\":\"old\"}\n")
	}
}

func TestRestoreConfigSnapshotsRestoresOriginalContent(t *testing.T) {
	baseDir := t.TempDir()
	configPath := filepath.Join(baseDir, "config.json")
	backupDir := filepath.Join(baseDir, "backup")
	if err := os.WriteFile(configPath, []byte("{\"mode\":\"old\"}\n"), 0o600); err != nil {
		t.Fatalf("WriteFile original config: %v", err)
	}

	snapshots, err := backupConfigSnapshots(InstallState{ConfigPath: configPath}, backupDir)
	if err != nil {
		t.Fatalf("backupConfigSnapshots: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("{\"mode\":\"new\"}\n"), 0o600); err != nil {
		t.Fatalf("WriteFile mutated config: %v", err)
	}

	if err := restoreConfigSnapshots(snapshots); err != nil {
		t.Fatalf("restoreConfigSnapshots: %v", err)
	}
	restored, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile restored config: %v", err)
	}
	if string(restored) != "{\"mode\":\"old\"}\n" {
		t.Fatalf("restored config = %q, want %q", string(restored), "{\"mode\":\"old\"}\n")
	}
}

func TestRestoreConfigSnapshotsRemovesFileCreatedByUpgrade(t *testing.T) {
	baseDir := t.TempDir()
	configPath := filepath.Join(baseDir, "config.json")
	backupDir := filepath.Join(baseDir, "backup")

	snapshots, err := backupConfigSnapshots(InstallState{ConfigPath: configPath}, backupDir)
	if err != nil {
		t.Fatalf("backupConfigSnapshots: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("{\"mode\":\"new\"}\n"), 0o600); err != nil {
		t.Fatalf("WriteFile created config: %v", err)
	}

	if err := restoreConfigSnapshots(snapshots); err != nil {
		t.Fatalf("restoreConfigSnapshots: %v", err)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("expected config file to be removed, stat err = %v", err)
	}
}
