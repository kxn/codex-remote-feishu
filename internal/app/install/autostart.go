package install

import (
	"context"
	"fmt"
	"os"
	"strings"
)

type AutostartStatus struct {
	Platform         string         `json:"platform"`
	Supported        bool           `json:"supported"`
	Manager          ServiceManager `json:"manager,omitempty"`
	CurrentManager   ServiceManager `json:"currentManager,omitempty"`
	Status           string         `json:"status"`
	Configured       bool           `json:"configured"`
	Enabled          bool           `json:"enabled"`
	InstallStatePath string         `json:"installStatePath,omitempty"`
	ServiceUnitPath  string         `json:"serviceUnitPath,omitempty"`
	CanApply         bool           `json:"canApply"`
	Warning          string         `json:"warning,omitempty"`
	LingerHint       string         `json:"lingerHint,omitempty"`
}

type AutostartApplyOptions struct {
	InstanceID      string
	StatePath       string
	BaseDir         string
	InstalledBinary string
	CurrentVersion  string
}

func DetectAutostart(statePath string) (AutostartStatus, error) {
	platform := strings.TrimSpace(serviceRuntimeGOOS)
	if platform == "" {
		platform = "unknown"
	}
	trimmedStatePath := strings.TrimSpace(statePath)
	status := AutostartStatus{
		Platform:         platform,
		Status:           "unsupported",
		InstallStatePath: trimmedStatePath,
	}
	switch platform {
	case "linux":
		return detectSystemdAutostart(status, trimmedStatePath)
	case "darwin":
		return detectLaunchdAutostart(status, trimmedStatePath)
	default:
		return status, nil
	}
}

func detectSystemdAutostart(status AutostartStatus, trimmedStatePath string) (AutostartStatus, error) {
	baseDir, err := resolveAutostartBaseDir(trimmedStatePath, "")
	if err != nil {
		return AutostartStatus{}, err
	}
	instanceID := inferInstanceID("", trimmedStatePath)

	status.Supported = true
	status.Manager = ServiceManagerSystemdUser
	status.CurrentManager = ServiceManagerDetached
	status.Status = "disabled"
	status.CanApply = true
	status.ServiceUnitPath = systemdUserUnitPathForInstance(baseDir, instanceID)
	status.LingerHint = `如希望机器重启后在用户未登录前也恢复，需要额外手工执行 loginctl enable-linger "$USER"。`

	state, err := loadAutostartStateIfPresent(trimmedStatePath)
	if err != nil {
		return AutostartStatus{}, err
	}
	if state != nil {
		ApplyStateMetadata(state, StateMetadataOptions{
			InstanceID:     state.InstanceID,
			StatePath:      trimmedStatePath,
			BaseDir:        baseDir,
			ServiceManager: state.ServiceManager,
		})
		status.CurrentManager = effectiveServiceManager(*state)
		if strings.TrimSpace(state.ServiceUnitPath) != "" {
			status.ServiceUnitPath = state.ServiceUnitPath
		}
		if effectiveServiceManager(*state) == ServiceManagerSystemdUser {
			status.Configured = true
			status.CurrentManager = ServiceManagerSystemdUser
		}
	}

	if strings.TrimSpace(status.ServiceUnitPath) != "" {
		info, statErr := os.Stat(status.ServiceUnitPath)
		switch {
		case statErr == nil && !info.IsDir():
			status.Configured = true
			status.CurrentManager = ServiceManagerSystemdUser
		case statErr != nil && !os.IsNotExist(statErr):
			status.Warning = statErr.Error()
		}
	}

	if status.Configured {
		enabled, warning, detectErr := detectSystemdUserEnabled(context.Background(), instanceID)
		if detectErr != nil {
			return AutostartStatus{}, detectErr
		}
		status.Enabled = enabled
		if strings.TrimSpace(warning) != "" {
			status.Warning = warning
		}
		if enabled {
			status.Status = "enabled"
		}
	}
	return status, nil
}

func detectLaunchdAutostart(status AutostartStatus, trimmedStatePath string) (AutostartStatus, error) {
	baseDir, err := resolveAutostartBaseDir(trimmedStatePath, "")
	if err != nil {
		return AutostartStatus{}, err
	}
	instanceID := inferInstanceID("", trimmedStatePath)

	status.Supported = true
	status.Manager = ServiceManagerLaunchdUser
	status.CurrentManager = ServiceManagerDetached
	status.Status = "disabled"
	status.CanApply = true
	status.ServiceUnitPath = launchdUserPlistPathForInstance(baseDir, instanceID)

	state, err := loadAutostartStateIfPresent(trimmedStatePath)
	if err != nil {
		return AutostartStatus{}, err
	}
	if state != nil {
		ApplyStateMetadata(state, StateMetadataOptions{
			InstanceID:     state.InstanceID,
			StatePath:      trimmedStatePath,
			BaseDir:        baseDir,
			ServiceManager: state.ServiceManager,
		})
		status.CurrentManager = effectiveServiceManager(*state)
		if strings.TrimSpace(state.ServiceUnitPath) != "" && effectiveServiceManager(*state) == ServiceManagerLaunchdUser {
			status.ServiceUnitPath = state.ServiceUnitPath
			status.Configured = true
			status.CurrentManager = ServiceManagerLaunchdUser
		}
	}

	if strings.TrimSpace(status.ServiceUnitPath) != "" {
		info, statErr := os.Stat(status.ServiceUnitPath)
		switch {
		case statErr == nil && !info.IsDir():
			status.Configured = true
			status.CurrentManager = ServiceManagerLaunchdUser
		case statErr != nil && !os.IsNotExist(statErr):
			status.Warning = statErr.Error()
		}
	}

	if status.Configured {
		running, sErr := launchdUserIsRunning(context.Background(), InstallState{
			InstanceID:      instanceID,
			BaseDir:         baseDir,
			ServiceUnitPath: status.ServiceUnitPath,
			ServiceManager:  ServiceManagerLaunchdUser,
		})
		if sErr != nil {
			if strings.TrimSpace(status.Warning) == "" {
				status.Warning = sErr.Error()
			}
		}
		status.Enabled = running
		if running {
			status.Status = "enabled"
		}
	}
	return status, nil
}

