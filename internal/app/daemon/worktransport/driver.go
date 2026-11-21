// Package worktransport connects durable worker commands to the live relay.
// Transport success is never treated as native execution or input consumption.
package worktransport

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workengine"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workruntime"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type Registrar interface {
	RegisterCommand(context.Context, string, agentproto.Command) error
}
type Transport interface {
	SendCommand(string, agentproto.Command) error
}
type Driver struct {
	store     *workstore.Store
	transport Transport
	registrar Registrar
	now       func() time.Time
	mu        sync.Mutex
	waiters   map[string]*inspectionWaiter
}
type inspectionReply struct {
	Result agentproto.RuntimeInspectionResult
	Source work.SourceRef
}
type inspectionWaiter struct {
	request workruntime.InspectionRequest
	result  chan inspectionReply
}

// BindRegistrar is construction-only: call it before exposing the driver or
// starting a relay. It breaks the coordinator/driver constructor dependency.
func (d *Driver) BindRegistrar(registrar Registrar) { d.registrar = registrar }

func New(store *workstore.Store, transport Transport) (*Driver, error) {
	if store == nil || transport == nil {
		return nil, fmt.Errorf("durable store and relay transport are required")
	}
	return &Driver{store: store, transport: transport, now: time.Now, waiters: make(map[string]*inspectionWaiter)}, nil
}

func (d *Driver) SendCommand(ctx context.Context, instanceID string, command agentproto.Command) error {
	if d.registrar == nil || command.Execution == nil {
		return fmt.Errorf("registered worker command required")
	}
	var incarnation string
	err := d.store.Within(ctx, func(tx *workstore.Tx) error {
		worker, err := tx.GetWorker(command.Execution.WorkerSessionID)
		if err != nil {
			return err
		}
		if worker.InstanceID != instanceID {
			return fmt.Errorf("command transport does not own worker")
		}
		incarnation = worker.CarrierIncarnation
		return nil
	})
	if err != nil {
		return err
	}
	if err = d.registrar.RegisterCommand(ctx, incarnation, command); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return d.transport.SendCommand(instanceID, command)
}

func (d *Driver) Send(ctx context.Context, instanceID string, command agentproto.Command) (workengine.DispatchReceipt, error) {
	err := d.SendCommand(ctx, instanceID, command)
	if err != nil {
		return workengine.DispatchReceipt{State: workengine.DispatchUnknown}, err
	}
	return workengine.DispatchReceipt{State: workengine.DispatchDelivered, TransportReceipt: "relay_write_completed"}, nil
}

func (d *Driver) Inspect(ctx context.Context, request workruntime.InspectionRequest) (workruntime.NativeObservation, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	random := make([]byte, 20)
	if _, err := rand.Read(random); err != nil {
		return workruntime.NativeObservation{}, err
	}
	id := "inspection_" + hex.EncodeToString(random)
	binding := request.Binding
	command := agentproto.Command{CommandID: id, IssuedAt: d.now(), Kind: agentproto.CommandRuntimeInspect, Execution: &binding, Target: agentproto.Target{ThreadID: request.SessionID, TurnID: request.TurnID}, RuntimeInspection: &agentproto.RuntimeInspectionRequest{Version: agentproto.RuntimeInspectionProtocolVersion, ExpectedInstanceGeneration: request.Carrier.InstanceGeneration, ExpectedProcessGeneration: request.Carrier.ProcessGeneration, ExpectedLocationGeneration: request.LocationGeneration, StopBackground: request.StopBackground, SubjectCommandID: request.SubjectCommandID}}
	if err := d.persistInspection(ctx, command); err != nil {
		return workruntime.NativeObservation{}, err
	}
	waiter := &inspectionWaiter{request: request, result: make(chan inspectionReply, 1)}
	d.mu.Lock()
	d.waiters[id] = waiter
	d.mu.Unlock()
	defer func() { d.mu.Lock(); delete(d.waiters, id); d.mu.Unlock() }()
	if err := d.SendCommand(ctx, request.Carrier.OriginInstanceID, command); err != nil {
		_ = d.finishInspection(ctx, id, "", err)
		return workruntime.NativeObservation{}, err
	}
	select {
	case <-ctx.Done():
		// A timeout records uncertainty; it neither stops native work nor
		// releases occupancy. A later journal receipt remains auditable.
		_ = d.finishInspection(context.WithoutCancel(ctx), id, "", ctx.Err())
		return workruntime.NativeObservation{}, ctx.Err()
	case reply := <-waiter.result:
		result := reply.Result
		if result.Problem != nil {
			return workruntime.NativeObservation{}, fmt.Errorf("native inspection: %s", result.Problem.Message)
		}
		observation, err := workruntime.ObserveRuntimeInspection(request, result)
		observation.NativeWriteWatermark = result.NativeWriteWatermark
		if err == nil {
			err = d.store.Within(ctx, func(tx *workstore.Tx) error {
				var e error
				observation.CanonicalCommandWatermark, e = tx.NativeCommandWatermark(command.CommandID)
				return e
			})
		}
		observation.InspectionCommandID = command.CommandID
		observation.Source = reply.Source
		return observation, err
	}
}

func (d *Driver) persistInspection(ctx context.Context, command agentproto.Command) error {
	raw, err := json.Marshal(command)
	if err != nil {
		return err
	}
	body := work.NewContent(command.CommandID+"_body", command.Execution.ScopeID, "application/json", raw, d.now())
	return d.store.Within(ctx, func(tx *workstore.Tx) error {
		if err := tx.PutContent(body); err != nil {
			return err
		}
		op := work.Operation{ID: command.CommandID, ScopeID: command.Execution.ScopeID, ContextID: command.Execution.ContextID, Actor: work.Actor{Kind: work.ActorSystem, ID: "runtime-inspector"}, Kind: string(command.Kind), TargetID: command.Execution.WorkerSessionID, TargetGeneration: command.Execution.SessionGeneration, IdempotencyKey: command.CommandID, RequestRef: body.ID, RequestHash: body.SHA256, State: work.OperationSending, Revision: 1, CreatedAt: d.now(), UpdatedAt: d.now()}
		_, _, err := tx.PutOperation(op, 0)
		return err
	})
}
