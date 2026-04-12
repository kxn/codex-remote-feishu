package install

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
)

func RunService(args []string, _ io.Reader, stdout, _ io.Writer, _ string) error {
	if len(args) == 0 {
		return errors.New("service subcommand is required")
	}

	subcommand := strings.TrimSpace(args[0])
	flagSet := flag.NewFlagSet("service "+subcommand, flag.ContinueOnError)
	flagSet.SetOutput(stdout)

	defaults, err := DetectPlatformDefaults()
	if err != nil {
		return err
	}
	baseDir := flagSet.String("base-dir", defaults.BaseDir, "base directory for config and install state")
	instanceIDFlag := flagSet.String("instance", defaultInstanceID, "install instance: stable or debug")
	statePath := flagSet.String("state-path", "", "path to install-state.json; empty derives from -base-dir and -instance")
	if err := flagSet.Parse(args[1:]); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	instanceID, err := parseInstanceID(*instanceIDFlag)
	if err != nil {
		return err
	}
	resolvedStatePath := strings.TrimSpace(*statePath)
	if resolvedStatePath == "" {
		resolvedStatePath = defaultInstallStatePathForInstance(*baseDir, instanceID)
	}

	ctx := context.Background()
	switch subcommand {
	case "install-user":
		return runServiceInstallUser(ctx, resolvedStatePath, stdout)
	case "uninstall-user":
		return runServiceUninstallUser(ctx, resolvedStatePath, stdout)
	case "enable":
		return runServiceEnable(ctx, resolvedStatePath, stdout)
	case "disable":
		return runServiceDisable(ctx, resolvedStatePath, stdout)
	case "start":
		return runServiceStart(ctx, resolvedStatePath, stdout)
	case "stop":
		return runServiceStop(ctx, resolvedStatePath, stdout)
	case "restart":
		return runServiceRestart(ctx, resolvedStatePath, stdout)
	case "status":
		return runServiceStatus(ctx, resolvedStatePath, stdout)
	default:
		return fmt.Errorf("unsupported service subcommand %q", subcommand)
	}
}

func loadServiceState(statePath string) (InstallState, error) {
	state, err := LoadState(statePath)
	if err != nil {
		return InstallState{}, err
	}
	state.StatePath = firstNonEmpty(strings.TrimSpace(state.StatePath), strings.TrimSpace(statePath))
	ApplyStateMetadata(&state, StateMetadataOptions{
		InstanceID:     state.InstanceID,
		StatePath:      state.StatePath,
		BaseDir:        state.BaseDir,
		ServiceManager: state.ServiceManager,
	})
	return state, nil
}

func ensureSystemdUserConfigured(state InstallState) error {
	if effectiveServiceManager(state) != ServiceManagerSystemdUser {
		return fmt.Errorf("service manager is %q; run `codex-remote service install-user` first", effectiveServiceManager(state))
	}
	return nil
}

func runServiceInstallUser(ctx context.Context, statePath string, stdout io.Writer) error {
	state, err := loadServiceState(statePath)
	if err != nil {
		return err
	}
	state.ServiceManager = ServiceManagerSystemdUser
	state, err = installSystemdUserUnit(ctx, state)
	if err != nil {
		return err
	}
	if err := WriteState(statePath, state); err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "service manager: %s\nunit: %s\n", state.ServiceManager, state.ServiceUnitPath)
	return err
}

func runServiceUninstallUser(ctx context.Context, statePath string, stdout io.Writer) error {
	state, err := loadServiceState(statePath)
	if err != nil {
		return err
	}
	if err := uninstallSystemdUserUnit(ctx, state); err != nil {
		return err
	}
	state.ServiceManager = ServiceManagerDetached
	state.ServiceUnitPath = ""
	ApplyStateMetadata(&state, StateMetadataOptions{
		StatePath:      state.StatePath,
		BaseDir:        state.BaseDir,
		ServiceManager: state.ServiceManager,
	})
	if err := WriteState(statePath, state); err != nil {
		return err
	}
	_, err = io.WriteString(stdout, "service manager: detached\n")
	return err
}

func runServiceEnable(ctx context.Context, statePath string, stdout io.Writer) error {
	state, err := loadServiceState(statePath)
	if err != nil {
		return err
	}
	if err := ensureSystemdUserConfigured(state); err != nil {
		return err
	}
	state, err = installSystemdUserUnit(ctx, state)
	if err != nil {
		return err
	}
	if err := WriteState(statePath, state); err != nil {
		return err
	}
	if err := systemdUserEnable(ctx, state); err != nil {
		return err
	}
	_, err = io.WriteString(stdout, "systemd user service enabled\n")
	return err
}

func runServiceDisable(ctx context.Context, statePath string, stdout io.Writer) error {
	state, err := loadServiceState(statePath)
	if err != nil {
		return err
	}
	if err := ensureSystemdUserConfigured(state); err != nil {
		return err
	}
	if err := systemdUserDisable(ctx, state); err != nil {
		return err
	}
	_, err = io.WriteString(stdout, "systemd user service disabled\n")
	return err
}

func runServiceStart(ctx context.Context, statePath string, stdout io.Writer) error {
	state, err := loadServiceState(statePath)
	if err != nil {
		return err
	}
	if err := ensureSystemdUserConfigured(state); err != nil {
		return err
	}
	state, err = installSystemdUserUnit(ctx, state)
	if err != nil {
		return err
	}
	if err := WriteState(statePath, state); err != nil {
		return err
	}
	if err := systemdUserStart(ctx, state); err != nil {
		return err
	}
	_, err = io.WriteString(stdout, "systemd user service started\n")
	return err
}

func runServiceStop(ctx context.Context, statePath string, stdout io.Writer) error {
	state, err := loadServiceState(statePath)
	if err != nil {
		return err
	}
	if err := ensureSystemdUserConfigured(state); err != nil {
		return err
	}
	if err := systemdUserStop(ctx, state); err != nil {
		return err
	}
	_, err = io.WriteString(stdout, "systemd user service stopped\n")
	return err
}

func runServiceRestart(ctx context.Context, statePath string, stdout io.Writer) error {
	state, err := loadServiceState(statePath)
	if err != nil {
		return err
	}
	if err := ensureSystemdUserConfigured(state); err != nil {
		return err
	}
	state, err = installSystemdUserUnit(ctx, state)
	if err != nil {
		return err
	}
	if err := WriteState(statePath, state); err != nil {
		return err
	}
	if err := systemdUserRestart(ctx, state); err != nil {
		return err
	}
	_, err = io.WriteString(stdout, "systemd user service restarted\n")
	return err
}

func runServiceStatus(ctx context.Context, statePath string, stdout io.Writer) error {
	state, err := loadServiceState(statePath)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "service manager: %s\n", effectiveServiceManager(state))
	if err != nil {
		return err
	}
	if effectiveServiceManager(state) != ServiceManagerSystemdUser {
		_, err = io.WriteString(stdout, "systemd user service is not configured\n")
		return err
	}
	output, err := systemdUserStatus(ctx, state)
	if strings.TrimSpace(output) != "" {
		if _, writeErr := io.WriteString(stdout, output+"\n"); writeErr != nil {
			return writeErr
		}
	}
	return err
}
