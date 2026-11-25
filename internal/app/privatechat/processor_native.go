package privatechat

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// NativeReducer is the local-only host port for old-core event application and
// daemon history/OAuth handling. It must not perform I/O or send any effect.
type NativeReducer interface {
	ReduceNative(*App, string, agentproto.Event) ([]eventcontract.Event, error)
}
type CoreNativeReducer struct{}

func (CoreNativeReducer) ReduceNative(a *App, instance string, event agentproto.Event) ([]eventcontract.Event, error) {
	if event.Kind == agentproto.EventNativeTurnInputAccepted {
		return a.Service().HandlePrivateSteerAccepted(instance, event.CommandID), nil
	}
	if event.Kind == agentproto.EventNativeShellRPCResult && event.Status == "acknowledged" {
		return a.Service().HandlePrivateShellRequestAcknowledged(instance, event.CommandID), nil
	}
	if event.Kind == agentproto.EventNativeTurnInputRejected || event.Kind == agentproto.EventNativeShellRPCResult && event.Status == "failed" {
		surface := a.instances[instance]
		if surface == "" || event.CommandID == "" {
			return nil, ErrJournalConflict
		}
		return a.Service().HandleCommandDispatchFailure(surface, event.CommandID, fmt.Errorf("%s", event.ErrorMessage)), nil
	}
	if event.Kind == agentproto.EventNativeShellItemObserved {
		switch event.Action {
		case "started":
			event.Kind = agentproto.EventItemStarted
		case "completed":
			event.Kind = agentproto.EventItemCompleted
		default:
			return nil, ErrJournalConflict
		}
		return a.AgentEvent(instance, event)
	}
	if event.Kind == agentproto.EventNativeControlResult && event.Action == "request.response.written" && event.Status == "sent" {
		return a.Service().HandlePrivateRequestResponseWritten(instance, event.CommandID), nil
	}
	if event.Kind == agentproto.EventNativeControlResult && event.Action == "review.start" {
		if event.Status == "accepted" {
			return a.Service().HandlePrivateReviewStartAcknowledged(instance, event.CommandID), nil
		}
		if event.Status == "failed" {
			surface := a.instances[instance]
			if surface == "" || event.CommandID == "" {
				return nil, ErrJournalConflict
			}
			return a.Service().HandleCommandDispatchFailure(surface, event.CommandID, fmt.Errorf("%s", event.ErrorMessage)), nil
		}
	}
	if event.Kind == agentproto.EventSystemError && event.Problem != nil && event.Problem.Operation == "thread.compact.start" && event.Problem.Stage == "thread_resume_response" {
		surface := a.instances[instance]
		if surface == "" || event.CommandID == "" {
			return nil, ErrJournalConflict
		}
		return a.Service().HandleCommandDispatchFailure(surface, event.CommandID, *event.Problem), nil
	}
	if event.Kind == agentproto.EventNativeControlResult && event.Action == "thread.compact.start" && event.Status == "failed" {
		surface := a.instances[instance]
		if surface == "" || event.CommandID == "" {
			return nil, ErrJournalConflict
		}
		return a.Service().HandleCommandDispatchFailure(surface, event.CommandID, fmt.Errorf("%s", event.ErrorMessage)), nil
	}
	if event.Kind == agentproto.EventNativeCommandRejected {
		surface := a.instances[instance]
		if surface == "" || event.CommandID == "" || event.Problem == nil || event.Problem.CommandID != event.CommandID {
			return nil, ErrJournalConflict
		}
		if effects, handled := a.Service().HandlePrivateQueryFailure(surface, instance, event.CommandID, *event.Problem); handled {
			return effects, nil
		}
		return a.Service().HandleCommandDispatchFailure(surface, event.CommandID, *event.Problem), nil
	}
	return a.AgentEvent(instance, event)
}

