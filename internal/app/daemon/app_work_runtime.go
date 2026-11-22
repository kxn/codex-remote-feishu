package daemon

import (
	"context"
	"errors"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workcontrol"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workengine"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workhost"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workingress"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workmigration"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workprovision"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workpublish"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workrequests"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workruntime"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workteamhome"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/worktransport"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type teamRuntime struct {
	humanActionsMu      sync.Mutex
	humanActionsPending map[string]bool
	catalogMu           sync.Mutex
	catalogPending      map[string]bool
	catalogViewCursor   string
	runContext          context.Context
	active              atomic.Bool
	legacyFenced        atomic.Bool
	migration           *workmigration.Coordinator
	app                 *App
	runCancel           context.CancelFunc
	runDone             chan struct{}
	requests            *workrequests.Service
	signer              *workauth.Signer
	handler             http.Handler
	store               *workstore.Store
	teamMaterials       *workteamhome.Service
	service             *workservice.Service
	engine              *workengine.Engine
	host                *workhost.Host
	codexUpgrader       teamCodexUpgrader
	codexUpgradeHostID  string
	stops               *workcontrol.Stops
	ingress             *workingress.Ingress
	provision           *workprovision.Service
	publisher           *workpublish.Publisher
	coordinator         *workruntime.Coordinator
	driver              *worktransport.Driver
	reconcileWake       chan struct{}
	pendingMu           sync.Mutex
	pending             map[string]bool
	workers             sync.WaitGroup
}

func (r *teamRuntime) enqueueReconcile(attemptID string) {
	r.pendingMu.Lock()
	r.pending[attemptID] = true
	r.pendingMu.Unlock()
	select {
	case r.reconcileWake <- struct{}{}:
	default:
	}
}

// Each lane has independent progress. Native inspection waits never block the
// relay ingress pump, and a slow document API cannot prevent teammate dispatch.
func (r *teamRuntime) run(ctx context.Context) {
	ctx, r.runCancel = context.WithCancel(ctx)
	r.runContext = ctx
	r.runDone = make(chan struct{})
	r.every(ctx, "ingress", time.Second, func(ctx context.Context) error {
		result, err := r.ingress.ProcessPending(ctx, 64)
		if err != nil {
			return err
		}
		if err = r.app.presentTeamSelections(ctx, result.Selections); err != nil {
			return err
		}
		return r.app.presentBlockedPrivateReplies(ctx, result.BlockedReplies)
	})
	r.every(ctx, "private-commands", time.Second, func(ctx context.Context) error {
		_, err := r.ingress.DispatchPrivateCommands(ctx, 32)
		return err
	})
	r.every(ctx, "human-actions", time.Second, r.app.processTeamInteractions)
	r.every(ctx, "menus", time.Second, r.app.runTeamMenus)
	r.every(ctx, "system-host", time.Second, r.app.runTeamSystemHostJobs)
	r.every(ctx, "native-maintenance", time.Second, r.app.runTeamNativeCommands)
	r.every(ctx, "codex-program-maintenance", time.Second, func(ctx context.Context) error {
		if r.codexUpgrader == nil {
			return nil
		}
		return r.codexUpgrader.Run(ctx, 4)
	})
	r.every(ctx, "codex-program-results", 5*time.Second, r.app.runTeamCodexUpgradeViews)
	r.every(ctx, "resource-write-recovery", 15*time.Second, r.app.runTeamResourceWriteRecoveryViews)
	r.every(ctx, "native-questions", time.Second, r.requests.ProjectForms)
	r.every(ctx, "runtime-views", 5*time.Second, r.app.runTeamRuntimeViews)
	r.every(ctx, "review-views", 5*time.Second, r.app.runTeamReviewViews)
	r.every(ctx, "project-provision-recovery", 15*time.Second, r.app.runTeamProvisionRecoveryViews)
	r.every(ctx, "publication-recovery", 15*time.Second, r.app.runTeamPublicationRecoveryViews)
	r.every(ctx, "project-membership", 2*time.Minute, r.app.runTeamProjectMembershipHealth)
	r.every(ctx, "project-views", 5*time.Second, r.app.runTeamProjectViews)
	r.every(ctx, "model-catalog-views", time.Second, r.app.advanceTeamModelCatalogViews)
	r.every(ctx, "settings", 5*time.Second, func(ctx context.Context) error { return r.app.syncWorkRoles(ctx, r.store) })
	r.every(ctx, "scheduler", time.Second, r.tickScheduler)
	r.every(ctx, "idle-carrier-recovery", 2*time.Second, func(ctx context.Context) error { return r.coordinator.ReconcileIdleCarriers(ctx, 8) })
	r.every(ctx, "startup-recovery", 2*time.Second, r.host.ReconcileStartupFailures)
	r.every(ctx, "project-lifecycle", time.Second, r.app.runTeamProjectLifecycle)
	r.every(ctx, "project-navigation", 30*time.Second, r.app.runTeamProjectNavigation)
	r.every(ctx, "team-materials", 30*time.Second, r.teamMaterials.Run)
	r.every(ctx, "native-goal-policy", time.Second, func(ctx context.Context) error { return r.coordinator.RunGoalPolicies(ctx, 32) })
	r.every(ctx, "native-stop", time.Second, r.stops.RunOnce)
	r.every(ctx, "publication", time.Second, func(ctx context.Context) error { _, err := r.publisher.RunOnce(ctx, 32); return err })
	r.every(ctx, "provisioning", 5*time.Second, r.runProvisioning)
	r.workers.Add(1)
	go func() { defer r.workers.Done(); r.reconcileLoop(ctx) }()
	go func() { r.workers.Wait(); close(r.runDone) }()
}

