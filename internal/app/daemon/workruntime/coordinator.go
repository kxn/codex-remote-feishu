// Package workruntime coordinates native evidence without deriving business
// completion or input consumption from a model's final text.
package workruntime

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type Driver interface {
	SendCommand(context.Context, string, agentproto.Command) error
	Inspect(context.Context, InspectionRequest) (NativeObservation, error)
}

type Options struct {
	DefaultGoalTokenBudget int64
	HostID                 string
	StateDir               string
	ApplyReviewEvent       func(*workstore.Tx, workstore.CarrierRegistration, agentproto.ExecutionBinding, agentproto.Event) error
	ApplyNativeRequest     func(*workstore.Tx, workstore.CarrierRegistration, agentproto.ExecutionBinding, agentproto.Event) error
	EnqueueReconcile       func(string)
	Driver                 Driver
	SendReceipt            func(string, agentproto.EventReceipt) error
	Clock                  func() time.Time
	AuthorizeControl       func(context.Context, work.Actor, string, string) error
}

func (c *Coordinator) scheduleReconcile(ctx context.Context, attemptID string) error {
	if c.options.EnqueueReconcile != nil {
		c.options.EnqueueReconcile(attemptID)
		return nil
	}
	return c.Reconcile(ctx, attemptID)
}

type Coordinator struct {
	store   *workstore.Store
	options Options
}

func New(store *workstore.Store, options Options) *Coordinator {
	if options.ApplyReviewEvent == nil {
		options.ApplyReviewEvent = recordReviewNativeEvent
	}
	if options.DefaultGoalTokenBudget == 0 {
		options.DefaultGoalTokenBudget = config.DefaultWorkGoalTokenBudget
	}
	if options.Clock == nil {
		options.Clock = func() time.Time { return time.Now().UTC() }
	}
	return &Coordinator{store: store, options: options}
}

func (c *Coordinator) RegisterCarrier(ctx context.Context, registration workstore.CarrierRegistration) error {
	return c.store.Within(ctx, func(tx *workstore.Tx) error {
		if err := tx.RegisterCarrier(registration); err != nil {
			return err
		}
		worker, err := tx.GetWorker(registration.Owner.WorkerSessionID)
		if err != nil {
			return err
		}
		if worker.State == work.WorkerRecovering {
			occupied, err := tx.HasAnyCarrierOccupancy(registration)
			if err != nil {
				return err
			}
			if _, err := tx.GetRuntimeGate(worker.ID); errors.Is(err, workstore.ErrNotFound) && !occupied {
				return tx.PutRuntimeGate(workstore.RuntimeGate{WorkerID: worker.ID, ScopeID: worker.ScopeID, Reason: "snapshot_rebuild_required", CreatedAt: c.options.Clock()})
			} else if err != nil && !errors.Is(err, workstore.ErrNotFound) {
				return err
			}
		}
		return nil
	})
}

func (c *Coordinator) HandleHello(ctx context.Context, hello agentproto.Hello) error {
	if !hello.Instance.Managed || hello.Instance.Source != "headless" || hello.WorkerProtocolVersion != agentproto.WorkerProtocolVersion || hello.JournalProtocolVersion != agentproto.JournalProtocolVersion || hello.CarrierIncarnation == "" {
		return fmt.Errorf("worker requires managed binding and durable journal protocol")
	}
	return c.store.Within(ctx, func(tx *workstore.Tx) error {
		carrier, err := tx.GetCarrier(hello.CarrierIncarnation)
		if err != nil {
			return err
		}
		if carrier.OriginInstanceID != hello.Instance.InstanceID || string(carrier.Backend) != string(hello.Instance.Backend) {
			return fmt.Errorf("hello does not match pre-registered carrier")
		}
		worker, err := tx.GetWorker(carrier.Owner.WorkerSessionID)
		if err != nil {
			return err
		}
		rebuilt, err := tx.IsRebuiltNativeCarrier(worker)
		if err != nil {
			return err
		}
		if rebuilt {
			return fmt.Errorf("hello arrived after verified carrier exit")
		}
		if hello.CarrierProcess != nil {
			if hello.CarrierProcess.PID != hello.Instance.PID {
				return fmt.Errorf("hello carrier PID mismatch")
			}
			if err := tx.PutCarrierProcess(carrier.Incarnation, hello.Instance.InstanceID, *hello.CarrierProcess); err != nil {
				return err
			}
		}
		if worker.Controller == work.ControllerHuman {
			if !carrier.MatchesPhysical(worker) {
				return fmt.Errorf("historical human carrier physical identity mismatch")
			}
			if err := tx.SetCarrierConnected(carrier.Incarnation, true); err != nil {
				return err
			}
			if err := tx.InvalidateContinuationGrants(carrier.Incarnation); err != nil {
				return err
			}
			if worker.State == work.WorkerReady || worker.State == work.WorkerBusy {
				before := worker.Revision
				worker.State = work.WorkerRecovering
				worker.Revision++
				worker.UpdatedAt = c.options.Clock()
				if err := tx.PutWorker(worker, before); err != nil {
					return err
				}
			}
			return tx.PutRuntimeGate(workstore.RuntimeGate{WorkerID: worker.ID, ScopeID: worker.ScopeID, Reason: "historical_human_controller_requires_reconciliation", RecoveryReason: "historical_human_controller_requires_reconciliation", CreatedAt: c.options.Clock()})
		}
		if !carrier.Matches(worker) {
			if carrier.MatchesPhysical(worker) && worker.Controller == work.ControllerHuman {
				return tx.SetCarrierConnected(carrier.Incarnation, true)
			}
			return fmt.Errorf("stale carrier hello")
		}
		if err := tx.SetCarrierConnected(carrier.Incarnation, true); err != nil {
			return err
		}
		if err := tx.InvalidateContinuationGrants(carrier.Incarnation); err != nil {
			return err
		}
		if worker.State == work.WorkerPreparing || worker.State == work.WorkerRecovering {
			gate, e := tx.GetRuntimeGate(worker.ID)
			if e == nil && (gate.Reason != "snapshot_rebuild_required" || gate.RecoveryReason != "") {
				return nil
			}
			if e != nil && !errors.Is(e, workstore.ErrNotFound) {
				return e
			}
			// An occupied recovering worker must be reconciled, not made ready by a connection.
			if worker.State == work.WorkerRecovering && gate.Reason != "snapshot_rebuild_required" {
				return nil
			}
			expected := worker.Revision
			worker.Revision++
			worker.State = work.WorkerReady
			worker.UpdatedAt = c.options.Clock()
			if err := tx.PutWorker(worker, expected); err != nil {
				return err
			}
			return tx.ClearRuntimeGate(worker.ID, "snapshot_rebuild_required")
		}
		return nil
	})
}

