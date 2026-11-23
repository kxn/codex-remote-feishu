package privatechat

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

var ErrProcessorPoisoned = errors.New("private processor requires restart and recovery")
var ErrRecoveryHeld = errors.New("private input remains pending during recovery")

// ErrDeferred leaves an admitted input pending when a required host port is not ready.
// Reducers may return it only before changing any application state.
var ErrDeferred = errors.New("private input requires an unavailable host effect")

// IngressDecoder is trusted local parsing of already authenticated, durable SDK
// facts. It must not perform I/O, send an effect, or mutate application state.
type IngressDecoder interface {
	Decode(JournalEntry) (control.Action, error)
}

// Reducer owns legacy daemon pre-processing omitted by the bare core. It must
// mutate only the supplied private app, perform no I/O and return every effect.
// The daemon must supply its real pre-processing before this can serve traffic.
type Reducer interface {
	Reduce(*App, control.Action) ([]eventcontract.Event, error)
}
type ObservationReducer interface {
	ReduceObservation(*App, control.Action) ([]eventcontract.Event, error)
}

// CardPlanner is a pure host projection. The returned response is committed
// with the applied input before it can be returned to the platform.
type CardPlanner interface {
	PlanCard(*App, control.Action, []eventcontract.Event) (*CardResponse, []eventcontract.Event, error)
}
type CoreReducer struct{}

func (CoreReducer) Reduce(a *App, action control.Action) ([]eventcontract.Event, error) {
	return a.Action(action)
}
func (CoreReducer) ReduceObservation(a *App, action control.Action) ([]eventcontract.Event, error) {
	return a.Observe(action)
}

type processorOwner struct{ revision, cursor, generation int64 }
type Processor struct {
	mu       sync.Mutex
	app      *App
	journal  *Journal
	decoder  IngressDecoder
	reducer  Reducer
	owners   map[Owner]processorOwner
	poisoned bool
}

// NewProcessor takes exclusive ownership of a fresh app. No host code may call
// its Service or App concurrently or use a second processor with the same app.
func NewProcessor(app *App, j *Journal, decoder IngressDecoder, reducer Reducer) (*Processor, error) {
	if app == nil || j == nil || decoder == nil || reducer == nil {
		return nil, ErrJournalState
	}
	app.processorMu.Lock()
	defer app.processorMu.Unlock()
	if app.processorOwned || len(app.owners) != 0 {
		return nil, ErrJournalState
	}
	j.processorMu.Lock()
	defer j.processorMu.Unlock()
	if j.processor != nil {
		j.processor.mu.Lock()
		poisoned := j.processor.poisoned
		j.processor.mu.Unlock()
		if !poisoned {
			return nil, ErrJournalState
		}
	}
	watermarks, err := j.privateWatermarks(context.Background())
	if err != nil {
		return nil, err
	}
	if err = app.service.SeedPrivateIDWatermarks(watermarks); err != nil {
		return nil, err
	}
	app.processorOwned = true
	p := &Processor{app: app, journal: j, decoder: decoder, reducer: reducer, owners: make(map[Owner]processorOwner)}
	j.processor = p
	return p, nil
}

