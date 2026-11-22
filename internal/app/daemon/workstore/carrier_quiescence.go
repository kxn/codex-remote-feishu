package workstore

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// CarrierQuiescenceAttempt resolves a committed stop receipt for this exact
// carrier boundary. It never uses activity time or a context's latest attempt.
// The caller must still prove exact OS exit and the native journal write fence.
func (t *Tx) CarrierQuiescenceAttempt(worker work.WorkerSession) (string, error) {
	carrier, err := t.GetCarrier(worker.CarrierIncarnation)
	if err != nil {
		return "", err
	}
	if !carrier.Matches(worker) {
		return "", ErrScopeDenied
	}
	var lastEffect int64
	err = t.tx.QueryRowContext(t.ctx, `SELECT coalesce(max(rowid),0) FROM native_commands WHERE incarnation=? AND json_extract(payload,'$.kind') NOT IN (?,?,?,?,?,?)`, worker.CarrierIncarnation, agentproto.CommandRuntimeInspect, agentproto.CommandModelList, agentproto.CommandThreadGoalGet, agentproto.CommandProcessExit, agentproto.CommandTurnInterrupt, agentproto.CommandThreadGoalClear).Scan(&lastEffect)
	if err != nil {
		return "", err
	}
	rows, err := t.tx.QueryContext(t.ctx, `SELECT q.attempt_id FROM quiescence_receipts q JOIN content c ON c.id=json_extract(q.payload,'$.source.evidence_ref') WHERE json_extract(q.payload,'$.run_ref.worker_id')=? AND json_extract(q.payload,'$.run_ref.generation')=? AND json_extract(q.payload,'$.carrier_incarnation')=? AND json_extract(q.payload,'$.instance_id')=? AND json_extract(q.payload,'$.instance_generation')=? AND json_extract(q.payload,'$.process_generation')=? AND json_extract(CAST(c.data AS TEXT),'$.session_id')=? AND json_extract(CAST(c.data AS TEXT),'$.canonical_command_watermark')>=? AND json_extract(CAST(c.data AS TEXT),'$.native_write_watermark')>0 LIMIT 3`, worker.ID, worker.Generation, worker.CarrierIncarnation, worker.InstanceID, worker.InstanceGeneration, worker.ProcessGeneration, worker.NativeSessionID, lastEffect)
	if err != nil {
		return "", err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return "", err
		}
		ids = append(ids, id)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return "", err
	}
	if err = rows.Close(); err != nil {
		return "", err
	}
	if len(ids) == 0 {
		return "", ErrNotFound
	}
	if len(ids) != 1 {
		return "", fmt.Errorf("carrier quiescence receipt is ambiguous")
	}
	id := ids[0]
	proof, err := t.GetQuiescenceReceipt(id)
	if err != nil {
		return "", err
	}
	if err = proof.Validate(); err != nil {
		return "", err
	}
	attempt, err := t.GetAttempt(id)
	if err != nil {
		return "", err
	}
	if attempt.ScopeID != worker.ScopeID || attempt.ContextID != worker.ContextID || attempt.RunRef != proof.RunRef || attempt.RunRef.WorkerID != worker.ID || attempt.RunRef.Generation != worker.Generation {
		return "", ErrScopeDenied
	}
	switch attempt.State {
	case work.AttemptCompleted, work.AttemptFailed, work.AttemptInterrupted:
	default:
		return "", ErrConflict
	}
	body, err := t.GetContent(worker.ScopeID, proof.Source.EvidenceRef)
	if err != nil {
		return "", err
	}
	var observation struct {
		InspectionCommandID       string `json:"inspection_command_id"`
		CanonicalCommandWatermark int64  `json:"canonical_command_watermark"`
		NativeWriteWatermark      int64  `json:"native_write_watermark"`
		CarrierIncarnation        string `json:"carrier_incarnation"`
		InstanceID                string `json:"instance_id"`
		InstanceGeneration        int64  `json:"instance_generation"`
		ProcessGeneration         int64  `json:"process_generation"`
		LocationGeneration        int64  `json:"location_generation"`
		SessionID                 string `json:"session_id"`
		TurnID                    string `json:"turn_id"`
		NativeStopped             bool   `json:"native_stopped"`
		ContinuationStopped       bool   `json:"continuation_stopped"`
		BackgroundWorkStopped     bool   `json:"background_work_stopped"`
	}
	if err = json.Unmarshal(body.Data, &observation); err != nil {
		return "", err
	}
	if observation.CarrierIncarnation != worker.CarrierIncarnation || observation.InstanceID != worker.InstanceID || observation.InstanceGeneration != worker.InstanceGeneration || observation.ProcessGeneration != worker.ProcessGeneration || observation.LocationGeneration != worker.LocationGeneration || observation.SessionID != worker.NativeSessionID || !observation.NativeStopped || !observation.ContinuationStopped || !observation.BackgroundWorkStopped || observation.CanonicalCommandWatermark <= 0 || observation.NativeWriteWatermark <= 0 {
		return "", ErrScopeDenied
	}
	inspection, err := t.GetNativeCommand(observation.InspectionCommandID)
	if err != nil {
		return "", err
	}
	if inspection.Kind != agentproto.CommandRuntimeInspect || inspection.Incarnation != worker.CarrierIncarnation || !carrier.Owner.SameOwner(inspection.Binding) || inspection.Binding.AttemptID != attempt.ID || inspection.Binding.RunRef != attempt.RunRef.RunID || inspection.SessionID != observation.SessionID {
		return "", ErrScopeDenied
	}
	index, err := t.NativeCommandWatermark(inspection.CommandID)
	if err != nil {
		return "", err
	}
	if index != observation.CanonicalCommandWatermark {
		return "", ErrScopeDenied
	}
	if err = t.AssertNoEffectfulCommandsAfter(worker.CarrierIncarnation, index); err != nil {
		return "", err
	}
	if observation.TurnID != "" {
		turn, err := t.GetNativeTurn(worker.CarrierIncarnation, observation.SessionID, observation.TurnID)
		if err != nil {
			return "", err
		}
		if turn.Binding != inspection.Binding || turn.TerminalSequence == 0 {
			return "", ErrScopeDenied
		}
	} else {
		command, err := t.GetNativeCommand(attempt.CommandID)
		if err != nil {
			return "", err
		}
		rejected := command.TerminalRejection != nil || command.Ack != nil && !command.Ack.Accepted && command.Ack.RuntimeReceipt != nil && command.Ack.RuntimeReceipt.Stage == "prepared" && command.Ack.RuntimeReceipt.PreparedFrames == 0
		if !rejected {
			return "", errors.New("carrier stop receipt has no exact native turn or pre-write rejection")
		}
	}
	return id, nil
}
