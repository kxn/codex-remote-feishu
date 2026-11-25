package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/relayws"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

type privateWorkRunner struct {
	host          *App
	journal       *privatechat.Journal
	processor     *privatechat.Processor
	publisher     *privatePublisher
	native        *privateNativeHost
	ingress       *applicationIngress
	wake          chan struct{}
	deadlineMu    sync.Mutex
	exitProbeAt   map[string]time.Time
	effectMu      sync.Mutex
	effectWG      sync.WaitGroup
	running       map[string]struct{}
	returnedLocal map[string]privatechat.PendingEffectWork
	normalSlots   chan struct{}
	controlSlots  chan struct{}
	publishSlots  chan struct{}

	effectCursors [privateEffectPoolCount]privatechat.HostEffectPosition
	diagnostics   privateRunnerDiagnostics
}

func (r *privateWorkRunner) signal() {
	if r != nil && r.wake != nil {
		select {
		case r.wake <- struct{}{}:
		default:
		}
	}
}

// Run reconciles durable inputs, native projections and effects after restart.
// A single runner owns these lanes; deferred/unknown work is never resent.
func (r *privateWorkRunner) Run(ctx context.Context) error {
	if r == nil || r.host == nil || r.journal == nil || r.processor == nil || r.publisher == nil || r.native == nil || r.ingress == nil || r.wake == nil {
		return fmt.Errorf("private work runner unavailable")
	}
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	r.effectWG.Add(1)
	go func() {
		defer r.effectWG.Done()
		timer := time.NewTicker(5 * time.Second)
		defer timer.Stop()
		for {
			if ctx.Err() != nil {
				return
			}
			_ = runPrivateStagingCleanup(ctx, r.host, r.journal)
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
			}
		}
	}()
	r.signal()
	for {
		select {
		case <-ctx.Done():
			r.effectWG.Wait()
			return ctx.Err()
		case <-tick.C:
		case <-r.wake:
		}
		// Stop admission at the next scheduling boundary, but let an already
		// running local projection finish its checkpoint transaction. Cancelling
		// it after the core mutation would poison the shared Processor.
		err := r.passUntilStopped(context.WithoutCancel(ctx), ctx)
		if err != nil && ctx.Err() != nil {
			r.effectWG.Wait()
			return ctx.Err()
		}
		r.diagnostics.report("pass", "", err)
	}
}
func (r *privateWorkRunner) pass(ctx context.Context) error {
	return r.passUntilStopped(ctx, ctx)
}

