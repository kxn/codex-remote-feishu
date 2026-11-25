package runtimejournal

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func (s *Store) PrepareCommand(command agentproto.Command) (*agentproto.RuntimeCommandReceipt, bool, error) {
	ownerOnly := command.IsOwnerOnlyRuntimeCommand()
	if command.NativeBinding != nil || command.CommandID == "" || command.Execution == nil || (ownerOnly && command.Execution.ValidateOwner() != nil) || (!ownerOnly && command.Execution.ValidateExecution() != nil) {
		return nil, false, ErrConflict
	}
	raw, err := json.Marshal(command)
	if err != nil {
		return nil, false, err
	}
	payloadHash := hash(raw)
	binding, err := json.Marshal(command.Execution)
	if err != nil {
		return nil, false, err
	}
	result, err := s.db.Exec(`INSERT OR IGNORE INTO commands(id,hash,incarnation,binding,payload,stage) VALUES(?,?,?,?,?,?)`, command.CommandID, payloadHash, s.incarnation, binding, raw, StagePrepared)
	if err != nil {
		return nil, false, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return nil, false, err
	}
	receipt, err := s.CommandReceipt(command.CommandID)
	if err != nil {
		return nil, false, err
	}
	if receipt.PayloadHash != payloadHash {
		return nil, false, ErrConflict
	}
	return receipt, n == 0, nil
}

