package workstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func (t *Tx) PutCarrierProcess(incarnation, instanceID string, identity agentproto.RuntimeProcessIdentity) error {
	carrier, err := t.GetCarrier(incarnation)
	if err != nil {
		return err
	}
	if carrier.OriginInstanceID != instanceID || identity.PID <= 0 || identity.StartIdentity == "" || identity.Platform == "" {
		return fmt.Errorf("carrier process identity mismatch")
	}
	data, err := json.Marshal(identity)
	if err != nil {
		return err
	}
	_, err = t.tx.ExecContext(t.ctx, `INSERT INTO runtime_carrier_processes(incarnation,instance_id,payload) VALUES(?,?,?) ON CONFLICT(incarnation) DO NOTHING`, incarnation, instanceID, string(data))
	if err != nil {
		return err
	}
	old, err := t.GetCarrierProcess(incarnation)
	if err != nil {
		return err
	}
	if old != identity {
		return ErrConflict
	}
	return nil
}
func (t *Tx) GetCarrierProcess(incarnation string) (agentproto.RuntimeProcessIdentity, error) {
	var value agentproto.RuntimeProcessIdentity
	var raw string
	err := t.tx.QueryRowContext(t.ctx, `SELECT payload FROM runtime_carrier_processes WHERE incarnation=?`, incarnation).Scan(&raw)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = ErrNotFound
		}
		return value, err
	}
	err = json.Unmarshal([]byte(raw), &value)
	return value, err
}