func (r *privateWorkRunner) passUntilStopped(ctx, stop context.Context) error {
	for offset := 0; ; offset += 128 {
		inputs, err := r.journal.PendingIngressWorkPage(ctx, 128, offset)
		if err != nil {
			return err
		}
		for _, item := range inputs {
			if stop.Err() != nil {
				return stop.Err()
			}
			if err = r.host.refreshPrivateBotSettings(ctx, r.processor, item.Owner, item.Generation); err != nil {
				continue
			}
			pending, e := r.journal.PendingSettingsReconciliations(ctx, item.Owner, item.Generation, 1)
			if e != nil {
				continue
			}
			lease, _ := r.native.settingsLease(ctx, item.Owner, item.Generation)
			e = reconcilePrivateBotSettings(ctx, r.host, r.processor, r.journal, lease, item.Owner, item.Generation)
			if e != nil && len(pending) > 0 && item.Lane == "input" {
				continue
			}
			entry, readErr := r.journal.ReadIngressAction(ctx, item.Owner, item.Generation, item.Sequence)
			if readErr != nil {
				continue
			}
			needHistory, historyErr := shouldPreparePrivateHistory(ctx, r.processor, item.Owner, item.Generation, entry.Ingress.Payload)
			if historyErr != nil {
				continue
			}
			if needHistory {
				scanSeq := int64(0)
				if historyAliasNeedsRefresh(entry.Ingress.Payload) {
					scanSeq = item.Sequence
				}
				if err = r.ingress.preparePrivateHistory(ctx, r.native, r.processor, item.Owner, item.Generation, scanSeq); err != nil {
					continue
				}
			}
			if item.Lane == "input" {
				if stop.Err() != nil {
					return stop.Err()
				}
				recovering, recoveryErr := r.prepareRetiredRuntime(ctx, item.Owner, item.Generation)
				if recovering && recoveryErr != nil {
					continue
				}
			}
			if stop.Err() != nil {
				return stop.Err()
			}
			if _, err = r.processor.ApplyIngress(ctx, item.Owner, item.Generation, item.Sequence); err != nil {
				continue
			}
			if paths, e := r.processor.CatalogSnapshot(); e == nil {
				r.host.SetPrivateCatalogSnapshot(paths)
			}
		}
		if len(inputs) < 128 {
			break
		}
	}
	if stop.Err() != nil {
		return stop.Err()
	}
	if err := r.runOwnerDeadlines(ctx, stop); err != nil {
		return err
	}

	retired, err := r.native.store.byState(ctx, "retired")
	if err != nil {
		return err
	}
	for _, rec := range retired {
		if stop.Err() != nil {
			return stop.Err()
		}
		if err := r.native.projectRetiredCarrierEvents(ctx, rec); err != nil {
			log.Printf("private retired projection owner=%s: %v", rec.Surface.SurfaceID, err)
		}
	}

	for _, state := range []string{"running", "draining", "sealed"} {
		records, e := r.native.store.byState(ctx, state)
		if e != nil {
			return e
		}
		for _, rec := range records {
			if stop.Err() != nil {
				return stop.Err()
			}
			if state == "sealed" && rec.StopEffectID != "" {
				if r.native.retire(ctx, rec.Snapshot.Owner.InstanceID) == nil {
					continue
				}
			}
			if state == "draining" && rec.StopEffectID != "" {
				r.native.mu.Lock()
				connected := r.native.connections[rec.Snapshot.Owner.InstanceID] != 0
				r.native.mu.Unlock()
				if connected {
					if r.native.advanceCarrierStop(ctx, rec, rec.StopEffectID) == nil {
						continue
					}
				} else if r.native.recoverPhysicalExitedCarrier(ctx, rec) == nil {
					continue
				}
			}
			if state == "running" {
				r.observeExitedPendingCarrier(ctx, rec)
			}
			lease, e := r.native.lease(ctx, rec, nil)
			if e != nil {
				continue
			}
			if e = r.native.updateThreadCarrier(ctx, r.processor, rec); e != nil {
				continue
			}
			pending, e := lease.PendingNativeEvents(ctx, 0, 128)
			if e != nil {
				continue
			}
			for _, item := range pending {
				if stop.Err() != nil {
					return stop.Err()
				}
				if _, e = r.processor.ApplyNative(ctx, lease, item.Ref, privatechat.CoreNativeReducer{}); e != nil {
					break
				}
			}
			if state == "running" {
				if stop.Err() != nil {
					return stop.Err()
				}
				r.tickCarrier(ctx, rec, lease)
			}
		}
	}
	return r.scheduleHostEffects(ctx, stop)
}

