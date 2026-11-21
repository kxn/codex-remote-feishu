package workstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type RuntimeModelCatalogKey struct {
	WorkerID            string       `json:"worker_id"`
	SessionGeneration   int64        `json:"session_generation"`
	CarrierIncarnation  string       `json:"carrier_incarnation"`
	SettingsRevision    int64        `json:"settings_revision"`
	PreferencesRevision int64        `json:"preferences_revision"`
	ProfileID           string       `json:"profile_id"`
	Backend             work.Backend `json:"backend"`
	BotGeneration       int64        `json:"bot_generation"`
}
type RuntimeModelCatalog struct {
	Query     agentproto.ModelListCommand     `json:"query"`
	Key       RuntimeModelCatalogKey          `json:"key"`
	CommandID string                          `json:"command_id"`
	Snapshot  agentproto.ModelCatalogSnapshot `json:"snapshot"`
	Journal   agentproto.RuntimeEventIdentity `json:"journal"`
}

func (t *Tx) RuntimeCatalogKey(workerID string) (RuntimeModelCatalogKey, error) {
	var key RuntimeModelCatalogKey
	worker, err := t.GetWorker(workerID)
	if err != nil {
		return key, err
	}
	return t.runtimeCatalogKeyForOwner(worker.CarrierIncarnation, workerID, worker.Generation)
}
func (t *Tx) runtimeCatalogKeyForOwner(incarnation, workerID string, generation int64) (RuntimeModelCatalogKey, error) {
	var key RuntimeModelCatalogKey
	carrier, err := t.GetCarrier(incarnation)
	if err != nil {
		return key, err
	}
	if carrier.Owner.WorkerSessionID != workerID || carrier.Owner.SessionGeneration != generation {
		return key, ErrConflict
	}
	rows, err := t.tx.QueryContext(t.ctx, `SELECT payload FROM operations WHERE kind='worker.launch' AND json_extract(payload,'$.target_id')=? AND json_extract(payload,'$.target_generation')=? LIMIT 2`, workerID, generation)
	if err != nil {
		return key, err
	}
	var launch work.Operation
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return key, err
		}
		var op work.Operation
		if err = json.Unmarshal([]byte(raw), &op); err != nil {
			rows.Close()
			return key, err
		}
		if op.Kind == "worker.launch" && op.TargetGeneration == generation {
			launch = op
			break
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return key, err
	}
	if launch.ID == "" {
		return key, ErrNotFound
	}
	body, err := t.GetContent(carrier.Owner.ScopeID, launch.RequestRef)
	if err != nil {
		return key, err
	}
	var record struct {
		SettingsRevision    int64                            `json:"settings_revision"`
		PreferencesRevision int64                            `json:"preferences_revision"`
		ProfileID           string                           `json:"profile_id"`
		Backend             work.Backend                     `json:"backend"`
		Snapshot            agentproto.RuntimeLaunchSnapshot `json:"snapshot"`
	}
	if err = json.Unmarshal(body.Data, &record); err != nil {
		return key, err
	}
	if record.Snapshot.CarrierIncarnation != incarnation || !record.Snapshot.Owner.SameOwner(carrier.Owner) || record.Backend != carrier.Backend {
		return key, fmt.Errorf("catalog launch contract differs from immutable carrier")
	}
	return RuntimeModelCatalogKey{WorkerID: workerID, SessionGeneration: generation, CarrierIncarnation: incarnation, SettingsRevision: record.SettingsRevision, PreferencesRevision: record.PreferencesRevision, ProfileID: record.ProfileID, Backend: record.Backend, BotGeneration: carrier.Owner.BotGeneration}, nil
}
func (t *Tx) RecordRuntimeModelCatalog(event agentproto.Event) error {
	if event.Kind != agentproto.EventModelCatalogUpdated || event.ModelCatalog == nil || event.Journal == nil || event.CommandID == "" {
		return fmt.Errorf("exact journaled model catalog command required")
	}
	command, err := t.GetNativeCommand(event.CommandID)
	if err != nil {
		return err
	}
	if command.Kind != agentproto.CommandModelList || command.Incarnation != event.Journal.CarrierIncarnation || event.Execution == nil || command.Binding != *event.Execution {
		return fmt.Errorf("catalog command owner mismatch")
	}
	recorded, err := t.GetRuntimeEvent(command.Incarnation, event.Journal.Sequence)
	if err != nil {
		return err
	}
	hash, err := agentproto.RuntimeEventPayloadHash(recorded)
	if err != nil || hash != event.Journal.PayloadHash {
		return fmt.Errorf("catalog event was not committed with this identity")
	}
	key, err := t.runtimeCatalogKeyForOwner(command.Incarnation, command.Binding.WorkerSessionID, command.Binding.SessionGeneration)
	if err != nil {
		return err
	}
	actualHash, e := agentproto.RuntimeEventPayloadHash(event)
	if e != nil || actualHash != event.Journal.PayloadHash {
		return fmt.Errorf("catalog event body hash mismatch")
	}
	canonical, e := t.NativeCommandPayload(command.CommandID, command.Incarnation)
	if e != nil {
		return e
	}
	value := RuntimeModelCatalog{Query: canonical.ModelList, Key: key, CommandID: command.CommandID, Snapshot: *event.ModelCatalog, Journal: *event.Journal}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	keyJSON, _ := json.Marshal(key)
	_, err = t.tx.ExecContext(t.ctx, `INSERT INTO runtime_model_catalogs(command_id,incarnation,sequence,catalog_key,payload) VALUES(?,?,?,?,?) ON CONFLICT(command_id) DO NOTHING`, command.CommandID, command.Incarnation, event.Journal.Sequence, string(keyJSON), string(data))
	if err != nil {
		return err
	}
	op, e := t.GetOperation(command.CommandID)
	if e != nil {
		return e
	}
	if op.State != work.OperationConfirmed {
		before := op.Revision
		op.Revision++
		op.State = work.OperationConfirmed
		op.ExternalReceipt = fmt.Sprintf("journal:%s:%d:%s", event.Journal.CarrierIncarnation, event.Journal.Sequence, event.Journal.PayloadHash)
		_, _, e = t.PutOperation(op, before)
		return e
	}
	return nil
}
func (t *Tx) GetRuntimeModelCatalog(key RuntimeModelCatalogKey) (RuntimeModelCatalog, error) {
	return t.GetRuntimeModelCatalogPage(key, "")
}
func (t *Tx) GetRuntimeModelCatalogPage(key RuntimeModelCatalogKey, cursor string) (RuntimeModelCatalog, error) {
	var value RuntimeModelCatalog
	data, _ := json.Marshal(key)
	var raw string
	err := t.tx.QueryRowContext(t.ctx, `SELECT payload FROM runtime_model_catalogs WHERE catalog_key=? AND coalesce(json_extract(payload,'$.query.cursor'),'')=? ORDER BY sequence DESC LIMIT 1`, string(data), cursor).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	if err != nil {
		return value, err
	}
	err = json.Unmarshal([]byte(raw), &value)
	return value, err
}
func (t *Tx) GetRuntimeModelCatalogForCommand(commandID string) (RuntimeModelCatalog, error) {
	var value RuntimeModelCatalog
	var raw string
	err := t.tx.QueryRowContext(t.ctx, `SELECT payload FROM runtime_model_catalogs WHERE command_id=?`, commandID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	if err != nil {
		return value, err
	}
	err = json.Unmarshal([]byte(raw), &value)
	return value, err
}
