package daemon

import (
	"context"
	"errors"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/adapter/relayws"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/executionowner"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workhost"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workruntime"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/worktransport"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// A launch without the frozen shared-credential marker remains on the original
// team protocol until it retires. Classification comes from its persisted
// launch, never from a failed authentication or inferred namespace.
func (a *teamExecutionOwnerAdapter) commandAuthority(ctx context.Context, incarnation, instance string) (executionowner.Registration, bool, error) {
	var carrier workstore.CarrierRegistration
	err := a.store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		carrier, e = tx.GetCarrier(incarnation)
		return e
	})
	if err != nil {
		return executionowner.Registration{}, false, err
	}
	if carrier.OriginInstanceID != instance {
		return executionowner.Registration{}, false, executionowner.ErrConflict
	}
	worker, launch, err := a.frozenTeamLaunch(ctx, carrier)
	if err != nil {
		return executionowner.Registration{}, false, err
	}
	if !launch.SharedRelayCredential {
		return executionowner.Registration{}, true, nil
	}
	if launch.NativeHistoryNamespace == "" {
		return executionowner.Registration{}, false, executionowner.ErrConflict
	}
	return teamRegistration(carrier, worker, launch.NativeHistoryNamespace), false, nil
}

func (a *teamExecutionOwnerAdapter) RegisterCommand(ctx context.Context, incarnation string, command agentproto.Command) error {
	var carrier workstore.CarrierRegistration
	if err := a.store.Within(ctx, func(tx *workstore.Tx) error { var e error; carrier, e = tx.GetCarrier(incarnation); return e }); err != nil {
		return err
	}
	reg, historical, err := a.commandAuthority(ctx, incarnation, carrier.OriginInstanceID)
	if err != nil {
		return err
	}
	if historical {
		return a.coordinator.RegisterCommand(ctx, incarnation, command)
	}
	return a.registry.WithLease(ctx, reg, func() error { return a.coordinator.RegisterCommand(ctx, incarnation, command) })
}

func (a *teamExecutionOwnerAdapter) SendRegistered(ctx context.Context, incarnation, instance string, command agentproto.Command, send func() error) error {
	reg, historical, err := a.commandAuthority(ctx, incarnation, instance)
	if err != nil {
		return &worktransport.PreSendError{Err: err}
	}
	if historical {
		if err = a.coordinator.RegisterCommand(ctx, incarnation, command); err != nil {
			return &worktransport.PreSendError{Err: err}
		}
		if err = ctx.Err(); err != nil {
			return &worktransport.PreSendError{Err: err}
		}
		return send()
	}
	if a.outbound == nil {
		return &worktransport.PreSendError{Err: fmt.Errorf("shared team outbound unavailable")}
	}
	a.connectionMu.Lock()
	connection, ok := a.connections[instance]
	a.connectionMu.Unlock()
	if !ok || connection.id == 0 || connection.lease == nil || connection.lease.registration != reg || connection.send == nil {
		return &worktransport.PreSendError{Err: fmt.Errorf("authenticated team connection unavailable")}
	}
	return a.outbound.SendLeased(ctx, reg, func(lease *executionowner.Lease) error {
		if command.Kind != agentproto.CommandTurnInterrupt && command.Kind != agentproto.CommandRuntimeInspect && command.Kind != agentproto.CommandProcessExit {
			if err := lease.CheckBusiness(reg); err != nil {
				return err
			}
		}
		if command.Target.ThreadID != "" && (command.Kind == agentproto.CommandPromptSend || command.Kind == agentproto.CommandTurnSteer || command.Kind == agentproto.CommandThreadShellCommand || command.Kind == agentproto.CommandReviewStart) {
			if err := lease.ReserveSession(reg, command.Target.ThreadID); err != nil {
				return err
			}
		}
		return a.coordinator.RegisterCommand(ctx, incarnation, command)
	}, func() error { return connection.send(command) })
}

