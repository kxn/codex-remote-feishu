package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/executionowner"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workhost"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workruntime"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// teamExecutionOwnerAdapter combines team registration with the independent
// ownership registry. Registering there first makes partial failure fail closed;
// replay of an identical launch completes the team registration.
type teamExecutionOwnerAdapter struct {
	connectionMu     sync.Mutex
	connections      map[string]teamConnection
	store            *workstore.Store
	coordinator      *workruntime.Coordinator
	registry         *executionowner.Registry
	outbound         *executionowner.Outbound
	stateDir, hostID string
}

type teamConnection struct {
	id    uint64
	lease *teamExecutionLease
	send  func(agentproto.Command) error
}

func openTeamExecutionOwners(store *workstore.Store, coordinator *workruntime.Coordinator, stateDir, hostID string) (*teamExecutionOwnerAdapter, error) {
	if store == nil || coordinator == nil || !filepath.IsAbs(stateDir) || hostID == "" {
		return nil, fmt.Errorf("execution owner adapter requires team store, coordinator and local host")
	}
	a := &teamExecutionOwnerAdapter{store: store, coordinator: coordinator, stateDir: stateDir, hostID: hostID}
	registry, err := executionowner.Open(filepath.Join(stateDir, "execution-owners.db"), a)
	if err != nil {
		return nil, err
	}
	a.registry = registry
	return a, nil
}
func (a *teamExecutionOwnerAdapter) Close() error { return a.registry.Close() }

// frozenTeamOwner validates the persisted launch before deriving a public lease.
// Historical launches without the shared-credential marker fail closed here;
// this adapter is enabled only for explicitly enrolled launches.
func (a *teamExecutionOwnerAdapter) frozenTeamLaunch(ctx context.Context, c workstore.CarrierRegistration) (work.WorkerSession, workhost.LaunchRecord, error) {
	var worker work.WorkerSession
	var frozen workhost.LaunchRecord
	err := a.store.Within(ctx, func(tx *workstore.Tx) error {
		var err error
		worker, err = tx.GetWorker(c.Owner.WorkerSessionID)
		if err != nil {
			return err
		}
		if !c.Matches(worker) {
			return fmt.Errorf("team carrier mismatch")
		}
		parts, _ := json.Marshal([]string{worker.ID, fmt.Sprint(worker.Generation)})
		sum := sha256.Sum256(parts)
		id := "launch_" + hex.EncodeToString(sum[:20])
		op, err := tx.GetOperation(id)
		if err != nil {
			return err
		}
		if op.Kind != "worker.launch" || op.TargetID != worker.ID || op.TargetGeneration != worker.Generation {
			return fmt.Errorf("team launch mismatch")
		}
		body, err := tx.GetContent(worker.ScopeID, op.RequestRef)
		if err != nil {
			return err
		}
		if err = json.Unmarshal(body.Data, &frozen); err != nil {
			return err
		}
		launch := frozen
		if launch.WorkerID != worker.ID || launch.ScopeID != worker.ScopeID || launch.Generation != worker.Generation || launch.Backend != worker.Backend || launch.Snapshot.CarrierIncarnation != c.Incarnation || !launch.Snapshot.Owner.SameOwner(c.Owner) {
			return fmt.Errorf("team launch ownership mismatch")
		}
		return nil
	})
	return worker, frozen, err
}
func (a *teamExecutionOwnerAdapter) frozenTeamOwner(ctx context.Context, c workstore.CarrierRegistration) (executionowner.Registration, work.WorkerSession, error) {
	worker, frozen, err := a.frozenTeamLaunch(ctx, c)
	if err != nil {
		return executionowner.Registration{}, worker, err
	}
	if !frozen.SharedRelayCredential || frozen.NativeHistoryNamespace == "" {
		return executionowner.Registration{}, worker, fmt.Errorf("frozen launch lacks native history namespace")
	}
	return teamRegistration(c, worker, frozen.NativeHistoryNamespace), worker, nil
}
func teamRegistration(c workstore.CarrierRegistration, worker work.WorkerSession, namespace string) executionowner.Registration {
	return executionowner.Registration{Carrier: executionowner.Carrier{InstanceID: c.OriginInstanceID, Incarnation: c.Incarnation, Backend: string(c.Backend), Namespace: namespace, Generation: c.InstanceGeneration}, Owner: executionowner.Owner{Application: executionowner.Team, ID: worker.ID, Epoch: c.Owner.ControllerGeneration}}
}
func (a *teamExecutionOwnerAdapter) RegisterCarrier(ctx context.Context, c workstore.CarrierRegistration) error {
	reg, _, err := a.frozenTeamOwner(ctx, c)
	if err != nil {
		return err
	}
	if err = a.registry.Register(ctx, reg); err != nil {
		return err
	}
	return a.registry.WithLease(ctx, reg, func() error { return a.coordinator.RegisterCarrier(ctx, c) })
}