// RegisterNativeHello is called only with the authenticated connection hello.
// The lease authority must independently verify its owner, generation and host.
// It does not replay a historical instance as online.
func (p *Processor) RegisterNativeHello(ctx context.Context, l *NativeLease, hello agentproto.Hello) error {
	unlockReference := p.journal.LockReferenceMutation()
	defer unlockReference()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.poisoned {
		return ErrProcessorPoisoned
	}
	if l == nil || l.journal != p.journal || hello.Probe || hello.PrivateNativeProtocolVersion != agentproto.PrivateNativeProtocolVersion || hello.CarrierIncarnation != l.native.CarrierIncarnation || hello.Instance.InstanceID != l.native.InstanceID || agentproto.EffectiveHelloBackend(hello) != l.native.Backend {
		return ErrJournalConflict
	}
	return l.with(ctx, func() error {
		ctx, projectionErr := beginProjection(ctx)
		if projectionErr != nil {
			return projectionErr
		}

		return p.registerNativeHelloLocked(ctx, l, hello)
	})
}

func (p *Processor) registerNativeHelloLocked(ctx context.Context, l *NativeLease, hello agentproto.Hello) error {
	if _, err := p.ensureOwner(ctx, l.surface, l.generation); err != nil {
		return err
	}
	if p.app.Service().PrivateDetachedInstance(l.surface.SurfaceID, l.native.InstanceID) {
		return nil
	}
	if old, ok := p.app.instances[l.native.InstanceID]; ok && old != l.surface.SurfaceID {
		return ErrJournalConflict
	}
	inst := p.app.Service().Instance(l.native.InstanceID)
	if inst == nil {
		inst = &state.InstanceRecord{InstanceID: l.native.InstanceID, Threads: map[string]*state.ThreadRecord{}}
	}
	inst.DisplayName = hello.Instance.DisplayName
	inst.WorkspaceRoot = state.NormalizeWorkspaceKey(hello.Instance.WorkspaceRoot)
	inst.WorkspaceKey = state.ResolveWorkspaceKey(hello.Instance.WorkspaceKey, inst.WorkspaceRoot)
	inst.ShortName = hello.Instance.ShortName
	inst.Backend = l.native.Backend
	state.ApplyInstanceHelloProfile(inst, l.native.Backend, hello.Instance)
	inst.Source = hello.Instance.Source
	inst.Managed = hello.Instance.Managed
	inst.PID = hello.Instance.PID
	inst.Capabilities = agentproto.EffectiveHelloCapabilities(hello)
	inst.CapabilitiesDeclared = hello.CapabilitiesDeclared
	inst.Online = true
	return p.app.RegisterInstance(l.surface.SurfaceID, inst)
}

// ApplyNative consumes only an event already committed by the native receiver.
// The exact lease remains held for reduction and atomic checkpoint/effects plus
// applied marking. Network I/O and native acknowledgement happen elsewhere.
func (p *Processor) ApplyNative(ctx context.Context, l *NativeLease, ref NativeEventRef, reducer NativeReducer) ([]EffectRecord, error) {
	return p.applyNative(ctx, l, ref, reducer, false)
}

// ApplyNativeForStop projects already durable events while the carrier is
// draining, with new queue dispatch paused until a replacement is ready.
func (p *Processor) ApplyNativeForStop(ctx context.Context, l *NativeLease, ref NativeEventRef, reducer NativeReducer) ([]EffectRecord, error) {
	return p.applyNative(ctx, l, ref, reducer, true)
}

