package install

import (
	"bufio"
	"context"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/config"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

type DaemonReadyStatus struct {
	AdminURL      string
	SetupURL      string
	SetupRequired bool
	LogPath       string
}

func EnsureDaemonReadyFromStatePath(ctx context.Context, statePath, version string) (DaemonReadyStatus, error) {
	state, err := loadServiceState(statePath)
	if err != nil {
		return DaemonReadyStatus{}, err
	}
	return ensureDaemonReady(ctx, state, version)
}

func currentExecutablePathOrEmpty() string {
	path, err := executablePath()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(path)
}

func ensureDaemonReady(ctx context.Context, state InstallState, version string) (DaemonReadyStatus, error) {
	paths := RuntimePathsForState(state)
	loaded, err := config.LoadAppConfigAtPath(state.ConfigPath)
	if err != nil {
		return DaemonReadyStatus{}, err
	}
	// Prefer the canonical current-binary path recorded in state (LoadState
	// promotes legacy installedBinary values into CurrentBinaryPath). The
	// running process executable is only a fallback: ensureDaemonReady is also
	// called from install flows whose process runs from the release/dist
	// directory, and using that path as the daemon binary would start the
	// daemon outside the installed entry point (and pin the release directory
	// while it is running).
	binaryPath := xutil.FirstNonEmpty(strings.TrimSpace(state.CurrentBinaryPath), currentExecutablePathOrEmpty())
	identity, err := relayruntime.BinaryIdentityForPath(binaryPath, version)
	if err != nil {
		return DaemonReadyStatus{}, err
	}

	manager := relayruntime.NewManager(relayruntime.ManagerConfig{
		RelayServerURL:       strings.TrimSpace(loaded.Config.Relay.ServerURL),
		Identity:             identity,
		ConfigPath:           state.ConfigPath,
		Paths:                paths,
		DaemonBinaryPath:     binaryPath,
		DaemonUseSystemProxy: loaded.Config.Feishu.UseSystemProxy,
		CapturedProxyEnv:     config.CaptureProxyEnv(),
	})
	if driver, ok := managedServiceDriverForManager(effectiveServiceManager(state)); ok {
		manager = relayruntime.NewManager(relayruntime.ManagerConfig{
			RelayServerURL: strings.TrimSpace(loaded.Config.Relay.ServerURL),
			Identity:       identity,
			ConfigPath:     state.ConfigPath,
			Paths:          paths,
			StartFunc: func(ctx context.Context) (int, error) {
				updated, err := driver.Install(ctx, state)
				if err != nil {
					return 0, err
				}
				if driver.EnableBeforeEnsureReady {
					if err := driver.Enable(ctx, updated); err != nil {
						return 0, err
					}
				}
				return 0, driver.Start(ctx, updated)
			},
			RestartFunc: func(ctx context.Context) error {
				updated, err := driver.Install(ctx, state)
				if err != nil {
					return err
				}
				if driver.EnableBeforeEnsureReady {
					if err := driver.Enable(ctx, updated); err != nil {
						return err
					}
				}
				return driver.Restart(ctx, updated)
			},
		})
	}
	if err := manager.EnsureReady(ctx); err != nil {
		if isManagedServiceManager(state) {
			return fallbackDaemonStatus(loaded.Config), err
		}
		return DaemonReadyStatus{LogPath: paths.DaemonLogFile}, err
	}
	if isManagedServiceManager(state) {
		return fallbackDaemonStatus(loaded.Config), nil
	}
	return discoverDaemonStatus(paths, loaded.Config), nil
}

func fallbackDaemonStatus(cfg config.AppConfig) DaemonReadyStatus {
	return DaemonReadyStatus{
		AdminURL:      fallbackAdminURL(cfg),
		SetupURL:      fallbackSetupURL(cfg),
		SetupRequired: configuredRuntimeAppCount(cfg) == 0,
	}
}

func discoverDaemonStatus(paths relayruntime.Paths, cfg config.AppConfig) DaemonReadyStatus {
	status := DaemonReadyStatus{
		AdminURL:      fallbackAdminURL(cfg),
		SetupURL:      fallbackSetupURL(cfg),
		SetupRequired: configuredRuntimeAppCount(cfg) == 0,
		LogPath:       paths.DaemonLogFile,
	}

	file, err := os.Open(paths.DaemonLogFile)
	if err != nil {
		return status
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if index := strings.Index(line, "web admin: "); index >= 0 {
			status.AdminURL = strings.TrimSpace(line[index+len("web admin: "):])
		}
		if index := strings.Index(line, "web setup: "); index >= 0 {
			status.SetupURL = strings.TrimSpace(line[index+len("web setup: "):])
			status.SetupRequired = true
		}
		if strings.Contains(line, "startup state: ready;") || strings.Contains(line, "startup state: ready_degraded;") {
			status.SetupRequired = false
		}
	}
	return status
}

func fallbackAdminURL(cfg config.AppConfig) string {
	return "http://" + net.JoinHostPort(displayAdminHost(cfg.Admin.ListenHost), adminPort(cfg)) + "/admin/"
}

func fallbackSetupURL(cfg config.AppConfig) string {
	return "http://" + net.JoinHostPort(displayAdminHost(cfg.Admin.ListenHost), adminPort(cfg)) + "/setup"
}

func displayAdminHost(host string) string {
	trimmed := strings.TrimSpace(strings.Trim(host, "[]"))
	if trimmed == "" || strings.EqualFold(trimmed, "localhost") || trimmed == "0.0.0.0" || trimmed == "::" {
		return "localhost"
	}
	ip := net.ParseIP(trimmed)
	if ip != nil && ip.IsLoopback() {
		return "localhost"
	}
	return trimmed
}

func adminPort(cfg config.AppConfig) string {
	port := cfg.Admin.ListenPort
	if port <= 0 {
		port = 9501
	}
	return strconv.Itoa(port)
}

func configuredRuntimeAppCount(cfg config.AppConfig) int {
	count := 0
	for _, app := range cfg.Feishu.Apps {
		if strings.TrimSpace(app.AppID) == "" || strings.TrimSpace(app.AppSecret) == "" {
			continue
		}
		count++
	}
	return count
}
