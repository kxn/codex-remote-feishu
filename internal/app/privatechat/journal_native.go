package privatechat

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

// NativeLeaseAuthority is supplied only by the authenticated host; its callback
// holds the durable executionowner lease until local work returns. It must not
// perform network I/O or permit another application/epoch to own the carrier.
type NativeLeaseAuthority interface {
	WithNativeLease(context.Context, agentproto.NativeOwner, Owner, int64, func() error) error
}

// NativeLease is bound to one authenticated private surface and carrier.
type NativeLease struct {
	journal    *Journal
	authority  NativeLeaseAuthority
	native     agentproto.NativeOwner
	surface    Owner
	generation int64
}

func (j *Journal) OpenNativeLease(ctx context.Context, authority NativeLeaseAuthority, native agentproto.NativeOwner, surface Owner, generation int64) (*NativeLease, error) {
	if authority == nil || native.Validate() != nil || !validOwner(surface) || generation < 1 {
		return nil, ErrJournalState
	}
	ownerJSON, _ := json.Marshal(native)
	surfaceJSON, _ := json.Marshal(surface)
	err := authority.WithNativeLease(ctx, native, surface, generation, func() error {
		tx, e := j.db.BeginTx(ctx, nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		if _, e = tx.ExecContext(ctx, `INSERT INTO native_carriers(incarnation,owner_json,surface_json,generation) VALUES(?,?,?,?) ON CONFLICT DO NOTHING`, native.CarrierIncarnation, ownerJSON, surfaceJSON, generation); e != nil {
			return e
		}
		var oldOwner, oldSurface []byte
		var oldGeneration int64
		if e = tx.QueryRowContext(ctx, `SELECT owner_json,surface_json,generation FROM native_carriers WHERE incarnation=?`, native.CarrierIncarnation).Scan(&oldOwner, &oldSurface, &oldGeneration); e != nil {
			return e
		}
		if string(oldOwner) != string(ownerJSON) || string(oldSurface) != string(surfaceJSON) || oldGeneration != generation {
			return ErrJournalConflict
		}
		return tx.Commit()
	})
	if err != nil {
		return nil, err
	}
	return &NativeLease{journal: j, authority: authority, native: native, surface: surface, generation: generation}, nil
}
func (l *NativeLease) with(ctx context.Context, fn func() error) error {
	if l == nil || l.journal == nil || l.authority == nil {
		return ErrJournalState
	}
	return l.authority.WithNativeLease(ctx, l.native, l.surface, l.generation, fn)
}

type NativeEventRef struct {
	Incarnation string
	Sequence    uint64
	PayloadHash string
}

// ReceiveNativeEvent commits the raw event and its pending projection before
// returning the acknowledgement. A nil receipt means nothing may be ACKed.
func (l *NativeLease) ReceiveNativeEvent(ctx context.Context, event agentproto.Event) (*agentproto.EventReceipt, error) {
	if l == nil || agentproto.PrivateDeliveryForEvent(event.Kind) != agentproto.PrivateEventDurable || event.Live != nil || event.Journal == nil || event.Journal.Version != agentproto.JournalProtocolVersion || event.Journal.CarrierIncarnation != l.native.CarrierIncarnation || event.Seq != event.Journal.Sequence || event.Seq == 0 || event.Seq > uint64(^uint64(0)>>1) || event.Execution != nil {
		return nil, ErrJournalState
	}
	if event.NativeBinding != nil && (event.NativeBinding.Owner != l.native || event.NativeBinding.Validate(false) != nil) {
		return nil, ErrJournalConflict
	}
	digest, err := agentproto.RuntimeEventPayloadHash(event)
	if err != nil || digest != event.Journal.PayloadHash {
		return nil, ErrJournalConflict
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}
	var receipt *agentproto.EventReceipt
	err = l.with(ctx, func() error {
		tx, e := l.journal.db.BeginTx(ctx, nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		if event.CommandID != "" {
			var commandRaw []byte
			var commandHash string
			e = tx.QueryRowContext(ctx, `SELECT payload,payload_hash FROM native_commands WHERE command_id=?`, event.CommandID).Scan(&commandRaw, &commandHash)
			if errors.Is(e, sql.ErrNoRows) {
				return ErrJournalConflict
			}
			if e != nil {
				return e
			}
			sum := sha256.Sum256(commandRaw)
			if hex.EncodeToString(sum[:]) != commandHash {
				return ErrJournalConflict
			}
			var command agentproto.Command
			if e = json.Unmarshal(commandRaw, &command); e != nil {
				return e
			}
			if event.NativeBinding == nil || command.NativeBinding == nil || *event.NativeBinding != *command.NativeBinding {
				return ErrJournalConflict
			}
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO native_events(incarnation,sequence,payload_hash,command_id,payload) VALUES(?,?,?,?,?) ON CONFLICT DO NOTHING`, l.native.CarrierIncarnation, event.Seq, digest, event.CommandID, raw); e != nil {
			return e
		}
		var oldHash string
		var oldRaw []byte
		if e = tx.QueryRowContext(ctx, `SELECT payload_hash,payload FROM native_events WHERE incarnation=? AND sequence=?`, l.native.CarrierIncarnation, event.Seq).Scan(&oldHash, &oldRaw); e != nil {
			return e
		}
		if oldHash != digest || string(oldRaw) != string(raw) {
			return ErrJournalConflict
		}
		if event.CommandID != "" {
			if e = resolveNativeEventTx(ctx, tx, event); e != nil {
				return e
			}
		}
		if e = observePrivateCompactTx(ctx, tx, l, event); e != nil {
			return e
		}
		if event.Kind == agentproto.EventRequestResolved {
			if e = resolvePrivateRequestResponseTx(ctx, tx, l, event); e != nil {
				return e
			}
		} else if event.Kind == agentproto.EventRequestStarted {
			if e = resolvePendingWrittenForRequestTx(ctx, tx, l, event); e != nil {
				return e
			}
		} else if event.Kind == agentproto.EventNativeControlResult && event.Action == "request.response.written" {
			if e = resolveAlreadyReceivedPrivateRequestTx(ctx, tx, l, event); e != nil {
				return e
			}
		}
		if e = tx.Commit(); e != nil {
			return e
		}
		receipt = &agentproto.EventReceipt{CarrierIncarnation: l.native.CarrierIncarnation, Sequence: event.Seq, PayloadHash: digest}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return receipt, nil
}

// recordCommandReceipt stores the authenticated wrapper's write journal for
// request-ID correlation. It never marks input accepted or failed.
func (l *NativeLease) recordCommandReceipt(ctx context.Context, r agentproto.RuntimeCommandReceipt) error {
	if l == nil || r.Version != agentproto.JournalProtocolVersion || r.CarrierIncarnation != l.native.CarrierIncarnation || strings.TrimSpace(r.CommandID) == "" || strings.TrimSpace(r.PayloadHash) == "" {
		return ErrJournalState
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return l.with(ctx, func() error {
		tx, e := l.journal.db.BeginTx(ctx, nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		var hash string
		var commandRaw []byte
		if e = tx.QueryRowContext(ctx, `SELECT payload_hash,payload FROM native_commands WHERE command_id=?`, r.CommandID).Scan(&hash, &commandRaw); e != nil {
			return e
		}
		var command agentproto.Command
		if e = json.Unmarshal(commandRaw, &command); e != nil {
			return e
		}
		if hash != r.PayloadHash || command.NativeBinding == nil || command.NativeBinding.Owner != l.native {
			return ErrJournalConflict
		}
		var oldRaw []byte
		e = tx.QueryRowContext(ctx, `SELECT payload FROM native_command_receipts WHERE command_id=?`, r.CommandID).Scan(&oldRaw)
		if e == nil {
			var old agentproto.RuntimeCommandReceipt
			if e = json.Unmarshal(oldRaw, &old); e != nil {
				return e
			}
			if old.CarrierIncarnation != r.CarrierIncarnation || old.PayloadHash != r.PayloadHash || r.PreparedFrames < old.PreparedFrames || r.WrittenFrames < old.WrittenFrames {
				return ErrJournalConflict
			}
			for _, prior := range old.Frames {
				found := false
				for _, frame := range r.Frames {
					if frame.FrameID == prior.FrameID && frame.NativeRequestID == prior.NativeRequestID && frame.NativeMethod == prior.NativeMethod {
						found = true
						break
					}
				}
				if !found {
					return ErrJournalConflict
				}
			}
		} else if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO native_command_receipts(command_id,incarnation,payload) VALUES(?,?,?) ON CONFLICT(command_id) DO UPDATE SET payload=excluded.payload`, r.CommandID, r.CarrierIncarnation, raw); e != nil {
			return e
		}
		return tx.Commit()
	})
}

// ResolveNativeDispatch consumes only a reference to a previously committed
// native event. Event, canonical command and wrapper write evidence are read
// and checked in the same transaction as the dispatch state change.
func (l *NativeLease) ResolveNativeDispatch(ctx context.Context, ref NativeEventRef) error {
	if l == nil || ref.Incarnation != l.native.CarrierIncarnation || ref.Sequence == 0 || ref.PayloadHash == "" {
		return ErrJournalState
	}
	return l.with(ctx, func() error {
		tx, e := l.journal.db.BeginTx(ctx, nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		var hash string
		var raw []byte
		if e = tx.QueryRowContext(ctx, `SELECT payload_hash,payload FROM native_events WHERE incarnation=? AND sequence=?`, ref.Incarnation, ref.Sequence).Scan(&hash, &raw); e != nil {
			return e
		}
		if hash != ref.PayloadHash {
			return ErrJournalConflict
		}
		var event agentproto.Event
		if e = json.Unmarshal(raw, &event); e != nil {
			return e
		}
		if e = resolveNativeEventTx(ctx, tx, event); e != nil {
			return e
		}
		if e = observePrivateCompactTx(ctx, tx, l, event); e != nil {
			return e
		}
		return tx.Commit()
	})
}
func resolveNativeEventTx(ctx context.Context, tx *sql.Tx, event agentproto.Event) error {
	var dispatchID, commandHash string
	var raw []byte
	err := tx.QueryRowContext(ctx, `SELECT dispatch_id,payload,payload_hash FROM native_commands WHERE command_id=?`, event.CommandID).Scan(&dispatchID, &raw, &commandHash)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != commandHash {
		return ErrJournalConflict
	}
	var command agentproto.Command
	if err = json.Unmarshal(raw, &command); err != nil {
		return err
	}
	if command.NativeBinding == nil || event.NativeBinding == nil || *command.NativeBinding != *event.NativeBinding {
		return ErrJournalConflict
	}
	d, state, _, err := readDispatch(ctx, tx, dispatchID)
	if err != nil {
		return err
	}
	outcome, method, definitive := nativeEventOutcome(command, event)
	if !definitive {
		return nil
	}
	if state == outcome || state == "resolved" && outcome == "sent" {
		var storedRequest string
		var storedSequence int64
		if err = tx.QueryRowContext(ctx, `SELECT request_id,event_sequence FROM dispatch WHERE id=?`, dispatchID).Scan(&storedRequest, &storedSequence); err != nil {
			return err
		}
		if storedRequest != event.RequestID || storedSequence != int64(event.Journal.Sequence) {
			return ErrJournalConflict
		}
		return nil
	}
	if state != "sending" && state != "unknown" {
		return ErrJournalState
	}
	if method == "shared" {
		var sourceID, sourceHash, requestID string
		var proofRaw []byte
		err = tx.QueryRowContext(ctx, `SELECT source_id,source_hash,request_id,payload FROM native_query_proofs WHERE waiter_id=?`, event.CommandID).Scan(&sourceID, &sourceHash, &requestID, &proofRaw)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if event.RequestID != requestID || event.Metadata["sourceCommandId"] != sourceID || event.Metadata["sourceCommandHash"] != sourceHash {
			return ErrJournalConflict
		}
		_, err = tx.ExecContext(ctx, `UPDATE dispatch SET state=?,evidence=?,request_id=?,event_sequence=? WHERE id=?`, outcome, []byte(fmt.Sprintf(`{"incarnation":%q,"sequence":%d,"hash":%q}`, event.Journal.CarrierIncarnation, event.Journal.Sequence, event.Journal.PayloadHash)), event.RequestID, event.Journal.Sequence, dispatchID)
		return err
	}
	if method == "local" {
		_, err = tx.ExecContext(ctx, `UPDATE dispatch SET state=?,evidence=?,request_id=?,event_sequence=? WHERE id=?`, outcome, []byte(fmt.Sprintf(`{"incarnation":%q,"sequence":%d,"hash":%q}`, event.Journal.CarrierIncarnation, event.Journal.Sequence, event.Journal.PayloadHash)), event.RequestID, event.Journal.Sequence, dispatchID)
		return err
	}
	var receiptRaw []byte
	err = tx.QueryRowContext(ctx, `SELECT payload FROM native_command_receipts WHERE command_id=? AND incarnation=?`, event.CommandID, event.Journal.CarrierIncarnation).Scan(&receiptRaw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var receipt agentproto.RuntimeCommandReceipt
	if err = json.Unmarshal(receiptRaw, &receipt); err != nil {
		return err
	}
	if method == "request-written" {
		frameID, err := strconv.ParseInt(fmt.Sprint(event.Metadata["frameID"]), 10, 64)
		if err != nil || frameID <= 0 {
			return ErrJournalConflict
		}
		matched := false
		for _, frame := range receipt.Frames {
			if frame.FrameID == frameID && frame.Stage == "written" {
				matched = true
				break
			}
		}
		if !matched {
			return nil
		}
		_, err = tx.ExecContext(ctx, `UPDATE dispatch SET state='sent',evidence=?,request_id=?,event_sequence=? WHERE id=?`, []byte(fmt.Sprintf(`{"incarnation":%q,"sequence":%d,"hash":%q}`, event.Journal.CarrierIncarnation, event.Journal.Sequence, event.Journal.PayloadHash)), event.RequestID, event.Journal.Sequence, dispatchID)
		return err
	}
	if method == "prewrite" {
		if receipt.Stage != "prepared" || receipt.WrittenFrames != 0 {
			return nil
		}
		for _, frame := range receipt.Frames {
			if frame.Stage != "prepared" {
				return nil
			}
		}
		_, err = tx.ExecContext(ctx, `UPDATE dispatch SET state=?,evidence=?,request_id=?,event_sequence=? WHERE id=?`, outcome, []byte(fmt.Sprintf(`{"incarnation":%q,"sequence":%d,"hash":%q}`, event.Journal.CarrierIncarnation, event.Journal.Sequence, event.Journal.PayloadHash)), event.RequestID, event.Journal.Sequence, dispatchID)
		return err
	}
	if method == "compact-prewrite" {
		frameID, parseErr := strconv.ParseInt(fmt.Sprint(event.Metadata["frameID"]), 10, 64)
		if parseErr != nil || frameID <= 0 {
			return ErrJournalConflict
		}
		matched := false
		for _, frame := range receipt.Frames {
			if frame.FrameID == frameID && frame.NativeMethod == "thread/compact/start" && frame.Stage == "prepared" {
				matched = true
				continue
			}
			if frame.NativeMethod != "thread/resume" || frame.Stage != "written" {
				return nil
			}
		}
		if !matched {
			return nil
		}
		_, err = tx.ExecContext(ctx, `UPDATE dispatch SET state='failed',evidence=?,request_id=?,event_sequence=? WHERE id=?`, []byte(fmt.Sprintf(`{"incarnation":%q,"sequence":%d,"hash":%q}`, event.Journal.CarrierIncarnation, event.Journal.Sequence, event.Journal.PayloadHash)), event.RequestID, event.Journal.Sequence, dispatchID)
		return err
	}
	matched := false
	for _, frame := range receipt.Frames {
		if frame.NativeRequestID == event.RequestID && frame.NativeMethod == method && (frame.Stage == "written" || frame.Stage == "child_write_started" || frame.Stage == "unknown") {
			if command.Kind == agentproto.CommandThreadCompactStart {
				if frame.EventWatermark == nil {
					return nil
				}
				if _, err = tx.ExecContext(ctx, `UPDATE private_compact_operations SET after_sequence=? WHERE command_id=? AND incarnation=? AND state='pending'`, *frame.EventWatermark, command.CommandID, event.Journal.CarrierIncarnation); err != nil {
					return err
				}
			}
			matched = true
			break
		}
	}
	if !matched {
		return nil
	}
	if command.Kind == agentproto.CommandPromptSend && d.Mode == "start" && outcome == "accepted" {
		d.Session.SessionID = event.ThreadID
	}
	_, err = tx.ExecContext(ctx, `UPDATE dispatch SET state=?,evidence=?,session=?,request_id=?,event_sequence=? WHERE id=?`, outcome, []byte(fmt.Sprintf(`{"incarnation":%q,"sequence":%d,"hash":%q}`, event.Journal.CarrierIncarnation, event.Journal.Sequence, event.Journal.PayloadHash)), d.Session.SessionID, event.RequestID, event.Journal.Sequence, dispatchID)
	return err
}

// PendingNativeEvents enumerates committed events awaiting application to the
// private projector; it includes notifications without a command association.
type PendingNativeEvent struct {
	Ref   NativeEventRef
	Event agentproto.Event
}

func (l *NativeLease) PendingNativeEvents(ctx context.Context, after uint64, limit int) ([]PendingNativeEvent, error) {
	if l == nil || limit < 1 || limit > 1000 {
		return nil, ErrJournalState
	}
	var out []PendingNativeEvent
	err := l.with(ctx, func() error {
		rows, e := l.journal.db.QueryContext(ctx, `SELECT e.sequence,e.payload_hash,e.payload,
CASE WHEN e.command_id<>'' THEN coalesce((SELECT CASE WHEN d.state IN ('prepared','sending','unknown') THEN 1 ELSE 0 END FROM native_commands c JOIN dispatch d ON d.id=c.dispatch_id WHERE c.command_id=e.command_id),1)
 ELSE EXISTS(SELECT 1 FROM dispatch d WHERE d.carrier=e.incarnation AND d.lane='input' AND d.state IN ('prepared','sending','unknown')) END
FROM native_events e WHERE e.incarnation=? AND e.sequence>? AND e.applied=0 ORDER BY e.sequence LIMIT ?`, l.native.CarrierIncarnation, after, limit)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			item := PendingNativeEvent{Ref: NativeEventRef{Incarnation: l.native.CarrierIncarnation}}
			var raw []byte
			var blocked bool
			if e = rows.Scan(&item.Ref.Sequence, &item.Ref.PayloadHash, &raw, &blocked); e != nil {
				return e
			}
			if blocked {
				break
			}
			if e = json.Unmarshal(raw, &item.Event); e != nil {
				return e
			}
			out = append(out, item)
		}
		return rows.Err()
	})
	return out, err
}

// NativeCommand reloads the frozen command for safe recovery; callers must
// still respect its dispatch state and never resend unknown deliveries.
func (l *NativeLease) NativeCommand(ctx context.Context, dispatchID string) (agentproto.Command, error) {
	var command agentproto.Command
	err := l.with(ctx, func() error {
		tx, e := l.journal.db.BeginTx(ctx, nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		d, _, _, e := readDispatch(ctx, tx, dispatchID)
		if e != nil {
			return e
		}
		if d.Owner != l.surface || d.Generation != l.generation {
			return ErrJournalConflict
		}
		var raw []byte
		var hash string
		if e = tx.QueryRowContext(ctx, `SELECT payload,payload_hash FROM native_commands WHERE dispatch_id=?`, dispatchID).Scan(&raw, &hash); e != nil {
			return e
		}
		sum := sha256.Sum256(raw)
		if hex.EncodeToString(sum[:]) != hash {
			return ErrJournalConflict
		}
		if e = json.Unmarshal(raw, &command); e != nil {
			return e
		}
		if command.NativeBinding == nil || command.NativeBinding.Owner != l.native {
			return ErrJournalConflict
		}
		return tx.Commit()
	})
	return command, err
}

// NativeCommandReceiptReader is a trusted host port. Implementations use the
// frozen launch and runtimejournal.ReadPrivateCommandReceipt; no user payload.
type NativeCommandReceiptReader interface {
	ReadNativeCommandReceipt(context.Context, agentproto.NativeOwner, string) (*agentproto.RuntimeCommandReceipt, error)
}

func (l *NativeLease) RecoverNativeCommandReceipt(ctx context.Context, reader NativeCommandReceiptReader, commandID string) error {
	if l == nil || reader == nil || strings.TrimSpace(commandID) == "" {
		return ErrJournalState
	}
	receipt, err := reader.ReadNativeCommandReceipt(ctx, l.native, commandID)
	if err != nil {
		return err
	}
	if receipt == nil {
		return ErrJournalState
	}
	return l.recordCommandReceipt(ctx, *receipt)
}

// NativeQueryProof is returned by a trusted journal reader after validating the
// wrapper's persisted waiter/source link and launch contract.
type NativeQueryProof struct {
	WaiterID, Incarnation, SourceCommandID, SourceCommandHash, RequestID string
	SourceReceipt                                                        agentproto.RuntimeCommandReceipt
}
type NativeQueryProofReader interface {
	ReadNativeQueryProof(context.Context, agentproto.NativeOwner, string) (NativeQueryProof, error)
}

func (l *NativeLease) RecoverNativeQueryProof(ctx context.Context, reader NativeQueryProofReader, waiter string) error {
	if l == nil || reader == nil || waiter == "" {
		return ErrJournalState
	}
	proof, err := reader.ReadNativeQueryProof(ctx, l.native, waiter)
	if err != nil {
		return err
	}
	if proof.WaiterID != waiter || proof.Incarnation != l.native.CarrierIncarnation || proof.SourceCommandID == "" || proof.RequestID == "" || proof.SourceReceipt.CommandID != proof.SourceCommandID || proof.SourceReceipt.CarrierIncarnation != proof.Incarnation || proof.SourceReceipt.PayloadHash != proof.SourceCommandHash {
		return ErrJournalConflict
	}
	matched := false
	for _, f := range proof.SourceReceipt.Frames {
		if f.NativeRequestID == proof.RequestID && f.NativeMethod == "thread/list" && (f.Stage == "written" || f.Stage == "child_write_started" || f.Stage == "unknown") {
			matched = true
		}
	}
	if !matched {
		return ErrJournalConflict
	}
	raw, err := json.Marshal(proof)
	if err != nil {
		return err
	}
	return l.with(ctx, func() error {
		tx, e := l.journal.db.BeginTx(ctx, nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		var waiterRaw, sourceRaw []byte
		var sourceHash string
		if e = tx.QueryRowContext(ctx, `SELECT payload FROM native_commands WHERE command_id=?`, waiter).Scan(&waiterRaw); e != nil {
			return e
		}
		if e = tx.QueryRowContext(ctx, `SELECT payload,payload_hash FROM native_commands WHERE command_id=?`, proof.SourceCommandID).Scan(&sourceRaw, &sourceHash); e != nil {
			return e
		}
		sourceDigest := sha256.Sum256(sourceRaw)
		if hex.EncodeToString(sourceDigest[:]) != sourceHash {
			return ErrJournalConflict
		}
		var w, s agentproto.Command
		if json.Unmarshal(waiterRaw, &w) != nil || json.Unmarshal(sourceRaw, &s) != nil || w.NativeBinding == nil || s.NativeBinding == nil || w.NativeBinding.Owner != l.native || s.NativeBinding.Owner != l.native || w.Kind != agentproto.CommandThreadsRefresh || s.Kind != agentproto.CommandThreadsRefresh || sourceHash != proof.SourceCommandHash {
			return ErrJournalConflict
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO native_query_proofs(waiter_id,source_id,source_hash,request_id,payload) VALUES(?,?,?,?,?) ON CONFLICT DO NOTHING`, waiter, proof.SourceCommandID, proof.SourceCommandHash, proof.RequestID, raw); e != nil {
			return e
		}
		var oldSource, oldHash, oldRequest string
		if e = tx.QueryRowContext(ctx, `SELECT source_id,source_hash,request_id FROM native_query_proofs WHERE waiter_id=?`, waiter).Scan(&oldSource, &oldHash, &oldRequest); e != nil {
			return e
		}
		if oldSource != proof.SourceCommandID || oldHash != proof.SourceCommandHash || oldRequest != proof.RequestID {
			return ErrJournalConflict
		}
		return tx.Commit()
	})
}

// MarkSending commits before invoking the native transport. It does not prove
// that bytes were sent or accepted.
func (l *NativeLease) MarkSending(ctx context.Context, id string) error {
	return l.markDispatchState(ctx, id, "prepared", "sending", nil)
}

// MarkDispatchUnknown conservatively fences a send whose outcome is uncertain.
func (l *NativeLease) MarkDispatchUnknown(ctx context.Context, id, reason string) error {
	if strings.TrimSpace(reason) == "" {
		return ErrJournalState
	}
	body, err := json.Marshal(struct {
		Reason string `json:"reason"`
	}{reason})
	if err != nil {
		return err
	}
	return l.markDispatchState(ctx, id, "sending", "unknown", body)
}
func (l *NativeLease) markDispatchState(ctx context.Context, id, old, next string, evidence []byte) error {
	if l == nil || id == "" {
		return ErrJournalState
	}
	return l.with(ctx, func() error {
		tx, e := l.journal.db.BeginTx(ctx, nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		d, state, previousEvidence, e := readDispatch(ctx, tx, id)
		if e != nil {
			return e
		}
		if d.Owner != l.surface || d.Generation != l.generation || d.Session.CarrierID != l.native.CarrierIncarnation {
			return ErrJournalConflict
		}
		var raw []byte
		if e = tx.QueryRowContext(ctx, `SELECT payload FROM native_commands WHERE dispatch_id=?`, id).Scan(&raw); e != nil {
			return e
		}
		var command agentproto.Command
		if e = json.Unmarshal(raw, &command); e != nil {
			return e
		}
		if command.NativeBinding == nil || command.NativeBinding.Owner != l.native {
			return ErrJournalConflict
		}
		if old == "prepared" && next == "sending" && command.Kind != agentproto.CommandTurnInterrupt && command.Kind != agentproto.CommandRuntimeInspect && command.Kind != agentproto.CommandProcessExit {
			if e = assertCarrierNotDetached(ctx, tx, l.surface, l.generation, l.native.InstanceID); e != nil {
				return e
			}
		}
		if state == next && next == "unknown" && string(previousEvidence) == string(evidence) {
			return nil
		}
		if state != old {
			return ErrJournalState
		}
		if old == "prepared" && next == "sending" {
			// An automatic command can wait in prepared while a menu disables
			// continuation. Recheck at the actual send admission boundary.
			var body []byte
			err := tx.QueryRowContext(ctx, `SELECT body FROM private_effect WHERE id || ':native' = ?`, id).Scan(&body)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if err == nil {
				_, inputs, err := DecodeStoredEffect(body)
				if err != nil {
					return err
				}
				if inputs != nil {
					if _, err = effectInputSequences(ctx, tx, l.surface, l.generation, inputs, true); err != nil {
						return err
					}
				}
			}
		}
		if _, e = tx.ExecContext(ctx, `UPDATE dispatch SET state=?,evidence=? WHERE id=?`, next, evidence, id); e != nil {
			return e
		}
		return tx.Commit()
	})
}