func (r *privateWorkRunner) tickCarrier(ctx context.Context, rec privateCarrierRecord, lease *privatechat.NativeLease) {
	if err := r.host.refreshPrivateBotSettings(ctx, r.processor, rec.Surface, rec.SurfaceGeneration); err != nil {
		return
	}
	_, err := r.journal.PendingSettingsReconciliations(ctx, rec.Surface, rec.SurfaceGeneration, 1)
	if err != nil {
		return
	}
	settingsLease, e := r.native.settingsLease(ctx, rec.Surface, rec.SurfaceGeneration)
	if e != nil {
		return
	}
	if err = reconcilePrivateBotSettings(ctx, r.host, r.processor, r.journal, settingsLease, rec.Surface, rec.SurfaceGeneration); err != nil {
		return
	}
	r.native.mu.Lock()
	connection := r.native.connections[rec.Snapshot.Owner.InstanceID]
	r.native.mu.Unlock()
	if connection == 0 {
		return
	}
	now := time.Now().UTC().Truncate(time.Second)
	_ = r.native.relay.WithCurrentConnection(rec.Snapshot.Owner.InstanceID, connection, func() error {
		if !r.native.validConnection(rec.Snapshot.Owner.InstanceID, relayws.ConnectionMeta{ConnectionID: connection, PrivateAuthenticated: true}) {
			return privatechat.ErrDeferred
		}
		due, err := r.processor.PrivateTickDue(ctx, lease, now)
		if err != nil || !due {
			return err
		}
		_, err = r.processor.ApplyPrivateTick(ctx, lease, fmt.Sprintf("tick:%d", now.Unix()), now)
		return err
	})
}

