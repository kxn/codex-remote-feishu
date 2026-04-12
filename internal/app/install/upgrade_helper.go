package install

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/config"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

const (
	upgradeHelperStopDelay      = 1200 * time.Millisecond
	upgradeHelperStopGrace      = 15 * time.Second
	upgradeHelperStartupTimeout = 45 * time.Second
	upgradeHelperPollInterval   = 500 * time.Millisecond
	upgradeGatewayGraceWindow   = 10 * time.Second
)

var (
	upgradeHelperObserveFunc             = observeUpgrade
	upgradeHelperSleepFunc               = time.Sleep
	upgradeHelperStartDetachedDaemonFunc = relayruntime.StartDetachedDaemon
	upgradeHelperReadPIDFunc             = relayruntime.ReadPID
	upgradeHelperTerminateProcessFunc    = relayruntime.TerminateProcess
	upgradeHelperRemoveFileFunc          = os.Remove
)

func RunUpgradeHelper(args []string, _ io.Reader, stdout, _ io.Writer, _ string) error {
	flagSet := flag.NewFlagSet("upgrade-helper", flag.ContinueOnError)
	flagSet.SetOutput(stdout)
	statePath := flagSet.String("state-path", "", "path to install-state.json")
	if err := flagSet.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if strings.TrimSpace(*statePath) == "" {
		return errors.New("-state-path is required")
	}
	return RunUpgradeHelperWithStatePath(context.Background(), *statePath)
}

func RunUpgradeHelperWithStatePath(ctx context.Context, statePath string) error {
	stateValue, err := LoadState(statePath)
	if err != nil {
		return err
	}
	if stateValue.PendingUpgrade == nil {
		return errors.New("pending upgrade is missing")
	}
	if stateValue.PendingUpgrade.Phase != PendingUpgradePhasePrepared {
		return fmt.Errorf("pending upgrade phase %q is not prepared", stateValue.PendingUpgrade.Phase)
	}
	if stateValue.RollbackCandidate == nil || strings.TrimSpace(stateValue.RollbackCandidate.BinaryPath) == "" {
		return errors.New("rollback candidate is missing")
	}

	cfg, err := loadUpgradeHelperConfig(stateValue)
	if err != nil {
		return err
	}
	paths := RuntimePathsForState(stateValue)

	stateValue.PendingUpgrade.Phase = PendingUpgradePhaseSwitching
	if err := WriteState(statePath, stateValue); err != nil {
		return err
	}

	if err := stopCurrentDaemon(ctx, stateValue, paths); err != nil {
		return rollbackUpgradeState(ctx, statePath, stateValue, cfg, paths, fmt.Errorf("stop current daemon: %w", err))
	}
	if err := switchUpgradeBinary(&stateValue); err != nil {
		return rollbackUpgradeState(ctx, statePath, stateValue, cfg, paths, fmt.Errorf("switch target binary: %w", err))
	}

	stateValue.PendingUpgrade.Phase = PendingUpgradePhaseObserving
	if err := WriteState(statePath, stateValue); err != nil {
		return err
	}

	if _, err := startUpgradeDaemon(ctx, cfg, stateValue, paths); err != nil {
		return rollbackUpgradeState(ctx, statePath, stateValue, cfg, paths, fmt.Errorf("start upgraded daemon: %w", err))
	}
	if err := upgradeHelperObserveFunc(ctx, cfg); err != nil {
		return rollbackUpgradeState(ctx, statePath, stateValue, cfg, paths, err)
	}

	stateValue.CurrentVersion = stateValue.PendingUpgrade.TargetVersion
	stateValue.CurrentSlot = firstNonEmpty(strings.TrimSpace(stateValue.PendingUpgrade.TargetSlot), strings.TrimSpace(stateValue.PendingUpgrade.TargetVersion))
	stateValue.LastKnownLatestVersion = stateValue.PendingUpgrade.TargetVersion
	stateValue.PendingUpgrade.Phase = PendingUpgradePhaseCommitted
	return WriteState(statePath, stateValue)
}

