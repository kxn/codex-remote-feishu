package runtimejournal

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

// BindPrivateLaunchSnapshot uses the same incarnation contract as team v1;
// changing mode or any launch field for an incarnation is a conflict.
func (s *Store) BindPrivateLaunchSnapshot(snapshot agentproto.PrivateNativeLaunchSnapshot) error {
	if err := snapshot.Validate(); err != nil || snapshot.Owner.CarrierIncarnation != s.incarnation {
		return ErrConflict
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	expected := hash(raw)
	if _, err = s.db.Exec(`INSERT OR IGNORE INTO launch_contracts(incarnation,payload_hash) VALUES(?,?)`, s.incarnation, expected); err != nil {
		return err
	}
	var stored string
	if err = s.db.QueryRow(`SELECT payload_hash FROM launch_contracts WHERE incarnation=?`, s.incarnation).Scan(&stored); err != nil {
		return err
	}
	if stored != expected {
		return ErrConflict
	}
	s.displayMu.Lock()
	s.privateLive = true
	owner := snapshot.Owner
	s.privateOwner = &owner
	s.displayMu.Unlock()
	return nil
}
func (s *Store) PreparePrivateCommand(command agentproto.Command) (*agentproto.RuntimeCommandReceipt, bool, error) {
	if command.Execution != nil || command.NativeBinding == nil || command.NativeBinding.Validate(false) != nil || command.NativeBinding.Owner.CarrierIncarnation != s.incarnation || command.CommandID == "" {
		return nil, false, ErrConflict
	}
	raw, err := json.Marshal(command)
	if err != nil {
		return nil, false, err
	}
	binding, err := json.Marshal(command.NativeBinding)
	if err != nil {
		return nil, false, err
	}
	digest := hash(raw)
	result, err := s.db.Exec(`INSERT OR IGNORE INTO commands(id,hash,incarnation,binding,payload,stage) VALUES(?,?,?,?,?,?)`, command.CommandID, digest, s.incarnation, binding, raw, StagePrepared)
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
	if receipt == nil || receipt.PayloadHash != digest || receipt.CarrierIncarnation != s.incarnation {
		return nil, false, ErrConflict
	}
	return receipt, n == 0, nil
}
func (s *Store) PrivateBinding(commandID string) (*agentproto.NativeTurnBinding, error) {
	var raw []byte
	err := s.db.QueryRow(`SELECT binding FROM commands WHERE id=? AND incarnation=?`, commandID, s.incarnation).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var binding agentproto.NativeTurnBinding
	if err = json.Unmarshal(raw, &binding); err != nil {
		return nil, err
	}
	if err = binding.Validate(false); err != nil {
		return nil, ErrConflict
	}
	return &binding, nil
}

// PrivateActiveTurn requires committed native association, not an in-memory
// observation. A terminal event always fences later steering.
func (s *Store) PrivateActiveTurn(session, turn string, binding agentproto.NativeTurnBinding) (bool, error) {
	rows, err := s.db.Query(`SELECT payload FROM events WHERE incarnation=? AND json_extract(payload,'$.threadId')=? AND json_extract(payload,'$.turnId')=? ORDER BY id`, s.incarnation, session, turn)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	active := false
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return false, err
		}
		var event agentproto.Event
		if err = json.Unmarshal(raw, &event); err != nil {
			return false, err
		}
		if event.Kind == agentproto.EventTurnCompleted {
			return false, nil
		}
		if event.NativeBinding == nil || *event.NativeBinding != binding || event.CommandID == "" || event.RequestID == "" {
			continue
		}
		if event.Kind == agentproto.EventNativeTurnBindingObserved && event.Status == "accepted" || event.Kind == agentproto.EventNativeInputReceipt && event.Status == "started" || event.Kind == agentproto.EventTurnStarted && event.Metadata["turnIdSource"] == "synthetic_from_observed_user_uuid" {
			active = true
		}
	}
	return active, rows.Err()
}

