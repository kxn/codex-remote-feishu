package daemon

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/install"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

type upgradeStartRequest struct {
	State            install.InstallState
	GatewayID        string
	SurfaceSessionID string
	SourceMessageID  string
}

func (a *App) beginPendingUpgradeLocked(command control.DaemonCommand, stateValue install.InstallState) []control.UIEvent {
	if stateValue.PendingUpgrade == nil || strings.TrimSpace(stateValue.PendingUpgrade.TargetVersion) == "" {
		return []control.UIEvent{upgradeNoticeEvent(command.SurfaceSessionID, "upgrade_missing_candidate", "当前没有可继续的升级候选，请先发送 /upgrade latest 检查最新版本。")}
	}
	now := time.Now().UTC()
	stateValue.PendingUpgrade.GatewayID = firstNonEmpty(strings.TrimSpace(command.GatewayID), a.service.SurfaceGatewayID(command.SurfaceSessionID))
	stateValue.PendingUpgrade.SurfaceSessionID = command.SurfaceSessionID
	stateValue.PendingUpgrade.ChatID = a.service.SurfaceChatID(command.SurfaceSessionID)
	stateValue.PendingUpgrade.ActorUserID = a.service.SurfaceActorUserID(command.SurfaceSessionID)
	stateValue.PendingUpgrade.SourceMessageID = command.SourceMessageID
	if stateValue.PendingUpgrade.RequestedAt == nil {
		stateValue.PendingUpgrade.RequestedAt = &now
	}
	if err := a.writeUpgradeStateLocked(stateValue); err != nil {
		return []control.UIEvent{upgradeNoticeEvent(command.SurfaceSessionID, "upgrade_prepare_failed", fmt.Sprintf("写入升级事务失败：%v", err))}
	}
	a.upgradeStartInFlight = true
	go a.runPendingUpgradeStart(upgradeStartRequest{
		State:            stateValue,
		GatewayID:        stateValue.PendingUpgrade.GatewayID,
		SurfaceSessionID: stateValue.PendingUpgrade.SurfaceSessionID,
		SourceMessageID:  stateValue.PendingUpgrade.SourceMessageID,
	})
	return []control.UIEvent{upgradeNoticeEvent(command.SurfaceSessionID, "upgrade_prepare_started", fmt.Sprintf("正在准备升级到 %s，服务会短暂重启。", firstNonEmpty(strings.TrimSpace(stateValue.PendingUpgrade.TargetSlot), strings.TrimSpace(stateValue.PendingUpgrade.TargetVersion))))}
}

func (a *App) runPendingUpgradeStart(request upgradeStartRequest) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	stateValue := request.State
	targetVersion := strings.TrimSpace(stateValue.PendingUpgrade.TargetVersion)
	targetBinary, err := install.EnsureReleaseBinary(ctx, install.ReleaseBinaryOptions{
		Repository:   strings.TrimSpace(os.Getenv("CODEX_REMOTE_REPO")),
		BaseURL:      strings.TrimSpace(os.Getenv("CODEX_REMOTE_BASE_URL")),
		Version:      targetVersion,
		VersionsRoot: stateValue.VersionsRoot,
	})
	if err != nil {
		a.finishUpgradeStartFailure(request, fmt.Errorf("下载目标版本失败：%w", err))
		return
	}

	rollbackCandidate, err := install.PrepareRollbackCandidate(stateValue, targetVersion)
	if err != nil {
		a.finishUpgradeStartFailure(request, fmt.Errorf("准备回滚候选失败：%w", err))
		return
	}
	identity, err := relayruntime.BinaryIdentityForPath(stateValue.CurrentBinaryPath, stateValue.CurrentVersion)
	if err != nil {
		a.finishUpgradeStartFailure(request, fmt.Errorf("读取当前版本指纹失败：%w", err))
		return
	}
	rollbackCandidate.Fingerprint = identity.BuildFingerprint
	stateValue.RollbackCandidate = rollbackCandidate
	stateValue.PendingUpgrade.Phase = install.PendingUpgradePhasePrepared
	stateValue.PendingUpgrade.Source = install.UpgradeSourceRelease
	stateValue.PendingUpgrade.TargetSlot = firstNonEmpty(strings.TrimSpace(stateValue.PendingUpgrade.TargetSlot), targetVersion)
	stateValue.PendingUpgrade.TargetBinaryPath = targetBinary

	a.mu.Lock()
	logPath := a.upgradeHelperLogPathLocked()
	if err := a.writeUpgradeStateLocked(stateValue); err != nil {
		a.upgradeStartInFlight = false
		a.mu.Unlock()
		a.finishUpgradeStartFailure(request, fmt.Errorf("写入 prepared journal 失败：%w", err))
		return
	}
	helperPath, err := a.copyUpgradeHelperBinaryLocked(stateValue)
	if err != nil {
		a.upgradeStartInFlight = false
		a.mu.Unlock()
		a.finishUpgradeStartFailure(request, fmt.Errorf("复制 upgrade helper 失败：%w", err))
		return
	}
	a.mu.Unlock()

	launchResult, err := install.StartUpgradeHelperProcess(context.Background(), install.UpgradeHelperLaunchOptions{
		State:        stateValue,
		HelperBinary: helperPath,
		StatePath:    stateValue.StatePath,
		LogPath:      logPath,
		Env:          append(append([]string{}, os.Environ()...), install.RuntimeEnvForState(stateValue)...),
	})
	if err != nil {
		a.finishUpgradeStartFailure(request, fmt.Errorf("启动 upgrade helper 失败：%w", err))
		return
	}
	a.mu.Lock()
	stateValue.PendingUpgrade.HelperUnitName = strings.TrimSpace(launchResult.UnitName)
	_ = a.writeUpgradeStateLocked(stateValue)
	a.mu.Unlock()

	_ = targetBinary
	a.mu.Lock()
	a.upgradeStartInFlight = false
	a.mu.Unlock()
}

