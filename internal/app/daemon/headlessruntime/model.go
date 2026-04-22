package headlessruntime

import (
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/state"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

const (
	StatusStarting = "starting"
	StatusStopping = "stopping"
	StatusBusy     = "busy"
	StatusIdle     = "idle"
	StatusOffline  = "offline"
)

type Process struct {
	InstanceID    string
	PID           int
	RequestedAt   time.Time
	StartedAt     time.Time
	IdleSince     time.Time
	ThreadID      string
	ThreadCWD     string
	WorkspaceRoot string
	DisplayName   string
	Status        string
	LastError     string
	LastHelloAt   time.Time

	RefreshCommandID       string
	RefreshInFlight        bool
	LastRefreshRequestedAt time.Time
	LastRefreshCompletedAt time.Time
}

type PrewarmLaunch struct {
	InstanceID string
	Options    relayruntime.HeadlessLaunchOptions
}

type State struct {
	Processes map[string]*Process
}

func NewState() State {
	return State{
		Processes: map[string]*Process{},
	}
}

func IsManagedInstance(inst *state.InstanceRecord) bool {
	return inst != nil && strings.EqualFold(strings.TrimSpace(inst.Source), "headless") && inst.Managed
}

func LastRefreshActivity(managed *Process) time.Time {
	if managed == nil {
		return time.Time{}
	}
	last := managed.LastRefreshCompletedAt
	for _, candidate := range []time.Time{
		managed.LastRefreshRequestedAt,
		managed.LastHelloAt,
		managed.StartedAt,
		managed.RequestedAt,
	} {
		if candidate.After(last) {
			last = candidate
		}
	}
	return last
}
