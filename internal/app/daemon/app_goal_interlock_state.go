package daemon

import (
	"log"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/goalinterlockstore"
)

func (a *App) configureGoalInterlockStateLocked(stateDir string) {
	path := goalinterlockstore.StatePath(stateDir)
	a.goalInterlockRuntime = loadPersistedStore("goal queue interlock", path, goalinterlockstore.LoadStore)
	if a.goalInterlockRuntime.store == nil || !a.goalInterlockRuntime.writable() {
		return
	}
	a.service.RestoreGoalInterlockLeases(a.goalInterlockRuntime.store.Leases())
}

func (a *App) persistGoalInterlockStateLocked(now time.Time) {
	if a.goalInterlockRuntime.store == nil || !a.goalInterlockRuntime.writable() {
		return
	}
	if now.Unix()%2 != 0 {
		return
	}
	leases := a.service.GoalInterlockLeases()
	if err := a.goalInterlockRuntime.store.ReplaceAll(leases); err != nil {
		log.Printf("persist goal queue interlock state failed: err=%v", err)
	}
}