func ApplyAutostart(opts AutostartApplyOptions) (AutostartStatus, error) {
	statePath := strings.TrimSpace(opts.StatePath)
	instanceID, err := parseInstanceID(opts.InstanceID)
	if err != nil {
		return AutostartStatus{}, err
	}
	baseDir, err := resolveAutostartBaseDir(statePath, opts.BaseDir)
	if err != nil {
		return AutostartStatus{}, err
	}
	if statePath == "" {
		statePath = defaultInstallStatePathForInstance(baseDir, instanceID)
	}

	state, err := loadAutostartStateIfPresent(statePath)
	if err != nil {
		return AutostartStatus{}, err
	}
	if state == nil {
		state = &InstallState{StatePath: statePath}
	}

	platform := strings.TrimSpace(serviceRuntimeGOOS)
	var targetManager ServiceManager
	switch platform {
	case "darwin":
		targetManager = ServiceManagerLaunchdUser
	default:
		targetManager = ServiceManagerSystemdUser
	}

	ApplyStateMetadata(state, StateMetadataOptions{
		InstanceID:      instanceID,
		StatePath:       statePath,
		BaseDir:         baseDir,
		InstalledBinary: strings.TrimSpace(opts.InstalledBinary),
		CurrentVersion:  strings.TrimSpace(opts.CurrentVersion),
		ServiceManager:  targetManager,
	})
	state.BaseDir = firstNonEmpty(strings.TrimSpace(state.BaseDir), baseDir)
	state.ServiceManager = targetManager
	state.InstalledBinary = firstNonEmpty(strings.TrimSpace(state.InstalledBinary), strings.TrimSpace(opts.InstalledBinary))
	state.InstalledWrapperBinary = firstNonEmpty(strings.TrimSpace(state.InstalledWrapperBinary), strings.TrimSpace(opts.InstalledBinary))
	state.InstalledRelaydBinary = firstNonEmpty(strings.TrimSpace(state.InstalledRelaydBinary), strings.TrimSpace(opts.InstalledBinary))
	state.CurrentBinaryPath = firstNonEmpty(strings.TrimSpace(state.CurrentBinaryPath), strings.TrimSpace(opts.InstalledBinary), strings.TrimSpace(state.InstalledBinary))
	if strings.TrimSpace(state.CurrentBinaryPath) == "" {
		return AutostartStatus{}, fmt.Errorf("current binary path is missing")
	}

	var updated InstallState
	switch targetManager {
	case ServiceManagerLaunchdUser:
		updated, err = installLaunchdUserPlist(context.Background(), *state)
		if err != nil {
			return AutostartStatus{}, err
		}
		if err := WriteState(statePath, updated); err != nil {
			return AutostartStatus{}, err
		}
		if err := launchdUserEnable(context.Background(), updated); err != nil {
			return AutostartStatus{}, err
		}
	default:
		updated, err = installSystemdUserUnit(context.Background(), *state)
		if err != nil {
			return AutostartStatus{}, err
		}
		if err := WriteState(statePath, updated); err != nil {
			return AutostartStatus{}, err
		}
		if err := systemdUserEnable(context.Background(), updated); err != nil {
			return AutostartStatus{}, err
		}
	}
	return DetectAutostart(statePath)
}

func DisableAutostart(statePath string) (AutostartStatus, error) {
	current, err := DetectAutostart(statePath)
	if err != nil {
		return AutostartStatus{}, err
	}
	if !current.Supported || !current.Configured || !current.Enabled {
		return current, nil
	}
	state, err := loadServiceState(statePath)
	if err != nil {
		return AutostartStatus{}, err
	}
	if err := ensureManagedServiceConfigured(state); err != nil {
		return current, nil
	}
	switch effectiveServiceManager(state) {
	case ServiceManagerLaunchdUser:
		err = launchdUserDisable(context.Background(), state)
	default:
		err = systemdUserDisable(context.Background(), state)
	}
	if err != nil {
		return AutostartStatus{}, err
	}
	return DetectAutostart(statePath)
}

func resolveAutostartBaseDir(statePath, preferredBaseDir string) (string, error) {
	if baseDir := strings.TrimSpace(preferredBaseDir); baseDir != "" {
		return baseDir, nil
	}
	if baseDir, ok := baseDirFromInstallStatePath(statePath); ok {
		return baseDir, nil
	}
	return serviceUserHomeDir()
}

func loadAutostartStateIfPresent(path string) (*InstallState, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	state, err := LoadState(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return &state, nil
}

func detectSystemdUserEnabled(ctx context.Context, instanceID string) (bool, string, error) {
	output, err := systemctlUserRunner(ctx, "is-enabled", systemdUserUnitName(InstallState{
		InstanceID: instanceID,
	}))
	switch strings.TrimSpace(output) {
	case "enabled", "enabled-runtime":
		return true, "", nil
	case "", "disabled", "indirect", "static", "linked", "masked", "not-found":
		return false, "", nil
	}
	if err != nil {
		return false, output, err
	}
	return false, output, nil
}