func (p *Processor) applyNative(ctx context.Context, l *NativeLease, ref NativeEventRef, reducer NativeReducer, stopping bool) (out []EffectRecord, err error) {
	unlockReference := p.journal.LockReferenceMutation()
	defer unlockReference()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.poisoned {
		return nil, ErrProcessorPoisoned
	}
	if l == nil || l.journal != p.journal || reducer == nil || ref.Incarnation != l.native.CarrierIncarnation || ref.Sequence == 0 || ref.PayloadHash == "" {
		return nil, ErrJournalState
	}
	err = l.with(ctx, func() error {
		ctx, projectionErr := beginProjection(ctx)
		if projectionErr != nil {
			return projectionErr
		}

		event, applied, revision, e := p.journal.processorNativeEvent(ctx, ref)
		if e != nil {
			return e
		}
		if applied {
			out, e = p.journal.processorEffects(ctx, l.surface, l.generation, revision)
			return e
		}
		if event.Kind == agentproto.EventItemDelta {
			return fmt.Errorf("live delta is not a durable completed event: %w", ErrJournalState)
		}
		if event.Live != nil || event.Journal == nil || event.Journal.CarrierIncarnation != l.native.CarrierIncarnation || event.Seq != ref.Sequence || event.Journal.PayloadHash != ref.PayloadHash || event.NativeBinding != nil && event.NativeBinding.Owner != l.native {
			return ErrJournalConflict
		}
		var compactCommand string
		if isNativeTurnProjection(event.Kind) {
			binding, command, err := p.journal.compactBindingForTurn(ctx, l, event)
			if err != nil {
				return err
			}
			if binding != nil {
				if event.NativeBinding != nil && *event.NativeBinding != *binding {
					return ErrJournalConflict
				}
				event.NativeBinding = binding
				compactCommand = command
			}
		}
		if isNativeTurnProjection(event.Kind) && event.NativeBinding == nil {
			binding, e := p.journal.nativeBindingFromProof(ctx, l, event)
			if e != nil {
				return e
			}
			event.NativeBinding = binding
		}
		if event.CommandID != "" {
			ready, e := p.journal.nativeCommandProjectionReady(ctx, l, event.CommandID)
			if e != nil {
				return e
			}
			if !ready {
				return ErrJournalState
			}
			if e = p.journal.validateNativeProjectionFact(ctx, l, ref, event); e != nil {
				return e
			}

		}
		current, e := p.ensureOwner(ctx, l.surface, l.generation)
		if e != nil {
			return e
		}
		if p.app.Service().PrivateDetachedInstance(l.surface.SurfaceID, l.native.InstanceID) {
			if !p.app.Service().RecordPrivateDetachedEvent(l.surface.SurfaceID, l.native.InstanceID, ref.Incarnation, ref.Sequence, ref.PayloadHash) {
				return ErrJournalConflict
			}
			core, e := p.app.Service().ExportPrivateCheckpoint(l.surface.SurfaceID)
			if e != nil {
				p.poisoned = true
				return e
			}
			cp := Checkpoint{Owner: l.surface, Generation: l.generation, Revision: current.revision + 1, Cursor: current.cursor, Core: core}
			if e = p.journal.commitCheckpoint(ctx, cp, current.revision, nil, 0, &ref); e != nil {
				p.poisoned = true
				return fmt.Errorf("%w: detached native event: %v", ErrProcessorPoisoned, e)
			}
			p.owners[l.surface] = processorOwner{cp.Revision, cp.Cursor, l.generation}
			out, e = p.journal.processorEffects(ctx, l.surface, l.generation, cp.Revision)
			return e
		}
		if stopping && p.app.instances[l.native.InstanceID] == "" {
			if e = p.app.Service().MaterializePrivateStoppedInstance(l.surface.SurfaceID, l.native); e != nil {
				return e
			}
			p.app.instances[l.native.InstanceID] = l.surface.SurfaceID
		}
		if p.app.instances[l.native.InstanceID] != l.surface.SurfaceID || p.app.Service().Instance(l.native.InstanceID) == nil || (!stopping && !p.app.Service().Instance(l.native.InstanceID).Online) {
			return fmt.Errorf("authenticated native hello required: %w", ErrJournalState)
		}
		if stopping {
			if e = p.app.Service().MarkPrivateStopProjectionOffline(l.surface.SurfaceID, l.native.InstanceID); e != nil {
				return e
			}
		}
		if event.Kind == agentproto.EventRequestResolved || event.Kind == agentproto.EventNativeControlResult && event.Action == "request.response.written" {
			commandID := ""
			if event.Kind == agentproto.EventNativeControlResult {
				commandID = event.CommandID
			}
			if e = p.app.Service().RestorePrivateRequestProjection(l.surface.SurfaceID, l.native.InstanceID, event.RequestID, commandID, event.ThreadID, event.TurnID); e != nil {
				return e
			}
		}
		if event.Kind == agentproto.EventNativeControlResult && (event.Action == "review.start" || event.Action == "thread.compact.start") {
			kind := agentproto.CommandReviewStart
			if event.Action == "thread.compact.start" {
				kind = agentproto.CommandThreadCompactStart
			}
			reviewThread, _ := event.Metadata["reviewThreadID"].(string)
			if e = p.app.Service().RestorePrivateControlProjection(l.surface.SurfaceID, l.native.InstanceID, event.CommandID, kind, event.ThreadID, reviewThread, event.TurnID); e != nil {
				return e
			}
		}
		if event.Kind == agentproto.EventNativeCommandRejected || event.Kind == agentproto.EventSystemError && event.Problem != nil && event.Problem.Operation == "thread.compact.start" && event.Problem.Stage == "thread_resume_response" {
			var raw []byte
			if e = p.journal.db.QueryRowContext(ctx, `SELECT n.payload FROM native_commands n JOIN dispatch d ON d.id=n.dispatch_id WHERE n.command_id=? AND d.carrier=?`, event.CommandID, l.native.CarrierIncarnation).Scan(&raw); e != nil {
				return e
			}
			var command agentproto.Command
			if e = json.Unmarshal(raw, &command); e != nil {
				return e
			}
			if command.Kind == agentproto.CommandReviewStart || command.Kind == agentproto.CommandThreadCompactStart {
				if e = p.app.Service().RestorePrivateControlProjection(l.surface.SurfaceID, l.native.InstanceID, event.CommandID, command.Kind, command.Target.ThreadID, "", ""); e != nil {
					return e
				}
			}
		}
		if compactCommand != "" {
			if e = p.app.Service().RestorePrivateControlProjection(l.surface.SurfaceID, l.native.InstanceID, compactCommand, agentproto.CommandThreadCompactStart, event.ThreadID, event.ThreadID, event.TurnID); e != nil {
				return e
			}
		}
		reviewEvent, e := p.journal.isPrivateReviewTurn(ctx, l, event)
		if e != nil {
			return e
		}
		projectedCommand := ""
		if reviewEvent {
			var command, parent string
			var count int
			if e = p.journal.db.QueryRowContext(ctx, `SELECT n.command_id,json_extract(n.payload,'$.target.threadId'),count(*) OVER() FROM native_commands n JOIN dispatch d ON d.id=n.dispatch_id WHERE d.carrier=? AND json_extract(n.payload,'$.kind')=? AND json_extract(n.payload,'$.nativeBinding.executionId')=?`, l.native.CarrierIncarnation, string(agentproto.CommandReviewStart), event.NativeBinding.ExecutionID).Scan(&command, &parent, &count); e != nil {
				return e
			}
			if count != 1 {
				return ErrJournalConflict
			}
			if e = p.app.Service().RestorePrivateControlProjection(l.surface.SurfaceID, l.native.InstanceID, command, agentproto.CommandReviewStart, parent, event.ThreadID, event.TurnID); e != nil {
				return e
			}
		}
		if isNativeTurnProjection(event.Kind) && !reviewEvent && compactCommand == "" {
			command, e := p.journal.nativeProjectionCommand(ctx, l, event)
			if e != nil {
				return e
			}
			projectedCommand = command
			if event.Kind == agentproto.EventTurnCompleted && event.TurnID != "" {
				if e = p.journal.prepareNativeImageCleanup(ctx, l, event, command); e != nil {
					return e
				}
			}
			if e = p.app.Service().RestorePrivateNativeProjection(l.surface.SurfaceID, l.native.InstanceID, command, event.ThreadID, event.TurnID); e != nil {
				p.poisoned = true
				return e
			}
		}
		restoredRequest, e := p.restoreRequestInventoryLocked(ctx, l, ref, event)
		if e != nil {
			return e
		}
		var events []eventcontract.Event
		if (event.Kind == agentproto.EventThreadHistoryRead || event.Kind == agentproto.EventNativeCommandRejected) && p.app.Service().PrivateReconnectQueryMatches(l.surface.SurfaceID, l.native.InstanceID, event.CommandID) {
			pending, _ := p.app.Service().PrivateReconnectPendingText(l.surface.SurfaceID)
			allowed, check := p.reconnectPendingInputProven(ctx, l.surface, l.generation, pending)
			if check != nil {
				return check
			}
			events = p.app.Service().CompletePrivateReconnectQuery(l.surface.SurfaceID, l.native.InstanceID, event, allowed)
		} else {
			events, e = reducer.ReduceNative(p.app, l.native.InstanceID, event)
		}
		if e != nil {
			p.poisoned = true
			return fmt.Errorf("%w: native reducer: %v", ErrProcessorPoisoned, e)
		}
		if event.Kind == agentproto.EventTurnCompleted {
			p.app.Service().ResolvePrivateOfflineStopTerminal(l.surface.SurfaceID, l.native.InstanceID, projectedCommand, event.ThreadID, event.TurnID)
		}
		if !stopping {
			follow, e := p.reconcileRecoveryGate(ctx, l.surface, l.generation)
			if e != nil {
				p.poisoned = true
				return e
			}
			events = append(events, follow...)
		}
		if !stopping && event.Kind != agentproto.EventTurnCompleted {
			events = append(events, p.app.Service().ResumePrivateOfflineStop(l.surface.SurfaceID, l.native.InstanceID)...)
		}
		if restoredRequest {
			events, e = p.appendRequestVisibility(ctx, l.surface, l.generation, events, false)
			if e != nil {
				p.poisoned = true
				return e
			}
		}
		effects, e := p.freezeEffects(l.surface, l.generation, fmt.Sprintf("native:%s:%d", ref.Incarnation, ref.Sequence), events)
		if e != nil {
			p.poisoned = true
			return fmt.Errorf("%w: native effects: %v", ErrProcessorPoisoned, e)
		}
		if e = bindNativeDisplayChains(l.surface, ref.Incarnation, events, effects); e != nil {
			p.poisoned = true
			return e
		}
		bindNativePublicationOrder(l.surface, l.generation, ref.Incarnation, event.ThreadID, event.TurnID, events, effects)
		core, e := p.app.Service().ExportPrivateCheckpoint(l.surface.SurfaceID)
		if e != nil {
			p.poisoned = true
			return fmt.Errorf("%w: native checkpoint: %v", ErrProcessorPoisoned, e)
		}
		cp := Checkpoint{Owner: l.surface, Generation: l.generation, Revision: current.revision + 1, Cursor: current.cursor, Core: core}
		if e = p.journal.commitCheckpoint(ctx, cp, current.revision, effects, 0, &ref); e != nil {
			p.poisoned = true
			return fmt.Errorf("%w: native commit: %v", ErrProcessorPoisoned, e)
		}
		p.owners[l.surface] = processorOwner{cp.Revision, cp.Cursor, l.generation}
		out, e = p.journal.processorEffects(ctx, l.surface, l.generation, cp.Revision)
		return e
	})
	return out, err
}
func isNativeTurnProjection(k agentproto.EventKind) bool {
	switch k {
	case agentproto.EventTurnStarted, agentproto.EventTurnCompleted, agentproto.EventItemStarted, agentproto.EventItemCompleted:
		return true
	}
	return false
}

