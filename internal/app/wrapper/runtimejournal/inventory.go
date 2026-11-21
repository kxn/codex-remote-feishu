package runtimejournal

import (
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func (s *Store) BindLaunchSnapshot(snapshot agentproto.RuntimeLaunchSnapshot) error {
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	expected := hash(raw)
	if _, err := s.db.Exec(`INSERT OR IGNORE INTO launch_contracts(incarnation,payload_hash) VALUES(?,?)`, s.incarnation, expected); err != nil {
		return err
	}
	var stored string
	if err := s.db.QueryRow(`SELECT payload_hash FROM launch_contracts WHERE incarnation=?`, s.incarnation).Scan(&stored); err != nil {
		return err
	}
	if stored != expected {
		return ErrConflict
	}
	return nil
}

func (s *Store) SaveInventory(raw []byte) error {
	if !json.Valid(raw) {
		return ErrConflict
	}
	_, err := s.db.Exec(`INSERT INTO inventory_state(incarnation,payload) VALUES(?,?) ON CONFLICT(incarnation) DO UPDATE SET payload=excluded.payload`, s.incarnation, raw)
	return err
}
func (s *Store) LoadInventory() ([]byte, error) {
	var raw []byte
	err := s.db.QueryRow(`SELECT payload FROM inventory_state WHERE incarnation=?`, s.incarnation).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return raw, err
}

func (s *Store) PendingNativeInputs(owner agentproto.ExecutionBinding) (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT count(*) FROM frames f JOIN commands c ON c.id=f.command_id WHERE json_extract(c.binding,'$.workerSessionId')=? AND json_extract(c.binding,'$.sessionGeneration')=? AND json_extract(c.binding,'$.controllerGeneration')=? AND f.native_method IN ('user','turn/start','turn/steer','session/prompt') AND NOT EXISTS (SELECT 1 FROM events e WHERE e.incarnation=c.incarnation AND json_extract(e.payload,'$.kind')='native.input.receipt' AND json_extract(e.payload,'$.requestId')=f.native_id AND json_extract(e.payload,'$.status') IN ('started','completed','cancelled','accepted','rejected')) AND NOT EXISTS (SELECT 1 FROM events e WHERE e.incarnation=c.incarnation AND json_extract(e.payload,'$.kind')='turn.completed' AND json_extract(e.payload,'$.commandId')=c.id AND json_extract(e.payload,'$.turnCompletionOrigin')='runtime')`, owner.WorkerSessionID, owner.SessionGeneration, owner.ControllerGeneration).Scan(&count)
	return count, err
}

func (s *Store) TerminalEvent(sessionID, turnID string) (*agentproto.Event, error) {
	var raw []byte
	err := s.db.QueryRow(`SELECT payload FROM events WHERE incarnation=? AND json_extract(payload,'$.kind')='turn.completed' AND json_extract(payload,'$.threadId')=? AND json_extract(payload,'$.turnId')=? ORDER BY id LIMIT 1`, s.incarnation, sessionID, turnID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var event agentproto.Event
	err = json.Unmarshal(raw, &event)
	return &event, err
}
func (s *Store) RejectedEvent(commandID string) (*agentproto.Event, error) {
	var raw []byte
	err := s.db.QueryRow(`SELECT payload FROM events WHERE incarnation=? AND json_extract(payload,'$.kind')='turn.completed' AND json_extract(payload,'$.commandId')=? AND coalesce(json_extract(payload,'$.turnId'),'')='' AND json_extract(payload,'$.turnCompletionOrigin') IN ('thread_start_rejected','thread_resume_rejected','turn_start_rejected') ORDER BY id LIMIT 1`, s.incarnation, commandID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var event agentproto.Event
	err = json.Unmarshal(raw, &event)
	return &event, err
}