// ApplyIngress applies one already admitted event. Only committed effect records
// are returned; this function never executes transport or external side effects.
// A failed post-reduction commit poisons the complete app, not only one owner.
func (p *Processor) ApplyIngress(ctx context.Context, o Owner, generation, seq int64) ([]EffectRecord, error) {
	unlockReference := p.journal.LockReferenceMutation()
	defer unlockReference()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.poisoned {
		return nil, ErrProcessorPoisoned
	}
	if !validOwner(o) || generation < 1 || seq < 1 {
		return nil, ErrJournalState
	}
	if current, ok := p.owners[o]; ok && current.generation != generation {
		return nil, ErrJournalConflict
	}
	entry, applied, revision, err := p.journal.processorIngress(ctx, o, generation, seq)
	if err != nil {
		return nil, err
	}
	if applied {
		return p.journal.processorEffects(ctx, o, generation, revision)
	}
	state, err := p.ensureOwner(ctx, o, generation)
	if err != nil {
		return nil, err
	}

	// The same surface cannot silently switch epochs inside a live app.
	if entry.Ingress.Generation != generation {
		return nil, ErrJournalConflict
	}
	action, err := p.decoder.Decode(entry)
	if err != nil {
		return nil, err
	}
	observation := action.Kind == control.ActionMessageRecalled || action.Kind == control.ActionReactionCreated
	if observation {
		if err = p.app.ValidateObservation(action); err != nil {
			return nil, err
		}
		var count int
		err = p.journal.db.QueryRowContext(ctx, `SELECT count(*) FROM ingress i, json_each(i.sources) src WHERE i.tenant=? AND i.app_id=? AND i.gateway=? AND i.surface=? AND i.chat=? AND i.user_id=? AND i.generation=? AND i.seq<? AND src.value=?`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation, seq, action.TargetMessageID).Scan(&count)
		if err != nil {
			return nil, err
		}
		if count == 0 {
			return nil, ErrInvalidOwner
		}
	} else if err = p.app.ValidateAction(action); err != nil {
		return nil, err
	}
	if entry.Ingress.Lane != "input" && (action.Kind == control.ActionTextMessage || action.Kind == control.ActionImageMessage || action.Kind == control.ActionFileMessage || action.Kind == control.ActionAsyncUserInput) {
		return nil, ErrJournalState
	}
	alreadyHistorical, err := p.journal.historicalInputAlreadyRecovered(ctx, o, generation, action)
	if err != nil {
		return nil, err
	}
	if !p.app.Service().PrivateRecoveryActionAllowed(action) && !isHistoricalInputReadAction(action) && !alreadyHistorical {
		return nil, ErrRecoveryHeld
	}
	if (entry.Ingress.Lane == "input" && !isHistoricalInputReadAction(action) && !alreadyHistorical) || observation {
		var pending int
		err = p.journal.db.QueryRowContext(ctx, `SELECT count(*) FROM private_effect e LEFT JOIN private_effect_applied a ON a.effect_id=e.id WHERE e.tenant=? AND e.app_id=? AND e.gateway=? AND e.surface=? AND e.chat=? AND e.user_id=? AND e.generation=? AND json_extract(e.body,'$.event.DaemonCommand.Kind')=? AND (e.state<>'confirmed' OR a.effect_id IS NULL)`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation, control.DaemonCommandPrivateStageInput).Scan(&pending)
		if err != nil {
			return nil, err
		}
		if pending != 0 {
			return nil, ErrDeferred
		}
	}
	if action.Kind == control.ActionWorkspaceNewWorktree || strings.HasPrefix(string(action.Kind), "surface.target_picker.") {
		var pending int
		err = p.journal.db.QueryRowContext(ctx, `SELECT count(*) FROM private_effect e LEFT JOIN private_effect_applied a ON a.effect_id=e.id WHERE e.tenant=? AND e.app_id=? AND e.gateway=? AND e.surface=? AND e.chat=? AND e.user_id=? AND e.generation=? AND json_extract(e.body,'$.event.DaemonCommand.Kind')=? AND (e.state<>'confirmed' OR a.effect_id IS NULL)`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation, control.DaemonCommandPrivateWorktreeObserve).Scan(&pending)
		if err != nil {
			return nil, err
		}
		if pending != 0 {
			return nil, ErrDeferred
		}
	}
	if !safeDuringBotSettings(action) {
		var reconcile int
		err = p.journal.db.QueryRowContext(ctx, `SELECT count(*) FROM private_settings_reconcile WHERE tenant=? AND app_id=? AND gateway=? AND surface=? AND chat=? AND user_id=? AND generation=? AND state='pending'`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation).Scan(&reconcile)
		if err != nil {
			return nil, err
		}
		if reconcile != 0 {
			return nil, ErrDeferred
		}
		var pending int
		err = p.journal.db.QueryRowContext(ctx, `SELECT count(*) FROM private_effect e LEFT JOIN private_effect_applied a ON a.effect_id=e.id WHERE e.tenant=? AND e.app_id=? AND e.gateway=? AND json_extract(e.body,'$.event.DaemonCommand.Kind')=? AND (e.state<>'confirmed' OR a.effect_id IS NULL)`, o.TenantID, o.AppID, o.GatewayID, control.DaemonCommandPrivateBotSettings).Scan(&pending)
		if err != nil {
			return nil, err
		}
		if pending != 0 {
			return nil, ErrDeferred
		}
	}
	wasHistoricalConfirmation := action.Kind == control.ActionTextMessage && strings.HasPrefix(strings.TrimSpace(action.Text), "/recover confirm ")
	action, historical, err := p.prepareHistoricalInput(ctx, o, generation, action)
	if err != nil {
		return nil, err
	}
	var events []eventcontract.Event
	if wasHistoricalConfirmation && historical == nil {
		text := "请先发送 /new 准备新会话，再查看历史输入并确认恢复。"
		if action.Text == "/recover already" {
			text = "这条历史输入已经确认恢复，不会重复加入。"
		}
		if action.Text == "/recover unavailable" {
			text = "这条输入的附件暂时无法取回，尚未加入会话。"
		}
		events = []eventcontract.Event{{Kind: eventcontract.KindNotice, GatewayID: o.GatewayID, SurfaceSessionID: o.SurfaceID, Notice: &control.Notice{Code: "private_history_recovery", Title: "历史输入", Text: text}}}
	} else if observation {
		r, ok := p.reducer.(ObservationReducer)
		if !ok {
			return nil, ErrDeferred
		}
		events, err = r.ReduceObservation(p.app, action)
	} else {
		events, err = p.reducer.Reduce(p.app, action)
	}
	if err != nil {
		if errors.Is(err, ErrDeferred) {
			return nil, err
		}
		p.poisoned = true
		return nil, fmt.Errorf("%w: reducer: %v", ErrProcessorPoisoned, err)
	}
	for _, event := range events {
		if event.Notice != nil && event.Notice.Code == "private_recovery_hold" {
			return nil, ErrRecoveryHeld
		}
	}
	var card *CardResponse
	if planner, ok := p.reducer.(CardPlanner); ok {
		card, events, err = planner.PlanCard(p.app, action, events)
		if err != nil {
			p.poisoned = true
			return nil, fmt.Errorf("%w: card plan: %v", ErrProcessorPoisoned, err)
		}
	}
	effects, err := p.freezeEffects(o, fmt.Sprintf("ingress:%d", seq), events)
	if err != nil {
		p.poisoned = true
		return nil, fmt.Errorf("%w: freeze: %v", ErrProcessorPoisoned, err)
	}
	core, err := p.app.Service().ExportPrivateCheckpoint(o.SurfaceID)
	if err != nil {
		p.poisoned = true
		return nil, fmt.Errorf("%w: snapshot: %v", ErrProcessorPoisoned, err)
	}
	if historical != nil {
		accepted := false
		for _, item := range core.QueueItems {
			if item != nil && item.SourceMessageID == action.MessageID && ((historical.Session == nil && item.FrozenDispatchPlan.ExecutionMode == agentproto.PromptExecutionModeStartNew && item.FrozenDispatchPlan.ExecutionThreadID == "") || (historical.Session != nil && item.FrozenDispatchPlan.ExecutionMode == agentproto.PromptExecutionModeResumeExisting && item.FrozenDispatchPlan.ExecutionThreadID == historical.Session.SessionID)) {
				accepted = true
				break
			}
		}
		if !accepted {
			for _, event := range events {
				if event.DaemonCommand != nil && event.DaemonCommand.Kind == control.DaemonCommandPrivateStageInput {
					accepted = true
					break
				}
			}
		}
		if !accepted {
			historical = nil
		}
	}
	cursor := state.cursor
	if seq > cursor {
		cursor = seq
	}
	checkpoint := Checkpoint{Owner: o, Generation: generation, Revision: state.revision + 1, Cursor: cursor, Core: core}
	var migration []*migrationResumeRef
	if historical != nil {
		migration = append(migration, &migrationResumeRef{Historical: historical})
	}
	if err = p.journal.commitCheckpointExtended(ctx, checkpoint, state.revision, effects, seq, nil, card, nil, nil, nil, nil, nil, migration...); err != nil {
		p.poisoned = true
		return nil, fmt.Errorf("%w: commit: %v", ErrProcessorPoisoned, err)
	}
	p.owners[o] = processorOwner{checkpoint.Revision, checkpoint.Cursor, generation}
	return p.journal.processorEffects(ctx, o, generation, checkpoint.Revision)
}

