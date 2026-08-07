package wrapper

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type instanceLifetime string

const (
	lifetimeHostBound   instanceLifetime = "host-bound"
	lifetimeDaemonOwned instanceLifetime = "daemon-owned"
	lifetimeStandalone  instanceLifetime = "standalone"
)

func resolveInstanceLifetime(source string, managed bool, lifetimeRaw, parentPIDRaw string, fallbackParentPID int) (instanceLifetime, int, error) {
	lifetime, err := parseLifetimeValue(lifetimeRaw)
	if err != nil {
		return "", 0, err
	}
	if lifetime == "" {
		lifetime = defaultInstanceLifetime(source, managed)
	}

	parentPID, hasParentPID, err := parseParentPIDValue(parentPIDRaw)
	if err != nil {
		return "", 0, err
	}
	if lifetime != lifetimeHostBound {
		return lifetime, 0, nil
	}
	if hasParentPID {
		return lifetime, parentPID, nil
	}
	if fallbackParentPID > 0 {
		return lifetime, fallbackParentPID, nil
	}
	return lifetimeStandalone, 0, nil
}

func parseLifetimeValue(raw string) (instanceLifetime, error) {
	value := strings.TrimSpace(strings.ToLower(raw))
	switch value {
	case "":
		return "", nil
	case string(lifetimeHostBound):
		return lifetimeHostBound, nil
	case string(lifetimeDaemonOwned):
		return lifetimeDaemonOwned, nil
	case string(lifetimeStandalone):
		return lifetimeStandalone, nil
	default:
		return "", fmt.Errorf("unsupported CODEX_REMOTE_LIFETIME: %q", raw)
	}
}

func parseParentPIDValue(raw string) (int, bool, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, false, nil
	}
	pid, err := strconv.Atoi(value)
	if err != nil || pid <= 0 {
		return 0, false, fmt.Errorf("invalid CODEX_REMOTE_PARENT_PID: %q", raw)
	}
	return pid, true, nil
}

func defaultInstanceLifetime(source string, managed bool) instanceLifetime {
	if state.IsInstanceSource(source, state.InstanceSourceHeadless) && managed {
		return lifetimeDaemonOwned
	}
	return lifetimeHostBound
}