func (j *Journal) processorNativeEvent(ctx context.Context, ref NativeEventRef) (agentproto.Event, bool, int64, error) {
	var event agentproto.Event
	var raw []byte
	var applied int
	var revision int64
	err := j.db.QueryRowContext(ctx, `SELECT payload,applied,applied_revision FROM native_events WHERE incarnation=? AND sequence=? AND payload_hash=?`, ref.Incarnation, ref.Sequence, ref.PayloadHash).Scan(&raw, &applied, &revision)
	if errors.Is(err, sql.ErrNoRows) {
		return event, false, 0, ErrJournalConflict
	}
	if err != nil {
		return event, false, 0, err
	}
	if err = json.Unmarshal(raw, &event); err != nil {
		return event, false, 0, err
	}
	if applied != 0 {
		if revision < 1 {
			return event, false, 0, ErrJournalConflict
		}
		return event, true, revision, nil
	}
	if ref.Sequence > 1 {
		var previous int
		if err = j.db.QueryRowContext(ctx, `SELECT count(*) FROM native_events WHERE incarnation=? AND sequence=? AND applied=1`, ref.Incarnation, ref.Sequence-1).Scan(&previous); err != nil {
			return event, false, 0, err
		}
		if previous != 1 {
			return event, false, 0, ErrJournalState
		}
	}
	return event, false, 0, nil
}

