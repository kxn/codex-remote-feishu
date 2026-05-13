package install

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	packagedInstallStopGrace    = 15 * time.Second
	packagedInstallPollInterval = 500 * time.Millisecond
)

var packagedInstallEnsureReadyFunc = EnsureDaemonReadyFromStatePath

type packagedInstallMode string

const (
	packagedInstallModeFirstInstall packagedInstallMode = "first_install"
	packagedInstallModeRepair       packagedInstallMode = "repair"
)

type PackagedInstallResult struct {
	OK              bool   `json:"ok"`
	Mode            string `json:"mode"`
	StatePath       string `json:"statePath,omitempty"`
	ConfigPath      string `json:"configPath,omitempty"`
	InstalledBinary string `json:"installedBinary,omitempty"`
	ServiceManager  string `json:"serviceManager,omitempty"`
	CurrentVersion  string `json:"currentVersion,omitempty"`
	CurrentTrack    string `json:"currentTrack,omitempty"`
	CurrentSlot     string `json:"currentSlot,omitempty"`
	AdminURL        string `json:"adminURL,omitempty"`
	SetupURL        string `json:"setupURL,omitempty"`
	SetupRequired   bool   `json:"setupRequired,omitempty"`
	LogPath         string `json:"logPath,omitempty"`
	Error           string `json:"error,omitempty"`
}

type packagedInstallOptions struct {
	Selection      *installInstanceSelection
	StatePath      string
	InstallBinDir  string
	SourceBinary   string
	ServiceManager ServiceManager
	CurrentVersion string
	InstallSource  InstallSource
	CurrentTrack   ReleaseTrack
	VersionsRoot   string
	CurrentSlot    string
	OutputFormat   string
}

func RunPackagedInstall(args []string, _ io.Reader, stdout, _ io.Writer, version string) error {
	defaults, err := DetectPlatformDefaults()
	if err != nil {
		return err
	}

	defaultBinary := defaultBinaryPath(runtime.GOOS)
	flagSet := flag.NewFlagSet("packaged-install", flag.ContinueOnError)
	flagSet.SetOutput(stdout)

	baseDir := flagSet.String("base-dir", "", "base directory for config and install state; empty auto-resolves to workspace binding or platform default")
	instanceIDFlag := flagSet.String("instance", "", "install instance id; empty auto-resolves to workspace binding or stable")
	statePath := flagSet.String("state-path", "", "path to install-state.json; empty derives from -base-dir and -instance")
	installBinDir := flagSet.String("install-bin-dir", "", "target directory for installed binary on first install; ignored for existing installs")
	binaryPath := flagSet.String("binary", defaultBinary, "packaged installer binary source path")
	installSource := flagSet.String("install-source", string(InstallSourceRelease), "install source metadata: release or repo")
	currentTrack := flagSet.String("current-track", "", "current release track metadata: production, beta, or alpha")
	currentVersion := flagSet.String("current-version", version, "current binary version metadata")
	versionsRoot := flagSet.String("versions-root", "", "version cache root metadata")
	currentSlot := flagSet.String("current-slot", "", "current version slot metadata; defaults to -current-version when set")
	serviceManagerFlag := flagSet.String("service-manager", string(ServiceManagerDetached), "service lifecycle manager for first install: detached, systemd_user, launchd_user, or task_scheduler_logon")
	format := flagSet.String("format", "text", "output format: text or json")
	jsonOutput := flagSet.Bool("json", false, "deprecated alias for -format json")

	if err := flagSet.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}

	if *jsonOutput {
		*format = "json"
	}
	outputFormat := strings.ToLower(strings.TrimSpace(*format))
	if outputFormat == "" {
		outputFormat = "text"
	}
	if outputFormat != "text" && outputFormat != "json" {
		return fmt.Errorf("unsupported output format %q", *format)
	}

	sourceBinary := strings.TrimSpace(*binaryPath)
	if sourceBinary == "" {
		return fmt.Errorf("binary path is required")
	}
	if err := sourceBinaryValidator(sourceBinary); err != nil {
		return fmt.Errorf("validate binary source %q: %w", sourceBinary, err)
	}

	resolvedStatePath := strings.TrimSpace(*statePath)
	var selection *installInstanceSelection
	if resolvedStatePath == "" {
		resolvedSelection, err := resolveInstallInstanceSelection(*instanceIDFlag, *baseDir, defaults.BaseDir, defaults.GOOS)
		if err != nil {
			return err
		}
		selection = &resolvedSelection
		resolvedStatePath = resolvedSelection.StatePath
	}

	requestedSlot, err := resolvePackagedInstallSlot(strings.TrimSpace(*currentSlot), strings.TrimSpace(*currentVersion), sourceBinary)
	if err != nil {
		return err
	}
	serviceManager, err := ParseServiceManager(*serviceManagerFlag, defaults.GOOS)
	if err != nil {
		return err
	}

	opts := packagedInstallOptions{
		Selection:      selection,
		StatePath:      resolvedStatePath,
		InstallBinDir:  strings.TrimSpace(*installBinDir),
		SourceBinary:   sourceBinary,
		ServiceManager: serviceManager,
		CurrentVersion: strings.TrimSpace(*currentVersion),
		InstallSource:  normalizeInstallSource(InstallSource(*installSource)),
		CurrentTrack:   ParseReleaseTrack(*currentTrack),
		VersionsRoot:   strings.TrimSpace(*versionsRoot),
		CurrentSlot:    requestedSlot,
		OutputFormat:   outputFormat,
	}
	if opts.InstallSource == "" {
		opts.InstallSource = InstallSourceRelease
	}

	result, runErr := runPackagedInstall(context.Background(), flagSet, opts)
	if outputErr := writePackagedInstallResult(stdout, outputFormat, result); outputErr != nil {
		return outputErr
	}
	return runErr
}

