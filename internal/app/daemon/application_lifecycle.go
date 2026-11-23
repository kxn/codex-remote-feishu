package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/ingressowner"
)

// Start before exposing relay/gateway listeners. These workers outlive the Run
// caller's cancellation so shutdown can stop admission before draining carriers.
func (r *applicationRuntime) startWorkers(ctx context.Context, errors chan<- error) error {
	if r == nil || r.runner == nil || r.ingress == nil || r.router == nil || r.controller == nil {
		return fmt.Errorf("application runtime is not fully assembled")
	}
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()
	if r.done != nil {
		return fmt.Errorf("application runtime already started")
	}
	if err := r.native.restoreAborted(ctx); err != nil {
		return err
	}
	if err := r.native.restoreSealed(ctx); err != nil {
		return err
	}
	workCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	r.cancel, r.done = cancel, make(chan struct{})
	var workers sync.WaitGroup
	start := func(name string, run func(context.Context) error) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if err := run(workCtx); err != nil && workCtx.Err() == nil {
				select {
				case errors <- fmt.Errorf("%s: %w", name, err):
				case <-workCtx.Done():
				}
			}
		}()
	}
	start("private execution", r.runner.Run)
	start("private ingress staging", r.ingress.RunStaging)
	start("team publication recovery", r.reconcileTeamPublications)
	// Independent owners cannot block each other's migration on a slow native
	// stop. The semaphore bounds concurrent source scans and stop preparations.
	slots := make(chan struct{}, 4)
	for _, binding := range r.historical {
		binding := binding
		start("private migration", func(ctx context.Context) error { return r.migrateOwner(ctx, binding, slots) })
	}
	go func() { workers.Wait(); close(r.done) }()
	return nil
}

func (r *applicationRuntime) stopWorkers(ctx context.Context) error {
	r.lifecycleMu.Lock()
	cancel, done := r.cancel, r.done
	r.lifecycleMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("application workers still draining: %w", ctx.Err())
	}
}

func (r *applicationRuntime) reconcileTeamPublications(ctx context.Context) error {
	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()
	last := ""
	for {
		if err := r.teamPublications.ReconcileConfirmed(ctx); err != nil && ctx.Err() == nil {
			if last != err.Error() {
				log.Printf("team publication reconciliation: %v", err)
				last = err.Error()
			}
		} else {
			last = ""
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}

func (r *applicationRuntime) migrateOwner(ctx context.Context, binding ingressowner.OwnerScopeBinding, slots chan struct{}) error {
	migrationID, importID, err := r.migrations.MigrationIDs(ctx, binding)
	if err != nil {
		return err
	}
	covered, last := false, ""
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case slots <- struct{}{}:
		}
		if !covered {
			err = r.recordCurrentCoverage(ctx, binding)
			covered = err == nil
		}
		var progress personalMigrationProgress
		if covered {
			progress, err = r.migrations.AdvanceOwner(ctx, binding, migrationID, importID, time.Now().UTC())
		}
		<-slots
		status := progress.Stage
		if err != nil {
			status += ": " + err.Error()
		}
		if status != last && ctx.Err() == nil {
			log.Printf("private migration scope=%s state=%s holds=%d", binding.ScopeID, status, progress.RemainingHolds)
			last = status
		}
		if err == nil && progress.Stage == "activated" {
			r.ingress.wakeStaging()
			r.runner.signal()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}

func (r *applicationRuntime) recordCurrentCoverage(ctx context.Context, binding ingressowner.OwnerScopeBinding) error {
	r.coverageMu.Lock()
	defer r.coverageMu.Unlock()
	startup, err := r.controller.StartupIngressProof(binding.GatewayID)
	if err != nil {
		return err
	}
	keyBytes, err := json.Marshal([]any{binding.Tenant, binding.AppID, binding.GatewayID, binding.BotGeneration})
	if err != nil {
		return err
	}
	key := string(keyBytes)
	if r.coveredGateways[key] == startup.Nonce {
		return nil
	}
	epoch, err := r.router.NextGatewayCoverageEpoch(ctx, binding.Tenant, binding.AppID, binding.GatewayID, binding.BotGeneration)
	if err != nil {
		return err
	}
	coverage := ingressowner.GatewayCoverage{Tenant: binding.Tenant, AppID: binding.AppID, GatewayID: binding.GatewayID, BotGeneration: binding.BotGeneration, RouterID: r.router.RouterID(), Epoch: epoch, CutoverID: startup.Nonce}
	previous, err := json.Marshal(struct {
		Kind      string `json:"kind"`
		RouterID  string `json:"router_id"`
		CutoverID string `json:"cutover_id"`
	}{"exclusive-daemon-lock", coverage.RouterID, coverage.CutoverID})
	if err != nil {
		return err
	}
	proof, err := json.Marshal(startupCoverageEvidence{Startup: startup, Previous: previous})
	if err != nil {
		return err
	}
	if err = r.router.RecordGatewayCoverage(ctx, coverage, proof); err != nil {
		return err
	}
	if r.coveredGateways == nil {
		r.coveredGateways = make(map[string]string)
	}
	r.coveredGateways[key] = startup.Nonce
	return nil
}