type upgradeHelperBootstrapState struct {
	SetupRequired bool `json:"setupRequired"`
	Gateways      []struct {
		State string `json:"state"`
	} `json:"gateways"`
}

func observeUpgrade(ctx context.Context, cfg config.LoadedAppConfig) error {
	adminURL := fallbackAdminURL(cfg.Config)
	healthURL := strings.TrimRight(adminURL, "/") + "/healthz"
	bootstrapURL := strings.TrimRight(adminURL, "/") + "/api/admin/bootstrap-state"
	runtimeStatusURL := strings.TrimRight(adminURL, "/") + "/api/admin/runtime-status"
	statusURL := strings.TrimRight(adminURL, "/") + "/v1/status"

	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			Proxy: nil,
		},
	}

	deadline := time.Now().Add(upgradeHelperStartupTimeout)
	var coreHealthyAt time.Time
	for time.Now().Before(deadline) {
		coreHealthy, gatewayReady, err := probeUpgradeHealth(ctx, client, healthURL, bootstrapURL, runtimeStatusURL, statusURL)
		if err == nil && coreHealthy {
			if coreHealthyAt.IsZero() {
				coreHealthyAt = time.Now()
			}
			if gatewayReady {
				return nil
			}
			if time.Since(coreHealthyAt) >= upgradeGatewayGraceWindow {
				return errors.New("feishu gateway did not recover within 10s after core health was restored")
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(upgradeHelperPollInterval):
		}
	}
	return errors.New("timed out waiting for upgraded daemon to become healthy")
}

func probeUpgradeHealth(ctx context.Context, client *http.Client, healthURL, bootstrapURL, runtimeStatusURL, statusURL string) (bool, bool, error) {
	if err := expectHTTPStatus(ctx, client, healthURL, http.StatusOK); err != nil {
		return false, false, err
	}
	var bootstrapState upgradeHelperBootstrapState
	if err := fetchJSON(ctx, client, bootstrapURL, &bootstrapState); err != nil {
		return false, false, err
	}
	if bootstrapState.SetupRequired {
		return false, false, errors.New("upgraded daemon unexpectedly returned to setup-required state")
	}
	if err := expectHTTPStatus(ctx, client, runtimeStatusURL, http.StatusOK); err != nil {
		return false, false, err
	}
	if err := expectHTTPStatus(ctx, client, statusURL, http.StatusOK); err != nil {
		return false, false, err
	}
	return true, gatewayRecovered(bootstrapState), nil
}

func gatewayRecovered(state upgradeHelperBootstrapState) bool {
	if len(state.Gateways) == 0 {
		return true
	}
	hasConnected := false
	for _, gateway := range state.Gateways {
		switch strings.TrimSpace(strings.ToLower(gateway.State)) {
		case "connected":
			hasConnected = true
		case "disabled":
		default:
			return false
		}
	}
	return hasConnected
}

func expectHTTPStatus(ctx context.Context, client *http.Client, rawURL string, want int) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != want {
		return fmt.Errorf("%s returned http %d", rawURL, resp.StatusCode)
	}
	return nil
}

func fetchJSON(ctx context.Context, client *http.Client, rawURL string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s returned http %d", rawURL, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func switchUpgradeBinary(stateValue *InstallState) error {
	if stateValue == nil || stateValue.PendingUpgrade == nil {
		return errors.New("pending upgrade is missing")
	}
	targetBinary := strings.TrimSpace(stateValue.PendingUpgrade.TargetBinaryPath)
	if targetBinary == "" {
		targetSlot := firstNonEmpty(strings.TrimSpace(stateValue.PendingUpgrade.TargetSlot), strings.TrimSpace(stateValue.PendingUpgrade.TargetVersion))
		targetBinary = filepath.Join(strings.TrimSpace(stateValue.VersionsRoot), targetSlot, executableName(runtime.GOOS))
	}
	if _, err := os.Stat(targetBinary); err != nil {
		return err
	}
	if err := copyFile(targetBinary, stateValue.CurrentBinaryPath); err != nil {
		return err
	}
	if stateValue.PendingUpgrade.Source == UpgradeSourceRelease {
		_ = updateCurrentReleaseLink(stateValue.VersionsRoot, firstNonEmpty(strings.TrimSpace(stateValue.PendingUpgrade.TargetSlot), strings.TrimSpace(stateValue.PendingUpgrade.TargetVersion)))
	}
	return nil
}

