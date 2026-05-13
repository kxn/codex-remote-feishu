package install

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type PackagedInstallProbeResult struct {
	OK                      bool   `json:"ok"`
	Mode                    string `json:"mode"`
	StatePath               string `json:"statePath,omitempty"`
	ConfigPath              string `json:"configPath,omitempty"`
	CurrentVersion          string `json:"currentVersion,omitempty"`
	CurrentTrack            string `json:"currentTrack,omitempty"`
	InstallerVersion        string `json:"installerVersion,omitempty"`
	SameVersion             bool   `json:"sameVersion,omitempty"`
	CurrentInstallBinDir    string `json:"currentInstallBinDir,omitempty"`
	SuggestedInstallBinDir  string `json:"suggestedInstallBinDir,omitempty"`
	InstallLocationEditable bool   `json:"installLocationEditable,omitempty"`
	ServiceManager          string `json:"serviceManager,omitempty"`
	Error                   string `json:"error,omitempty"`
}

type packagedInstallProbeOptions struct {
	Selection              *installInstanceSelection
	StatePath              string
	InstallerVersion       string
	SuggestedInstallBinDir string
	GOOS                   string
	OutputFormat           string
}

func RunPackagedInstallProbe(args []string, _ io.Reader, stdout, _ io.Writer, version string) error {
	defaults, err := DetectPlatformDefaults()
	if err != nil {
		return err
	}

	flagSet := flag.NewFlagSet("packaged-install-probe", flag.ContinueOnError)
	flagSet.SetOutput(stdout)

	baseDir := flagSet.String("base-dir", "", "base directory for config and install state; empty auto-resolves to workspace binding or platform default")
	instanceIDFlag := flagSet.String("instance", "", "install instance id; empty auto-resolves to workspace binding or stable")
	statePath := flagSet.String("state-path", "", "path to install-state.json; empty derives from -base-dir and -instance")
	installBinDir := flagSet.String("install-bin-dir", "", "preferred target directory for first install; ignored for existing installs")
	currentVersion := flagSet.String("current-version", version, "installer version metadata used to compare same-version repair")
	format := flagSet.String("format", "json", "output format: json or text")
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
		outputFormat = "json"
	}
	if outputFormat != "json" && outputFormat != "text" {
		return fmt.Errorf("unsupported output format %q", *format)
	}

	selection, err := resolveInstallInstanceSelection(*instanceIDFlag, *baseDir, defaults.BaseDir, defaults.GOOS)
	if err != nil {
		return err
	}
	resolvedStatePath := strings.TrimSpace(*statePath)
	if resolvedStatePath == "" {
		resolvedStatePath = selection.StatePath
	}

	opts := packagedInstallProbeOptions{
		Selection:              &selection,
		StatePath:              resolvedStatePath,
		InstallerVersion:       strings.TrimSpace(*currentVersion),
		SuggestedInstallBinDir: resolveTargetInstallBinDir(selection, *installBinDir),
		GOOS:                   defaults.GOOS,
		OutputFormat:           outputFormat,
	}

	result, runErr := runPackagedInstallProbe(opts)
	if outputErr := writePackagedInstallProbeResult(stdout, outputFormat, result); outputErr != nil {
		return outputErr
	}
	return runErr
}

func runPackagedInstallProbe(opts packagedInstallProbeOptions) (PackagedInstallProbeResult, error) {
	result := PackagedInstallProbeResult{
		StatePath:               opts.StatePath,
		InstallerVersion:        opts.InstallerVersion,
		SuggestedInstallBinDir:  strings.TrimSpace(opts.SuggestedInstallBinDir),
		InstallLocationEditable: true,
	}
	if opts.Selection != nil {
		result.ConfigPath = opts.Selection.ConfigPath
	}
	if manager, ok := managedServiceManagerForGOOS(opts.GOOS); ok {
		result.ServiceManager = string(manager)
	}
	if strings.TrimSpace(opts.StatePath) == "" {
		err := fmt.Errorf("state path is required")
		result.Error = err.Error()
		return result, err
	}

	if _, err := os.Stat(opts.StatePath); err != nil {
		if os.IsNotExist(err) {
			result.Mode = string(packagedInstallModeFirstInstall)
			result.OK = true
			return result, nil
		}
		result.Error = err.Error()
		return result, err
	}

	state, err := loadServiceState(opts.StatePath)
	if err != nil {
		result.Mode = string(packagedInstallModeRepair)
		result.Error = err.Error()
		return result, err
	}
	result = packagedInstallProbeResultForState(state, opts)
	return result, nil
}

func packagedInstallProbeResultForState(state InstallState, opts packagedInstallProbeOptions) PackagedInstallProbeResult {
	currentBinary := firstNonEmpty(strings.TrimSpace(state.CurrentBinaryPath), strings.TrimSpace(state.InstalledBinary))
	currentInstallBinDir := ""
	if currentBinary != "" {
		currentInstallBinDir = filepath.Dir(currentBinary)
	}
	return PackagedInstallProbeResult{
		OK:                      true,
		Mode:                    string(packagedInstallModeRepair),
		StatePath:               state.StatePath,
		ConfigPath:              state.ConfigPath,
		CurrentVersion:          state.CurrentVersion,
		CurrentTrack:            string(state.CurrentTrack),
		InstallerVersion:        opts.InstallerVersion,
		SameVersion:             strings.TrimSpace(state.CurrentVersion) != "" && strings.TrimSpace(state.CurrentVersion) == strings.TrimSpace(opts.InstallerVersion),
		CurrentInstallBinDir:    currentInstallBinDir,
		SuggestedInstallBinDir:  currentInstallBinDir,
		InstallLocationEditable: false,
		ServiceManager:          string(effectiveServiceManager(state)),
	}
}

func writePackagedInstallProbeResult(stdout io.Writer, format string, result PackagedInstallProbeResult) error {
	if stdout == nil {
		return nil
	}
	switch format {
	case "json":
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	default:
		if _, err := fmt.Fprintf(stdout, "mode: %s\nstate: %s\n", result.Mode, result.StatePath); err != nil {
			return err
		}
		if result.ConfigPath != "" {
			if _, err := fmt.Fprintf(stdout, "config: %s\n", result.ConfigPath); err != nil {
				return err
			}
		}
		if result.CurrentVersion != "" {
			if _, err := fmt.Fprintf(stdout, "current version: %s\n", result.CurrentVersion); err != nil {
				return err
			}
		}
		if result.InstallerVersion != "" {
			if _, err := fmt.Fprintf(stdout, "installer version: %s\n", result.InstallerVersion); err != nil {
				return err
			}
		}
		if result.CurrentInstallBinDir != "" {
			if _, err := fmt.Fprintf(stdout, "current install dir: %s\n", result.CurrentInstallBinDir); err != nil {
				return err
			}
		}
		if result.SuggestedInstallBinDir != "" {
			if _, err := fmt.Fprintf(stdout, "suggested install dir: %s\n", result.SuggestedInstallBinDir); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(stdout, "install location editable: %s\nsame version: %s\n", boolString(result.InstallLocationEditable), boolString(result.SameVersion)); err != nil {
			return err
		}
		if result.ServiceManager != "" {
			if _, err := fmt.Fprintf(stdout, "service manager: %s\n", result.ServiceManager); err != nil {
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
