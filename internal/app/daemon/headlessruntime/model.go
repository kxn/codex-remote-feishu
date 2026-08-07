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
	// StatusOnline 是 admin summary 的对外协议状态：实例在线但无
	// managed 进程状态覆盖时的展示值。
	StatusOnline = "online"
	// StatusStopped / StatusDeleted 是 Online 推导的防御值域成员；
	// 当前没有生产者，保留以覆盖历史/外部状态值。
	StatusStopped = "stopped"
	StatusDeleted = "deleted"
)

// OnlineState 是实例在线状态的推导结果。
type OnlineState int

const (
	// OnlineUnknown 表示状态不在已知值域内，保持调用方既有值。
	OnlineUnknown OnlineState = iota
	// OnlineTrue 表示状态视为在线。
	OnlineTrue
	// OnlineFalse 表示状态视为离线。
	OnlineFalse
)

// StatusOnlineState 是 "状态 → 在线/离线" 推导的唯一实现。
// 所有 Online 布尔推导必须委托本函数，禁止在调用点维护第二套状态表。
func StatusOnlineState(status string) OnlineState {
	switch strings.TrimSpace(status) {
	case StatusBusy, StatusIdle, StatusOnline:
		return OnlineTrue
	case StatusOffline, StatusStarting, StatusStopping, StatusStopped, StatusDeleted:
		return OnlineFalse
	default:
		return OnlineUnknown
	}
}

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
	return state.IsManagedHeadlessInstance(inst)
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