func (s *Store) CommandReceipt(commandID string) (*agentproto.RuntimeCommandReceipt, error) {
	receipt := &agentproto.RuntimeCommandReceipt{Version: agentproto.JournalProtocolVersion, CommandID: commandID}
	err := s.db.QueryRow(`SELECT incarnation,hash,stage,(SELECT count(*) FROM frames WHERE command_id=commands.id),(SELECT count(*) FROM frames WHERE command_id=commands.id AND stage='written') FROM commands WHERE id=?`, commandID).Scan(&receipt.CarrierIncarnation, &receipt.PayloadHash, &receipt.Stage, &receipt.PreparedFrames, &receipt.WrittenFrames)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT f.id,f.native_id,f.native_method,f.stage,w.sequence FROM frames f LEFT JOIN private_frame_event_watermarks w ON w.frame_id=f.id WHERE f.command_id=? ORDER BY f.id LIMIT 256`, commandID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var frame agentproto.RuntimeFrameReceipt
		if err := rows.Scan(&frame.FrameID, &frame.NativeRequestID, &frame.NativeMethod, &frame.Stage, &frame.EventWatermark); err != nil {
			return nil, err
		}
		receipt.Frames = append(receipt.Frames, frame)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return receipt, nil
}

func (s *Store) PrepareFrame(commandID string, frame []byte) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	nativeID, method := nativeFrameMetadata(frame)
	result, err := tx.Exec(`INSERT INTO frames(command_id,hash,native_id,native_method,stage) VALUES(?,?,?,?,?)`, commandID, hash(frame), nativeID, method, StagePrepared)
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	if method == "thread/start" || method == "review/start" {
		var raw []byte
		if err = tx.QueryRow(`SELECT payload FROM commands WHERE id=?`, commandID).Scan(&raw); err != nil {
			return 0, err
		}
		var command agentproto.Command
		if json.Unmarshal(raw, &command) != nil {
			return 0, ErrConflict
		}
		if command.Kind == agentproto.CommandReviewStart && command.NativeBinding != nil {
			if _, err = tx.Exec(`INSERT INTO private_review_frames(frame_id,payload) VALUES(?,?)`, id, frame); err != nil {
				return 0, err
			}
		}
	}
	if _, err := tx.Exec(`UPDATE commands SET stage=? WHERE id=? AND stage='written'`, StagePartiallyWritten, commandID); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

func nativeFrameMetadata(frame []byte) (string, string) {
	var message map[string]any
	if json.Unmarshal(frame, &message) != nil {
		return "", ""
	}
	method, _ := message["method"].(string)
	if method != "" {
		if id, ok := message["id"]; ok {
			return fmt.Sprint(id), method
		}
		return "", method
	}
	kind, _ := message["type"].(string)
	if kind == "user" {
		uuid, _ := message["uuid"].(string)
		return uuid, "user"
	}
	requestID, _ := message["request_id"].(string)
	if kind == "control_request" {
		if request, ok := message["request"].(map[string]any); ok {
			if subtype, ok := request["subtype"].(string); ok {
				return requestID, "control_request/" + subtype
			}
		}
	}
	return requestID, kind
}

func (s *Store) StartFrame(frameID int64, frame []byte) error {
	return s.startFrame(frameID, frame, 0, "")
}

func (s *Store) StartMaintenanceFrame(frameID int64, frame []byte, watermark int64, commandID string) error {
	if watermark <= 0 || commandID == "" {
		return ErrConflict
	}
	return s.startFrame(frameID, frame, watermark, commandID)
}

func (s *Store) startFrame(frameID int64, frame []byte, watermark int64, allowedCommand string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if watermark > 0 {
		var count int
		if err := tx.QueryRow(`SELECT count(*) FROM frames f JOIN commands c ON c.id=f.command_id WHERE c.incarnation=? AND f.id>? AND f.command_id<>? AND f.native_method NOT IN ('thread/read','thread/goal/get','model/list')`, s.incarnation, watermark, allowedCommand).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return ErrConflict
		}
	}
	var expected, stage, commandID string
	if err := tx.QueryRow(`SELECT hash,stage,command_id FROM frames WHERE id=?`, frameID).Scan(&expected, &stage, &commandID); err != nil {
		return err
	}
	if (allowedCommand != "" && commandID != allowedCommand) || expected != hash(frame) || stage != StagePrepared {
		return ErrConflict
	}
	if s.privateLive {
		var commandRaw []byte
		if err := tx.QueryRow(`SELECT payload FROM commands WHERE id=? AND incarnation=?`, commandID, s.incarnation).Scan(&commandRaw); err != nil {
			return err
		}
		var command agentproto.Command
		if err := json.Unmarshal(commandRaw, &command); err != nil {
			return err
		}
		_, nativeMethod := nativeFrameMetadata(frame)
		if command.Kind == agentproto.CommandThreadCompactStart && nativeMethod == "thread/compact/start" {
			if err := privateCompactIdleEvidence(tx, s.incarnation, command.Target.ThreadID); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`INSERT INTO private_frame_event_watermarks(frame_id,sequence) SELECT ?,sequence FROM streams WHERE incarnation=?`, frameID, s.incarnation); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE frames SET stage=? WHERE id=?`, StageWriteStarted, frameID); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE commands SET stage=CASE WHEN stage='unknown' THEN stage ELSE ? END WHERE id=?`, StageWriteStarted, commandID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) FinishFrame(frameID int64, writeErr error) error {
	_, err := s.finishFrame(frameID, writeErr, false)
	return err
}

// FinishPrivateFrame commits a response-written fact in the same transaction
// as the exact successful child write. It is replayable even if the process
// exits before the live notification reaches the relay.
func (s *Store) FinishPrivateFrame(frameID int64, writeErr error) (*agentproto.Event, error) {
	return s.finishFrame(frameID, writeErr, true)
}

func (s *Store) finishFrame(frameID int64, writeErr error, private bool) (*agentproto.Event, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var stage, commandID string
	if err := tx.QueryRow(`SELECT stage,command_id FROM frames WHERE id=?`, frameID).Scan(&stage, &commandID); err != nil {
		return nil, err
	}
	if stage != StageWriteStarted {
		return nil, ErrConflict
	}
	stage = StageWritten
	if writeErr != nil {
		stage = StageUnknown
	}
	if _, err := tx.Exec(`UPDATE frames SET stage=? WHERE id=?`, stage, frameID); err != nil {
		return nil, err
	}
	// A known successful frame never resolves an earlier uncertain frame.
	if _, err := tx.Exec(`UPDATE commands SET stage=CASE WHEN stage='unknown' OR EXISTS(SELECT 1 FROM frames WHERE command_id=? AND stage IN ('unknown','child_write_started')) THEN 'unknown' WHEN EXISTS(SELECT 1 FROM frames WHERE command_id=? AND stage='prepared') THEN 'partially_written' ELSE ? END WHERE id=?`, commandID, commandID, stage, commandID); err != nil {
		return nil, err
	}
	var event *agentproto.Event
	if private && writeErr == nil {
		var raw []byte
		if err := tx.QueryRow(`SELECT payload FROM commands WHERE id=? AND incarnation=?`, commandID, s.incarnation).Scan(&raw); err != nil {
			return nil, err
		}
		var command agentproto.Command
		if err := json.Unmarshal(raw, &command); err != nil {
			return nil, err
		}
		if command.Kind == agentproto.CommandRequestRespond {
			if command.NativeBinding == nil || command.Request.RequestID == "" {
				return nil, ErrConflict
			}
			var count int
			if err := tx.QueryRow(`SELECT count(*) FROM frames WHERE command_id=? AND stage<>'written'`, commandID).Scan(&count); err != nil {
				return nil, err
			}
			if count == 0 {
				e := agentproto.Event{Kind: agentproto.EventNativeControlResult, CommandID: commandID, RequestID: command.Request.RequestID, ThreadID: command.Target.ThreadID, TurnID: command.Target.TurnID, Action: "request.response.written", Status: "sent", NativeBinding: command.NativeBinding, Metadata: map[string]any{"frameID": frameID}}
				if err := tx.QueryRow(`UPDATE streams SET sequence=sequence+1 WHERE incarnation=? RETURNING sequence`, s.incarnation).Scan(&e.Seq); err != nil {
					return nil, err
				}
				digest, err := agentproto.RuntimeEventPayloadHash(e)
				if err != nil {
					return nil, err
				}
				e.Journal = &agentproto.RuntimeEventIdentity{Version: agentproto.JournalProtocolVersion, CarrierIncarnation: s.incarnation, Sequence: e.Seq, PayloadHash: digest}
				raw, err := json.Marshal(e)
				if err != nil {
					return nil, err
				}
				if _, err = tx.Exec(`INSERT INTO events(incarnation,sequence,hash,payload) VALUES(?,?,?,?)`, s.incarnation, e.Seq, digest, raw); err != nil {
					return nil, err
				}
				event = &e
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return event, nil
}