// Find the original prompt for this frozen execution; a steer command's ID is
// not the old core's active-turn binding. No thread or timing heuristic is used.
func (j *Journal) nativeProjectionCommand(ctx context.Context, l *NativeLease, event agentproto.Event) (string, error) {
	if event.NativeBinding == nil || event.NativeBinding.Owner != l.native || event.NativeBinding.ExecutionID == "" {
		return "", ErrJournalConflict
	}
	allowFailed := event.Kind == agentproto.EventTurnCompleted && event.Status == "failed" && event.TurnID == "" && event.CommandID != ""
	rows, err := j.db.QueryContext(ctx, `SELECT n.payload FROM native_commands n JOIN dispatch d ON d.id=n.dispatch_id WHERE d.tenant=? AND d.app_id=? AND d.gateway=? AND d.surface=? AND d.chat=? AND d.user_id=? AND d.generation=? AND d.carrier=? AND (d.state='accepted' OR (? AND d.state='failed' AND d.command_id=?)) AND json_extract(n.payload,'$.nativeBinding.executionId')=? AND json_extract(n.payload,'$.kind')=?`, l.surface.TenantID, l.surface.AppID, l.surface.GatewayID, l.surface.SurfaceID, l.surface.ChatID, l.surface.UserID, l.generation, l.native.CarrierIncarnation, allowFailed, event.CommandID, event.NativeBinding.ExecutionID, string(agentproto.CommandPromptSend))
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var result string
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return "", err
		}
		var command agentproto.Command
		if err = json.Unmarshal(raw, &command); err != nil {
			return "", err
		}
		if command.NativeBinding == nil || *command.NativeBinding != *event.NativeBinding {
			return "", ErrJournalConflict
		}
		if result != "" {
			return "", ErrJournalConflict
		}
		result = command.CommandID
	}
	if err = rows.Err(); err != nil {
		return "", err
	}
	if result == "" {
		return "", ErrJournalConflict
	}
	return result, nil
}