func (c *Coordinator) RegisterCommand(ctx context.Context, incarnation string, command agentproto.Command) error {
	if command.Execution == nil {
		return fmt.Errorf("explicit command owner required")
	}
	return c.store.Within(ctx, func(tx *workstore.Tx) error {
		carrier, err := tx.GetCarrier(incarnation)
		if err != nil {
			return err
		}
		worker, err := tx.GetWorker(carrier.Owner.WorkerSessionID)
		if err != nil {
			return err
		}
		if worker.Controller == work.ControllerHuman && command.Kind != agentproto.CommandRuntimeInspect {
			return fmt.Errorf("historical human controller is isolated from new execution")
		}
		if !carrier.Matches(worker) {
			readonlyOwner := command.IsOwnerOnlyRuntimeCommand()
			rebuilding := false
			if worker.Controller == work.ControllerAutomatic && worker.State == work.WorkerRecovering {
				gate, e := tx.GetRuntimeGate(worker.ID)
				rebuilding = e == nil && gate.Reason == "snapshot_rebuild_required" && gate.RecoveryReason == ""
			}
			if !readonlyOwner || !carrier.MatchesPhysical(worker) || (worker.Controller != work.ControllerHuman && !rebuilding) {
				return fmt.Errorf("command targets stale carrier")
			}
		}
		if !safeReadOrStopCommand(command.Kind) {
			location, e := tx.GetLocation(worker.LocationID)
			if e != nil {
				return e
			}
			if e = tx.AssertNativeUpgradeAvailable(worker.Backend, location.HostID); e != nil {
				return e
			}
			binding, e := tx.GetRoleBotBinding(worker.RoleID)
			if e != nil {
				return e
			}
			if binding.IdentityGeneration != command.Execution.BotGeneration {
				return fmt.Errorf("effectful native command targets a replaced bot identity")
			}
		}
		if command.Kind == agentproto.CommandRuntimeMCPRead || command.Kind == agentproto.CommandMCPOAuthLogin {
			if command.Target.ThreadID != worker.NativeSessionID {
				return fmt.Errorf("MCP command requires the exact current native session")
			}
			if command.Kind == agentproto.CommandMCPOAuthLogin && (command.MCP.OAuthLogin == nil || command.MCP.OAuthLogin.ThreadID != command.Target.ThreadID) {
				return fmt.Errorf("MCP login target must match the bound native session")
			}
		}
		if command.IsOwnerOnlyRuntimeCommand() && (command.Kind == agentproto.CommandThreadGoalGet || command.Kind == agentproto.CommandThreadGoalClear) && (worker.NativeSessionID == "" || command.Target.ThreadID != worker.NativeSessionID) {
			return fmt.Errorf("idle Goal command requires the exact current native session")
		}
		if command.Kind == agentproto.CommandTurnSteer {
			if err := tx.ValidateAttemptInputAdmissionCommand(command); err != nil {
				return err
			}
		}
		if err := validateMaintenanceCommand(tx, carrier, worker, command); err != nil {
			return err
		}
		if command.Kind == agentproto.CommandPromptSend {
			occupied, e := tx.HasCarrierOccupancy(carrier, command.Execution.AttemptID)
			if e != nil {
				return e
			}
			if !occupied {
				return fmt.Errorf("prompt requires the exact occupied business attempt")
			}
		}
		if command.Kind == agentproto.CommandPromptSend {
			namespace := worker.NativeNamespace
			if namespace == "" {
				namespace = string(carrier.Backend)
			}
			for _, session := range []string{command.Target.ThreadID, command.Target.SourceThreadID} {
				if session != "" {
					if err := tx.AssertMigrationResourceAvailable(workstore.MigrationNativeResourceKey(string(carrier.Backend), namespace, session)); err != nil {
						return err
					}
				}
			}
		}
		if command.Kind == agentproto.CommandThreadGoalSet {
			allowed, e := nativeGoalAdmission(tx, worker)
			if e != nil {
				return e
			}
			if !allowed {
				return fmt.Errorf("native Goal is paused or management-only")
			}
			context, e := tx.GetContext(worker.ContextID)
			if e != nil {
				return e
			}
			occupied, e := tx.HasCarrierOccupancy(carrier, command.Execution.AttemptID)
			if e != nil {
				return e
			}
			if !occupied {
				return fmt.Errorf("Goal requires the exact currently occupied business attempt")
			}
			if context.Purpose == work.PurposeControl {
				return fmt.Errorf("management context cannot authorize native Goal continuation")
			}
		}
		if gate, err := tx.GetRuntimeGate(worker.ID); err == nil && !allowedWhileRuntimeGated(command.Kind) && !((gate.Reason == "native_goal_budget_required" || gate.Reason == "native_ownership_unproven") && gate.RecoveryReason == "" && c.authorizedGoalPolicy(tx, command)) {
			return fmt.Errorf("worker dispatch gate is closed")
		} else if err != nil && !errors.Is(err, workstore.ErrNotFound) {
			return err
		}
		grant, err := tx.GetContinuationGrant(incarnation, command.Target.ThreadID)
		if err == nil && command.Kind == agentproto.CommandPromptSend && grant.Active && command.Execution != nil && grant.Binding != *command.Execution {
			return fmt.Errorf("continuation must stop before changing attempt")
		}
		if err != nil && !errors.Is(err, workstore.ErrNotFound) {
			return err
		}
		_, err = tx.RegisterNativeCommand(incarnation, command)
		return err
	})
}