func (r *teamRuntime) every(ctx context.Context, name string, interval time.Duration, fn func(context.Context) error) {
	r.workers.Add(1)
	go func() {
		defer r.workers.Done()
		timer := time.NewTicker(interval)
		defer timer.Stop()
		var lastProblem string
		for {
			if err := fn(ctx); err != nil && ctx.Err() == nil {
				if err.Error() != lastProblem {
					log.Printf("team %s: %v", name, err)
					lastProblem = err.Error()
				}
			} else {
				lastProblem = ""
			}
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
			}
		}
	}()
}

func (r *teamRuntime) runProvisioning(ctx context.Context) error {
	var ids []string
	err := r.store.Within(ctx, func(tx *workstore.Tx) error {
		scopes, err := tx.ListScopes()
		if err != nil {
			return err
		}
		for _, scope := range scopes {
			ops, err := tx.ListOperations(scope.ID)
			if err != nil {
				return err
			}
			for _, op := range ops {
				if (op.Kind == "project.provision" || op.Kind == "project.member.provision") && (op.State == work.OperationPrepared || op.State == work.OperationSending || op.State == work.OperationRetryable) {
					ids = append(ids, op.ID)
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	var failures []error
	for _, id := range ids {
		if _, err = r.provision.Run(ctx, id); err != nil {
			failures = append(failures, err)
		}
		if ctx.Err() != nil {
			break
		}
	}
	return errors.Join(failures...)
}

func (r *teamRuntime) reconcileLoop(ctx context.Context) {
	timer := time.NewTicker(15 * time.Second)
	defer timer.Stop()
	for {
		// Recovery also scans durable attempts. Wakeups only reduce latency;
		// an in-memory queue is never the sole recovery authority.
		_ = r.store.Within(ctx, func(tx *workstore.Tx) error {
			scopes, err := tx.ListScopes()
			if err != nil {
				return err
			}
			for _, scope := range scopes {
				attempts, err := tx.ListAttempts(scope.ID)
				if err != nil {
					return err
				}
				for _, attempt := range attempts {
					if _, e := tx.GetQuiescenceReceipt(attempt.ID); errors.Is(e, workstore.ErrNotFound) {
						r.pendingMu.Lock()
						r.pending[attempt.ID] = true
						r.pendingMu.Unlock()
					} else if e != nil {
						return e
					}
				}
			}
			return nil
		})
		r.pendingMu.Lock()
		pending := r.pending
		r.pending = make(map[string]bool)
		r.pendingMu.Unlock()
		var observations sync.WaitGroup
		slots := make(chan struct{}, 8)
		for id := range pending {
			if ctx.Err() != nil {
				observations.Wait()
				return
			}
			// Uncertain native state remains held. A later event or durable
			// scan retries observation, never dispatches another execution.
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				observations.Wait()
				return
			}
			observations.Add(1)
			go func(attemptID string) {
				defer observations.Done()
				defer func() { <-slots }()
				_ = r.coordinator.Reconcile(ctx, attemptID)
			}(id)
		}
		observations.Wait()
		select {
		case <-ctx.Done():
			return
		case <-r.reconcileWake:
		case <-timer.C:
		}
	}
}

func (r *teamRuntime) ownsInstance(ctx context.Context, id string) bool {
	owned := false
	err := r.store.Within(ctx, func(tx *workstore.Tx) error {
		rows, err := tx.ListCarriersForInstance(id)
		owned = len(rows) > 0
		return err
	})
	// A failed authority read must not hand a managed worker to the legacy
	// session controller. The caller will report the store error separately.
	return owned || err != nil
}

func (a *App) handleWorkHello(ctx context.Context, hello agentproto.Hello) bool {
	r := a.teamRuntime
	if r == nil {
		return false
	}
	if hello.WorkerProtocolVersion == 0 && !r.ownsInstance(ctx, hello.Instance.InstanceID) {
		return a.observeLegacyWork(ctx, hello.Instance.InstanceID, "hello", hello)
	}
	if err := r.coordinator.HandleHello(ctx, hello); err != nil {
		log.Printf("team hello rejected: instance=%s error=%v", hello.Instance.InstanceID, err)
	} else {
		a.prefetchTeamModelCatalog(ctx, hello)
	}
	return true
}

func (a *App) handleWorkEvents(ctx context.Context, instanceID string, events []agentproto.Event) bool {
	r := a.teamRuntime
	if r == nil {
		return false
	}
	owned := r.ownsInstance(ctx, instanceID)
	if !owned {
		for _, event := range events {
			if event.Execution != nil || event.Journal != nil {
				owned = true
				break
			}
		}
	}
	if !owned {
		return a.observeLegacyWork(ctx, instanceID, "events", events)
	}
	for _, event := range events {
		if err := r.coordinator.HandleEvent(ctx, instanceID, event); err != nil {
			log.Printf("team event: instance=%s kind=%s error=%v", instanceID, event.Kind, err)
		}
		if event.Kind == agentproto.EventRuntimeInspectionCompleted {
			if err := r.driver.DeliverInspection(ctx, event); err != nil {
				log.Printf("team inspection receipt: instance=%s error=%v", instanceID, err)
			}
		}
	}
	return true
}

func (a *App) handleWorkAck(ctx context.Context, instanceID string, ack agentproto.CommandAck) bool {
	r := a.teamRuntime
	if r == nil {
		return false
	}
	if ack.RuntimeReceipt == nil && !r.ownsInstance(ctx, instanceID) {
		return a.observeLegacyWork(ctx, instanceID, "ack", ack)
	}
	if err := r.coordinator.HandleAck(ctx, instanceID, ack); err != nil {
		log.Printf("team command receipt: instance=%s command=%s error=%v", instanceID, ack.CommandID, err)
	}
	return true
}

func (a *App) handleWorkDisconnect(ctx context.Context, instanceID string) bool {
	r := a.teamRuntime
	if r == nil {
		return false
	}
	if !r.ownsInstance(ctx, instanceID) {
		return a.observeLegacyWork(ctx, instanceID, "disconnect", map[string]bool{"disconnected": true})
	}
	if err := r.coordinator.Disconnect(ctx, instanceID); err != nil {
		log.Printf("team disconnect: instance=%s error=%v", instanceID, err)
	}
	return true
}
