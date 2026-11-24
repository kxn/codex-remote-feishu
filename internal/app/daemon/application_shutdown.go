package daemon

import (
	"context"
	"errors"
	"fmt"
	"log"
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
	// Keep the outbound gateway available for final durable notices, while
	// rejecting new human input before workers and native carriers drain.
	var problems []error
	// Close intake immediately, then publish with a separate budget before
	// slow native/team shutdown can exhaust the drain context.
	r.platformDeliveries.closeReady()
	noticeCtx, noticeCancel := context.WithTimeout(ctx, a.shutdownGracePeriodValue())
	if err := a.publishPrivateShutdownNotices(noticeCtx); err != nil {
		log.Printf("private shutdown notice delivery incomplete: %v", err)
	}
	noticeCancel()
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
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
	}
	a.stopGatewayRuntime(true)
	problems = append(problems, r.drainNativeDeliveries(ctx))
	a.stopIngressAndServers()
	return a.finishShutdown(problems...)
}

func (a *App) publishPrivateShutdownNotices(ctx context.Context) error {
	r := a.applications
	var problems []error
	for offset := 0; ; offset += 128 {
		owners, err := r.journal.DeadlineOwnersPage(ctx, 128, offset)
		if err != nil {
			return errors.Join(append(problems, err)...)
		}
		for _, entry := range owners {
			if _, err = r.ingress.privatePublicationBindingFor(ctx, entry.Owner, entry.Generation); err != nil {
				continue
			}
			effects, e := r.processor.ApplyOwnerHostLifecycle(ctx, entry.Owner, entry.Generation, "shutdown:"+a.daemonLifecycleID, a.daemonStartedAt, func(app *privatechat.App, owner privatechat.Owner) []eventcontract.Event {
				surface := app.Service().Surface(owner.SurfaceID)
				if surface == nil || !surfaceAllowsDaemonLifecycleNotice(surface) {
					return nil
				}
				notice := orchestrator.GlobalRuntimeShutdownNotice(daemonShutdownNoticeText)
				return []eventcontract.Event{{Kind: eventcontract.KindNotice, SurfaceSessionID: owner.SurfaceID, GatewayID: owner.GatewayID, Notice: &notice}}
			})
			if e != nil {
				problems = append(problems, fmt.Errorf("shutdown notice owner: %w", e))
				continue
			}
			for _, item := range effects {
				publishCtx, publishCancel := context.WithTimeout(ctx, a.shutdownNoticeTimeoutValue())
				_, e = r.publisher.Publish(publishCtx, item.Owner, item.Generation, item.Effect.Effect.ID)
				publishCancel()
				if e != nil {
					problems = append(problems, fmt.Errorf("shutdown display surface=%s: %w", item.Owner.SurfaceID, e))
				}
			}
		}
		if len(owners) < 128 {
			break
		}
	}
	return errors.Join(problems...)
}
