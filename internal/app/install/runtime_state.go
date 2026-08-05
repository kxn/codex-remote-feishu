package install

import (
	"context"
	"strconv"
	"strings"
	"time"
)

type RuntimeStateRepairOptions struct {
	CurrentBinaryPath string
	CurrentVersion    string
	ConfigPath        string
	PID               int
}

func RepairRuntimeState(state *InstallState, opts RuntimeStateRepairOptions) bool {
	if state == nil {
		return false
	}

	changed := false
	if currentBinary := strings.TrimSpace(opts.CurrentBinaryPath); currentBinary != "" {
		if strings.TrimSpace(state.CurrentBinaryPath) != currentBinary {
			state.CurrentBinaryPath = currentBinary
			changed = true
		}
	}
	if currentVersion := strings.TrimSpace(opts.CurrentVersion); currentVersion != "" && strings.TrimSpace(state.CurrentVersion) != currentVersion {
		state.CurrentVersion = currentVersion
		changed = true
	}
	if configPath := strings.TrimSpace(opts.ConfigPath); configPath != "" && strings.TrimSpace(state.ConfigPath) != configPath {
		state.ConfigPath = configPath
		changed = true
	}
	if unitPath, ok := detectRuntimeSystemdUserUnit(*state, opts.PID); ok {
		if state.ServiceManager != ServiceManagerSystemdUser {
			state.ServiceManager = ServiceManagerSystemdUser
			changed = true
		}
		if strings.TrimSpace(state.ServiceUnitPath) != unitPath {
			state.ServiceUnitPath = unitPath
			changed = true
		}
	}
	return changed
}

func detectRuntimeSystemdUserUnit(state InstallState, pid int) (string, bool) {
	if serviceRuntimeGOOS != "linux" || pid <= 0 {
		return "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	state = normalizedServiceState(state)
	current, err := systemdUserReadUnitState(ctx, state)
	if err != nil {
		return "", false
	}
	if strings.TrimSpace(current.MainPID) != strconv.Itoa(pid) {
		return "", false
	}
	unitPath := strings.TrimSpace(state.ServiceUnitPath)
	if unitPath == "" {
		unitPath = systemdUserUnitPathForInstance(state.BaseDir, state.InstanceID)
	}
	if unitPath == "" {
		return "", false
	}
	return unitPath, true
}
