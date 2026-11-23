package daemon

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

// Stop intake and new effects first. Both native runtimes retain the relay
// receiver until actual stop receipts have been processed; disconnect is not
// treated as completion. A timeout leaves the durable recovery evidence intact.
func (a *App) shutdownApplications(ctx context.Context) error {
	r := a.applications
	a.mu.Lock()
	a.shuttingDown = true
	a.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	// Keep the outbound gateway available for final durable notices, while
	// rejecting new human input before workers and native carriers drain.
	var problems []error
	problems = append(problems, r.platformDeliveries.drain(ctx))
	if team := a.teamRuntime; team != nil {
		team.active.Store(false)
		if team.runCancel != nil {
			team.runCancel()
		}
	}
	if err := r.stopWorkers(ctx); err != nil {
		problems = append(problems, err)
		problems = append(problems, a.shutdownWorkRuntime(ctx))
	} else {
		var privateErr, teamErr error
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); privateErr = r.native.Shutdown(ctx) }()
		go func() { defer wg.Done(); teamErr = a.shutdownWorkRuntime(ctx) }()
		wg.Wait()
		problems = append(problems, privateErr, teamErr)
		problems = append(problems, a.publishPrivateShutdownNotices(ctx))
	}
	a.stopGatewayRuntime(true)
	problems = append(problems, r.drainNativeDeliveries(ctx))
	a.stopIngressAndServers()
	return a.finishShutdown(problems...)
}

func (a *App) publishPrivateShutdownNotices(ctx context.Context) error {
	r := a.applications
	effects, err := r.processor.ApplyHostLifecycle(ctx, "shutdown:"+a.daemonLifecycleID, a.daemonStartedAt, func(app *privatechat.App, owner privatechat.Owner) []eventcontract.Event {
		surface := app.Service().Surface(owner.SurfaceID)
		if surface == nil || !surfaceAllowsDaemonLifecycleNotice(surface) {
			return nil
		}
		notice := orchestrator.GlobalRuntimeShutdownNotice(daemonShutdownNoticeText)
		return []eventcontract.Event{{Kind: eventcontract.KindNotice, SurfaceSessionID: owner.SurfaceID, GatewayID: owner.GatewayID, Notice: &notice}}
	})
	if err != nil {
		return err
	}
	var problems []error
	for _, item := range effects {
		if _, err := r.publisher.Publish(ctx, item.Owner, item.Generation, item.Effect.Effect.ID); err != nil {
			problems = append(problems, fmt.Errorf("shutdown display surface=%s: %w", item.Owner.SurfaceID, err))
		}
	}
	return errors.Join(problems...)
}