func stopCurrentDaemon(ctx context.Context, stateValue InstallState, paths relayruntime.Paths) error {
	upgradeHelperSleepFunc(upgradeHelperStopDelay)
	if effectiveServiceManager(stateValue) == ServiceManagerSystemdUser {
		return systemdUserStop(ctx, stateValue)
	}

	pid, err := upgradeHelperReadPIDFunc(paths.PIDFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if pid <= 0 {
		return nil
	}
	if err := upgradeHelperTerminateProcessFunc(pid, upgradeHelperStopGrace); err != nil {
		return err
	}
	_ = upgradeHelperRemoveFileFunc(paths.PIDFile)
	_ = upgradeHelperRemoveFileFunc(paths.IdentityFile)
	return nil
}

func startUpgradeDaemon(ctx context.Context, cfg config.LoadedAppConfig, stateValue InstallState, paths relayruntime.Paths) (int, error) {
	if effectiveServiceManager(stateValue) == ServiceManagerSystemdUser {
		return 0, systemdUserStart(ctx, stateValue)
	}
	env := config.FilterEnvWithoutProxy(os.Environ())
	if cfg.Config.Feishu.UseSystemProxy {
		env = append(env, config.CaptureProxyEnv()...)
	}
	return upgradeHelperStartDetachedDaemonFunc(relayruntime.LaunchOptions{
		BinaryPath: stateValue.CurrentBinaryPath,
		ConfigPath: firstNonEmpty(strings.TrimSpace(stateValue.ConfigPath), cfg.Path),
		Env:        env,
		Paths:      paths,
	})
}

func rollbackUpgradeState(ctx context.Context, statePath string, stateValue InstallState, cfg config.LoadedAppConfig, paths relayruntime.Paths, cause error) error {
	stopErr := stopCurrentDaemon(ctx, stateValue, paths)
	if stateValue.RollbackCandidate != nil {
		if err := restoreConfigSnapshots(stateValue.RollbackCandidate.ConfigSnapshots); err != nil {
			stateValue.PendingUpgrade.Phase = PendingUpgradePhaseFailed
			_ = WriteState(statePath, stateValue)
			return fmt.Errorf("rollback config restore failed after %v: %w", cause, err)
		}
	}
	if stateValue.RollbackCandidate != nil && strings.TrimSpace(stateValue.RollbackCandidate.BinaryPath) != "" {
		if err := copyFile(stateValue.RollbackCandidate.BinaryPath, stateValue.CurrentBinaryPath); err != nil {
			stateValue.PendingUpgrade.Phase = PendingUpgradePhaseFailed
			_ = WriteState(statePath, stateValue)
			return fmt.Errorf("rollback copy failed after %v: %w", cause, err)
		}
	}
	if stateValue.RollbackCandidate != nil {
		stateValue.CurrentVersion = stateValue.RollbackCandidate.Version
		stateValue.CurrentSlot = stateValue.RollbackCandidate.Version
	}
	stateValue.PendingUpgrade.Phase = PendingUpgradePhaseRolledBack
	if err := WriteState(statePath, stateValue); err != nil {
		return err
	}
	if _, err := startUpgradeDaemon(ctx, cfg, stateValue, paths); err != nil {
		stateValue.PendingUpgrade.Phase = PendingUpgradePhaseFailed
		_ = WriteState(statePath, stateValue)
		return fmt.Errorf("restart rollback daemon failed after %v: %w", cause, err)
	}
	if stopErr != nil {
		return fmt.Errorf("%w (rollback stop warning: %v)", cause, stopErr)
	}
	return cause
}

func loadUpgradeHelperConfig(stateValue InstallState) (config.LoadedAppConfig, error) {
	if strings.TrimSpace(stateValue.ConfigPath) != "" {
		return config.LoadAppConfigAtPath(stateValue.ConfigPath)
	}
	return config.LoadAppConfig()
}
