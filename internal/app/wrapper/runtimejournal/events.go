package runtimejournal

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func Critical(event agentproto.Event) bool {
	switch event.Kind {
	case agentproto.EventItemDelta, agentproto.EventThreadTokenUsageUpdated:
		return false
	default:
		return true
	}
}

func (s *Store) PersistEvents(events []agentproto.Event) ([]agentproto.Event, error) {
	s.displayMu.Lock()
	defer s.displayMu.Unlock()
	output := make([]agentproto.Event, 0, len(events))
	var rollbacks []func()
	committed := false
	defer func() {
		if !committed {
			for i := len(rollbacks) - 1; i >= 0; i-- {
				rollbacks[i]()
			}
		}
	}()
	for _, event := range events {
		event = s.foldDisplayEvidence(event)
		frames, rollback := s.publicSummaryEvents(&event, time.Now())
		output = append(output, frames...)
		output = append(output, event)
		if rollback != nil {
			rollbacks = append(rollbacks, rollback)
		}
	}
	if s.privateLive {
		for i := range output {
			if output[i].Kind != agentproto.EventItemDelta {
				continue
			}
			if output[i].Live != nil || output[i].Journal != nil {
				return nil, ErrConflict
			}
			if s.liveNext == 0 || s.liveNext > s.liveEnd {
				var end uint64
				if err := s.db.QueryRow(`INSERT INTO private_live_streams(incarnation,reserved_through) VALUES(?,1024) ON CONFLICT(incarnation) DO UPDATE SET reserved_through=reserved_through+1024 RETURNING reserved_through`, s.incarnation).Scan(&end); err != nil {
					return nil, err
				}
				s.liveNext = end - 1023
				s.liveEnd = end
			}
			digest, err := agentproto.HashRuntimeLiveEvent(output[i])
			if err != nil {
				return nil, err
			}
			output[i].Live = &agentproto.RuntimeLiveIdentity{Version: agentproto.RuntimeLiveProtocolVersion, CarrierIncarnation: s.incarnation, Sequence: s.liveNext, PayloadHash: digest}
			s.liveNext++
		}
	}
	critical := false
	for _, event := range output {
		if Critical(event) {
			critical = true
			break
		}
	}
	if !critical {
		return output, nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for i, event := range output {
		if !Critical(event) {
			continue
		}
		if event.Journal != nil {
			var stored string
			if err := tx.QueryRow(`SELECT hash FROM events WHERE incarnation=? AND sequence=?`, event.Journal.CarrierIncarnation, event.Journal.Sequence).Scan(&stored); err != nil {
				return nil, err
			}
			actual, err := agentproto.RuntimeEventPayloadHash(event)
			if err != nil {
				return nil, err
			}
			if stored != event.Journal.PayloadHash || actual != stored {
				return nil, ErrConflict
			}
			continue
		}
		var sequence uint64
		if err := tx.QueryRow(`UPDATE streams SET sequence=sequence+1 WHERE incarnation=? RETURNING sequence`, s.incarnation).Scan(&sequence); err != nil {
			return nil, err
		}
		event.Seq = sequence
		payloadHash, err := agentproto.RuntimeEventPayloadHash(event)
		if err != nil {
			return nil, err
		}
		event.Journal = &agentproto.RuntimeEventIdentity{Version: agentproto.JournalProtocolVersion, CarrierIncarnation: s.incarnation, Sequence: sequence, PayloadHash: payloadHash}
		raw, err := json.Marshal(event)
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec(`INSERT INTO events(incarnation,sequence,hash,payload) VALUES(?,?,?,?)`, s.incarnation, sequence, payloadHash, raw); err != nil {
			return nil, err
		}
		var frozen agentproto.Event
		if err := json.Unmarshal(raw, &frozen); err != nil {
			return nil, err
		}
		output[i] = frozen
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	committed = true
	s.discardCommittedDisplayEvidence(output)
	return output, nil
}

// Replay pages use a private database cursor, independent of per-incarnation
// sequences. Old incarnations and their original execution binding are retained.
func (s *Store) Replay(after int64, limit int) ([]agentproto.Event, int64, error) {
	if limit <= 0 || limit > 256 {
		limit = 128
	}
	rows, err := s.db.Query(`SELECT id,payload FROM events WHERE acked=0 AND id>? ORDER BY id LIMIT ?`, after, limit)
	if err != nil {
		return nil, after, err
	}
	defer rows.Close()
	var events []agentproto.Event
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&after, &raw); err != nil {
			return nil, after, err
		}
		var event agentproto.Event
		if err := json.Unmarshal(raw, &event); err != nil {
			return nil, after, err
		}
		events = append(events, event)
	}
	return events, after, rows.Err()
}

func (s *Store) Acknowledge(receipt agentproto.EventReceipt) error {
	if receipt.Sequence == 0 || receipt.CarrierIncarnation == "" || receipt.PayloadHash == "" {
		return ErrConflict
	}
	var expected string
	err := s.db.QueryRow(`SELECT hash FROM events WHERE incarnation=? AND sequence=?`, receipt.CarrierIncarnation, receipt.Sequence).Scan(&expected)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	if receipt.PayloadHash != expected {
		return ErrConflict
	}
	_, err = s.db.Exec(`UPDATE events SET acked=1 WHERE incarnation=? AND sequence=? AND hash=?`, receipt.CarrierIncarnation, receipt.Sequence, expected)
	return err
}
