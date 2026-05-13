package install

import (
	"fmt"
	"path/filepath"
	"strings"
)

type ServiceManager string

const (
	ServiceManagerDetached           ServiceManager = "detached"
	ServiceManagerSystemdUser        ServiceManager = "systemd_user"
	ServiceManagerLaunchdUser        ServiceManager = "launchd_user"
	ServiceManagerTaskSchedulerLogon ServiceManager = "task_scheduler_logon"
)

type installLayout struct {
	ConfigDir  string
	StateDir   string
	StatePath  string
	ConfigHome string
	DataHome   string
	StateHome  string
}

func ParseServiceManager(value, goos string) (ServiceManager, error) {
	switch normalizeServiceManager(ServiceManager(value)) {
	case ServiceManagerDetached:
		return ServiceManagerDetached, nil
	case ServiceManagerSystemdUser:
		if goos != "linux" {
			return "", fmt.Errorf("service manager %q is only supported on linux", ServiceManagerSystemdUser)
		}
		return ServiceManagerSystemdUser, nil
	case ServiceManagerLaunchdUser:
		if goos != "darwin" {
			return "", fmt.Errorf("service manager %q is only supported on darwin", ServiceManagerLaunchdUser)
		}
		return ServiceManagerLaunchdUser, nil
	case ServiceManagerTaskSchedulerLogon:
		if goos != "windows" {
			return "", fmt.Errorf("service manager %q is only supported on windows", ServiceManagerTaskSchedulerLogon)
		}
		return ServiceManagerTaskSchedulerLogon, nil
	default:
		return "", fmt.Errorf("unsupported service manager %q (want detached, systemd_user, launchd_user, or task_scheduler_logon)", strings.TrimSpace(value))
	}
}

func normalizeServiceManager(value ServiceManager) ServiceManager {
	switch strings.ToLower(strings.TrimSpace(string(value))) {
	case "", string(ServiceManagerDetached):
		return ServiceManagerDetached
	case string(ServiceManagerSystemdUser):
		return ServiceManagerSystemdUser
	case string(ServiceManagerLaunchdUser):
		return ServiceManagerLaunchdUser
	case string(ServiceManagerTaskSchedulerLogon):
		return ServiceManagerTaskSchedulerLogon
	default:
		return ""
	}
}

func effectiveServiceManager(state InstallState) ServiceManager {
	if normalized := normalizeServiceManager(state.ServiceManager); normalized != "" {
		return normalized
	}
	return ServiceManagerDetached
}

func installLayoutForBaseDir(baseDir string) installLayout {
	return installLayoutForInstance(baseDir, defaultInstanceID)
}

func installLayoutForInstance(baseDir, instanceID string) installLayout {
	baseDir = filepath.Clean(strings.TrimSpace(baseDir))
	paths := instancePathsForBaseDir(baseDir, instanceID)
	configDir := filepath.Join(paths.ConfigHome, productName)
	stateDir := filepath.Join(paths.DataHome, productName)
	return installLayout{
		ConfigDir:  configDir,
		StateDir:   stateDir,
		StatePath:  filepath.Join(stateDir, "install-state.json"),
		ConfigHome: paths.ConfigHome,
		DataHome:   paths.DataHome,
		StateHome:  paths.StateHome,
	}
}

func defaultInstallStatePath(baseDir string) string {
	return defaultInstallStatePathForInstance(baseDir, defaultInstanceID)
}

func defaultInstallStatePathForInstance(baseDir, instanceID string) string {
	return installLayoutForInstance(baseDir, instanceID).StatePath
}

func defaultConfigPath(baseDir string) string {
	return defaultConfigPathForInstance(baseDir, defaultInstanceID)
}

func defaultConfigPathForInstance(baseDir, instanceID string) string {
	return filepath.Join(installLayoutForInstance(baseDir, instanceID).ConfigDir, "config.json")
}

func baseDirFromConfigPath(path string) (string, bool) {
	baseDir, _, ok := inferBaseDirAndInstanceFromConfigPath(path)
	return baseDir, ok
}

func baseDirFromInstallStatePath(path string) (string, bool) {
	baseDir, _, ok := inferBaseDirAndInstanceFromStatePath(path)
	return baseDir, ok
}

func inferBaseDir(configPath, statePath string) string {
	if baseDir, ok := baseDirFromInstallStatePath(statePath); ok {
		return baseDir
	}
	if baseDir, ok := baseDirFromConfigPath(configPath); ok {
		return baseDir
	}
	return ""
}

func systemdUserUnitPath(baseDir string) string {
	return systemdUserUnitPathForInstance(baseDir, defaultInstanceID)
}

func systemdUserUnitPathForInstance(baseDir, instanceID string) string {
	unitBaseDir := filepath.Clean(strings.TrimSpace(baseDir))
	if homeDir, err := serviceUserHomeDir(); err == nil && strings.TrimSpace(homeDir) != "" {
		unitBaseDir = filepath.Clean(strings.TrimSpace(homeDir))
	}
	if unitBaseDir == "" {
		return ""
	}
	return filepath.Join(unitBaseDir, ".config", "systemd", "user", systemdUserServiceNameForInstance(instanceID))
}

func launchdUserPlistPathForInstance(baseDir, instanceID string) string {
	if homeDir, err := serviceUserHomeDir(); err == nil && strings.TrimSpace(homeDir) != "" {
		return filepath.Join(homeDir, "Library", "LaunchAgents", launchdLabelForInstance(instanceID)+".plist")
	}
	return ""
}

func isManagedServiceManager(state InstallState) bool {
	_, ok := managedServiceDriverForManager(effectiveServiceManager(state))
	return ok
}

func serviceNameForInstallInstance(goos, instanceID string) string {
	if driver, ok := managedServiceDriverForGOOS(goos); ok {
		return driver.ServiceName(instanceID)
	}
	return systemdUserServiceNameForInstance(instanceID)
}

func serviceUnitPathForInstallInstance(goos, baseDir, instanceID string) string {
	if driver, ok := managedServiceDriverForGOOS(goos); ok {
		return driver.ServiceUnitPath(baseDir, instanceID)
	}
	return systemdUserUnitPathForInstance(baseDir, instanceID)
}
