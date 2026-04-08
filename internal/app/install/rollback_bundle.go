package install

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PrepareRollbackCandidate snapshots the current stable binary and live config files.
func PrepareRollbackCandidate(stateValue InstallState, targetVersion string) (*RollbackCandidate, error) {
	backupDir := filepath.Join(filepath.Dir(stateValue.StatePath), "upgrade-backups", strings.TrimSpace(targetVersion))
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return nil, err
	}

	backupPath := filepath.Join(backupDir, filepath.Base(stateValue.CurrentBinaryPath))
	if err := copyFile(stateValue.CurrentBinaryPath, backupPath); err != nil {
		return nil, err
	}

	snapshots, err := backupConfigSnapshots(stateValue, filepath.Join(backupDir, "config"))
	if err != nil {
		return nil, err
	}

	return &RollbackCandidate{
		Version:         stateValue.CurrentVersion,
		BinaryPath:      backupPath,
		Source:          stateValue.InstallSource,
		ConfigSnapshots: snapshots,
	}, nil
}

func backupConfigSnapshots(stateValue InstallState, backupDir string) ([]ConfigSnapshot, error) {
	configPaths := liveConfigPaths(stateValue)
	if len(configPaths) == 0 {
		return nil, nil
	}
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return nil, err
	}

	snapshots := make([]ConfigSnapshot, 0, len(configPaths))
	for index, path := range configPaths {
		snapshot := ConfigSnapshot{Path: path}
		info, err := os.Stat(path)
		switch {
		case os.IsNotExist(err):
			snapshots = append(snapshots, snapshot)
			continue
		case err != nil:
			return nil, err
		case info.IsDir():
			return nil, fmt.Errorf("config path %s is a directory", path)
		}

		snapshot.Existed = true
		snapshot.BackupPath = filepath.Join(backupDir, fmt.Sprintf("%02d%s", index, filepath.Ext(path)))
		if err := copyFile(path, snapshot.BackupPath); err != nil {
			return nil, err
		}
		snapshots = append(snapshots, snapshot)
	}
	return snapshots, nil
}

func restoreConfigSnapshots(snapshots []ConfigSnapshot) error {
	for _, snapshot := range snapshots {
		path := strings.TrimSpace(snapshot.Path)
		if path == "" {
			continue
		}
		if !snapshot.Existed {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
			continue
		}
		if strings.TrimSpace(snapshot.BackupPath) == "" {
			return fmt.Errorf("config snapshot backup path is required for %s", path)
		}
		if err := copyFile(snapshot.BackupPath, path); err != nil {
			return err
		}
	}
	return nil
}

func liveConfigPaths(stateValue InstallState) []string {
	seen := map[string]bool{}
	paths := make([]string, 0, 3)
	for _, path := range []string{stateValue.ConfigPath, stateValue.WrapperConfigPath, stateValue.ServicesConfigPath} {
		trimmed := strings.TrimSpace(path)
		if trimmed == "" {
			continue
		}
		cleaned := filepath.Clean(trimmed)
		if seen[cleaned] {
			continue
		}
		seen[cleaned] = true
		paths = append(paths, cleaned)
	}
	return paths
}
