package daemon

import (
	"time"

	headlessruntime "github.com/kxn/codex-remote-feishu/internal/app/daemon/headlessruntime"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const (
	managedHeadlessStatusStarting = headlessruntime.StatusStarting
	managedHeadlessStatusStopping = headlessruntime.StatusStopping
	managedHeadlessStatusBusy     = headlessruntime.StatusBusy
	managedHeadlessStatusIdle     = headlessruntime.StatusIdle
	managedHeadlessStatusOffline  = headlessruntime.StatusOffline
)

type managedHeadlessProcess = headlessruntime.Process
type managedHeadlessPrewarmLaunch = headlessruntime.PrewarmLaunch
type managedHeadlessRuntimeState = headlessruntime.State

func newManagedHeadlessRuntimeState() managedHeadlessRuntimeState {
	return headlessruntime.NewState()
}

func isManagedHeadlessInstance(inst *state.InstanceRecord) bool {
	return headlessruntime.IsManagedInstance(inst)
}

func managedHeadlessLastRefreshActivity(managed *managedHeadlessProcess) time.Time {
	return headlessruntime.LastRefreshActivity(managed)
}
