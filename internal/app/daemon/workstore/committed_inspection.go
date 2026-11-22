package workstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type CommittedInspection struct {
	Command            agentproto.Command
	Event              agentproto.Event
	Source             work.SourceRef
	CanonicalWatermark int64
}

// Select only an actual committed RPC for the same attempt and target. Later
// unknown/offline inspections cannot replace its frozen native evidence.
func (t *Tx) LatestCommittedInspection(incarnation string, binding agentproto.ExecutionBinding, sessionID, turnID string) (CommittedInspection, error) {
	var result CommittedInspection
	var id string
	err := t.tx.QueryRowContext(t.ctx, `SELECT o.id FROM operations o JOIN native_commands n ON n.command_id=o.id JOIN runtime_receipts r ON r.carrier_incarnation=n.incarnation AND json_extract(r.payload,'$.commandId')=o.id WHERE n.incarnation=? AND n.attempt_id=? AND o.kind='runtime.inspect' AND json_extract(o.payload,'$.state')='confirmed' AND json_extract(r.payload,'$.kind')='runtime.inspection.completed' AND coalesce(json_extract(r.payload,'$.threadId'),'')=? AND coalesce(json_extract(r.payload,'$.turnId'),'')=? ORDER BY n.rowid DESC LIMIT 1`, incarnation, binding.AttemptID, sessionID, turnID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return result, ErrNotFound
	}
	if err != nil {
		return result, err
	}
	op, err := t.GetOperation(id)
	if err != nil {
		return result, err
	}
	if op.Actor != (work.Actor{Kind: work.ActorSystem, ID: "runtime-inspector"}) || op.ScopeID != binding.ScopeID || op.TargetID != binding.WorkerSessionID || op.TargetGeneration != binding.SessionGeneration {
		return result, ErrScopeDenied
	}
	native, err := t.GetNativeCommand(id)
	if err != nil {
		return result, err
	}
	if native.Kind != agentproto.CommandRuntimeInspect || native.Incarnation != incarnation || native.Binding != binding {
		return result, ErrScopeDenied
	}
	result.Command, err = t.NativeCommandPayload(id, incarnation)
	if err != nil {
		return result, err
	}
	canonical := result.Command
	if canonical.Execution == nil || *canonical.Execution != binding || canonical.Target.ThreadID != sessionID || canonical.Target.TurnID != turnID || canonical.RuntimeInspection == nil {
		return result, ErrScopeDenied
	}
	var receipt agentproto.RuntimeInspectionResult
	if err = json.Unmarshal([]byte(op.ExternalReceipt), &receipt); err != nil {
		return result, err
	}
	content, err := t.GetContent(binding.ScopeID, id+"_inspection_result")
	if err != nil {
		return result, err
	}
	if err = json.Unmarshal(content.Data, &result.Event); err != nil {
		return result, err
	}
	event := result.Event
	if event.Kind != agentproto.EventRuntimeInspectionCompleted || event.CommandID != id || event.Execution == nil || *event.Execution != binding || event.ThreadID != sessionID || event.TurnID != turnID || event.Journal == nil || event.Journal.CarrierIncarnation != incarnation || event.RuntimeInspection == nil || !reflect.DeepEqual(*event.RuntimeInspection, receipt) {
		return result, fmt.Errorf("committed inspection differs from its exact canonical RPC")
	}
	persisted, err := t.GetRuntimeEvent(incarnation, event.Journal.Sequence)
	if err != nil {
		return result, err
	}
	hash, err := agentproto.RuntimeEventPayloadHash(event)
	if err != nil {
		return result, err
	}
	if hash != event.Journal.PayloadHash || !reflect.DeepEqual(persisted, event) {
		return result, ErrScopeDenied
	}
	if receipt.RequestCommandID != id || receipt.CarrierIncarnation != incarnation || receipt.Problem != nil || receipt.InstanceGeneration != canonical.RuntimeInspection.ExpectedInstanceGeneration || receipt.ProcessGeneration != canonical.RuntimeInspection.ExpectedProcessGeneration || receipt.LocationGeneration != canonical.RuntimeInspection.ExpectedLocationGeneration {
		return result, ErrScopeDenied
	}
	result.CanonicalWatermark, err = t.NativeCommandWatermark(id)
	if err != nil {
		return result, err
	}
	result.Source = work.SourceRef{Kind: "runtime_inspection", ID: id, Version: hash, EvidenceRef: content.ID}
	return result, nil
}
