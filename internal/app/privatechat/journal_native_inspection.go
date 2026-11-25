package privatechat

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

// CommittedNativeInspection reads only an authenticated, durable inspection
// response and its canonical owner-only request. It does not itself establish
// quiescence; the host must validate all domains and native/OS write fences.
func (l *NativeLease) CommittedNativeInspection(ctx context.Context, ref NativeEventRef) (agentproto.RuntimeInspectionResult, error) {
	var result agentproto.RuntimeInspectionResult
	if l == nil || ref.Incarnation != l.native.CarrierIncarnation || ref.Sequence == 0 || ref.PayloadHash == "" {
		return result, ErrJournalState
	}
	err := l.with(ctx, func() error {
		var raw, commandRaw []byte
		var hash, commandHash string
		if err := l.journal.db.QueryRowContext(ctx, `SELECT e.payload,e.payload_hash,c.payload,c.payload_hash FROM native_events e JOIN native_commands c ON c.command_id=e.command_id WHERE e.incarnation=? AND e.sequence=?`, ref.Incarnation, ref.Sequence).Scan(&raw, &hash, &commandRaw, &commandHash); err != nil {
			return err
		}
		sum := sha256.Sum256(commandRaw)
		if hash != ref.PayloadHash || hex.EncodeToString(sum[:]) != commandHash {
			return ErrJournalConflict
		}
		var event agentproto.Event
		var command agentproto.Command
		if json.Unmarshal(raw, &event) != nil || json.Unmarshal(commandRaw, &command) != nil {
			return ErrJournalConflict
		}
		actual, err := agentproto.RuntimeEventPayloadHash(event)
		if err != nil || actual != hash || event.Journal == nil || event.Journal.CarrierIncarnation != ref.Incarnation || event.Journal.Sequence != ref.Sequence || event.Journal.PayloadHash != hash {
			return ErrJournalConflict
		}
		if command.Kind != agentproto.CommandRuntimeInspect || command.NativeBinding == nil || command.NativeBinding.Owner != l.native || command.NativeBinding.ExecutionID != "" || command.RuntimeInspection == nil || command.RuntimeInspection.SubjectCommandID != "" || command.RuntimeInspection.ExpectedInstanceGeneration != l.native.InstanceGeneration || command.RuntimeInspection.ExpectedProcessGeneration != l.native.ProcessGeneration || command.RuntimeInspection.ExpectedLocationGeneration != 0 || event.Kind != agentproto.EventRuntimeInspectionCompleted || event.CommandID != command.CommandID || event.NativeBinding == nil || *event.NativeBinding != *command.NativeBinding || event.RuntimeInspection == nil || event.RuntimeInspection.RequestCommandID != command.CommandID {
			return ErrJournalConflict
		}
		result = *event.RuntimeInspection
		return nil
	})
	return result, err
}

// CommittedNativeSession returns a session only for an accepted prompt whose
// event and transport/native evidence have already resolved the exact dispatch.
func (l *NativeLease) CommittedNativeSession(ctx context.Context, ref NativeEventRef) (string, error) {
	if l == nil || ref.Incarnation != l.native.CarrierIncarnation || ref.Sequence == 0 || ref.PayloadHash == "" {
		return "", ErrJournalState
	}
	var session string
	err := l.with(ctx, func() error {
		var raw []byte
		var hash, state string
		var seq int64
		if err := l.journal.db.QueryRowContext(ctx, `SELECT e.payload,e.payload_hash,d.state,d.event_sequence FROM native_events e JOIN native_commands c ON c.command_id=e.command_id JOIN dispatch d ON d.id=c.dispatch_id WHERE e.incarnation=? AND e.sequence=?`, ref.Incarnation, ref.Sequence).Scan(&raw, &hash, &state, &seq); err != nil {
			return err
		}
		if hash != ref.PayloadHash || state != "accepted" || seq != int64(ref.Sequence) {
			return ErrJournalConflict
		}
		var event agentproto.Event
		if json.Unmarshal(raw, &event) != nil || event.NativeBinding == nil || event.NativeBinding.Owner != l.native || event.ThreadID == "" {
			return ErrJournalConflict
		}
		actual, err := agentproto.RuntimeEventPayloadHash(event)
		if err != nil || actual != hash {
			return ErrJournalConflict
		}
		if event.Kind != agentproto.EventNativeTurnBindingObserved && event.Kind != agentproto.EventTurnStarted {
			return ErrJournalConflict
		}
		session = event.ThreadID
		return nil
	})
	return session, err
}