// VerifyStop accepts only the exact attempt selected by the canonical team
// quiescence lookup and re-verifies journal and OS exit on the local host.
func (a *teamExecutionOwnerAdapter) VerifyStop(ctx context.Context, reg executionowner.Registration, attemptID string) error {
	var c workstore.CarrierRegistration
	if err := a.store.Within(ctx, func(tx *workstore.Tx) error {
		var err error
		c, err = tx.GetCarrier(reg.Carrier.Incarnation)
		return err
	}); err != nil {
		return err
	}
	frozen, worker, err := a.frozenTeamOwner(ctx, c)
	if err != nil {
		return err
	}
	if frozen != reg {
		return fmt.Errorf("stop proof owner mismatch")
	}
	if strings.HasPrefix(attemptID, "rebuild:") {
		operationID := strings.TrimPrefix(attemptID, "rebuild:")
		if operationID != workhost.RebuildOperationID(worker) {
			return executionowner.ErrConflict
		}
		return workruntime.VerifyRetiredRebuild(ctx, a.store, a.stateDir, a.hostID, worker, operationID)
	}
	if err = a.store.Within(ctx, func(tx *workstore.Tx) error {
		id, e := tx.CarrierQuiescenceAttempt(worker)
		if e != nil {
			return e
		}
		if id != attemptID {
			return fmt.Errorf("stop proof attempt mismatch")
		}
		return nil
	}); err != nil {
		return err
	}
	_, err = workruntime.VerifyRetiredQuiescence(ctx, a.store, a.stateDir, a.hostID, worker, attemptID)
	return err
}

// teamExecutionLease binds an authenticated connection to its frozen carrier
// and epoch. Every call rechecks the durable lease, so retirement fences it.
type teamExecutionLease struct {
	adapter      *teamExecutionOwnerAdapter
	registration executionowner.Registration
}

// acceptHello is called only for the authenticated relay connection that sent
// hello; it never searches another application on failure.
func (a *teamExecutionOwnerAdapter) acceptHello(ctx context.Context, authenticatedInstance string, hello agentproto.Hello) (*teamExecutionLease, error) {
	var carrier workstore.CarrierRegistration
	err := a.store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		carrier, e = tx.GetCarrier(hello.CarrierIncarnation)
		return e
	})
	if err != nil {
		return nil, err
	}
	reg, _, err := a.frozenTeamOwner(ctx, carrier)
	if err != nil {
		return nil, err
	}
	if authenticatedInstance == "" || authenticatedInstance != reg.Carrier.InstanceID || hello.Instance.InstanceID != authenticatedInstance || string(hello.Instance.Backend) != reg.Carrier.Backend {
		return nil, fmt.Errorf("hello carrier identity mismatch")
	}
	if err = a.registry.WithLease(ctx, reg, func() error { return a.coordinator.HandleHello(ctx, hello) }); err != nil {
		return nil, err
	}
	return &teamExecutionLease{adapter: a, registration: reg}, nil
}
func (l *teamExecutionLease) validate(ctx context.Context) error {
	owner, err := l.adapter.registry.Resolve(ctx, l.registration.Carrier, l.registration.Owner.Epoch)
	if err != nil {
		return err
	}
	if owner != l.registration.Owner {
		return executionowner.ErrConflict
	}
	return nil
}
func (l *teamExecutionLease) handleEvent(ctx context.Context, instance string, event agentproto.Event) error {
	committed, err := l.commitEvent(ctx, instance, event)
	if err != nil {
		return err
	}
	return l.adapter.coordinator.PublishCommittedEvent(ctx, committed)
}
func (l *teamExecutionLease) commitEvent(ctx context.Context, instance string, event agentproto.Event) (*workruntime.CommittedEvent, error) {
	if instance != l.registration.Carrier.InstanceID {
		return nil, executionowner.ErrConflict
	}
	if event.Journal != nil && event.Journal.CarrierIncarnation != l.registration.Carrier.Incarnation {
		return nil, executionowner.ErrConflict
	}
	if event.Journal == nil && event.Kind != agentproto.EventItemDelta && event.Kind != agentproto.EventThreadTokenUsageUpdated {
		return nil, fmt.Errorf("critical worker event requires durable journal identity")
	}
	var committed *workruntime.CommittedEvent
	err := l.adapter.registry.WithScopedLease(ctx, l.registration, func(scoped *executionowner.Lease) error {
		var e error
		committed, e = l.adapter.coordinator.CommitEvent(ctx, instance, event)
		if e == nil && committed.NativeSession() != "" {
			e = scoped.BindSession(l.registration, committed.NativeSession())
		}
		return e
	})
	if err != nil {
		return nil, err
	}
	return committed, nil
}

func (l *teamExecutionLease) validateSession(ctx context.Context, sessionID string) error {
	owner, err := l.adapter.registry.ResolveSession(ctx, l.registration.Carrier, l.registration.Owner.Epoch, sessionID)
	if err != nil {
		return err
	}
	if owner != l.registration.Owner {
		return executionowner.ErrConflict
	}
	return nil
}
func (l *teamExecutionLease) registerCommand(ctx context.Context, command agentproto.Command) error {
	return l.adapter.registry.WithLease(ctx, l.registration, func() error {
		return l.adapter.coordinator.RegisterCommand(ctx, l.registration.Carrier.Incarnation, command)
	})
}
