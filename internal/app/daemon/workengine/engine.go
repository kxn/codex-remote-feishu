// Package workengine admits durable queued work. Planning and transport happen
// outside SQLite transactions; only workstore owns execution facts.
package workengine

import (
	"context"
	"errors"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"sync"
	"time"
)

type Candidate struct {
	Scope      work.WorkScope
	Context    work.WorkContext
	Worker     work.WorkerSession
	Role       work.Role
	BotBinding work.RoleBotBinding
	Task       *work.Task
	Inputs     []work.Inbox
}
type DispatchPlan struct {
	WorkerRevision      int64
	InstructionRevision string
	Command             agentproto.Command
	FrozenRefs          []work.SourceRef
}

var ErrPreparationRequired = errors.New("worker requires safe preparation")

type Planner interface {
	Plan(context.Context, Candidate) (DispatchPlan, error)
}
type DispatchState string

const (
	DispatchDelivered DispatchState = "delivered"
	DispatchUnknown   DispatchState = "unknown"
)

type DispatchReceipt struct {
	State            DispatchState                     `json:"state"`
	Journal          *agentproto.RuntimeCommandReceipt `json:"journal,omitempty"`
	TransportReceipt string                            `json:"transport_receipt,omitempty"`
}
type Dispatcher interface {
	Send(context.Context, string, agentproto.Command) (DispatchReceipt, error)
}
type WorkerNeed struct {
	ScopeID   string
	RoleID    string
	TaskID    string
	ContextID string
}
type Provisioner interface {
	Prepare(context.Context, WorkerNeed) error
}
type Config struct {
	ControlReservedSlots int
	MaxConcurrent        int
	MaxInputsPerAttempt  int
	SchedulerID          string
	Now                  func() time.Time
	Provisioner          Provisioner
}
type Engine struct {
	prepareMu          sync.Mutex
	prepareWG          sync.WaitGroup
	preparationsClosed bool
	preparing          map[string]work.ContextPurpose
	preparationResults chan preparationResult
	store              *workstore.Store
	config             Config
	planner            Planner
	dispatcher         Dispatcher
}

func New(store *workstore.Store, config Config, planner Planner, dispatcher Dispatcher) (*Engine, error) {
	if store == nil || planner == nil || dispatcher == nil || config.MaxConcurrent < 1 {
		return nil, fmt.Errorf("scheduler requires store, planner, dispatcher and positive concurrency")
	}
	if config.ControlReservedSlots < 0 || config.ControlReservedSlots >= config.MaxConcurrent {
		return nil, fmt.Errorf("reserved control slots must be nonnegative and smaller than total slots")
	}
	if config.MaxInputsPerAttempt < 1 {
		config.MaxInputsPerAttempt = 16
	}
	if config.SchedulerID == "" {
		config.SchedulerID = "global"
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &Engine{store: store, config: config, planner: planner, dispatcher: dispatcher, preparing: map[string]work.ContextPurpose{}, preparationResults: make(chan preparationResult, max(64, config.MaxConcurrent*2))}, nil
}

type Deferral struct {
	Need   WorkerNeed
	Reason string
}
type DispatchOutcome struct {
	OperationID string
	AttemptID   string
	InstanceID  string
	State       work.OperationState
	Err         error
}
type TickResult struct {
	Dispatched []DispatchOutcome
	Deferred   []Deferral
}

func (e *Engine) Tick(ctx context.Context) (TickResult, error) {
	result := TickResult{}
	candidates, deferred, needs, err := e.snapshot(ctx)
	if err != nil {
		return result, err
	}
	result.Deferred = deferred
	pending := []PendingDispatch{}
	for _, candidate := range candidates {
		if len(pending) >= e.config.MaxConcurrent {
			break
		}
		if err = ctx.Err(); err != nil {
			return result, err
		}
		var occupied int
		if err = e.store.Within(ctx, func(tx *workstore.Tx) error { var err error; occupied, err = tx.ActiveSlotCount(); return err }); err != nil {
			return result, err
		}
		if occupied >= e.config.MaxConcurrent {
			break
		}
		plan, err := e.planner.Plan(ctx, candidate)
		if err != nil {
			if errors.Is(err, ErrPreparationRequired) {
				needs = append(needs, needFor(candidate))
			}
			result.Deferred = append(result.Deferred, Deferral{Need: needFor(candidate), Reason: err.Error()})
			continue
		}
		dispatch, err := e.admit(ctx, candidate, plan)
		if err != nil {
			result.Deferred = append(result.Deferred, Deferral{Need: needFor(candidate), Reason: err.Error()})
			continue
		}
		pending = append(pending, dispatch)
	}
	// Claims commit before transport. Independent carriers can receive commands
	// concurrently even when one transport is slow or disconnected.
	result.Dispatched = make([]DispatchOutcome, len(pending))
	var wg sync.WaitGroup
	defer wg.Wait()
	for i, dispatch := range pending {
		wg.Add(1)
		go func(i int, dispatch PendingDispatch) { defer wg.Done(); result.Dispatched[i] = e.send(ctx, dispatch) }(i, dispatch)
	}
	if err = e.startPreparations(ctx, needs, &result); err != nil {
		return result, err
	}
	wg.Wait()
	return result, nil
}
func needFor(c Candidate) WorkerNeed {
	taskID := ""
	if c.Task != nil {
		taskID = c.Task.ID
	}
	return WorkerNeed{ScopeID: c.Scope.ID, RoleID: c.Worker.RoleID, TaskID: taskID, ContextID: c.Context.ID}
}

func (e *Engine) AdmissionLimits() workstore.AdmissionLimits {
	return workstore.AdmissionLimits{MaxActiveSlots: e.config.MaxConcurrent, ControlReservedSlots: e.config.ControlReservedSlots}
}