// CommittedReviewSession returns the distinct child only after the exact review
// command, native frame and accepted result have resolved the owned dispatch.
func (l *NativeLease) CommittedReviewSession(ctx context.Context, ref NativeEventRef) (string, error) {
	if l == nil || ref.Incarnation != l.native.CarrierIncarnation || ref.Sequence == 0 || ref.PayloadHash == "" {
		return "", ErrJournalState
	}
	var session string
	err := l.with(ctx, func() error {
		var err error
		session, err = l.committedReviewSession(ctx, ref)
		return err
	})
	return session, err
}

// committedReviewSession is called only under a validated owner fence or
// before binding the result using the same immutable durable proof.
func (l *NativeLease) committedReviewSession(ctx context.Context, ref NativeEventRef) (string, error) {
	var eventRaw, commandRaw []byte
	var hash, state string
	var seq int64
	err := l.journal.db.QueryRowContext(ctx, `SELECT e.payload,e.payload_hash,c.payload,d.state,d.event_sequence FROM native_events e JOIN native_commands c ON c.command_id=e.command_id JOIN dispatch d ON d.id=c.dispatch_id WHERE e.incarnation=? AND e.sequence=? AND d.tenant=? AND d.app_id=? AND d.gateway=? AND d.surface=? AND d.chat=? AND d.user_id=? AND d.generation=? AND d.carrier=?`, ref.Incarnation, ref.Sequence, l.surface.TenantID, l.surface.AppID, l.surface.GatewayID, l.surface.SurfaceID, l.surface.ChatID, l.surface.UserID, l.generation, ref.Incarnation).Scan(&eventRaw, &hash, &commandRaw, &state, &seq)
	if err != nil {
		return "", err
	}
	var event agentproto.Event
	var command agentproto.Command
	if hash != ref.PayloadHash || state != "accepted" || seq != int64(ref.Sequence) || json.Unmarshal(eventRaw, &event) != nil || json.Unmarshal(commandRaw, &command) != nil {
		return "", ErrJournalConflict
	}
	actual, err := agentproto.RuntimeEventPayloadHash(event)
	if err != nil || actual != hash || command.Kind != agentproto.CommandReviewStart || command.NativeBinding == nil || event.NativeBinding == nil || *command.NativeBinding != *event.NativeBinding || event.NativeBinding.Owner != l.native || command.CommandID != event.CommandID || command.Target.ThreadID == "" || event.ThreadID != command.Target.ThreadID || event.Kind != agentproto.EventNativeControlResult || event.Action != "review.start" || event.Status != "accepted" || event.RequestID == "" || event.TurnID == "" {
		return "", ErrJournalConflict
	}
	child, ok := event.Metadata["reviewThreadID"].(string)
	if !ok || child == "" || child == command.Target.ThreadID {
		return "", ErrJournalConflict
	}
	var createdRaw, receiptRaw []byte
	var createdHash string
	var proofCount int
	err = l.journal.db.QueryRowContext(ctx, `SELECT payload,payload_hash,count(*) OVER() FROM native_events WHERE incarnation=? AND sequence<? AND command_id=? AND json_extract(payload,'$.kind')=? AND json_extract(payload,'$.threadId')=?`, ref.Incarnation, ref.Sequence, command.CommandID, string(agentproto.EventReviewChildCreated), child).Scan(&createdRaw, &createdHash, &proofCount)
	if err != nil {
		return "", err
	}
	if proofCount != 1 {
		return "", ErrJournalConflict
	}
	var created agentproto.Event
	if json.Unmarshal(createdRaw, &created) != nil {
		return "", ErrJournalConflict
	}
	createdActual, err := agentproto.RuntimeEventPayloadHash(created)
	if err != nil || createdActual != createdHash || created.NativeBinding == nil || *created.NativeBinding != *command.NativeBinding || created.Metadata["parentThreadID"] != command.Target.ThreadID || created.Metadata["stage"] != "thread/start" || created.Action != "review.start" || created.RequestID == "" {
		return "", ErrJournalConflict
	}
	if err = l.journal.db.QueryRowContext(ctx, `SELECT payload FROM native_command_receipts WHERE command_id=? AND incarnation=?`, command.CommandID, ref.Incarnation).Scan(&receiptRaw); err != nil {
		return "", err
	}
	var receipt agentproto.RuntimeCommandReceipt
	if json.Unmarshal(receiptRaw, &receipt) != nil {
		return "", ErrJournalConflict
	}
	matched := false
	for _, frame := range receipt.Frames {
		if frame.NativeRequestID == created.RequestID && frame.NativeMethod == "thread/start" && (frame.Stage == "written" || frame.Stage == "child_write_started" || frame.Stage == "unknown") {
			matched = true
		}
	}
	if !matched {
		return "", ErrJournalConflict
	}
	return child, nil
}

