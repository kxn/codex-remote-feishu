package workstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type PrivateNativeSettingObservation struct {
	Event      agentproto.Event
	Receipt    agentproto.EventReceipt
	ObservedAt time.Time
}

type PrivateNativeSettingsObservation struct {
	Permission, Plan *PrivateNativeSettingObservation
}

// Only accepted native settings count as observations. Codex config.observed
// is generated from outbound client parameters, so cannot establish effect.
func (t *Tx) ReadPrivateNativeSettings(worker work.WorkerSession) (PrivateNativeSettingsObservation, error) {
	var result PrivateNativeSettingsObservation
	if worker.NativeSessionID == "" || worker.CarrierIncarnation == "" {
		return result, nil
	}
	carrier, err := t.GetCarrier(worker.CarrierIncarnation)
	if errors.Is(err, ErrNotFound) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	current, err := t.GetWorker(worker.ID)
	if err != nil {
		return result, err
	}
	if !carrier.Matches(worker) || !carrier.Matches(current) || current.NativeSessionID != worker.NativeSessionID {
		return result, nil
	}
	bot, err := t.GetRoleBotBinding(worker.RoleID)
	if err != nil {
		return result, err
	}
	if bot.IdentityGeneration != carrier.Owner.BotGeneration {
		return result, nil
	}
	for _, permission := range []bool{true, false} {
		observation, err := t.readPrivateNativeSetting(worker, carrier, permission)
		if err != nil {
			return result, err
		}
		if permission {
			result.Permission = observation
		} else {
			result.Plan = observation
		}
	}
	return result, nil
}

func (t *Tx) readPrivateNativeSetting(worker work.WorkerSession, carrier CarrierRegistration, permission bool) (*PrivateNativeSettingObservation, error) {
	fields := `(coalesce(json_extract(r.payload,'$.planMode'),'')<>'' OR coalesce(json_extract(r.payload,'$.observedPermission.projectedPlanMode'),'')<>'')`
	if permission {
		fields = `(coalesce(json_extract(r.payload,'$.accessMode'),'')<>'' OR coalesce(json_extract(r.payload,'$.observedPermission.nativeMode'),'')<>'' OR coalesce(json_extract(r.payload,'$.observedPermission.projectedAccessMode'),'')<>'' OR coalesce(json_extract(r.payload,'$.threadSettings.approvalPolicy'),'')<>'' OR coalesce(json_extract(r.payload,'$.threadSettings.sandbox'),'')<>'')`
	}
	var raw, hash, instance, received string
	var sequence uint64
	err := t.tx.QueryRowContext(t.ctx, `SELECT r.payload,r.payload_hash,r.instance_id,r.sequence,r.received_at FROM runtime_receipts r WHERE r.carrier_incarnation=? AND r.instance_id=? AND json_extract(r.payload,'$.threadId')=? AND (json_extract(r.payload,'$.kind')=? OR (json_extract(r.payload,'$.kind')=? AND ?=? AND json_extract(r.payload,'$.configScope')='thread' AND json_type(r.payload,'$.observedPermission')='object')) AND `+fields+` AND NOT EXISTS(SELECT 1 FROM runtime_quarantine q WHERE q.incarnation=r.carrier_incarnation AND q.sequence=r.sequence AND q.resolved=0) ORDER BY r.sequence DESC LIMIT 1`, carrier.Incarnation, carrier.OriginInstanceID, worker.NativeSessionID, agentproto.EventThreadSettingsUpdated, agentproto.EventConfigObserved, worker.Backend, work.BackendClaude).Scan(&raw, &hash, &instance, &sequence, &received)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var event agentproto.Event
	if err = json.Unmarshal([]byte(raw), &event); err != nil {
		return nil, err
	}
	computed, err := agentproto.RuntimeEventPayloadHash(event)
	if err != nil {
		return nil, err
	}
	if event.Journal == nil || event.Journal.CarrierIncarnation != carrier.Incarnation || event.Journal.Sequence != sequence || event.Seq != sequence || event.Journal.PayloadHash != hash || computed != hash {
		return nil, fmt.Errorf("private status native observation receipt mismatch")
	}
	if event.Execution != nil && !carrier.Owner.SameOwner(*event.Execution) {
		return nil, nil
	}
	if event.Execution != nil && (event.Execution.AttemptID != "" || event.Execution.RunRef != "") {
		if _, err := t.ValidateNativeBinding(*event.Execution); errors.Is(err, ErrNotFound) {
			return nil, nil
		} else if err != nil {
			return nil, err
		}
	}
	if event.ThreadSettings != nil && event.ThreadSettings.ThreadID != "" && event.ThreadSettings.ThreadID != worker.NativeSessionID {
		return nil, nil
	}
	if event.CommandID != "" {
		command, err := t.ResolveNativeCommand(carrier.Incarnation, event.CommandID)
		if errors.Is(err, ErrNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		if !carrier.Owner.SameOwner(command.Binding) || command.SessionID != "" && command.SessionID != worker.NativeSessionID || event.Execution != nil && command.Binding != *event.Execution {
			return nil, nil
		}
	}
	at, err := time.Parse(time.RFC3339Nano, received)
	if err != nil {
		return nil, err
	}
	return &PrivateNativeSettingObservation{Event: event, Receipt: agentproto.EventReceipt{InstanceID: instance, CarrierIncarnation: carrier.Incarnation, Sequence: sequence, PayloadHash: hash}, ObservedAt: at}, nil
}
