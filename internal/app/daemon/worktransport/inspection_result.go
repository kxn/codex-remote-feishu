package worktransport

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// DeliverInspection runs only after the coordinator has committed this journal
// event. It also verifies that fact, so callers cannot accidentally turn an
// uncommitted or unrelated transport frame into trusted inspection evidence.
func (d *Driver) DeliverInspection(ctx context.Context, event agentproto.Event) error {
	if event.Kind != agentproto.EventRuntimeInspectionCompleted || event.RuntimeInspection == nil || event.Journal == nil {
		return fmt.Errorf("journaled inspection result required")
	}
	result := *event.RuntimeInspection
	actualHash, err := agentproto.RuntimeEventPayloadHash(event)
	if err != nil || actualHash != event.Journal.PayloadHash {
		return fmt.Errorf("inspection event payload hash mismatch")
	}
	if result.Version != agentproto.RuntimeInspectionProtocolVersion || result.RequestCommandID == "" || result.RequestCommandID != event.CommandID || result.CarrierIncarnation != event.Journal.CarrierIncarnation {
		return fmt.Errorf("inspection result identity mismatch")
	}
	err = d.store.Within(ctx, func(tx *workstore.Tx) error {
		recorded, err := tx.GetRuntimeEvent(event.Journal.CarrierIncarnation, event.Journal.Sequence)
		if err != nil {
			return err
		}
		hash, err := agentproto.RuntimeEventPayloadHash(recorded)
		if err != nil {
			return err
		}
		if hash != event.Journal.PayloadHash {
			return fmt.Errorf("inspection event not committed with this payload")
		}
		command, err := tx.GetNativeCommand(result.RequestCommandID)
		if err != nil {
			return err
		}
		if command.Kind != agentproto.CommandRuntimeInspect || command.Incarnation != result.CarrierIncarnation || event.Execution == nil || command.Binding != *event.Execution {
			return fmt.Errorf("inspection command ownership mismatch")
		}
		content, err := tx.GetContent(command.Binding.ScopeID, command.RequestRef)
		if err != nil {
			return err
		}
		var canonical agentproto.Command
		if err := json.Unmarshal(content.Data, &canonical); err != nil {
			return err
		}
		if canonical.Target.ThreadID != event.ThreadID || canonical.Target.TurnID != event.TurnID {
			return fmt.Errorf("inspection target does not match canonical request")
		}
		if result.Problem == nil && (canonical.RuntimeInspection == nil || result.InstanceGeneration != canonical.RuntimeInspection.ExpectedInstanceGeneration || result.ProcessGeneration != canonical.RuntimeInspection.ExpectedProcessGeneration || result.LocationGeneration != canonical.RuntimeInspection.ExpectedLocationGeneration) {
			return fmt.Errorf("inspection carrier generations do not match canonical request")
		}
		raw, e := json.Marshal(event)
		if e != nil {
			return e
		}
		return tx.PutContent(work.NewContent(event.CommandID+"_inspection_result", command.Binding.ScopeID, "application/json", raw, d.now()))
	})
	if err != nil {
		return err
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	if err = d.finishInspection(ctx, result.RequestCommandID, string(raw), nil); err != nil {
		return err
	}
	d.mu.Lock()
	waiter := d.waiters[result.RequestCommandID]
	if waiter != nil && waiter.request.Carrier.Incarnation == result.CarrierIncarnation && event.Execution != nil && *event.Execution == waiter.request.Binding && event.ThreadID == waiter.request.SessionID && event.TurnID == waiter.request.TurnID {
		select {
		case waiter.result <- inspectionReply{Result: result, Source: work.SourceRef{Kind: "runtime_inspection", ID: event.CommandID, Version: event.Journal.PayloadHash, EvidenceRef: event.CommandID + "_inspection_result"}}:
		default:
		}
	}
	d.mu.Unlock()
	return nil
}

func (d *Driver) finishInspection(ctx context.Context, id, receipt string, problem error) error {
	return d.store.Within(ctx, func(tx *workstore.Tx) error {
		op, err := tx.GetOperation(id)
		if err != nil {
			return err
		}
		if op.Kind != string(agentproto.CommandRuntimeInspect) && op.Kind != string(agentproto.CommandModelList) && op.Kind != string(agentproto.CommandRuntimeMCPRead) {
			return fmt.Errorf("not an inspection operation")
		}
		if op.State == work.OperationConfirmed {
			return nil
		}
		expected := op.Revision
		if problem != nil {
			op.State = work.OperationUnknown
			op.LastError = problem.Error()
		} else {
			op.State = work.OperationConfirmed
			op.ExternalReceipt = receipt
			op.LastError = ""
		}
		op.Revision++
		op.UpdatedAt = d.now()
		_, _, err = tx.PutOperation(op, expected)
		return err
	})
}