func (a *App) finishUpgradeStartFailure(request upgradeStartRequest, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.upgradeStartInFlight = false

	stateValue, ok, loadErr := a.loadUpgradeStateLocked(true)
	if loadErr == nil && ok && stateValue.PendingUpgrade != nil {
		stateValue.PendingUpgrade = nil
		_ = a.writeUpgradeStateLocked(stateValue)
	}
	if request.SurfaceSessionID != "" {
		a.handleUIEvents(context.Background(), []control.UIEvent{
			upgradeNoticeEvent(request.SurfaceSessionID, "upgrade_prepare_failed", err.Error()),
		})
	}
}

func (a *App) copyUpgradeHelperBinaryLocked(stateValue install.InstallState) (string, error) {
	helperDir := filepath.Join(filepath.Dir(a.installStatePath()), "upgrade-helper")
	if err := os.MkdirAll(helperDir, 0o755); err != nil {
		return "", err
	}
	name := "codex-remote-upgrade-helper"
	sourceBinary := firstNonEmpty(strings.TrimSpace(stateValue.CurrentBinaryPath), strings.TrimSpace(a.serverIdentity.BinaryPath))
	if ext := filepath.Ext(sourceBinary); ext != "" {
		name += ext
	}
	helperPath := filepath.Join(helperDir, fmt.Sprintf("%s-%d", name, time.Now().UTC().UnixNano()))
	if err := copyFileLocal(sourceBinary, helperPath); err != nil {
		return "", err
	}
	return helperPath, nil
}

func (a *App) upgradeHelperLogPathLocked() string {
	if strings.TrimSpace(a.headlessRuntime.Paths.DaemonLogFile) != "" {
		return a.headlessRuntime.Paths.DaemonLogFile
	}
	paths, err := relayruntime.DefaultPaths()
	if err != nil {
		return filepath.Join(filepath.Dir(a.installStatePath()), "upgrade-helper.log")
	}
	return paths.DaemonLogFile
}

func (a *App) maybeFlushUpgradeResultLocked(now time.Time) []control.UIEvent {
	if a.upgradeResultScanEvery <= 0 {
		return nil
	}
	if !a.upgradeNextResultScan.IsZero() && now.Before(a.upgradeNextResultScan) {
		return nil
	}
	a.upgradeNextResultScan = now.Add(a.upgradeResultScanEvery)

	stateValue, ok, err := a.loadUpgradeStateLocked(false)
	if err != nil || !ok || stateValue.PendingUpgrade == nil {
		return nil
	}
	switch stateValue.PendingUpgrade.Phase {
	case install.PendingUpgradePhaseCommitted, install.PendingUpgradePhaseRolledBack, install.PendingUpgradePhaseFailed:
	default:
		return nil
	}
	pending := stateValue.PendingUpgrade
	if strings.TrimSpace(pending.SurfaceSessionID) == "" || strings.TrimSpace(pending.ChatID) == "" {
		return nil
	}
	a.service.MaterializeSurface(pending.SurfaceSessionID, pending.GatewayID, pending.ChatID, pending.ActorUserID)
	events := []control.UIEvent{debugNoticeEvent(pending.SurfaceSessionID, "debug_upgrade_result", buildUpgradeResultText(stateValue))}
	stateValue.PendingUpgrade = nil
	if err := a.writeUpgradeStateLocked(stateValue); err != nil {
		return nil
	}
	return events
}

func buildUpgradeResultText(stateValue install.InstallState) string {
	pending := stateValue.PendingUpgrade
	if pending == nil {
		return "升级结果已就绪。"
	}
	switch pending.Phase {
	case install.PendingUpgradePhaseCommitted:
		return fmt.Sprintf("已升级到 %s。", firstNonEmpty(strings.TrimSpace(stateValue.CurrentVersion), strings.TrimSpace(pending.TargetSlot), strings.TrimSpace(pending.TargetVersion)))
	case install.PendingUpgradePhaseRolledBack:
		return fmt.Sprintf("升级到 %s 失败，已自动回滚到 %s。", firstNonEmpty(strings.TrimSpace(pending.TargetSlot), strings.TrimSpace(pending.TargetVersion), "unknown"), firstNonEmpty(strings.TrimSpace(stateValue.CurrentVersion), "unknown"))
	default:
		return "升级失败。"
	}
}

func copyFileLocal(sourcePath, targetPath string) error {
	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	info, err := sourceFile.Stat()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}
	targetFile, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	defer targetFile.Close()
	if _, err := io.Copy(targetFile, sourceFile); err != nil {
		return err
	}
	return targetFile.Chmod(info.Mode().Perm())
}