func (p *Processor) ensureOwner(ctx context.Context, o Owner, generation int64) (processorOwner, error) {
	state, ok := p.owners[o]
	if ok {
		if state.generation != generation {
			return state, ErrJournalConflict
		}
		return state, nil
	}
	if err := p.app.RegisterOwner(o); err != nil {
		return state, err
	}
	checkpoint, err := p.journal.LoadCheckpoint(ctx, o, generation)
	if err == nil {
		if err = p.app.Service().RestorePrivateCheckpoint(checkpoint.Core); err != nil {
			p.poisoned = true
			return state, err
		}
		state = processorOwner{checkpoint.Revision, checkpoint.Cursor, generation}
	} else if !errors.Is(err, sql.ErrNoRows) {
		p.poisoned = true
		return state, err
	}
	state.generation = generation
	p.owners[o] = state
	p.app.ownerGenerations[o.SurfaceID] = generation
	return state, nil
}
func (p *Processor) freezeEffects(o Owner, key string, events []eventcontract.Event) ([]Effect, error) {
	effects := make([]Effect, 0, len(events))
	for i, event := range events {
		event = event.Normalized()
		if event.PendingInput != nil && event.PendingInput.SourceMessageID == "" {
			return nil, fmt.Errorf("pending input lacks source message identity")
		}
		digest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%d", o.TenantID, o.AppID, key, i)))
		effectID := "private-effect:" + hex.EncodeToString(digest[:])
		if event.Command != nil {
			if event.SurfaceSessionID != o.SurfaceID {
				p.poisoned = true
				return nil, fmt.Errorf("command surface mismatch")
			}
			if event.Command.CommandID == "" {
				event.Command.CommandID = "private-command:" + hex.EncodeToString(digest[:])
			}
			switch event.Command.Kind {
			case agentproto.CommandReviewStart:
				p.app.Service().BindPendingReviewStartCommand(o.SurfaceID, event.Command.CommandID)
			case agentproto.CommandThreadShellCommand:
				p.app.Service().BindPendingShellCommand(o.SurfaceID, event.Command.CommandID)
			case agentproto.CommandPromptSend:
				p.app.Service().BindPrivatePromptCommand(o.SurfaceID, event.Command.CommandID)
			case agentproto.CommandTurnSteer:
				p.app.Service().BindPrivateSteerCommand(o.SurfaceID, event.Command.CommandID)
			case agentproto.CommandThreadCompactStart:
				p.app.Service().BindPrivateCompactCommand(o.SurfaceID, event.Command.CommandID)
			}
			// Normalized payload may have been copied before assigning the ID.
			// Rebuild it so both representations carry the same command.
			event.Payload = nil
			event = event.Normalized()
		}
		var inputs *orchestrator.FrozenCommandInputs
		if event.Command != nil {
			var e error
			inputs, e = p.app.Service().FrozenCommandInputs(o.SurfaceID, event.Command.CommandID)
			if e != nil {
				return nil, e
			}
			if (event.Command.Kind == agentproto.CommandPromptSend || event.Command.Kind == agentproto.CommandTurnSteer || event.Command.Kind == agentproto.CommandThreadShellCommand) && inputs == nil {
				return nil, fmt.Errorf("native input command lacks frozen selection")
			}
		}
		var target *string
		if event.Command != nil {
			instance := p.app.Service().AttachedInstanceID(o.SurfaceID)
			target = &instance
		}
		body, e := encodeEffectTarget(event, inputs, target)
		if e != nil {
			p.poisoned = true
			return nil, e
		}
		lane := "interactive"
		if event.Command != nil || event.DaemonCommand != nil {
			lane = "background"
		}
		effects = append(effects, Effect{ID: effectID, Lane: lane, Body: body, DisplayKey: explicitCardDisplayKey(o, event)})
	}
	return effects, nil
}