// reviewProjectionSession finds only a committed accepted review proof. An
// earlier turn notification is associated by exact child and turn identity.
func (j *Journal) reviewProjectionSession(ctx context.Context, l *NativeLease, ref NativeEventRef) (string, error) {
	event, _, _, err := j.processorNativeEvent(ctx, ref)
	if err != nil {
		return "", err
	}
	if event.Kind == agentproto.EventNativeControlResult && event.Action == "review.start" && event.Status == "accepted" {
		return l.committedReviewSession(ctx, ref)
	}
	if !isNativeTurnProjection(event.Kind) || event.ThreadID == "" || event.TurnID == "" {
		return "", nil
	}
	root, err := j.reviewRootTurn(ctx, l, event)
	if err != nil {
		return "", err
	}
	rows, err := j.db.QueryContext(ctx, `SELECT sequence,payload_hash FROM native_events WHERE incarnation=? AND json_extract(payload,'$.action')='review.start' AND json_extract(payload,'$.status')='accepted' AND json_extract(payload,'$.metadata.reviewThreadID')=? AND json_extract(payload,'$.turnId')=?`, ref.Incarnation, event.ThreadID, event.TurnID)
	if root != "" && root != event.TurnID {
		if err == nil {
			rows.Close()
		}
		rows, err = j.db.QueryContext(ctx, `SELECT sequence,payload_hash FROM native_events WHERE incarnation=? AND json_extract(payload,'$.action')='review.start' AND json_extract(payload,'$.status')='accepted' AND json_extract(payload,'$.metadata.reviewThreadID')=? AND json_extract(payload,'$.turnId')=?`, ref.Incarnation, event.ThreadID, root)
	}
	if err != nil {
		return "", err
	}
	var refs []NativeEventRef
	for rows.Next() {
		var proof NativeEventRef
		proof.Incarnation = ref.Incarnation
		if err = rows.Scan(&proof.Sequence, &proof.PayloadHash); err != nil {
			rows.Close()
			return "", err
		}
		refs = append(refs, proof)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return "", err
	}
	if len(refs) == 0 {
		return "", nil
	}
	if len(refs) != 1 {
		return "", ErrJournalConflict
	}
	return l.committedReviewSession(ctx, refs[0])
}

