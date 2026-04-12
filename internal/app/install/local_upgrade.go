package install

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

type LocalBinaryUpgradeOptions struct {
	StatePath    string
	SourceBinary string
	Slot         string
	HelperBinary string
}

func LocalUpgradeArtifactPath(stateValue InstallState) string {
	if strings.TrimSpace(stateValue.StatePath) != "" {
		return filepath.Join(filepath.Dir(stateValue.StatePath), "local-upgrade", executableName(runtime.GOOS))
	}
	if strings.TrimSpace(stateValue.BaseDir) != "" {
		layout := installLayoutForInstance(stateValue.BaseDir, stateValue.InstanceID)
		return filepath.Join(layout.StateDir, "local-upgrade", executableName(runtime.GOOS))
	}
	paths, err := relayruntime.DefaultPaths()
	if err == nil && strings.TrimSpace(paths.StateDir) != "" {
		return filepath.Join(paths.StateDir, "local-upgrade", executableName(runtime.GOOS))
	}
	return filepath.Join(os.TempDir(), "codex-remote-local-upgrade", executableName(runtime.GOOS))
}

func RunLocalBinaryUpgradeWithStatePath(opts LocalBinaryUpgradeOptions) (string, error) {
	statePath := strings.TrimSpace(opts.StatePath)
	if statePath == "" {
		return "", fmt.Errorf("state path is required")
	}

	stateValue, err := LoadState(statePath)
	if err != nil {
		return "", err
	}
	stateValue.StatePath = firstNonEmpty(strings.TrimSpace(stateValue.StatePath), statePath)
	ApplyStateMetadata(&stateValue, StateMetadataOptions{
		InstanceID:      stateValue.InstanceID,
		StatePath:       stateValue.StatePath,
		InstalledBinary: firstNonEmpty(strings.TrimSpace(stateValue.InstalledBinary), strings.TrimSpace(stateValue.CurrentBinaryPath)),
		CurrentVersion:  stateValue.CurrentVersion,
		ServiceManager:  stateValue.ServiceManager,
	})
	stateValue.CurrentBinaryPath = firstNonEmpty(strings.TrimSpace(stateValue.CurrentBinaryPath), strings.TrimSpace(stateValue.InstalledBinary))
	if strings.TrimSpace(stateValue.CurrentBinaryPath) == "" {
		return "", fmt.Errorf("current binary path is missing from install state")
	}
	if strings.TrimSpace(stateValue.VersionsRoot) == "" {
		return "", fmt.Errorf("versions root is missing from install state")
	}
	if localPendingUpgradeBusy(stateValue.PendingUpgrade) {
		return "", fmt.Errorf("pending upgrade phase %q is already in progress", stateValue.PendingUpgrade.Phase)
	}

	resolvedSlot, err := importLocalBinaryForUpgrade(stateValue, opts.SourceBinary, opts.Slot)
	if err != nil {
		return "", err
	}
	targetIdentity, err := relayruntime.BinaryIdentityForPath(opts.SourceBinary, "")
	if err != nil {
		return "", err
	}
	rollbackCandidate, err := PrepareRollbackCandidate(stateValue, resolvedSlot)
	if err != nil {
		return "", err
	}
	identity, err := relayruntime.BinaryIdentityForPath(stateValue.CurrentBinaryPath, stateValue.CurrentVersion)
	if err != nil {
		return "", err
	}
	rollbackCandidate.Fingerprint = identity.BuildFingerprint

	now := time.Now().UTC()
	stateValue.RollbackCandidate = rollbackCandidate
	stateValue.LastKnownLatestVersion = ""
	stateValue.PendingUpgrade = &PendingUpgrade{
		Phase:            PendingUpgradePhasePrepared,
		Source:           UpgradeSourceLocal,
		TargetTrack:      stateValue.CurrentTrack,
		TargetVersion:    firstNonEmpty(strings.TrimSpace(targetIdentity.Version), resolvedSlot),
		TargetSlot:       resolvedSlot,
		TargetBinaryPath: filepath.Join(strings.TrimSpace(stateValue.VersionsRoot), resolvedSlot, executableName(runtime.GOOS)),
		RequestedAt:      &now,
	}
	if err := WriteState(statePath, stateValue); err != nil {
		return "", err
	}

	helperBinary := strings.TrimSpace(opts.HelperBinary)
	if helperBinary == "" {
		stateValue.PendingUpgrade = nil
		stateValue.RollbackCandidate = nil
		_ = WriteState(statePath, stateValue)
		return "", fmt.Errorf("helper binary path is required")
	}
	helperPath, err := copyUpgradeHelperBinary(helperBinary, statePath)
	if err != nil {
		stateValue.PendingUpgrade = nil
		stateValue.RollbackCandidate = nil
		_ = WriteState(statePath, stateValue)
		return "", err
	}
	logPath := localUpgradeLogPath(stateValue)
	launchResult, err := StartUpgradeHelperProcess(context.Background(), UpgradeHelperLaunchOptions{
		State:        stateValue,
		HelperBinary: helperPath,
		StatePath:    statePath,
		LogPath:      logPath,
		Env:          append(append([]string{}, os.Environ()...), RuntimeEnvForState(stateValue)...),
	})
	if err != nil {
		stateValue.PendingUpgrade = nil
		stateValue.RollbackCandidate = nil
		_ = WriteState(statePath, stateValue)
		return "", err
	}
	stateValue.PendingUpgrade.HelperUnitName = strings.TrimSpace(launchResult.UnitName)
	if err := WriteState(statePath, stateValue); err != nil {
		return "", err
	}
	return resolvedSlot, nil
}