func (c *Coordinator) Disconnect(ctx context.Context, instanceID string) error {
	return c.store.Within(ctx, func(tx *workstore.Tx) error {
		carriers, err := tx.ListCarriersForInstance(instanceID)
		if err != nil {
			return err
		}
		for _, carrier := range carriers {
			if err := tx.SetCarrierConnected(carrier.Incarnation, false); err != nil {
				return err
			}
			if err := tx.InvalidateContinuationGrants(carrier.Incarnation); err != nil {
				return err
			}
			worker, err := tx.GetWorker(carrier.Owner.WorkerSessionID)
			if err != nil {
				return err
			}
			if !carrier.Matches(worker) {
				continue
			}
			gate, e := tx.GetRuntimeGate(worker.ID)
			if e == nil && gate.Reason == "snapshot_rebuild_required" && gate.RecoveryReason == "" {
				occupied, e := tx.HasAnyCarrierOccupancy(carrier)
				if e != nil {
					return e
				}
				if !occupied {
					continue
				}
			} else if e != nil && !errors.Is(e, workstore.ErrNotFound) {
				return e
			}
			if worker.State == work.WorkerRetired || worker.State == work.WorkerUnavailable {
				continue
			}
			expected := worker.Revision
			worker.Revision++
			worker.State = work.WorkerRecovering
			worker.UpdatedAt = c.options.Clock()
			if err := tx.PutWorker(worker, expected); err != nil {
				return err
			}
			if err := tx.PutRuntimeGate(workstore.RuntimeGate{WorkerID: worker.ID, ScopeID: worker.ScopeID, Reason: "carrier_disconnected_reconcile", CreatedAt: c.options.Clock()}); err != nil {
				return err
			}
		}
		return nil
	})
}

func (c *Coordinator) gate(tx *workstore.Tx, carrier workstore.CarrierRegistration, attemptID, reason string) error {
	worker, err := tx.GetWorker(carrier.Owner.WorkerSessionID)
	if err != nil {
		return err
	}
	if !carrier.Matches(worker) {
		return nil
	}
	if worker.State != work.WorkerRecovering && worker.State != work.WorkerRetired && worker.State != work.WorkerUnavailable {
		expected := worker.Revision
		worker.Revision++
		worker.State = work.WorkerRecovering
		worker.UpdatedAt = c.options.Clock()
		if err := tx.PutWorker(worker, expected); err != nil {
			return err
		}
	}
	return tx.PutRuntimeGate(workstore.RuntimeGate{WorkerID: worker.ID, ScopeID: worker.ScopeID, AttemptID: attemptID, Reason: reason, CreatedAt: c.options.Clock()})
}

func allowedWhileRuntimeGated(kind agentproto.CommandKind) bool {
	switch kind {
	case agentproto.CommandRuntimeInspect, agentproto.CommandProcessExit, agentproto.CommandThreadGoalGet, agentproto.CommandThreadGoalClear, agentproto.CommandTurnInterrupt:
		return true
	}
	return false
}
