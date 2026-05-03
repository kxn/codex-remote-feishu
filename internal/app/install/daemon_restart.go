package install

import (
	"context"
	"fmt"
	"strings"
)

// RestartInstalledDaemon restarts the daemon through the install lifecycle
// manager recorded in install-state. It intentionally refuses detached installs
// so callers do not kill an unmanaged process.
func RestartInstalledDaemon(ctx context.Context, state InstallState) error {
	if err := ensureSystemdUserConfigured(state); err != nil {
		return err
	}
	state, err := installSystemdUserUnit(ctx, state)
	if err != nil {
		return err
	}
	statePath := strings.TrimSpace(state.StatePath)
	if statePath == "" {
		return fmt.Errorf("install state path is missing")
	}
	if err := WriteState(statePath, state); err != nil {
		return err
	}
	return systemdUserRestart(ctx, state)
}