func runPackagedInstall(ctx context.Context, flagSet *flag.FlagSet, opts packagedInstallOptions) (PackagedInstallResult, error) {
	if opts.StatePath == "" {
		return PackagedInstallResult{}, fmt.Errorf("state path is required")
	}
	if _, err := os.Stat(opts.StatePath); err != nil {
		if os.IsNotExist(err) {
			return runPackagedFirstInstall(ctx, opts)
		}
		return PackagedInstallResult{StatePath: opts.StatePath}, err
	}
	return runPackagedRepair(ctx, flagSet, opts)
}

func runPackagedFirstInstall(ctx context.Context, opts packagedInstallOptions) (PackagedInstallResult, error) {
	if opts.Selection == nil {
		return PackagedInstallResult{StatePath: opts.StatePath}, fmt.Errorf("install selection is required for first install")
	}
	resolvedInstallBinDir := resolveTargetInstallBinDir(*opts.Selection, opts.InstallBinDir)
	versionsRoot := firstNonEmpty(strings.TrimSpace(opts.VersionsRoot), defaultVersionsRootForStatePath(opts.Selection.StatePath))
	stageState := InstallState{
		InstanceID:   opts.Selection.InstanceID,
		BaseDir:      opts.Selection.BaseDir,
		StatePath:    opts.Selection.StatePath,
		VersionsRoot: versionsRoot,
	}
	targetSlot, err := importLocalBinaryForUpgrade(stageState, opts.SourceBinary, opts.CurrentSlot)
	if err != nil {
		return PackagedInstallResult{
			Mode:      string(packagedInstallModeFirstInstall),
			StatePath: opts.Selection.StatePath,
			Error:     err.Error(),
		}, err
	}
	stagedBinary := filepath.Join(versionsRoot, targetSlot, executableName(runtime.GOOS))
	service := NewService()
	state, err := service.Bootstrap(Options{
		InstanceID:     opts.Selection.InstanceID,
		BaseDir:        opts.Selection.BaseDir,
		InstallBinDir:  resolvedInstallBinDir,
		BinaryPath:     stagedBinary,
		ServiceManager: opts.ServiceManager,
		CurrentVersion: opts.CurrentVersion,
		InstallSource:  opts.InstallSource,
		CurrentTrack:   opts.CurrentTrack,
		VersionsRoot:   versionsRoot,
		CurrentSlot:    targetSlot,
		BootstrapOnly:  true,
	})
	result := packagedInstallResultForState(packagedInstallModeFirstInstall, state)
	if err != nil {
		result.Error = err.Error()
		return result, err
	}
	if state.InstallSource == InstallSourceRelease {
		_ = updateCurrentReleaseLink(state.VersionsRoot, state.CurrentSlot)
	}
	if err := persistInstallInstanceSelection(*opts.Selection); err != nil {
		result.Error = err.Error()
		return result, err
	}
	status, readyErr := packagedInstallEnsureReadyFunc(ctx, state.StatePath, state.CurrentVersion)
	result.applyDaemonReadyStatus(status)
	if readyErr != nil {
		result.Error = readyErr.Error()
		return result, readyErr
	}
	result.OK = true
	return result, nil
}

