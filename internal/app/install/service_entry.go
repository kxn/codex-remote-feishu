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
	baseDir := flagSet.String("base-dir", "", "base directory for config and install state; empty auto-resolves to workspace binding or platform default")
	instanceIDFlag := flagSet.String("instance", "", "install instance id; empty auto-resolves to workspace binding or stable")
	statePath := flagSet.String("state-path", "", "path to install-state.json; empty derives from -base-dir and -instance")
	if err := flagSet.Parse(args[1:]); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	resolvedStatePath := strings.TrimSpace(*statePath)
	if resolvedStatePath == "" {
		selection, err := resolveInstallInstanceSelection(*instanceIDFlag, *baseDir, defaults.BaseDir, defaults.GOOS)
		if err != nil {
			return err
		}
		resolvedStatePath = selection.StatePath
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

func ensureManagedServiceConfigured(state InstallState) error {
	if !isManagedServiceManager(state) {
		return fmt.Errorf("service manager is %q; run `codex-remote service install-user` first", effectiveServiceManager(state))
	}
	return nil
}

func managedServiceManagerForCurrentPlatform() (ServiceManager, error) {
	manager, ok := managedServiceManagerForGOOS(serviceRuntimeGOOS)
	if !ok {
		return "", fmt.Errorf("managed user service is not supported on %s", serviceRuntimeGOOS)
	}
	return manager, nil
}

func installManagedService(ctx context.Context, state InstallState) (InstallState, error) {
	driver, err := managedServiceDriverForState(state)
	if err != nil {
		return InstallState{}, err
	}
	return driver.Install(ctx, state)
}

func uninstallManagedService(ctx context.Context, state InstallState) error {
	driver, ok := managedServiceDriverForManager(effectiveServiceManager(state))
	if !ok {
		return nil
	}
	return driver.Uninstall(ctx, state)
}

func enableManagedService(ctx context.Context, state InstallState) error {
	driver, err := managedServiceDriverForState(state)
	if err != nil {
		return err
	}
	return driver.Enable(ctx, state)
}

func disableManagedService(ctx context.Context, state InstallState) error {
	driver, err := managedServiceDriverForState(state)
	if err != nil {
		return err
	}
	return driver.Disable(ctx, state)
}

func startManagedService(ctx context.Context, state InstallState) error {
	driver, err := managedServiceDriverForState(state)
	if err != nil {
		return err
	}
	return driver.Start(ctx, state)
}

func stopManagedService(ctx context.Context, state InstallState) error {
	driver, err := managedServiceDriverForState(state)
	if err != nil {
		return err
	}
	return driver.Stop(ctx, state)
}

func restartManagedService(ctx context.Context, state InstallState) error {
	driver, err := managedServiceDriverForState(state)
	if err != nil {
		return err
	}
	return driver.Restart(ctx, state)
}

func managedServiceStatus(ctx context.Context, state InstallState) (string, error) {
	driver, err := managedServiceDriverForState(state)
	if err != nil {
		return "", err
	}
	return driver.Status(ctx, state)
}

func runServiceInstallUser(ctx context.Context, statePath string, stdout io.Writer) error {
	state, err := loadServiceState(statePath)
	if err != nil {
		return err
	}
	manager, err := managedServiceManagerForCurrentPlatform()
	if err != nil {
		return err
	}
	state.ServiceManager = manager
	state, err = installManagedService(ctx, state)
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
	if err := uninstallManagedService(ctx, state); err != nil {
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
	if err := ensureManagedServiceConfigured(state); err != nil {
		return err
	}
	state, err = installManagedService(ctx, state)
	if err != nil {
		return err
	}
	if err := WriteState(statePath, state); err != nil {
		return err
	}
	if err := enableManagedService(ctx, state); err != nil {
		return err
	}
	_, err = io.WriteString(stdout, "service enabled\n")
	return err
}

func runServiceDisable(ctx context.Context, statePath string, stdout io.Writer) error {
	state, err := loadServiceState(statePath)
	if err != nil {
		return err
	}
	if err := ensureManagedServiceConfigured(state); err != nil {
		return err
	}
	if err := disableManagedService(ctx, state); err != nil {
		return err
	}
	_, err = io.WriteString(stdout, "service disabled\n")
	return err
}

func runServiceStart(ctx context.Context, statePath string, stdout io.Writer) error {
	state, err := loadServiceState(statePath)
	if err != nil {
		return err
	}
	if err := ensureManagedServiceConfigured(state); err != nil {
		return err
	}
	state, err = installManagedService(ctx, state)
	if err != nil {
		return err
	}
	if err := WriteState(statePath, state); err != nil {
		return err
	}
	if err := startManagedService(ctx, state); err != nil {
		return err
	}
	_, err = io.WriteString(stdout, "service started\n")
	return err
}

func runServiceStop(ctx context.Context, statePath string, stdout io.Writer) error {
	state, err := loadServiceState(statePath)
	if err != nil {
		return err
	}
	if err := ensureManagedServiceConfigured(state); err != nil {
		return err
	}
	if err := stopManagedService(ctx, state); err != nil {
		return err
	}
	_, err = io.WriteString(stdout, "service stopped\n")
	return err
}

func runServiceRestart(ctx context.Context, statePath string, stdout io.Writer) error {
	state, err := loadServiceState(statePath)
	if err != nil {
		return err
	}
	if err := ensureManagedServiceConfigured(state); err != nil {
		return err
	}
	state, err = installManagedService(ctx, state)
	if err != nil {
		return err
	}
	if err := WriteState(statePath, state); err != nil {
		return err
	}
	if err := restartManagedService(ctx, state); err != nil {
		return err
	}
	_, err = io.WriteString(stdout, "service restarted\n")
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
	if !isManagedServiceManager(state) {
		_, err = io.WriteString(stdout, "managed service is not configured\n")
		return err
	}
	output, sErr := managedServiceStatus(ctx, state)
	if strings.TrimSpace(output) != "" {
		if _, writeErr := io.WriteString(stdout, output+"\n"); writeErr != nil {
			return writeErr
		}
	}
	return sErr
}
