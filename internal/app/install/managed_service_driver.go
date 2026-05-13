package install

import (
	"context"
	"fmt"
	"time"
)

type managedServiceDriver struct {
	Manager                 ServiceManager
	Install                 func(context.Context, InstallState) (InstallState, error)
	Uninstall               func(context.Context, InstallState) error
	Enable                  func(context.Context, InstallState) error
	Disable                 func(context.Context, InstallState) error
	Start                   func(context.Context, InstallState) error
	Stop                    func(context.Context, InstallState) error
	Restart                 func(context.Context, InstallState) error
	Status                  func(context.Context, InstallState) (string, error)
	StopAndWait             func(context.Context, InstallState, time.Duration, time.Duration) error
	ServiceName             func(string) string
	ServiceUnitPath         func(string, string) string
	AutostartProbe          func(context.Context, InstallState) (configured bool, enabled bool, warning string, err error)
	EnableBeforeEnsureReady bool
	InstallBeforeUpgradeRun bool
}

func managedServiceDriverForManager(manager ServiceManager) (managedServiceDriver, bool) {
	switch normalizeServiceManager(manager) {
	case ServiceManagerSystemdUser:
		return managedServiceDriver{
			Manager:                 ServiceManagerSystemdUser,
			Install:                 installSystemdUserUnit,
			Uninstall:               uninstallSystemdUserUnit,
			Enable:                  systemdUserEnable,
			Disable:                 systemdUserDisable,
			Start:                   systemdUserStart,
			Stop:                    systemdUserStop,
			Restart:                 systemdUserRestart,
			Status:                  systemdUserStatus,
			StopAndWait:             systemdUserStopAndWait,
			ServiceName:             systemdUserServiceNameForInstance,
			ServiceUnitPath:         systemdUserUnitPathForInstance,
			AutostartProbe:          probeSystemdAutostart,
			EnableBeforeEnsureReady: true,
		}, true
	case ServiceManagerLaunchdUser:
		return managedServiceDriver{
			Manager:                 ServiceManagerLaunchdUser,
			Install:                 installLaunchdUserPlist,
			Uninstall:               uninstallLaunchdUserPlist,
			Enable:                  launchdUserEnable,
			Disable:                 launchdUserDisable,
			Start:                   launchdUserStart,
			Stop:                    launchdUserStop,
			Restart:                 launchdUserRestart,
			Status:                  launchdUserStatus,
			StopAndWait:             launchdUserStopAndWait,
			ServiceName:             launchdLabelForInstance,
			ServiceUnitPath:         launchdUserPlistPathForInstance,
			AutostartProbe:          probeLaunchdAutostart,
			InstallBeforeUpgradeRun: true,
		}, true
	case ServiceManagerTaskSchedulerLogon:
		return managedServiceDriver{
			Manager:                 ServiceManagerTaskSchedulerLogon,
			Install:                 installTaskSchedulerLogonTask,
			Uninstall:               uninstallTaskSchedulerLogonTask,
			Enable:                  taskSchedulerLogonEnable,
			Disable:                 taskSchedulerLogonDisable,
			Start:                   taskSchedulerLogonStart,
			Stop:                    taskSchedulerLogonStop,
			Restart:                 taskSchedulerLogonRestart,
			Status:                  taskSchedulerLogonStatus,
			StopAndWait:             taskSchedulerLogonStopAndWait,
			ServiceName:             taskSchedulerTaskNameForInstance,
			ServiceUnitPath:         taskSchedulerXMLPathForInstance,
			AutostartProbe:          probeTaskSchedulerAutostart,
			EnableBeforeEnsureReady: true,
			InstallBeforeUpgradeRun: true,
		}, true
	default:
		return managedServiceDriver{}, false
	}
}

func managedServiceManagerForGOOS(goos string) (ServiceManager, bool) {
	switch goos {
	case "linux":
		return ServiceManagerSystemdUser, true
	case "darwin":
		return ServiceManagerLaunchdUser, true
	case "windows":
		return ServiceManagerTaskSchedulerLogon, true
	default:
		return "", false
	}
}

func managedServiceDriverForGOOS(goos string) (managedServiceDriver, bool) {
	manager, ok := managedServiceManagerForGOOS(goos)
	if !ok {
		return managedServiceDriver{}, false
	}
	return managedServiceDriverForManager(manager)
}

func managedServiceDriverForState(state InstallState) (managedServiceDriver, error) {
	manager := effectiveServiceManager(state)
	driver, ok := managedServiceDriverForManager(manager)
	if !ok {
		return managedServiceDriver{}, fmt.Errorf("service manager is %q; run `codex-remote service install-user` first", manager)
	}
	return driver, nil
}
