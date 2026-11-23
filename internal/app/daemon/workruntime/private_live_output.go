package workruntime

import (
	"context"
	"errors"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workdisplay"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// Live presentation never advances journal watermarks or settles business input.
// A thread/turn key must resolve to an already committed exact native binding.
func (c *Coordinator) applyLivePrivateOutput(ctx context.Context, instance string, event agentproto.Event) error {
	if event.ThreadID == "" || event.TrafficClass == agentproto.TrafficClassInternalHelper || event.Initiator.Kind == agentproto.InitiatorInternalHelper {
		return nil
	}
	if event.Kind == agentproto.EventItemDelta && !workdisplay.IsOutput(event) {
		return nil
	}
	return c.store.Within(ctx, func(tx *workstore.Tx) error {
		carriers, err := tx.ListCarriersForInstance(instance)
		if err != nil {
			return err
		}
		var matched *workstore.CarrierRegistration
		for _, carrier := range carriers {
			worker, e := tx.GetWorker(carrier.Owner.WorkerSessionID)
			if e != nil {
				return e
			}
			connected, e := tx.CarrierConnected(carrier.Incarnation)
			if e != nil {
				return e
			}
			if !connected || !carrier.Matches(worker) {
				continue
			}
			scope, e := tx.GetScope(carrier.Owner.ScopeID)
			if e != nil {
				return e
			}
			if scope.Kind != work.ScopePersonal && event.Kind != agentproto.EventThreadTokenUsageUpdated {
				continue
			}
			if event.Execution != nil && !carrier.Owner.SameOwner(*event.Execution) {
				continue
			}
			if matched != nil {
				return nil
			}
			copy := carrier
			matched = &copy
		}
		if matched == nil {
			return nil
		}
		if event.Kind == agentproto.EventThreadTokenUsageUpdated && event.TokenUsage != nil {
			if err = tx.RecordThreadTokenUsage(matched.Incarnation, event.ThreadID, event.TokenUsage, c.options.Clock()); err != nil {
				return err
			}
			if event.TurnID == "" {
				return nil
			}
		}
		scope, err := tx.GetScope(matched.Owner.ScopeID)
		if err != nil {
			return err
		}
		if scope.Kind != work.ScopePersonal {
			return nil
		}
		if event.TurnID == "" {
			return nil
		}
		turn, err := tx.GetNativeTurn(matched.Incarnation, event.ThreadID, event.TurnID)
		if errors.Is(err, workstore.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if event.Execution != nil && *event.Execution != turn.Binding {
			return nil
		}
		if _, err = tx.ValidateNativeBinding(turn.Binding); err != nil {
			return nil
		}
		event.Execution = agentproto.CloneExecutionBinding(&turn.Binding)
		return c.applyNativeOutput(tx, *matched, event)
	})
}