func (r *privateWorkRunner) runEffect(ctx context.Context, work privatechat.PendingEffectWork) error {
	e := work.Effect
	if e.Effect.Lane == "interactive" {
		_, err := r.publisher.Publish(ctx, work.Owner, work.Generation, e.Effect.ID)
		return err
	}
	event, _, err := privatechat.DecodeStoredEffect(e.Effect.Body)
	if err != nil {
		return err
	}
	if event.DaemonCommand != nil {
		switch event.DaemonCommand.Kind {
		case privatePreviewRetryKind:
			_, err = runPrivatePreviewRetry(ctx, r.host, r.processor, r.journal, work.Owner, work.Generation, e.Effect.ID)
			return err
		case control.DaemonCommandPrivateProjectQuery, control.DaemonCommandPrivateProjectShare:
			_, err = runPrivateProjectEffect(ctx, r.host, r.processor, r.journal, work.Owner, work.Generation, e.Effect.ID)
			return err
		case control.DaemonCommandPrivateMkdir:
			_, err = runPrivateMkdir(ctx, r.host, r.processor, r.journal, work.Owner, work.Generation, e.Effect.ID)
			return err
		case control.DaemonCommandPrivateBotSettings:
			_, err = runPrivateBotSettings(ctx, r.host, r.processor, r.journal, work.Owner, work.Generation, e.Effect.ID)
			return err
		case control.DaemonCommandPrivateModelCatalogQuery:
			return runPrivateModelCatalogQuery(ctx, r.host, r.processor, r.journal, work.Owner, work.Generation, e.Effect.ID)
		case control.DaemonCommandPrivateClaudeWorkspace:
			return runPrivateClaudeWorkspace(ctx, r.host, r.processor, r.journal, work.Owner, work.Generation, e.Effect.ID)
		case control.DaemonCommandPrivateStageInput:
			_, err = runPrivateStageInput(ctx, r.host, r.processor, r.journal, work.Owner, work.Generation, e.Effect.ID)
			return err
		case control.DaemonCommandPrivateWorktreeObserve:
			_, err = runPrivateWorktreeObservation(ctx, r.host, r.processor, r.journal, work.Owner, work.Generation, e.Effect.ID)
			return err
		case control.DaemonCommandGitWorkspaceImport, control.DaemonCommandGitWorkspaceWorktreeCreate:
			_, err = runPrivateGitWorkspace(ctx, r.host, r.processor, r.journal, work.Owner, work.Generation, e.Effect.ID)
			return err
		case control.DaemonCommandGitWorkspaceImportCancel, control.DaemonCommandGitWorkspaceWorktreeCancel:
			_, err = runPrivateGitCancel(ctx, r.host, r.processor, r.journal, work.Owner, work.Generation, e.Effect.ID)
			return err
		case control.DaemonCommandAdmin, control.DaemonCommandDebug:
			_, err = runPrivateSystemCommand(ctx, r.host, r.processor, r.journal, work.Owner, work.Generation, e.Effect.ID)
			return err
		case control.DaemonCommandUpgrade, control.DaemonCommandUpgradeOwnerFlow:
			_, err = runPrivateUpgradeCommand(ctx, r.host, r.processor, r.journal, work.Owner, work.Generation, e.Effect.ID)
			return err
		case control.DaemonCommandSendIMFile, control.DaemonCommandPrivateSendFile:
			_, err = runPrivateSendFile(ctx, r.host, r.processor, r.journal, work.Owner, work.Generation, e.Effect.ID)
			return err
		case control.DaemonCommandStartHeadless:
			_, err = runPrivateHeadlessStart(ctx, r.host, r.native, r.ingress, r.processor, r.journal, work.Owner, work.Generation, e.Effect.ID)
			return err
		case control.DaemonCommandKillHeadless:
			return r.runHeadlessStop(ctx, work)
		}
	}
	if event.Command != nil {
		return r.runNativeEffect(ctx, work)
	}
	return privatechat.ErrDeferred
}
func (r *privateWorkRunner) runNativeEffect(ctx context.Context, work privatechat.PendingEffectWork) error {
	if work.Effect.State == "confirmed" {
		var observation privateReadExpiryReceipt
		if json.Unmarshal(work.Effect.Receipt, &observation) == nil && observation.Kind == "native_read_observation_expired" {
			_, err := r.processor.ApplyLocalHostReceipt(ctx, work.Owner, work.Generation, work.Effect.Effect.ID, privateReadExpiryReducer{})
			return err
		}
		var receipt privatechat.KnownUnsentReceipt
		if json.Unmarshal(work.Effect.Receipt, &receipt) != nil || receipt.Version != 1 || receipt.EffectID != work.Effect.Effect.ID || receipt.InstanceID == "" {
			return privatechat.ErrJournalConflict
		}
		rec, err := r.native.store.lookup(ctx, receipt.InstanceID)
		if err != nil {
			return err
		}
		if rec.Surface != work.Owner || rec.SurfaceGeneration != work.Generation {
			return privatechat.ErrJournalConflict
		}
		if receipt.Reason == "shutdown_prepared_unstarted" {
			_, err = r.processor.ApplyLocalHostReceipt(ctx, work.Owner, work.Generation, work.Effect.Effect.ID, privatechat.ShutdownKnownUnsentReducer{})
		} else {
			_, err = r.processor.ApplyLocalHostReceipt(ctx, work.Owner, work.Generation, work.Effect.Effect.ID, privateKnownUnsentReducer{host: r.host})
		}
		return err
	}
	_, inputs, target, err := privatechat.DecodeStoredEffectTarget(work.Effect.Effect.Body)
	if err != nil {
		return err
	}
	if target == nil {
		return privatechat.ErrDeferred
	}
	if *target == "" {
		return r.failNoInstance(ctx, work)
	}
	already, err := r.journal.HasNativeDispatch(ctx, work.Effect.Effect.ID)
	if err != nil {
		return err
	}
	rec, err := r.native.store.lookup(ctx, *target)
	if err != nil {
		return err
	}
	if rec.Surface != work.Owner || rec.SurfaceGeneration != work.Generation {
		return privatechat.ErrJournalConflict
	}
	event, _, _, err := privatechat.DecodeStoredEffectTarget(work.Effect.Effect.Body)
	if err != nil {
		return err
	}
	command := event.Command
	if command == nil {
		return privatechat.ErrJournalConflict
	}
	if already {
		state, e := r.journal.DispatchState(ctx, work.Effect.Effect.ID+":native")
		if e != nil {
			return e
		}
		if state != "prepared" {
			return r.expireNativeReadObservation(ctx, work, rec, *command)
		}
	}
	if rec.State != "running" {
		return privatechat.ErrJournalConflict
	}
	historicalDenied := false
	if privateEffectTargetsSession(command.Kind) && command.Target.ThreadID != "" {
		internal, e := r.journal.IsInternalNativeSession(ctx, string(rec.Snapshot.Owner.Backend), rec.Snapshot.Owner.HistoryNamespace, command.Target.ThreadID)
		if e != nil {
			return e
		}
		if internal {
			// An exact managed helper/review identity is never a user-selected
			// session. Settle this command as locally known unsent rather than
			// leaving its prepared effect retrying indefinitely.
			historicalDenied = true
		}
		if !internal {
			known, _, e := r.native.registry.SessionAccess(ctx, string(rec.Snapshot.Owner.Backend), rec.Snapshot.Owner.HistoryNamespace, command.Target.ThreadID, privateRegistration(rec.Snapshot.Owner).Owner)
			if e != nil {
				return e
			}
			if !known {
				restricted, e := teamHistoricalSession(ctx, r.native.teamHistory, rec.Snapshot.Owner.Backend, rec.Snapshot.Owner.HistoryNamespace, command.Target.ThreadID)
				if e != nil {
					return e
				}
				if restricted {
					allowed, aliasErr := r.ingress.legacyHistoryAllowed(ctx, rec.Surface, rec.SurfaceGeneration, rec.Snapshot.Owner.Backend, rec.Snapshot.Owner.HistoryNamespace, command.Target.ThreadID, true)
					if aliasErr != nil {
						return aliasErr
					}
					if !allowed {
						historicalDenied = true
					}
				}
			}
		}
	}
	binding := agentproto.NativeTurnBinding{Version: agentproto.PrivateNativeProtocolVersion, Owner: rec.Snapshot.Owner}
	switch command.Kind {
	case agentproto.CommandPromptSend, agentproto.CommandThreadShellCommand, agentproto.CommandReviewStart, agentproto.CommandThreadCompactStart:
		sum := sha256.Sum256([]byte(work.Effect.Effect.ID))
		binding.ExecutionID = "execution_" + hex.EncodeToString(sum[:20])
	case agentproto.CommandTurnSteer, agentproto.CommandTurnInterrupt, agentproto.CommandRequestRespond:
		lease, e := r.native.lease(ctx, rec, nil)
		if e != nil {
			return e
		}
		thread, turn := command.Target.ThreadID, command.Target.TurnID
		if inputs != nil {
			if thread == "" {
				thread = inputs.ThreadID
			}
			if turn == "" {
				turn = inputs.TurnID
			}
		}
		known, e := lease.BindingForAcceptedTurn(ctx, thread, turn)
		if e != nil {
			return e
		}
		if known == nil {
			return privatechat.ErrDeferred
		}
		binding = *known
	}
	r.native.mu.Lock()
	connection := r.native.connections[*target]
	r.native.mu.Unlock()
	if connection == 0 {
		return privatechat.ErrDeferred
	}
	session := ""
	if privateEffectTargetsSession(command.Kind) {
		session = command.Target.ThreadID
	}
	return r.native.sendEffect(ctx, relayws.ConnectionMeta{ConnectionID: connection, PrivateAuthenticated: true}, *target, work.Effect.Effect.ID, binding, session, command.Kind, historicalDenied)
}

func privateEffectTargetsSession(kind agentproto.CommandKind) bool {
	return privateSessionCommandScope(kind) != privateSessionNone
}

type privateSessionScope uint8

const (
	privateSessionNone privateSessionScope = iota
	privateSessionRead
	privateSessionWrite
	privateSessionControl
)

func privateSessionCommandScope(kind agentproto.CommandKind) privateSessionScope {
	if agentproto.PrivateSessionReadCommand(kind) {
		return privateSessionRead
	}
	switch kind {
	case agentproto.CommandPromptSend, agentproto.CommandTurnSteer, agentproto.CommandThreadShellCommand, agentproto.CommandReviewStart, agentproto.CommandThreadCompactStart, agentproto.CommandThreadGoalSet, agentproto.CommandThreadGoalClear:
		return privateSessionWrite
	case agentproto.CommandTurnInterrupt, agentproto.CommandRequestRespond:
		return privateSessionControl
	}
	return privateSessionNone
}
