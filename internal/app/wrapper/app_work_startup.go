package wrapper

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/runtimejournal"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/debuglog"
)

func recordUnstartedWorkFailure(snapshot *agentproto.RuntimeLaunchSnapshot, stage string) {
	if snapshot == nil || snapshot.Validate() != nil || snapshot.CarrierIncarnation == "" || snapshot.JournalPath == "" {
		return
	}
	process, err := hostinventory.CaptureProcess(os.Getpid())
	if err != nil {
		log.Printf("capture managed wrapper startup identity: %v", err)
		return
	}
	receipt := runtimejournal.StartupFailure{
		Owner: snapshot.Owner, CarrierIncarnation: snapshot.CarrierIncarnation, Stage: stage, ObservedAt: time.Now().UTC(),
		Process: agentproto.RuntimeProcessIdentity{PID: process.PID, Platform: process.Platform, StartIdentity: process.StartIdentity, HostID: snapshot.HostID},
	}
	if err = runtimejournal.WriteStartupFailure(*snapshot, receipt); err != nil {
		log.Printf("write managed wrapper startup failure: %v", err)
	}
}

func (a *App) launchInitialChild(ctx context.Context, logger *debuglog.RawLogger, report func(agentproto.ErrorInfo)) (*childSession, error) {
	child, err := a.runtime.Launch(ctx, a, logger, report)
	if err != nil {
		a.recordWorkStartupFailure("native_bootstrap")
	}
	return child, err
}

func (a *App) recordWorkStartupFailure(stage string) {
	if !a.managedInspectionEnabled() {
		return
	}
	identity := a.carrierProcessIdentity()
	if identity == nil {
		return
	}
	inspection := a.work.inspection
	inspection.mu.Lock()
	scopes := append([]*hostinventory.Scope(nil), inspection.scopes...)
	untracked := inspection.startupProcessUntracked
	inspection.mu.Unlock()
	if untracked {
		return
	}
	receipt := runtimejournal.StartupFailure{Owner: a.work.snapshot.Owner, CarrierIncarnation: a.work.snapshot.CarrierIncarnation, Process: *identity, Stage: stage}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for _, scope := range scopes {
		// Startup callers have already cancelled and waited on the root after
		// an initialize failure. Inspect the tracked descendants separately.
		scope.MarkControlledStop()
		if err := scope.StopChildren(ctx); err != nil {
			a.debugf("work startup cleanup: %v", err)
			return
		}
		for {
			state := scope.Inspect()
			if state.Complete && !state.RootAlive && len(state.ProcessIDs) == 0 {
				receipt.Scopes = append(receipt.Scopes, state)
				break
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(25 * time.Millisecond):
			}
		}
	}
	receipt.ObservedAt = time.Now().UTC()
	if err := runtimejournal.WriteStartupFailure(a.work.snapshot, receipt); err != nil {
		a.debugf("work startup failure receipt: %v", err)
	}
}