func runPackagedRepair(ctx context.Context, flagSet *flag.FlagSet, opts packagedInstallOptions) (PackagedInstallResult, error) {
	state, err := loadServiceState(opts.StatePath)
	if err != nil {
		return PackagedInstallResult{Mode: string(packagedInstallModeRepair), StatePath: opts.StatePath, Error: err.Error()}, err
	}
	result := packagedInstallResultForState(packagedInstallModeRepair, state)
	if flagSet != nil {
		if flagWasProvided(flagSet, "install-bin-dir") {
			err := fmt.Errorf("-install-bin-dir cannot be used for existing installs; packaged repair keeps the current live binary path")
			result.Error = err.Error()
			return result, err
		}
		if flagWasProvided(flagSet, "service-manager") && effectiveServiceManager(state) != opts.ServiceManager {
			err := fmt.Errorf("-service-manager cannot change an existing install from %q to %q during packaged repair", effectiveServiceManager(state), opts.ServiceManager)
			result.Error = err.Error()
			return result, err
		}
	}

	liveBinaryPath := firstNonEmpty(strings.TrimSpace(state.CurrentBinaryPath), strings.TrimSpace(state.InstalledBinary))
	if liveBinaryPath == "" {
		err := fmt.Errorf("current binary path is missing from install state")
		result.Error = err.Error()
		return result, err
	}
	state.CurrentBinaryPath = liveBinaryPath
	state.InstalledBinary = liveBinaryPath
	state.VersionsRoot = firstNonEmpty(strings.TrimSpace(opts.VersionsRoot), strings.TrimSpace(state.VersionsRoot), defaultVersionsRootForStatePath(state.StatePath))

	targetSlot, err := importLocalBinaryForUpgrade(state, opts.SourceBinary, opts.CurrentSlot)
	if err != nil {
		result.Error = err.Error()
		return result, err
	}
	stagedBinary := filepath.Join(state.VersionsRoot, targetSlot, executableName(runtime.GOOS))

	if err := stopInstallStateProcess(ctx, state, RuntimePathsForState(state), stopInstallStateOptions{
		StopGrace:    packagedInstallStopGrace,
		PollInterval: packagedInstallPollInterval,
	}, defaultRuntimeControlHooks()); err != nil {
		result.Error = err.Error()
		return result, err
	}
	if err := copyFile(stagedBinary, liveBinaryPath); err != nil {
		result.Error = err.Error()
		return result, err
	}

	state.PendingUpgrade = nil
	state.RollbackCandidate = nil
	state.LastKnownLatestVersion = firstNonEmpty(strings.TrimSpace(opts.CurrentVersion), strings.TrimSpace(state.LastKnownLatestVersion))
	state.CurrentVersion = firstNonEmpty(strings.TrimSpace(opts.CurrentVersion), strings.TrimSpace(state.CurrentVersion), targetSlot)
	state.CurrentSlot = targetSlot
	state.InstallSource = opts.InstallSource
	if opts.CurrentTrack != "" {
		state.CurrentTrack = opts.CurrentTrack
	} else {
		state.CurrentTrack = inferReleaseTrack(state.CurrentVersion, string(state.InstallSource))
	}
	state.InstalledBinary = liveBinaryPath
	state.CurrentBinaryPath = liveBinaryPath
	ApplyStateMetadata(&state, StateMetadataOptions{
		InstanceID:      state.InstanceID,
		StatePath:       state.StatePath,
		BaseDir:         state.BaseDir,
		InstalledBinary: liveBinaryPath,
		CurrentVersion:  state.CurrentVersion,
		InstallSource:   state.InstallSource,
		CurrentTrack:    state.CurrentTrack,
		VersionsRoot:    state.VersionsRoot,
		CurrentSlot:     state.CurrentSlot,
		ServiceManager:  state.ServiceManager,
	})
	if err := WriteState(state.StatePath, state); err != nil {
		result.Error = err.Error()
		return result, err
	}
	if state.InstallSource == InstallSourceRelease {
		_ = updateCurrentReleaseLink(state.VersionsRoot, state.CurrentSlot)
	}
	if opts.Selection != nil {
		updatedSelection := *opts.Selection
		updatedSelection.StatePath = state.StatePath
		updatedSelection.ConfigPath = state.ConfigPath
		updatedSelection.InstallBinDir = filepath.Dir(liveBinaryPath)
		updatedSelection.LogPath = filepath.Join(filepath.Dir(state.StatePath), "logs", "codex-remote-relayd.log")
		if err := persistInstallInstanceSelection(updatedSelection); err != nil {
			result.Error = err.Error()
			return result, err
		}
	}

	status, readyErr := packagedInstallEnsureReadyFunc(ctx, state.StatePath, state.CurrentVersion)
	result = packagedInstallResultForState(packagedInstallModeRepair, state)
	result.applyDaemonReadyStatus(status)
	if readyErr != nil {
		result.Error = readyErr.Error()
		return result, readyErr
	}
	result.OK = true
	return result, nil
}