// teamHello binds a new-protocol carrier to its exact current websocket. The
// legacy source version is explicitly returned to the existing coordinator.
func (a *App) teamHello(ctx context.Context, meta relayws.ConnectionMeta, hello agentproto.Hello) (bool, error) {
	if a.applications == nil || a.applications.teamOwners == nil || hello.WorkerProtocolVersion == 0 {
		return false, nil
	}
	owners := a.applications.teamOwners
	_, historical, err := owners.commandAuthority(ctx, hello.CarrierIncarnation, hello.Instance.InstanceID)
	if err != nil {
		return true, err
	}
	if historical {
		return false, nil
	}
	if !meta.CarrierAuthenticated {
		return true, executionowner.ErrConflict
	}
	err = a.relay.WithCurrentConnection(hello.Instance.InstanceID, meta.ConnectionID, func() error {
		owners.connectionMu.Lock()
		defer owners.connectionMu.Unlock()
		lease, e := owners.acceptHello(ctx, hello.Instance.InstanceID, hello)
		if e != nil {
			return e
		}
		if owners.connections == nil {
			owners.connections = make(map[string]teamConnection)
		}
		owners.connections[hello.Instance.InstanceID] = teamConnection{id: meta.ConnectionID, lease: lease, send: func(command agentproto.Command) error {
			return a.relay.SendCommandToConnection(hello.Instance.InstanceID, meta.ConnectionID, command)
		}}
		return nil
	})
	if err == nil && a.teamRuntime != nil {
		a.teamRuntime.wakeLane("scheduler")
	}
	return true, err
}
func (a *App) teamEvents(ctx context.Context, meta relayws.ConnectionMeta, instance string, events []agentproto.Event) (bool, error) {
	if a.applications == nil || a.applications.teamOwners == nil {
		return false, nil
	}
	owners := a.applications.teamOwners
	owners.connectionMu.Lock()
	current, ok := owners.connections[instance]
	owners.connectionMu.Unlock()
	if !ok {
		var carriers []workstore.CarrierRegistration
		if err := owners.store.Within(ctx, func(tx *workstore.Tx) error {
			var e error
			carriers, e = tx.ListCarriersForInstance(instance)
			return e
		}); err != nil {
			return true, err
		}
		for _, carrier := range carriers {
			_, historical, e := owners.commandAuthority(ctx, carrier.Incarnation, instance)
			if e != nil {
				return true, e
			}
			if !historical {
				return true, executionowner.ErrConflict
			}
		}
		return false, nil
	}
	if current.id != meta.ConnectionID {
		return true, executionowner.ErrConflict
	}
	var committed []*workruntime.CommittedEvent
	err := a.relay.WithCurrentConnection(instance, meta.ConnectionID, func() error {
		owners.connectionMu.Lock()
		defer owners.connectionMu.Unlock()
		now, ok := owners.connections[instance]
		if !ok || now.id != meta.ConnectionID || now.lease != current.lease {
			return executionowner.ErrConflict
		}
		for _, event := range events {
			token, e := now.lease.commitEvent(ctx, instance, event)
			if e != nil {
				return e
			}
			committed = append(committed, token)
		}
		return nil
	})
	for i, token := range committed {
		if e := owners.coordinator.PublishCommittedEventTo(ctx, token, func(instance string, receipt agentproto.EventReceipt) error {
			return a.relay.SendEventReceiptToConnection(instance, meta.ConnectionID, receipt)
		}); e != nil {
			err = errors.Join(err, e)
		}
		if events[i].Kind == agentproto.EventRuntimeInspectionCompleted && a.teamRuntime != nil {
			if e := a.teamRuntime.driver.DeliverInspection(ctx, events[i]); e != nil {
				err = errors.Join(err, e)
			}
		}
	}
	return true, err
}

