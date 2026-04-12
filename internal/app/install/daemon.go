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
)

type daemonStatus struct {
	AdminURL      string
	SetupURL      string
	SetupRequired bool
	LogPath       string
}

func ensureDaemonReady(ctx context.Context, state InstallState, version string) (daemonStatus, error) {
	paths := RuntimePathsForState(state)
	loaded, err := config.LoadAppConfigAtPath(state.ConfigPath)
	if err != nil {
		return daemonStatus{}, err
	}
	identity, err := relayruntime.BinaryIdentityForPath(state.InstalledBinary, version)
	if err != nil {
		return daemonStatus{}, err
	}

	manager := relayruntime.NewManager(relayruntime.ManagerConfig{
		RelayServerURL:       strings.TrimSpace(loaded.Config.Relay.ServerURL),
		Identity:             identity,
		ConfigPath:           state.ConfigPath,
		Paths:                paths,
		DaemonBinaryPath:     state.InstalledBinary,
		DaemonUseSystemProxy: loaded.Config.Feishu.UseSystemProxy,
		CapturedProxyEnv:     config.CaptureProxyEnv(),
	})
	if effectiveServiceManager(state) == ServiceManagerSystemdUser {
		manager = relayruntime.NewManager(relayruntime.ManagerConfig{
			RelayServerURL: strings.TrimSpace(loaded.Config.Relay.ServerURL),
			Identity:       identity,
			ConfigPath:     state.ConfigPath,
			Paths:          paths,
			StartFunc: func(ctx context.Context) (int, error) {
				if _, err := installSystemdUserUnit(ctx, state); err != nil {
					return 0, err
				}
				if err := systemdUserEnable(ctx, state); err != nil {
					return 0, err
				}
				return 0, systemdUserStart(ctx, state)
			},
			RestartFunc: func(ctx context.Context) error {
				if _, err := installSystemdUserUnit(ctx, state); err != nil {
					return err
				}
				if err := systemdUserEnable(ctx, state); err != nil {
					return err
				}
				return systemdUserRestart(ctx, state)
			},
		})
	}
	if err := manager.EnsureReady(ctx); err != nil {
		if effectiveServiceManager(state) == ServiceManagerSystemdUser {
			return fallbackDaemonStatus(loaded.Config), err
		}
		return daemonStatus{LogPath: paths.DaemonLogFile}, err
	}
	if effectiveServiceManager(state) == ServiceManagerSystemdUser {
		return fallbackDaemonStatus(loaded.Config), nil
	}
	return discoverDaemonStatus(paths, loaded.Config), nil
}

func fallbackDaemonStatus(cfg config.AppConfig) daemonStatus {
	return daemonStatus{
		AdminURL:      fallbackAdminURL(cfg),
		SetupURL:      fallbackSetupURL(cfg),
		SetupRequired: configuredRuntimeAppCount(cfg) == 0,
	}
}

func discoverDaemonStatus(paths relayruntime.Paths, cfg config.AppConfig) daemonStatus {
	status := daemonStatus{
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
	return "http://" + net.JoinHostPort(displayAdminHost(cfg.Admin.ListenHost), adminPort(cfg)) + "/"
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
