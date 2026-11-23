package daemon

import (
	"fmt"

	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

// The next daemon uses this lock as evidence that the previous source writer
// is gone. A shutdown deadline cannot grant that evidence while a callback or
// worker can still commit. Keep the PID/token record on failure; stale-lock
// recovery checks the actual process exit before a replacement may start.
func releaseApplicationRuntimeLock(lock *relayruntime.FileLock, app *App) error {
	if app != nil {
		if !app.applicationSharedWorkersJoined() {
			return fmt.Errorf("team workers remain active")
		}
		if r := app.applications; r != nil {
			if !r.platformDeliveries.closeReady() {
				return fmt.Errorf("platform callbacks remain active")
			}
			if err := r.close(); err != nil {
				return err
			}
		}
	}
	return lock.Release()
}

func (app *App) applicationSharedWorkersJoined() bool {
	if r := app.teamRuntime; r != nil && r.runDone != nil {
		select {
		case <-r.runDone:
		default:
			return false
		}
	}
	return true
}