func (j *Journal) nativeCommandProjectionReady(ctx context.Context, l *NativeLease, commandID string) (bool, error) {
	var status string
	err := j.db.QueryRowContext(ctx, `SELECT d.state FROM native_commands n JOIN dispatch d ON d.id=n.dispatch_id WHERE n.command_id=? AND d.tenant=? AND d.app_id=? AND d.gateway=? AND d.surface=? AND d.chat=? AND d.user_id=? AND d.generation=? AND d.carrier=?`, commandID, l.surface.TenantID, l.surface.AppID, l.surface.GatewayID, l.surface.SurfaceID, l.surface.ChatID, l.surface.UserID, l.generation, l.native.CarrierIncarnation).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrJournalConflict
	}
	if err != nil {
		return false, err
	}
	return status == "accepted" || status == "failed" || status == "sent" || status == "resolved", nil
}

// nativeBindingFromProof associates an early unbound notification only after a
// journaled native receipt proves the exact same session and turn. It does not
// infer ownership from arrival time, a current focus, or the newest command.
func (j *Journal) nativeBindingFromProof(ctx context.Context, l *NativeLease, event agentproto.Event) (*agentproto.NativeTurnBinding, error) {
	if event.ThreadID == "" || event.TurnID == "" {
		return nil, ErrJournalState
	}
	rows, err := j.db.QueryContext(ctx, `SELECT payload FROM native_events WHERE incarnation=? AND (json_extract(payload,'$.threadId')=? OR (json_extract(payload,'$.action')='review.start' AND json_extract(payload,'$.metadata.reviewThreadID')=?)) AND json_extract(payload,'$.turnId')=? AND json_extract(payload,'$.commandId') IS NOT NULL`, l.native.CarrierIncarnation, event.ThreadID, event.ThreadID, event.TurnID)
	if err != nil {
		return nil, err
	}
	var proofs []agentproto.Event
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return nil, err
		}
		var proof agentproto.Event
		if err = json.Unmarshal(raw, &proof); err != nil {
			rows.Close()
			return nil, err
		}
		if proof.NativeBinding == nil || proof.NativeBinding.Owner != l.native || proof.CommandID == "" || proof.RequestID == "" {
			continue
		}
		valid := (l.native.Backend == agentproto.BackendCodex || l.native.Backend == agentproto.BackendOpenCode) && proof.Kind == agentproto.EventNativeTurnBindingObserved && proof.Status == "accepted" || l.native.Backend == agentproto.BackendClaude && proof.Kind == agentproto.EventTurnStarted && proof.Metadata["turnIdSource"] == "synthetic_from_observed_user_uuid" && proof.Metadata["nativeUserUUID"] == proof.RequestID && proof.TurnID == "relay-claude-turn-"+proof.RequestID
		if proof.Kind == agentproto.EventNativeControlResult && proof.Action == "review.start" && proof.Status == "accepted" && proof.Metadata["reviewThreadID"] == event.ThreadID {
			valid = true
		}
		if valid {
			proofs = append(proofs, proof)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	var binding *agentproto.NativeTurnBinding
	for _, proof := range proofs {
		ready, e := j.nativeCommandProjectionReady(ctx, l, proof.CommandID)
		if e != nil {
			return nil, e
		}
		if !ready {
			continue
		}
		if binding != nil && *binding != *proof.NativeBinding {
			return nil, ErrJournalConflict
		}
		copy := *proof.NativeBinding
		binding = &copy
	}
	if binding == nil {
		return nil, ErrJournalState
	}
	return binding, nil
}

