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
	driver, ok := managedServiceDriverForGOOS(platform)
	if !ok {
		return status, nil
	}
	return detectManagedAutostart(status, trimmedStatePath, driver)
}

func detectManagedAutostart(status AutostartStatus, trimmedStatePath string, driver managedServiceDriver) (AutostartStatus, error) {
	baseDir, err := resolveAutostartBaseDir(trimmedStatePath, "")
	if err != nil {
		return AutostartStatus{}, err
	}
	instanceID := inferInstanceID("", trimmedStatePath)

	status.Supported = true
	status.Manager = driver.Manager
	status.CurrentManager = ServiceManagerDetached
	status.Status = "disabled"
	status.CanApply = true
	status.ServiceUnitPath = driver.ServiceUnitPath(baseDir, instanceID)
	if driver.Manager == ServiceManagerSystemdUser {
		status.LingerHint = `如希望机器重启后在用户未登录前也恢复，需要额外手工执行 loginctl enable-linger "$USER"。`
	}

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
		if effectiveServiceManager(*state) == driver.Manager && strings.TrimSpace(state.ServiceUnitPath) != "" {
			status.ServiceUnitPath = state.ServiceUnitPath
		}
	}

	probeState := InstallState{
		InstanceID:      instanceID,
		BaseDir:         baseDir,
		ServiceUnitPath: status.ServiceUnitPath,
		ServiceManager:  driver.Manager,
	}
	configured, enabled, warning, probeErr := driver.AutostartProbe(context.Background(), probeState)
	if probeErr != nil {
		return AutostartStatus{}, probeErr
	}
	status.Configured = configured
	status.Enabled = enabled
	if configured {
		status.CurrentManager = driver.Manager
	}
	if strings.TrimSpace(warning) != "" {
		status.Warning = warning
	}
	if enabled {
		status.Status = "enabled"
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
	targetManager, ok := managedServiceManagerForGOOS(platform)
	if !ok {
		return AutostartStatus{}, fmt.Errorf("autostart is not supported on %s", platform)
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
	updated, err = installManagedService(context.Background(), *state)
	if err != nil {
		return AutostartStatus{}, err
	}
	if err := WriteState(statePath, updated); err != nil {
		return AutostartStatus{}, err
	}
	if err := enableManagedService(context.Background(), updated); err != nil {
		return AutostartStatus{}, err
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
	if err := disableManagedService(context.Background(), state); err != nil {
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

func probeSystemdAutostart(ctx context.Context, state InstallState) (bool, bool, string, error) {
	configured, warning, err := probeFileServiceDefinition(state.ServiceUnitPath)
	if err != nil || !configured {
		return configured, false, warning, err
	}
	enabled, enabledWarning, err := detectSystemdUserEnabled(ctx, state.InstanceID)
	if strings.TrimSpace(enabledWarning) != "" {
		warning = enabledWarning
	}
	return configured, enabled, warning, err
}

func probeLaunchdAutostart(ctx context.Context, state InstallState) (bool, bool, string, error) {
	configured, warning, err := probeFileServiceDefinition(state.ServiceUnitPath)
	if err != nil || !configured {
		return configured, false, warning, err
	}
	enabled, enabledWarning, err := detectLaunchdUserEnabled(ctx, state)
	if strings.TrimSpace(enabledWarning) != "" {
		warning = enabledWarning
	}
	return configured, enabled, warning, err
}

func probeTaskSchedulerAutostart(ctx context.Context, state InstallState) (bool, bool, string, error) {
	output, queryErr := taskSchedulerRunner(ctx, "/Query", "/TN", taskSchedulerTaskNameForInstance(state.InstanceID), "/XML")
	switch {
	case queryErr == nil:
		enabled, ok := parseTaskSchedulerEnabled(output)
		if !ok {
			return true, false, "无法解析自动启动任务状态。", nil
		}
		return true, enabled, "", nil
	case isTaskSchedulerMissingErr(queryErr):
		return false, false, "", nil
	default:
		return false, false, "", fmt.Errorf("query task scheduler task %s: %w", taskSchedulerTaskNameForInstance(state.InstanceID), queryErr)
	}
}

func probeFileServiceDefinition(path string) (bool, string, error) {
	if strings.TrimSpace(path) == "" {
		return false, "", nil
	}
	info, statErr := os.Stat(path)
	switch {
	case statErr == nil && !info.IsDir():
		return true, "", nil
	case statErr == nil && info.IsDir():
		return false, "", nil
	case os.IsNotExist(statErr):
		return false, "", nil
	default:
		return false, statErr.Error(), nil
	}
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
