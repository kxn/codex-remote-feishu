package install

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var executablePath = os.Executable
var sourceBinaryValidator = validateSourceBinary

func RunMain(args []string, stdin io.Reader, stdout, stderr io.Writer, version string) error {
	defaults, err := DetectPlatformDefaults()
	if err != nil {
		return err
	}

	defaultBinary := defaultBinaryPath(runtime.GOOS)
	flagSet := flag.NewFlagSet("install", flag.ContinueOnError)
	flagSet.SetOutput(stdout)

	interactive := flagSet.Bool("interactive", false, "run interactive installer wizard")
	bootstrapOnly := flagSet.Bool("bootstrap-only", false, "install binary and config only; do not patch VS Code integration")
	instanceIDFlag := flagSet.String("instance", defaultInstanceID, "install instance: stable or debug")
	startDaemon := flagSet.Bool("start-daemon", false, "ensure the local daemon is running after install")
	baseDir := flagSet.String("base-dir", defaults.BaseDir, "base directory for config and install state")
	installBinDir := flagSet.String("install-bin-dir", defaults.InstallBinDir, "target directory for installed binary; empty keeps source path")
	binaryPath := flagSet.String("binary", defaultBinary, "codex-remote binary source path")
	installSource := flagSet.String("install-source", "", "install source metadata: release or repo")
	currentTrack := flagSet.String("current-track", "", "current upgrade track metadata: production, beta, or alpha")
	currentVersion := flagSet.String("current-version", version, "current binary version metadata")
	versionsRoot := flagSet.String("versions-root", "", "version cache root metadata")
	currentSlot := flagSet.String("current-slot", "", "current version slot metadata")
	serviceManagerFlag := flagSet.String("service-manager", string(ServiceManagerDetached), "daemon lifecycle manager: detached or systemd_user (linux only)")
	relayURL := flagSet.String("relay-url", "", "relay websocket url; empty preserves existing or default config")
	codexBinary := flagSet.String("codex-binary", "", "real codex binary path; empty keeps wrapper default and lets managed_shim auto-resolve codex.real")
	integrationMode := flagSet.String("integration", "auto", "integration mode: auto or managed_shim; legacy editor_settings/both inputs are accepted and normalized to managed_shim")
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
	serviceManager, err := ParseServiceManager(*serviceManagerFlag, defaults.GOOS)
	if err != nil {
		return err
	}
	instanceID, err := parseInstanceID(*instanceIDFlag)
	if err != nil {
		return err
	}
	resolvedInstallBinDir := *installBinDir
	if filepath.Clean(strings.TrimSpace(resolvedInstallBinDir)) == filepath.Clean(strings.TrimSpace(defaults.InstallBinDir)) {
		resolvedInstallBinDir = defaultInstallBinDirForInstance(defaults.GOOS, defaults.HomeDir, instanceID)
	}

	service := NewService()
	opts := Options{
		InstanceID:         instanceID,
		BaseDir:            *baseDir,
		InstallBinDir:      resolvedInstallBinDir,
		BinaryPath:         *binaryPath,
		ServiceManager:     serviceManager,
		CurrentVersion:     *currentVersion,
		InstallSource:      InstallSource(*installSource),
		CurrentTrack:       ReleaseTrack(*currentTrack),
		VersionsRoot:       *versionsRoot,
		CurrentSlot:        *currentSlot,
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

	sourceBinary, err := resolveBinaryPath(opts)
	if err != nil {
		return err
	}
	if err := sourceBinaryValidator(sourceBinary); err != nil {
		return fmt.Errorf("validate binary source %q: %w", sourceBinary, err)
	}
	opts.BinaryPath = sourceBinary

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

func resolveUpgradeHelperBinary(statePath string) (string, error) {
	stateValue, err := LoadState(statePath)
	if err == nil {
		helperBinary := firstNonEmpty(
			strings.TrimSpace(stateValue.CurrentBinaryPath),
			strings.TrimSpace(stateValue.InstalledBinary),
		)
		if helperBinary != "" {
			return helperBinary, nil
		}
	}
	return executablePath()
}

func defaultBinaryPath(goos string) string {
	name := executableName(goos)
	path, err := executablePath()
	if err == nil {
		path = filepath.Clean(strings.TrimSpace(path))
		if path != "" && strings.EqualFold(filepath.Base(path), name) {
			return path
		}
	}
	return filepath.Join(".", "bin", name)
}

func validateSourceBinary(path string) error {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" {
		return fmt.Errorf("binary path is empty")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, path, "version")
	output, err := cmd.CombinedOutput()
	trimmed := strings.TrimSpace(string(output))
	if err != nil {
		if trimmed == "" {
			return err
		}
		return fmt.Errorf("%w: %s", err, trimmed)
	}
	if trimmed == "" {
		return fmt.Errorf("empty version output")
	}
	return nil
}

func executableName(goos string) string {
	if goos == "windows" {
		return "codex-remote.exe"
	}
	return "codex-remote"
}
