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
		return p.registerNativeHelloLocked(ctx, l, hello)
	})
}

func (p *Processor) registerNativeHelloLocked(ctx context.Context, l *NativeLease, hello agentproto.Hello) error {
	if _, err := p.ensureOwner(ctx, l.surface, l.generation); err != nil {
		return err
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
func (p *Processor) ApplyNative(ctx context.Context, l *NativeLease, ref NativeEventRef, reducer NativeReducer) (out []EffectRecord, err error) {
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
			if event.Kind == agentproto.EventNativeCommandRejected {
				var status string
				if e = p.journal.db.QueryRowContext(ctx, `SELECT d.state FROM native_commands n JOIN dispatch d ON d.id=n.dispatch_id WHERE n.command_id=? AND d.carrier=?`, event.CommandID, l.native.CarrierIncarnation).Scan(&status); e != nil {
					return e
				}
				if status != "failed" {
					return ErrJournalConflict
				}
			}
		}
		current, e := p.ensureOwner(ctx, l.surface, l.generation)
		if e != nil {
			return e
		}
		if p.app.instances[l.native.InstanceID] != l.surface.SurfaceID || p.app.Service().Instance(l.native.InstanceID) == nil || !p.app.Service().Instance(l.native.InstanceID).Online {
			return fmt.Errorf("authenticated native hello required: %w", ErrJournalState)
		}
		if isNativeTurnProjection(event.Kind) {
			command, e := p.journal.nativeProjectionCommand(ctx, l, event)
			if e != nil {
				return e
			}
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
		events, e := reducer.ReduceNative(p.app, l.native.InstanceID, event)
		if e != nil {
			p.poisoned = true
			return fmt.Errorf("%w: native reducer: %v", ErrProcessorPoisoned, e)
		}
		if event.Kind == agentproto.EventTurnCompleted {
			follow, e := p.app.Service().ResolvePrivateOfflineAfterTerminal(l.surface.SurfaceID)
			if e != nil {
				p.poisoned = true
				return e
			}
			events = append(events, follow...)
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
	return status == "accepted" || status == "failed", nil
}

// nativeBindingFromProof associates an early unbound notification only after a
// journaled native receipt proves the exact same session and turn. It does not
// infer ownership from arrival time, a current focus, or the newest command.
func (j *Journal) nativeBindingFromProof(ctx context.Context, l *NativeLease, event agentproto.Event) (*agentproto.NativeTurnBinding, error) {
	if event.ThreadID == "" || event.TurnID == "" {
		return nil, ErrJournalState
	}
	rows, err := j.db.QueryContext(ctx, `SELECT payload FROM native_events WHERE incarnation=? AND json_extract(payload,'$.threadId')=? AND json_extract(payload,'$.turnId')=? AND json_extract(payload,'$.commandId') IS NOT NULL`, l.native.CarrierIncarnation, event.ThreadID, event.TurnID)
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