func importLocalBinaryForUpgrade(stateValue InstallState, sourceBinary, requestedSlot string) (string, error) {
	sourceBinary = filepath.Clean(strings.TrimSpace(sourceBinary))
	if sourceBinary == "" {
		return "", fmt.Errorf("upgrade source binary is required")
	}
	info, err := os.Stat(sourceBinary)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("upgrade source binary %q is not a regular file", sourceBinary)
	}

	slot, err := resolveLocalUpgradeSlot(sourceBinary, requestedSlot)
	if err != nil {
		return "", err
	}
	targetDir := filepath.Join(strings.TrimSpace(stateValue.VersionsRoot), slot)
	targetBinary := filepath.Join(targetDir, executableName(runtime.GOOS))
	if err := os.MkdirAll(strings.TrimSpace(stateValue.VersionsRoot), 0o755); err != nil {
		return "", err
	}
	if err := os.RemoveAll(targetDir); err != nil {
		return "", err
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return "", err
	}
	if err := copyFile(sourceBinary, targetBinary); err != nil {
		return "", err
	}
	return slot, nil
}

func resolveLocalUpgradeSlot(sourceBinary, requestedSlot string) (string, error) {
	requestedSlot = strings.TrimSpace(requestedSlot)
	if requestedSlot == "" {
		return deriveLocalUpgradeSlot(sourceBinary)
	}
	return validateUpgradeSlot(requestedSlot)
}

func deriveLocalUpgradeSlot(sourceBinary string) (string, error) {
	identity, err := relayruntime.BinaryIdentityForPath(sourceBinary, "")
	if err != nil {
		return "", err
	}
	fingerprint := strings.TrimPrefix(strings.TrimSpace(identity.BuildFingerprint), "sha256:")
	if fingerprint == "" {
		return "", fmt.Errorf("unable to derive local upgrade slot from %q", sourceBinary)
	}
	if len(fingerprint) > 12 {
		fingerprint = fingerprint[:12]
	}
	return validateUpgradeSlot("local-" + fingerprint)
}

func validateUpgradeSlot(slot string) (string, error) {
	slot = strings.TrimSpace(slot)
	if slot == "" {
		return "", fmt.Errorf("upgrade slot is required")
	}
	if slot == "." || slot == ".." || strings.Contains(slot, "/") || strings.Contains(slot, "\\") {
		return "", fmt.Errorf("upgrade slot %q is invalid", slot)
	}
	for _, r := range slot {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '.', r == '_', r == '-', r == '+':
		default:
			return "", fmt.Errorf("upgrade slot %q is invalid", slot)
		}
	}
	return slot, nil
}

func copyUpgradeHelperBinary(sourceBinary, statePath string) (string, error) {
	helperDir := filepath.Join(filepath.Dir(statePath), "upgrade-helper")
	if err := os.MkdirAll(helperDir, 0o755); err != nil {
		return "", err
	}
	name := "codex-remote-upgrade-helper"
	if ext := filepath.Ext(sourceBinary); ext != "" {
		name += ext
	}
	helperPath := filepath.Join(helperDir, fmt.Sprintf("%s-%d", name, time.Now().UTC().UnixNano()))
	if err := copyFile(sourceBinary, helperPath); err != nil {
		return "", err
	}
	return helperPath, nil
}

func localUpgradeLogPath(stateValue InstallState) string {
	if strings.TrimSpace(stateValue.StatePath) != "" {
		return filepath.Join(filepath.Dir(stateValue.StatePath), "logs", "codex-remote-relayd.log")
	}
	if strings.TrimSpace(stateValue.BaseDir) != "" {
		return filepath.Join(installLayoutForInstance(stateValue.BaseDir, stateValue.InstanceID).StateDir, "logs", "codex-remote-relayd.log")
	}
	paths, err := relayruntime.DefaultPaths()
	if err == nil && strings.TrimSpace(paths.DaemonLogFile) != "" {
		return paths.DaemonLogFile
	}
	return filepath.Join(os.TempDir(), "codex-remote-upgrade-helper.log")
}

func localPendingUpgradeBusy(pending *PendingUpgrade) bool {
	if pending == nil {
		return false
	}
	switch strings.TrimSpace(pending.Phase) {
	case PendingUpgradePhasePrepared,
		PendingUpgradePhaseSwitching,
		PendingUpgradePhaseObserving:
		return true
	default:
		return false
	}
}
