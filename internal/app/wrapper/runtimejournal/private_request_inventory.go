package runtimejournal

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

// PendingPrivateRequests compares the live wrapper inventory to its durable
// stream in one read transaction. An uncertain response or mismatch is never
// presented as a complete inventory.
func (s *Store) PendingPrivateRequests(ctx context.Context, owner agentproto.NativeOwner, live map[string]string) (agentproto.RuntimePendingRequestInventory, error) {
	result := agentproto.RuntimePendingRequestInventory{Version: 1, CarrierIncarnation: owner.CarrierIncarnation, InstanceGeneration: owner.InstanceGeneration, ProcessGeneration: owner.ProcessGeneration, Requests: []agentproto.RuntimePendingRequest{}}
	if owner.Validate() != nil || owner.CarrierIncarnation != s.incarnation {
		return result, ErrConflict
	}
	s.displayMu.Lock()
	bound := s.privateOwner != nil && *s.privateOwner == owner
	s.displayMu.Unlock()
	if !bound {
		return result, ErrConflict
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	err = tx.QueryRowContext(ctx, `SELECT sequence FROM streams WHERE incarnation=?`, s.incarnation).Scan(&result.EventSequence)
	if err == sql.ErrNoRows {
		err = nil
	}
	if err != nil {
		return result, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT sequence,hash,payload FROM events WHERE incarnation=? AND sequence<=? ORDER BY sequence`, s.incarnation, result.EventSequence)
	if err != nil {
		return result, err
	}
	active := map[string]agentproto.RuntimePendingRequest{}
	sourceCommands := map[string]string{}
	consistent := true
	var lastSequence uint64
	for rows.Next() {
		var seq uint64
		var hash string
		var raw []byte
		if err = rows.Scan(&seq, &hash, &raw); err != nil {
			break
		}
		if seq != lastSequence+1 {
			consistent = false
		}
		lastSequence = seq
		var e agentproto.Event
		if json.Unmarshal(raw, &e) != nil {
			consistent = false
			continue
		}
		actual, eerr := agentproto.RuntimeEventPayloadHash(e)
		if eerr != nil || actual != hash || e.Seq != seq || e.Journal == nil || e.Journal.CarrierIncarnation != owner.CarrierIncarnation || e.Journal.Sequence != seq || e.Journal.PayloadHash != hash {
			consistent = false
			continue
		}
		if e.Kind != agentproto.EventRequestStarted && e.Kind != agentproto.EventRequestResolved {
			continue
		}
		if e.NativeBinding == nil || e.NativeBinding.Owner != owner || e.RequestID == "" {
			consistent = false
			continue
		}
		if e.Kind == agentproto.EventRequestResolved {
			if prior, ok := active[e.RequestID]; !ok || (e.ThreadID != "" && prior.ThreadID != e.ThreadID) || (e.TurnID != "" && prior.TurnID != e.TurnID) {
				consistent = false
				continue
			}
			delete(active, e.RequestID)
			delete(sourceCommands, e.RequestID)
			continue
		}
		if e.ThreadID == "" || e.TurnID == "" {
			consistent = false
			continue
		}
		if _, ok := active[e.RequestID]; ok {
			consistent = false
			continue
		}
		active[e.RequestID] = agentproto.RuntimePendingRequest{RequestID: e.RequestID, ThreadID: e.ThreadID, TurnID: e.TurnID, StartedSequence: seq, StartedHash: hash}
		sourceCommands[e.RequestID] = e.CommandID
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return result, err
	}
	if lastSequence != result.EventSequence {
		consistent = false
	}
	rows, err = tx.QueryContext(ctx, `SELECT payload FROM commands WHERE incarnation=?`, s.incarnation)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			break
		}
		var c agentproto.Command
		if json.Unmarshal(raw, &c) != nil {
			consistent = false
			continue
		}
		if c.Kind == agentproto.CommandRequestRespond {
			if _, ok := active[c.Request.RequestID]; ok {
				consistent = false
			}
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return result, err
	}
	if len(active) != len(live) {
		consistent = false
	}
	for id, v := range active {
		if source, ok := live[id]; !ok || source != sourceCommands[id] {
			consistent = false
		}
		result.Requests = append(result.Requests, v)
	}
	sort.Slice(result.Requests, func(i, j int) bool { return result.Requests[i].StartedSequence < result.Requests[j].StartedSequence })
	result.Complete = consistent
	return result, tx.Commit()
}