// ReadPrivateCommandReceipt reads a committed snapshot directly from the
// wrapper journal without its exclusive writer lock. The caller must obtain
// launch from its trusted store and verify the carrier/host lease separately.
func ReadPrivateCommandReceipt(ctx context.Context, snapshot agentproto.PrivateNativeLaunchSnapshot, stateDir, commandID string) (*agentproto.RuntimeCommandReceipt, error) {
	if snapshot.Validate() != nil || strings.TrimSpace(commandID) == "" {
		return nil, ErrConflict
	}
	path, err := ValidatePath(snapshot.JournalPath, stateDir, snapshot.CWD)
	if err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(path), RawQuery: "mode=ro&_pragma=busy_timeout(5000)"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return nil, err
	}
	var launchHash string
	if err = tx.QueryRowContext(ctx, `SELECT payload_hash FROM launch_contracts WHERE incarnation=?`, snapshot.Owner.CarrierIncarnation).Scan(&launchHash); err != nil {
		return nil, err
	}
	if launchHash != hash(encoded) {
		return nil, ErrConflict
	}
	r := &agentproto.RuntimeCommandReceipt{Version: agentproto.JournalProtocolVersion, CommandID: commandID}
	err = tx.QueryRowContext(ctx, `SELECT incarnation,hash,stage,(SELECT count(*) FROM frames WHERE command_id=commands.id),(SELECT count(*) FROM frames WHERE command_id=commands.id AND stage='written') FROM commands WHERE id=?`, commandID).Scan(&r.CarrierIncarnation, &r.PayloadHash, &r.Stage, &r.PreparedFrames, &r.WrittenFrames)
	if err != nil {
		return nil, err
	}
	if r.CarrierIncarnation != snapshot.Owner.CarrierIncarnation {
		return nil, ErrConflict
	}
	var watermarkTable int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name='private_frame_event_watermarks'`).Scan(&watermarkTable); err != nil {
		return nil, err
	}
	query := `SELECT f.id,f.native_id,f.native_method,f.stage,NULL FROM frames f WHERE f.command_id=? ORDER BY f.id LIMIT 256`
	if watermarkTable != 0 {
		query = `SELECT f.id,f.native_id,f.native_method,f.stage,w.sequence FROM frames f LEFT JOIN private_frame_event_watermarks w ON w.frame_id=f.id WHERE f.command_id=? ORDER BY f.id LIMIT 256`
	}
	rows, err := tx.QueryContext(ctx, query, commandID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var f agentproto.RuntimeFrameReceipt
		if err = rows.Scan(&f.FrameID, &f.NativeRequestID, &f.NativeMethod, &f.Stage, &f.EventWatermark); err != nil {
			rows.Close()
			return nil, err
		}
		r.Frames = append(r.Frames, f)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return r, nil
}

// PrivateQueryLink records the exact owner command and native frame shared by a
// coalesced query. A waiter never acquires a fabricated native frame.
type PrivateQueryLink struct {
	WaiterID, Incarnation, SourceCommandID, SourceCommandHash, RequestID string
	SourceReceipt                                                        agentproto.RuntimeCommandReceipt
}

func (s *Store) RegisterPrivateQueryLink(waiterID, sourceID, requestID string) (PrivateQueryLink, error) {
	var link PrivateQueryLink
	if waiterID == "" || sourceID == "" || requestID == "" {
		return link, ErrConflict
	}
	tx, err := s.db.Begin()
	if err != nil {
		return link, err
	}
	defer tx.Rollback()
	var waiterRaw, sourceRaw []byte
	var sourceHash string
	var waiterInc, sourceInc string
	if err = tx.QueryRow(`SELECT payload,incarnation FROM commands WHERE id=?`, waiterID).Scan(&waiterRaw, &waiterInc); err != nil {
		return link, err
	}
	if err = tx.QueryRow(`SELECT payload,incarnation,hash FROM commands WHERE id=?`, sourceID).Scan(&sourceRaw, &sourceInc, &sourceHash); err != nil {
		return link, err
	}
	var waiter, source agentproto.Command
	if json.Unmarshal(waiterRaw, &waiter) != nil || json.Unmarshal(sourceRaw, &source) != nil || waiter.NativeBinding == nil || source.NativeBinding == nil || waiter.Execution != nil || source.Execution != nil || waiter.NativeBinding.Owner != source.NativeBinding.Owner || waiter.NativeBinding.Owner.CarrierIncarnation != s.incarnation || waiterInc != s.incarnation || sourceInc != s.incarnation || waiter.Kind != agentproto.CommandThreadsRefresh || source.Kind != agentproto.CommandThreadsRefresh {
		return link, ErrConflict
	}
	var n int
	if err = tx.QueryRow(`SELECT count(*) FROM frames WHERE command_id=? AND native_id=? AND native_method='thread/list'`, sourceID, requestID).Scan(&n); err != nil {
		return link, err
	}
	if n != 1 {
		return link, ErrConflict
	}
	if _, err = tx.Exec(`INSERT OR IGNORE INTO private_query_links(waiter_id,incarnation,source_id,source_hash,request_id) VALUES(?,?,?,?,?)`, waiterID, s.incarnation, sourceID, sourceHash, requestID); err != nil {
		return link, err
	}
	if err = tx.QueryRow(`SELECT waiter_id,incarnation,source_id,source_hash,request_id FROM private_query_links WHERE waiter_id=?`, waiterID).Scan(&link.WaiterID, &link.Incarnation, &link.SourceCommandID, &link.SourceCommandHash, &link.RequestID); err != nil {
		return link, err
	}
	if link.Incarnation != s.incarnation || link.SourceCommandID != sourceID || link.SourceCommandHash != sourceHash || link.RequestID != requestID {
		return link, ErrConflict
	}
	return link, tx.Commit()
}

// ReadPrivateQueryLink validates both the immutable launch and source frame
// against a consistent read-only journal snapshot, including after reconnect.
func ReadPrivateQueryLink(ctx context.Context, snapshot agentproto.PrivateNativeLaunchSnapshot, stateDir, waiterID string) (PrivateQueryLink, error) {
	var link PrivateQueryLink
	if snapshot.Validate() != nil || waiterID == "" {
		return link, ErrConflict
	}
	path, err := ValidatePath(snapshot.JournalPath, stateDir, snapshot.CWD)
	if err != nil {
		return link, err
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(path), RawQuery: "mode=ro&_pragma=busy_timeout(5000)"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return link, err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return link, err
	}
	defer tx.Rollback()
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return link, err
	}
	var digest string
	if err = tx.QueryRowContext(ctx, `SELECT payload_hash FROM launch_contracts WHERE incarnation=?`, snapshot.Owner.CarrierIncarnation).Scan(&digest); err != nil {
		return link, err
	}
	if digest != hash(raw) {
		return link, ErrConflict
	}
	err = tx.QueryRowContext(ctx, `SELECT waiter_id,incarnation,source_id,source_hash,request_id FROM private_query_links WHERE waiter_id=?`, waiterID).Scan(&link.WaiterID, &link.Incarnation, &link.SourceCommandID, &link.SourceCommandHash, &link.RequestID)
	if err != nil {
		return link, err
	}
	if link.Incarnation != snapshot.Owner.CarrierIncarnation {
		return link, ErrConflict
	}
	var sourceRaw, waiterRaw []byte
	var sourceHash, sourceInc, waiterInc string
	if err = tx.QueryRowContext(ctx, `SELECT payload,incarnation,hash FROM commands WHERE id=?`, link.SourceCommandID).Scan(&sourceRaw, &sourceInc, &sourceHash); err != nil {
		return link, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT payload,incarnation FROM commands WHERE id=?`, waiterID).Scan(&waiterRaw, &waiterInc); err != nil {
		return link, err
	}
	var source, waiter agentproto.Command
	if json.Unmarshal(sourceRaw, &source) != nil || json.Unmarshal(waiterRaw, &waiter) != nil || source.NativeBinding == nil || waiter.NativeBinding == nil || source.NativeBinding.Owner != snapshot.Owner || waiter.NativeBinding.Owner != snapshot.Owner || source.Kind != agentproto.CommandThreadsRefresh || waiter.Kind != agentproto.CommandThreadsRefresh || sourceInc != link.Incarnation || waiterInc != link.Incarnation || sourceHash != link.SourceCommandHash || hash(sourceRaw) != sourceHash {
		return link, ErrConflict
	}
	r := agentproto.RuntimeCommandReceipt{Version: agentproto.JournalProtocolVersion, CarrierIncarnation: link.Incarnation, CommandID: link.SourceCommandID, PayloadHash: sourceHash}
	var f agentproto.RuntimeFrameReceipt
	if err = tx.QueryRowContext(ctx, `SELECT id,native_id,native_method,stage FROM frames WHERE command_id=? AND native_id=? AND native_method='thread/list'`, link.SourceCommandID, link.RequestID).Scan(&f.FrameID, &f.NativeRequestID, &f.NativeMethod, &f.Stage); err != nil {
		return link, err
	}
	r.Frames = []agentproto.RuntimeFrameReceipt{f}
	link.SourceReceipt = r
	return link, tx.Commit()
}

