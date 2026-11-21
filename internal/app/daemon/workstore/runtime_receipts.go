package workstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

// AcceptRuntimeEvent stores evidence and its domain effects in the same commit.
// The caller may transmit the returned receipt ONLY after Store.Within commits.
// apply must not perform external effects; a rollback also removes the receipt.
func (t *Tx) AcceptRuntimeEvent(instanceID string, event agentproto.Event, now time.Time, apply func(*Tx) error) (receipt agentproto.EventReceipt, replayed bool, err error) {
	j := event.Journal
	if instanceID == "" || j == nil || j.Version != agentproto.JournalProtocolVersion || strings.TrimSpace(j.CarrierIncarnation) == "" || j.Sequence == 0 || j.Sequence > math.MaxInt64 || event.Seq != j.Sequence || now.IsZero() {
		return receipt, false, fmt.Errorf("invalid durable runtime event")
	}
	hash, e := agentproto.RuntimeEventPayloadHash(event)
	if e != nil {
		return receipt, false, e
	}
	if hash != j.PayloadHash {
		return receipt, false, fmt.Errorf("runtime payload hash mismatch")
	}
	err = t.atomic(func() error {
		var oldHash, oldInstance string
		e := t.tx.QueryRowContext(t.ctx, "SELECT payload_hash,instance_id FROM runtime_receipts WHERE carrier_incarnation=? AND sequence=?", j.CarrierIncarnation, j.Sequence).Scan(&oldHash, &oldInstance)
		if e == nil {
			if oldHash != hash || oldInstance != instanceID {
				return ErrIdempotencyConflict
			}
			replayed = true
			return nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		payload, e := json.Marshal(event)
		if e != nil {
			return e
		}
		if _, e = t.tx.ExecContext(t.ctx, "INSERT INTO runtime_receipts(carrier_incarnation,sequence,payload_hash,instance_id,payload,received_at) VALUES(?,?,?,?,?,?)", j.CarrierIncarnation, j.Sequence, hash, instanceID, string(payload), now.UTC().Format(time.RFC3339Nano)); e != nil {
			return e
		}
		if apply != nil {
			return apply(t)
		}
		return nil
	})
	if err == nil {
		receipt = agentproto.EventReceipt{InstanceID: instanceID, CarrierIncarnation: j.CarrierIncarnation, Sequence: j.Sequence, PayloadHash: hash}
	}
	return
}

func (t *Tx) GetRuntimeEvent(incarnation string, sequence uint64) (agentproto.Event, error) {
	var event agentproto.Event
	if sequence == 0 || sequence > math.MaxInt64 {
		return event, fmt.Errorf("invalid runtime sequence")
	}
	var payload string
	err := t.tx.QueryRowContext(t.ctx, "SELECT payload FROM runtime_receipts WHERE carrier_incarnation=? AND sequence=?", incarnation, sequence).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return event, ErrNotFound
	}
	if err != nil {
		return event, err
	}
	err = json.Unmarshal([]byte(payload), &event)
	return event, err
}