func (a *App) teamAck(ctx context.Context, meta relayws.ConnectionMeta, instance string, ack agentproto.CommandAck) (bool, error) {
	if a.applications == nil || a.applications.teamOwners == nil {
		return false, nil
	}
	owners := a.applications.teamOwners
	owners.connectionMu.Lock()
	current, ok := owners.connections[instance]
	owners.connectionMu.Unlock()
	if !ok {
		if ack.RuntimeReceipt == nil {
			return owners.rejectUnboundSharedInstance(ctx, instance)
		}
		_, old, err := owners.commandAuthority(ctx, ack.RuntimeReceipt.CarrierIncarnation, instance)
		if err != nil {
			return true, err
		}
		if old {
			return false, nil
		}
		return true, executionowner.ErrConflict
	}
	if current.id != meta.ConnectionID || ack.RuntimeReceipt == nil || ack.RuntimeReceipt.CarrierIncarnation != current.lease.registration.Carrier.Incarnation {
		return true, executionowner.ErrConflict
	}
	var committed *workruntime.CommittedAck
	err := a.relay.WithCurrentConnection(instance, meta.ConnectionID, func() error {
		owners.connectionMu.Lock()
		defer owners.connectionMu.Unlock()
		now, exists := owners.connections[instance]
		if !exists || now.id != meta.ConnectionID || now.lease != current.lease {
			return executionowner.ErrConflict
		}
		return owners.registry.WithLease(ctx, current.lease.registration, func() error { var e error; committed, e = owners.coordinator.CommitAck(ctx, instance, ack); return e })
	})
	if err == nil {
		err = owners.coordinator.PublishCommittedAck(ctx, committed)
	}
	return true, err
}
func (a *App) teamDisconnect(ctx context.Context, meta relayws.ConnectionMeta, instance string) (bool, error) {
	if a.applications == nil || a.applications.teamOwners == nil {
		return false, nil
	}
	owners := a.applications.teamOwners
	owners.connectionMu.Lock()
	current, ok := owners.connections[instance]
	if !ok {
		owners.connectionMu.Unlock()
		return owners.rejectUnboundSharedInstance(context.WithoutCancel(ctx), instance)
	}
	defer owners.connectionMu.Unlock()
	if current.id != meta.ConnectionID {
		return true, nil
	}
	ctx = context.WithoutCancel(ctx)
	err := owners.registry.WithLease(ctx, current.lease.registration, func() error { return owners.coordinator.Disconnect(ctx, instance) })
	if err == nil {
		delete(owners.connections, instance)
	}
	return true, err
}

func (a *teamExecutionOwnerAdapter) rejectUnboundSharedInstance(ctx context.Context, instance string) (bool, error) {
	var carriers []workstore.CarrierRegistration
	if err := a.store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		carriers, e = tx.ListCarriersForInstance(instance)
		return e
	}); err != nil {
		return true, err
	}
	for _, carrier := range carriers {
		_, old, err := a.commandAuthority(ctx, carrier.Incarnation, instance)
		if err != nil {
			return true, err
		}
		if !old {
			return true, executionowner.ErrConflict
		}
	}
	return false, nil
}

// RetireStoppedCarrier is invoked only after team host persisted its stopped
// inspection and exact OS exit. Historical source carriers stay on their
// original lifecycle; new carriers drain the shared outbound before retirement.
func (a *teamExecutionOwnerAdapter) RetireStoppedCarrier(ctx context.Context, worker work.WorkerSession, carrier workstore.CarrierRegistration) error {
	reg, old, err := a.commandAuthority(ctx, carrier.Incarnation, carrier.OriginInstanceID)
	if err != nil {
		return err
	}
	if old {
		return nil
	}
	if a.outbound == nil {
		return executionowner.ErrConflict
	}
	var attempt string
	if err = a.store.Within(ctx, func(tx *workstore.Tx) error { var e error; attempt, e = tx.CarrierQuiescenceAttempt(worker); return e }); err != nil {
		if !errors.Is(err, workstore.ErrNotFound) {
			return err
		}
		attempt = "rebuild:" + workhost.RebuildOperationID(worker)
	}
	if err = a.outbound.Drain(ctx, reg); err != nil {
		// Exact replay after a crash between registry retirement and host progress.
		if e := a.registry.Retire(ctx, reg, attempt); e == nil {
			return nil
		}
		return err
	}
	return a.outbound.Retire(ctx, reg, attempt)
}