// PrivateSubjectOwnsTurn proves a turn through its exact canonical prompt and
// journaled native start association; it does not infer by recency.
func (s *Store) PrivateSubjectOwnsTurn(subject, session, turn string, binding agentproto.NativeTurnBinding) (bool, error) {
	if subject == "" || session == "" || turn == "" || binding.Validate(true) != nil {
		return false, ErrConflict
	}
	var raw []byte
	if err := s.db.QueryRow(`SELECT payload FROM commands WHERE id=? AND incarnation=?`, subject, s.incarnation).Scan(&raw); err != nil {
		return false, err
	}
	var command agentproto.Command
	if err := json.Unmarshal(raw, &command); err != nil {
		return false, err
	}
	if command.Kind != agentproto.CommandPromptSend || command.NativeBinding == nil || *command.NativeBinding != binding || command.Target.ThreadID != "" && command.Target.ThreadID != session {
		return false, ErrConflict
	}
	rows, err := s.db.Query(`SELECT payload FROM events WHERE incarnation=? AND json_extract(payload,'$.commandId')=? AND json_extract(payload,'$.threadId')=? AND json_extract(payload,'$.turnId')=? ORDER BY sequence`, s.incarnation, subject, session, turn)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var eventRaw []byte
		if err = rows.Scan(&eventRaw); err != nil {
			return false, err
		}
		var e agentproto.Event
		if err = json.Unmarshal(eventRaw, &e); err != nil {
			return false, err
		}
		if e.NativeBinding == nil || *e.NativeBinding != binding || e.RequestID == "" {
			continue
		}
		if e.Kind == agentproto.EventNativeTurnBindingObserved && e.Status == "accepted" || e.Kind == agentproto.EventTurnStarted && e.Metadata["turnIdSource"] == "synthetic_from_observed_user_uuid" && e.Metadata["nativeUserUUID"] == e.RequestID && e.TurnID == "relay-claude-turn-"+e.RequestID {
			return true, nil
		}
	}
	return false, rows.Err()
}