func resolvePackagedInstallSlot(requestedSlot, currentVersion, sourceBinary string) (string, error) {
	if strings.TrimSpace(requestedSlot) != "" {
		return validateUpgradeSlot(requestedSlot)
	}
	if strings.TrimSpace(currentVersion) != "" {
		return validateUpgradeSlot(currentVersion)
	}
	return deriveLocalUpgradeSlot(sourceBinary)
}

func writePackagedInstallResult(stdout io.Writer, format string, result PackagedInstallResult) error {
	if stdout == nil {
		return nil
	}
	switch format {
	case "json":
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	default:
		if _, err := fmt.Fprintf(stdout, "mode: %s\nstate: %s\nbinary: %s\nservice manager: %s\n", result.Mode, result.StatePath, result.InstalledBinary, result.ServiceManager); err != nil {
			return err
		}
		if result.CurrentVersion != "" {
			if _, err := fmt.Fprintf(stdout, "version: %s\n", result.CurrentVersion); err != nil {
				return err
			}
		}
		if result.CurrentSlot != "" {
			if _, err := fmt.Fprintf(stdout, "slot: %s\n", result.CurrentSlot); err != nil {
				return err
			}
		}
		if result.AdminURL != "" {
			if _, err := fmt.Fprintf(stdout, "web admin: %s\n", result.AdminURL); err != nil {
				return err
			}
		}
		if result.SetupRequired && result.SetupURL != "" {
			if _, err := fmt.Fprintf(stdout, "web setup: %s\n", result.SetupURL); err != nil {
				return err
			}
		}
		if result.LogPath != "" {
			if _, err := fmt.Fprintf(stdout, "logs: %s\n", result.LogPath); err != nil {
				return err
			}
		}
		if result.Error != "" {
			_, err := fmt.Fprintf(stdout, "error: %s\n", result.Error)
			return err
		}
		return nil
	}
}

func packagedInstallResultForState(mode packagedInstallMode, state InstallState) PackagedInstallResult {
	logPath := ""
	if strings.TrimSpace(state.StatePath) != "" {
		logPath = RuntimePathsForState(state).DaemonLogFile
	}
	return PackagedInstallResult{
		Mode:            string(mode),
		StatePath:       state.StatePath,
		ConfigPath:      state.ConfigPath,
		InstalledBinary: firstNonEmpty(strings.TrimSpace(state.InstalledBinary), strings.TrimSpace(state.CurrentBinaryPath)),
		ServiceManager:  string(effectiveServiceManager(state)),
		CurrentVersion:  state.CurrentVersion,
		CurrentTrack:    string(state.CurrentTrack),
		CurrentSlot:     state.CurrentSlot,
		LogPath:         logPath,
	}
}

func (r *PackagedInstallResult) applyDaemonReadyStatus(status DaemonReadyStatus) {
	if r == nil {
		return
	}
	if strings.TrimSpace(status.AdminURL) != "" {
		r.AdminURL = status.AdminURL
	}
	if strings.TrimSpace(status.SetupURL) != "" {
		r.SetupURL = status.SetupURL
	}
	r.SetupRequired = status.SetupRequired
	if strings.TrimSpace(status.LogPath) != "" {
		r.LogPath = status.LogPath
	}
}
