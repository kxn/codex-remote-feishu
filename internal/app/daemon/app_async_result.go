package daemon

import (
	"context"

	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

type daemonAsyncResult struct {
	Apply func(context.Context, *App)
}

func (a *App) enqueueDaemonAsyncResult(result daemonAsyncResult) {
	if result.Apply == nil {
		return
	}
	a.mu.Lock()
	a.enqueueDaemonAsyncResultLocked(result)
	a.mu.Unlock()
}

func (a *App) enqueueDaemonAsyncResultLocked(result daemonAsyncResult) {
	if result.Apply == nil {
		return
	}
	if a.shuttingDown {
		return
	}
	a.daemonAsyncRuntime.pending = append(a.daemonAsyncRuntime.pending, result)
	if a.daemonAsyncRuntime.drainQueued {
		return
	}
	a.daemonAsyncRuntime.drainQueued = true

	go a.runQueuedDaemonAsyncResults()
}

func (a *App) runQueuedDaemonAsyncResults() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.drainDaemonAsyncResultsLocked(context.Background())
}

func (a *App) drainDaemonAsyncResultsLocked(ctx context.Context) {
	for {
		if len(a.daemonAsyncRuntime.pending) == 0 {
			a.daemonAsyncRuntime.drainQueued = false
			return
		}
		result := a.daemonAsyncRuntime.pending[0]
		copy(a.daemonAsyncRuntime.pending, a.daemonAsyncRuntime.pending[1:])
		a.daemonAsyncRuntime.pending[len(a.daemonAsyncRuntime.pending)-1] = daemonAsyncResult{}
		a.daemonAsyncRuntime.pending = a.daemonAsyncRuntime.pending[:len(a.daemonAsyncRuntime.pending)-1]
		if result.Apply != nil && !a.shuttingDown {
			result.Apply(ctx, a)
		}
	}
}

func daemonAsyncUIEvents(events []eventcontract.Event) daemonAsyncResult {
	return daemonAsyncResult{Apply: func(ctx context.Context, a *App) {
		a.handleUIEventsLocked(ctx, events)
	}}
}

func (a *App) queueDaemonAsyncUIEventsLocked(events []eventcontract.Event) {
	if len(events) == 0 {
		return
	}
	a.enqueueDaemonAsyncResultLocked(daemonAsyncUIEvents(events))
}
