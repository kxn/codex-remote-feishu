package install

import (
	"context"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"runtime"
)

func RunMain(args []string, stdin io.Reader, stdout, stderr io.Writer, version string) error {
	defaults, err := DetectPlatformDefaults()
	if err != nil {
		return err
	}

	defaultBinary := filepath.Join(".", "bin", executableName(runtime.GOOS))
	flagSet := flag.NewFlagSet("install", flag.ContinueOnError)
	flagSet.SetOutput(stdout)

	interactive := flagSet.Bool("interactive", false, "run interactive installer wizard")
	bootstrapOnly := flagSet.Bool("bootstrap-only", false, "install binary and config only; do not patch VS Code integration")
	startDaemon := flagSet.Bool("start-daemon", false, "ensure the local daemon is running after install")
	baseDir := flagSet.String("base-dir", defaults.BaseDir, "base directory for config and install state")
	installBinDir := flagSet.String("install-bin-dir", defaults.InstallBinDir, "target directory for installed binary; empty keeps source path")
	binaryPath := flagSet.String("binary", defaultBinary, "codex-remote binary source path")
	relayURL := flagSet.String("relay-url", "", "relay websocket url; empty preserves existing or default config")
	codexBinary := flagSet.String("codex-binary", "", "real codex binary path; empty keeps wrapper default and lets managed_shim auto-resolve codex.real")
	integrationMode := flagSet.String("integration", "auto", "integration mode: auto, editor_settings, managed_shim, both, or comma list")
	feishuAppID := flagSet.String("feishu-app-id", "", "feishu app id")
	feishuSecret := flagSet.String("feishu-app-secret", "", "feishu app secret")
	useSystemProxy := flagSet.Bool("use-system-proxy", false, "whether relayd should use system proxy for Feishu API")
	settingsPath := flagSet.String("vscode-settings", defaults.VSCodeSettingsPath, "vscode settings path")
	bundleEntrypoint := flagSet.String("bundle-entrypoint", recommendedBundleEntrypoint(defaults), "VS Code extension bundle codex entrypoint path")

	legacyWrapperBinary := flagSet.String("wrapper-binary", "", "deprecated: use -binary")
	legacyRelaydBinary := flagSet.String("relayd-binary", "", "deprecated: use -binary")

	if err := flagSet.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}

	if *interactive && *bootstrapOnly {
		return fmt.Errorf("-interactive cannot be combined with -bootstrap-only")
	}

	var integrations []WrapperIntegrationMode
	if !*bootstrapOnly {
		integrations, err = ParseIntegrations(*integrationMode, defaults.GOOS)
		if err != nil {
			return err
		}
	}

	service := NewService()
	opts := Options{
		BaseDir:            *baseDir,
		InstallBinDir:      *installBinDir,
		BinaryPath:         *binaryPath,
		WrapperBinary:      *legacyWrapperBinary,
		RelaydBinary:       *legacyRelaydBinary,
		RelayServerURL:     *relayURL,
		CodexRealBinary:    *codexBinary,
		VSCodeSettingsPath: *settingsPath,
		BundleEntrypoint:   *bundleEntrypoint,
		FeishuAppID:        *feishuAppID,
		FeishuAppSecret:    *feishuSecret,
		UseSystemProxy:     *useSystemProxy,
		Integrations:       integrations,
		BootstrapOnly:      *bootstrapOnly,
	}
	if *interactive {
		opts, err = RunInteractiveWizard(stdin, stdout, defaults, opts)
		if err != nil {
			return err
		}
	}

	state, err := service.Bootstrap(opts)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(
		stdout,
		"installed config: %s\nstate: %s\nbinary: %s\nintegrations: %v\n",
		state.ConfigPath,
		state.StatePath,
		state.InstalledBinary,
		state.Integrations,
	)
	if err != nil {
		return err
	}
	if !*startDaemon {
		return nil
	}

	status, err := ensureDaemonReady(context.Background(), state, version)
	if err != nil {
		if stderr != nil {
			_, _ = fmt.Fprintf(stderr, "daemon startup log: %s\n", status.LogPath)
		}
		return err
	}
	if _, err := fmt.Fprintf(stdout, "daemon: ready\nweb admin: %s\n", status.AdminURL); err != nil {
		return err
	}
	if status.SetupRequired {
		if _, err := fmt.Fprintf(stdout, "web setup: %s\n", status.SetupURL); err != nil {
			return err
		}
	}
	if status.LogPath != "" {
		_, err = fmt.Fprintf(stdout, "logs: %s\n", status.LogPath)
	}
	return err
}

func executableName(goos string) string {
	if goos == "windows" {
		return "codex-remote.exe"
	}
	return "codex-remote"
}