// reviewRootTurn preserves the native subturn ID while obtaining its root only
// from the persisted turn.started event for this exact carrier/session/turn.
func (j *Journal) reviewRootTurn(ctx context.Context, l *NativeLease, event agentproto.Event) (string, error) {
	rows, err := j.db.QueryContext(ctx, `SELECT json_extract(payload,'$.rootTurnId') FROM native_events WHERE incarnation=? AND json_extract(payload,'$.kind')=? AND json_extract(payload,'$.threadId')=? AND json_extract(payload,'$.turnId')=? AND coalesce(json_extract(payload,'$.rootTurnId'),'')<>''`, l.native.CarrierIncarnation, string(agentproto.EventTurnStarted), event.ThreadID, event.TurnID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var root string
	for rows.Next() {
		var value string
		if err = rows.Scan(&value); err != nil {
			return "", err
		}
		if root != "" && root != value {
			return "", ErrJournalConflict
		}
		root = value
	}
	return root, rows.Err()
}

// ReadCommittedNativeInspection is a read-only recovery port for the host's
// stop verifier while the registry write transaction is already held. It checks
// the durable carrier/owner mapping and never mutates the application journal.
func (j *Journal) ReadCommittedNativeInspection(ctx context.Context, native agentproto.NativeOwner, surface Owner, generation int64, ref NativeEventRef) (agentproto.RuntimeInspectionResult, error) {
	var ownerRaw, surfaceRaw []byte
	var storedGeneration int64
	if j == nil || native.Validate() != nil || !validOwner(surface) || generation <= 0 {
		return agentproto.RuntimeInspectionResult{}, ErrJournalState
	}
	if err := j.db.QueryRowContext(ctx, `SELECT owner_json,surface_json,generation FROM native_carriers WHERE incarnation=?`, native.CarrierIncarnation).Scan(&ownerRaw, &surfaceRaw, &storedGeneration); err != nil {
		return agentproto.RuntimeInspectionResult{}, err
	}
	expectedOwner, _ := json.Marshal(native)
	expectedSurface, _ := json.Marshal(surface)
	if string(ownerRaw) != string(expectedOwner) || string(surfaceRaw) != string(expectedSurface) || storedGeneration != generation {
		return agentproto.RuntimeInspectionResult{}, ErrJournalConflict
	}
	lease := &NativeLease{journal: j, authority: readOnlyNativeAuthority{}, native: native, surface: surface, generation: generation}
	return lease.CommittedNativeInspection(ctx, ref)
}

type readOnlyNativeAuthority struct{}

func (readOnlyNativeAuthority) WithNativeLease(ctx context.Context, _ agentproto.NativeOwner, _ Owner, _ int64, read func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return read()
}

// ProveCommittedNativeTerminal resolves an unbound stream terminal using the
// exact accepted session/turn proof; it never relies on event ordering.
func (j *Journal) ProveCommittedNativeTerminal(ctx context.Context, native agentproto.NativeOwner, surface Owner, generation int64, event agentproto.Event) error {
	if event.Journal == nil || event.Journal.CarrierIncarnation != native.CarrierIncarnation || event.Journal.Sequence == 0 {
		return ErrJournalConflict
	}
	var ownerRaw, surfaceRaw, raw []byte
	var gen int64
	var hash string
	if err := j.db.QueryRowContext(ctx, `SELECT c.owner_json,c.surface_json,c.generation,e.payload,e.payload_hash FROM native_carriers c JOIN native_events e ON e.incarnation=c.incarnation WHERE c.incarnation=? AND e.sequence=?`, native.CarrierIncarnation, event.Journal.Sequence).Scan(&ownerRaw, &surfaceRaw, &gen, &raw, &hash); err != nil {
		return err
	}
	expectedOwner, _ := json.Marshal(native)
	expectedSurface, _ := json.Marshal(surface)
	encoded, _ := json.Marshal(event)
	digest, err := agentproto.RuntimeEventPayloadHash(event)
	if err != nil || string(ownerRaw) != string(expectedOwner) || string(surfaceRaw) != string(expectedSurface) || gen != generation || string(raw) != string(encoded) || hash != event.Journal.PayloadHash || digest != hash {
		return ErrJournalConflict
	}
	lease := &NativeLease{journal: j, authority: readOnlyNativeAuthority{}, native: native, surface: surface, generation: generation}
	binding, err := j.nativeBindingFromProof(ctx, lease, event)
	if err != nil {
		return err
	}
	if binding.Owner != native {
		return ErrJournalConflict
	}
	return nil
}

// BindingForAcceptedTurn resolves only a uniquely journaled accepted native
// start for the exact session/turn. It never selects the current or latest turn.
func (l *NativeLease) BindingForAcceptedTurn(ctx context.Context, session, turn string) (*agentproto.NativeTurnBinding, error) {
	if l == nil || session == "" || turn == "" {
		return nil, ErrJournalState
	}
	var binding *agentproto.NativeTurnBinding
	err := l.with(ctx, func() error {
		var e error
		binding, e = l.journal.nativeBindingFromProof(ctx, l, agentproto.Event{ThreadID: session, TurnID: turn})
		return e
	})
	return binding, err
}
func (j *Journal) HasNativeDispatch(ctx context.Context, effectID string) (bool, error) {
	var n int
	if j == nil || effectID == "" {
		return false, ErrJournalState
	}
	err := j.db.QueryRowContext(ctx, `SELECT count(*) FROM native_commands WHERE dispatch_id=?`, effectID+":native").Scan(&n)
	return n != 0, err
}

type AcceptedNativeTurn struct {
	SessionID, TurnID string
	Binding           agentproto.NativeTurnBinding
}

// ActiveAcceptedTurns enumerates exact accepted start facts without using UI
// focus. A missing terminal remains active; unknown input prevents quiescence.
func (l *NativeLease) ActiveAcceptedTurns(ctx context.Context) ([]AcceptedNativeTurn, error) {
	if l == nil {
		return nil, ErrJournalState
	}
	var out []AcceptedNativeTurn
	err := l.with(ctx, func() error {
		rows, e := l.journal.db.QueryContext(ctx, `SELECT e.payload FROM native_events e JOIN native_commands c ON c.command_id=e.command_id JOIN dispatch d ON d.id=c.dispatch_id WHERE e.incarnation=? AND d.state='accepted' AND d.event_sequence=e.sequence AND json_extract(c.payload,'$.kind')='prompt.send' ORDER BY e.sequence`, l.native.CarrierIncarnation)
		if e != nil {
			return e
		}
		var events []agentproto.Event
		for rows.Next() {
			var raw []byte
			if e = rows.Scan(&raw); e != nil {
				rows.Close()
				return e
			}
			var event agentproto.Event
			if e = json.Unmarshal(raw, &event); e != nil {
				rows.Close()
				return e
			}
			events = append(events, event)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		seen := map[string]agentproto.NativeTurnBinding{}
		for _, event := range events {
			if event.NativeBinding == nil || event.NativeBinding.Owner != l.native || event.ThreadID == "" || event.TurnID == "" {
				return ErrJournalConflict
			}
			key := event.ThreadID + "\x00" + event.TurnID
			if old, ok := seen[key]; ok {
				if old != *event.NativeBinding {
					return ErrJournalConflict
				}
				continue
			}
			seen[key] = *event.NativeBinding
			var terminal int
			e = l.journal.db.QueryRowContext(ctx, `SELECT count(*) FROM native_events WHERE incarnation=? AND json_extract(payload,'$.kind')='turn.completed' AND json_extract(payload,'$.threadId')=? AND json_extract(payload,'$.turnId')=?`, l.native.CarrierIncarnation, event.ThreadID, event.TurnID).Scan(&terminal)
			if e != nil {
				return e
			}
			if terminal == 0 {
				out = append(out, AcceptedNativeTurn{SessionID: event.ThreadID, TurnID: event.TurnID, Binding: *event.NativeBinding})
			}
		}
		return nil
	})
	return out, err
}
func (l *NativeLease) FindNativeEvent(ctx context.Context, commandID string, kind agentproto.EventKind) (NativeEventRef, error) {
	var ref NativeEventRef
	if l == nil || commandID == "" || kind == "" {
		return ref, ErrJournalState
	}
	err := l.with(ctx, func() error {
		ref.Incarnation = l.native.CarrierIncarnation
		return l.journal.db.QueryRowContext(ctx, `SELECT sequence,payload_hash FROM native_events WHERE incarnation=? AND command_id=? AND json_extract(payload,'$.kind')=? ORDER BY sequence LIMIT 1`, ref.Incarnation, commandID, string(kind)).Scan(&ref.Sequence, &ref.PayloadHash)
	})
	return ref, err
}
func (l *NativeLease) HostCommandState(ctx context.Context, id string) (string, error) {
	var state string
	if l == nil || id == "" {
		return "", ErrJournalState
	}
	err := l.with(ctx, func() error {
		return l.journal.db.QueryRowContext(ctx, `SELECT d.state FROM native_commands c JOIN dispatch d ON d.id=c.dispatch_id WHERE c.command_id=? AND d.carrier=?`, id, l.native.CarrierIncarnation).Scan(&state)
	})
	return state, err
}