// PrivateSubjectRejection returns only a definitive journaled native prompt
// rejection for the exact private execution; missing evidence is not a stop.
func (s *Store) PrivateSubjectRejection(subject, session string, binding agentproto.NativeTurnBinding) (*agentproto.Event, error) {
	if subject == "" || binding.Validate(true) != nil {
		return nil, ErrConflict
	}
	var raw []byte
	if err := s.db.QueryRow(`SELECT payload FROM commands WHERE id=? AND incarnation=?`, subject, s.incarnation).Scan(&raw); err != nil {
		return nil, err
	}
	var command agentproto.Command
	if err := json.Unmarshal(raw, &command); err != nil {
		return nil, err
	}
	if command.Kind != agentproto.CommandPromptSend || command.NativeBinding == nil || *command.NativeBinding != binding || command.Target.ThreadID != "" && command.Target.ThreadID != session {
		return nil, ErrConflict
	}
	event, err := s.RejectedEvent(subject)
	if err != nil || event == nil {
		return event, err
	}
	if event.NativeBinding == nil || *event.NativeBinding != binding || event.CommandID != subject || event.Status != "failed" || event.RequestID == "" || event.ThreadID != "" && event.ThreadID != session {
		return nil, ErrConflict
	}
	return event, nil
}

// PrivateBindingForTurn returns a unique binding proved by the exact persisted
// native start event and canonical prompt. It never chooses a recent command.
func (s *Store) PrivateBindingForTurn(session, turn string) (*agentproto.NativeTurnBinding, error) {
	if session == "" || turn == "" {
		return nil, nil
	}
	rows, err := s.db.Query(`SELECT DISTINCT json_extract(payload,'$.commandId') FROM events WHERE incarnation=? AND json_extract(payload,'$.threadId')=? AND json_extract(payload,'$.turnId')=? AND json_extract(payload,'$.commandId') IS NOT NULL`, s.incarnation, session, turn)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	// Review start returns its own exact native turn and may name a separate
	// review thread. This is independent of the originating prompt turn.
	var root string
	rootRows, err := s.db.Query(`SELECT json_extract(payload,'$.rootTurnId') FROM events WHERE incarnation=? AND json_extract(payload,'$.kind')=? AND json_extract(payload,'$.threadId')=? AND json_extract(payload,'$.turnId')=? AND coalesce(json_extract(payload,'$.rootTurnId'),'')<>''`, s.incarnation, string(agentproto.EventTurnStarted), session, turn)
	if err != nil {
		return nil, err
	}
	for rootRows.Next() {
		var v string
		if err = rootRows.Scan(&v); err != nil {
			rootRows.Close()
			return nil, err
		}
		if root != "" && root != v {
			rootRows.Close()
			return nil, ErrConflict
		}
		root = v
	}
	err = rootRows.Err()
	rootRows.Close()
	if err != nil {
		return nil, err
	}
	reviewRows, err := s.db.Query(`SELECT e.payload,c.payload FROM events e JOIN commands c ON c.id=json_extract(e.payload,'$.commandId') AND c.incarnation=e.incarnation WHERE e.incarnation=? AND json_extract(e.payload,'$.kind')='native.control.result' AND json_extract(e.payload,'$.action')='review.start' AND json_extract(e.payload,'$.status')='accepted' AND (json_extract(e.payload,'$.turnId')=? OR (?<>'' AND json_extract(e.payload,'$.turnId')=?)) AND json_extract(e.payload,'$.metadata.reviewThreadID')=?`, s.incarnation, turn, root, root, session)
	if err != nil {
		return nil, err
	}
	var found *agentproto.NativeTurnBinding
	for reviewRows.Next() {
		var raw, commandRaw []byte
		if err = reviewRows.Scan(&raw, &commandRaw); err != nil {
			reviewRows.Close()
			return nil, err
		}
		var event agentproto.Event
		var command agentproto.Command
		if json.Unmarshal(raw, &event) != nil || json.Unmarshal(commandRaw, &command) != nil || command.Kind != agentproto.CommandReviewStart || command.CommandID != event.CommandID || command.Target.ThreadID != event.ThreadID || event.RequestID == "" || command.NativeBinding == nil || event.NativeBinding == nil || *command.NativeBinding != *event.NativeBinding || event.NativeBinding.Validate(true) != nil {
			reviewRows.Close()
			return nil, ErrConflict
		}
		if found != nil && *found != *event.NativeBinding {
			reviewRows.Close()
			return nil, ErrConflict
		}
		copy := *event.NativeBinding
		found = &copy
	}
	err = reviewRows.Err()
	reviewRows.Close()
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		binding, e := s.PrivateBinding(id)
		if e != nil {
			return nil, e
		}
		if binding == nil || binding.Validate(true) != nil {
			continue
		}
		ok, e := s.PrivateSubjectOwnsTurn(id, session, turn, *binding)
		if e != nil {
			if errors.Is(e, ErrConflict) {
				continue
			}
			return nil, e
		}
		if !ok {
			continue
		}
		if found != nil && *found != *binding {
			return nil, ErrConflict
		}
		copy := *binding
		found = &copy
	}
	return found, nil
}