// isPrivateReviewTurn recognizes only the durable exact native review RPC
// result, never a thread prefix or current surface selection.
func (j *Journal) isPrivateReviewTurn(ctx context.Context, l *NativeLease, event agentproto.Event) (bool, error) {
	if !isNativeTurnProjection(event.Kind) || event.NativeBinding == nil || event.ThreadID == "" || event.TurnID == "" {
		return false, nil
	}
	var n int
	err := j.db.QueryRowContext(ctx, `SELECT count(*) FROM native_events e JOIN native_commands c ON c.command_id=e.command_id JOIN dispatch d ON d.id=c.dispatch_id WHERE e.incarnation=? AND d.state='accepted' AND d.event_sequence=e.sequence AND json_extract(c.payload,'$.kind')=? AND json_extract(c.payload,'$.nativeBinding.executionId')=? AND json_extract(e.payload,'$.action')='review.start' AND json_extract(e.payload,'$.status')='accepted' AND json_extract(e.payload,'$.metadata.reviewThreadID')=? AND json_extract(e.payload,'$.turnId')=?`, l.native.CarrierIncarnation, string(agentproto.CommandReviewStart), event.NativeBinding.ExecutionID, event.ThreadID, event.TurnID).Scan(&n)
	if err != nil {
		return false, err
	}
	if n > 1 {
		return false, ErrJournalConflict
	}
	return n == 1, nil
}

// ApplyStoppedNativeEvents only projects committed events under exact retired
// carrier authority. It does not expose a lease capable of sending commands.
func (p *Processor) ApplyStoppedNativeEvents(ctx context.Context, authority NativeLeaseAuthority, native agentproto.NativeOwner, surface Owner, generation int64) error {
	lease, err := p.journal.openStoppedNativeProjection(ctx, authority, native, surface, generation)
	if err != nil {
		return err
	}
	for {
		events, err := lease.PendingBusinessNativeEvents(ctx, 0, 128)
		if err != nil {
			return err
		}
		for _, event := range events {
			if _, err = p.ApplyNativeForStop(ctx, lease, event.Ref, CoreNativeReducer{}); err != nil {
				return err
			}
		}
		if len(events) < 128 {
			return nil
		}
	}
}

// MarkNativeStopping serializes the exact carrier's offline projection with
// native reduction before the host sends any stop commands.
func (p *Processor) MarkNativeStopping(ctx context.Context, l *NativeLease) error {
	if l == nil || l.journal != p.journal {
		return ErrJournalState
	}
	unlock := p.journal.LockReferenceMutation()
	defer unlock()
	p.mu.Lock()
	defer p.mu.Unlock()
	return l.with(ctx, func() error {
		ctx, projectionErr := beginProjection(ctx)
		if projectionErr != nil {
			return projectionErr
		}

		if p.poisoned {
			return ErrProcessorPoisoned
		}
		if _, err := p.ensureOwner(ctx, l.surface, l.generation); err != nil {
			return err
		}
		surface := p.app.Service().Surface(l.surface.SurfaceID)
		if surface == nil || surface.AttachedInstanceID != l.native.InstanceID {
			return nil // An unattached carrier cannot receive this surface's queued work.
		}
		if p.app.instances[l.native.InstanceID] == "" {
			if err := p.app.Service().MaterializePrivateStoppedInstance(l.surface.SurfaceID, l.native); err != nil {
				return err
			}
			p.app.instances[l.native.InstanceID] = l.surface.SurfaceID
		}
		if p.app.instances[l.native.InstanceID] != l.surface.SurfaceID {
			return ErrJournalConflict
		}
		return p.app.Service().MarkPrivateStopProjectionOffline(l.surface.SurfaceID, l.native.InstanceID)
	})
}
