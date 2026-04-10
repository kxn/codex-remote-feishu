package daemon

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/adapter/relayws"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (a *App) startIngressPump(parent context.Context, errCh chan<- error) {
	a.ingressRunMu.Lock()
	defer a.ingressRunMu.Unlock()
	if a.ingressCancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(parent)
	a.ingressCancel = cancel
	a.ingressStarted = true
	a.ingressWG.Add(1)
	go func() {
		defer a.ingressWG.Done()
		if err := a.ingress.Run(ctx, a.processIngressWork); err != nil && err != context.Canceled {
			if errCh == nil {
				log.Printf("daemon ingress pump failed: %v", err)
				return
			}
			select {
			case errCh <- err:
			default:
				log.Printf("daemon ingress pump failed after shutdown: %v", err)
			}
		}
	}()
}

func (a *App) stopIngressPump() {
	a.ingressRunMu.Lock()
	cancel := a.ingressCancel
	started := a.ingressStarted
	a.ingressCancel = nil
	a.ingressStarted = false
	a.ingressRunMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if a.ingress != nil {
		a.ingress.Close()
	}
	if started {
		a.ingress.Wait()
	}
}

func (a *App) enqueueHello(_ context.Context, meta relayws.ConnectionMeta, hello agentproto.Hello) {
	a.rememberRelayConnectionWithPID(hello.Instance.InstanceID, meta.ConnectionID, hello.Instance.PID)
	item := ingressWorkItem{
		instanceID:   hello.Instance.InstanceID,
		connectionID: meta.ConnectionID,
		kind:         ingressWorkHello,
		hello:        &hello,
	}
	if err := a.ingress.Enqueue(item); err != nil && !errors.Is(err, errIngressPumpClosed) {
		log.Printf("daemon ingress enqueue hello failed: instance=%s err=%v", hello.Instance.InstanceID, err)
	}
}

func (a *App) enqueueEvents(_ context.Context, meta relayws.ConnectionMeta, instanceID string, events []agentproto.Event) {
	item := ingressWorkItem{
		instanceID:   instanceID,
		connectionID: meta.ConnectionID,
		kind:         ingressWorkEvents,
		events:       append([]agentproto.Event(nil), events...),
	}
	if err := a.ingress.Enqueue(item); errors.Is(err, errIngressQueueFull) {
		go a.handleIngressOverload(instanceID, meta.ConnectionID)
		return
	} else if err != nil && !errors.Is(err, errIngressPumpClosed) {
		log.Printf("daemon ingress enqueue events failed: instance=%s err=%v", instanceID, err)
	}
}

func (a *App) enqueueCommandAck(_ context.Context, meta relayws.ConnectionMeta, instanceID string, ack agentproto.CommandAck) {
	item := ingressWorkItem{
		instanceID:   instanceID,
		connectionID: meta.ConnectionID,
		kind:         ingressWorkCommandAck,
		ack:          &ack,
	}
	if err := a.ingress.Enqueue(item); errors.Is(err, errIngressQueueFull) {
		go a.handleIngressOverload(instanceID, meta.ConnectionID)
		return
	} else if err != nil && !errors.Is(err, errIngressPumpClosed) {
		log.Printf("daemon ingress enqueue command ack failed: instance=%s command=%s err=%v", instanceID, ack.CommandID, err)
	}
}

func (a *App) enqueueDisconnect(_ context.Context, meta relayws.ConnectionMeta, instanceID string) {
	item := ingressWorkItem{
		instanceID:   instanceID,
		connectionID: meta.ConnectionID,
		kind:         ingressWorkDisconnect,
	}
	if err := a.ingress.Enqueue(item); err != nil && !errors.Is(err, errIngressPumpClosed) {
		log.Printf("daemon ingress enqueue disconnect failed: instance=%s err=%v", instanceID, err)
	}
}

func (a *App) processIngressWork(item ingressWorkItem) {
	switch item.kind {
	case ingressWorkHello:
		if item.hello != nil && a.currentRelayConnection(item.instanceID) == item.connectionID {
			a.onHello(context.Background(), *item.hello)
			return
		}
	case ingressWorkEvents:
		if len(item.events) != 0 && a.currentRelayConnection(item.instanceID) == item.connectionID {
			a.onEvents(context.Background(), item.instanceID, item.events)
			return
		}
	case ingressWorkCommandAck:
		if item.ack != nil && a.currentRelayConnection(item.instanceID) == item.connectionID {
			a.onCommandAck(context.Background(), item.instanceID, *item.ack)
			return
		}
	case ingressWorkDisconnect:
		current, degraded := a.markRelayConnectionDropped(item.instanceID, item.connectionID)
		if degraded {
			a.debugf("suppress disconnect after transport degraded: instance=%s connection=%d", item.instanceID, item.connectionID)
			return
		}
		if current {
			a.onDisconnect(context.Background(), item.instanceID)
			return
		}
	}
	stats := a.ingress.MarkStaleDrop(item.instanceID)
	a.debugf(
		"drop stale ingress item: instance=%s connection=%d current=%d kind=%s depth=%d peak=%d stale=%d",
		item.instanceID,
		item.connectionID,
		a.currentRelayConnection(item.instanceID),
		item.kind,
		stats.CurrentDepth,
		stats.PeakDepth,
		stats.StaleDropCount,
	)
}

func (a *App) handleIngressOverload(instanceID string, connectionID uint64) {
	apply, emitNotice := a.beginRelayTransportDegrade(instanceID, connectionID, time.Now())
	if !apply {
		return
	}
	stats := a.ingress.Stats(instanceID)
	log.Printf(
		"daemon ingress overload: instance=%s connection=%d depth=%d peak=%d overloads=%d",
		instanceID,
		connectionID,
		stats.CurrentDepth,
		stats.PeakDepth,
		stats.OverloadCount,
	)
	closed := a.relay.CloseConnection(instanceID, connectionID)
	a.mu.Lock()
	defer a.mu.Unlock()
	a.handleUIEvents(context.Background(), a.service.ApplyInstanceTransportDegraded(instanceID, emitNotice))
	if !closed {
		a.debugf("transport degraded connection already replaced: instance=%s connection=%d", instanceID, connectionID)
	}
}

func (a *App) HandleAction(ctx context.Context, action control.Action) {
	_ = a.handleAction(ctx, action)
}

func (a *App) HandleGatewayAction(ctx context.Context, action control.Action) *feishu.ActionResult {
	return a.handleAction(ctx, action)
}

func (a *App) handleAction(ctx context.Context, action control.Action) *feishu.ActionResult {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.shuttingDown {
		log.Printf(
			"surface action ignored during shutdown: surface=%s chat=%s actor=%s kind=%s message=%s",
			action.SurfaceSessionID,
			action.ChatID,
			action.ActorUserID,
			action.Kind,
			action.MessageID,
		)
		return nil
	}
	action = a.classifyInboundAction(action)
	before := a.service.SurfaceSnapshot(action.SurfaceSessionID)
	log.Printf(
		"surface action: surface=%s chat=%s actor=%s kind=%s message=%s instance=%s thread=%s verdict=%s reason=%s event=%s request=%s message_time=%s menu_time=%s card_lifecycle=%s text=%q",
		action.SurfaceSessionID,
		action.ChatID,
		action.ActorUserID,
		action.Kind,
		action.MessageID,
		action.InstanceID,
		action.ThreadID,
		inboundVerdict(action),
		inboundReason(action),
		strings.TrimSpace(action.Inbound.EventID),
		strings.TrimSpace(action.Inbound.RequestID),
		inboundTimeValue(action.Inbound.MessageCreateTime),
		inboundTimeValue(action.Inbound.MenuClickTime),
		strings.TrimSpace(action.Inbound.CardDaemonLifecycleID),
		actionTextPreview(action.Text),
	)
	if notice := rejectedInboundNotice(action); notice != nil {
		a.ensureSurfaceRouteForNotice(action)
		a.handleUIEvents(ctx, []control.UIEvent{{
			Kind:             control.UIEventNotice,
			GatewayID:        action.GatewayID,
			SurfaceSessionID: action.SurfaceSessionID,
			Notice:           notice,
		}})
		a.syncSurfaceResumeStateLocked(nil)
		return nil
	}
	events := a.service.ApplySurfaceAction(action)
	inlineResult := a.inlineCardActionResultLocked(action, events)
	if inlineResult == nil {
		a.handleUIEvents(ctx, events)
	}
	a.syncHeadlessRestoreHintAfterActionLocked(action, before)
	var clearTargets map[string]bool
	if a.shouldClearSurfaceResumeTargetLocked(action, before) {
		clearTargets = map[string]bool{strings.TrimSpace(action.SurfaceSessionID): true}
	}
	a.syncSurfaceResumeStateLocked(clearTargets)
	if action.Kind == control.ActionModeCommand {
		after := a.service.SurfaceSnapshot(action.SurfaceSessionID)
		switchedIntoVSCode := after != nil &&
			state.NormalizeProductMode(state.ProductMode(after.ProductMode)) == state.ProductModeVSCode &&
			(before == nil || state.NormalizeProductMode(state.ProductMode(before.ProductMode)) != state.ProductModeVSCode)
		if !switchedIntoVSCode {
			return inlineResult
		}
		promptEvents, _ := a.maybePromptVSCodeCompatibilityLocked(action.SurfaceSessionID)
		if inlineResult == nil {
			a.handleUIEvents(ctx, promptEvents)
		}
	}
	return inlineResult
}

func (a *App) inlineCardActionResultLocked(action control.Action, events []control.UIEvent) *feishu.ActionResult {
	if !shouldInlineReplaceCurrentCard(action) || len(events) != 1 {
		return nil
	}
	event := events[0]
	if event.Command != nil || event.DaemonCommand != nil {
		return nil
	}
	switch event.Kind {
	case control.UIEventCommandCatalog, control.UIEventSelectionPrompt:
	default:
		return nil
	}
	event.DaemonLifecycleID = a.daemonLifecycleID
	ops := a.projector.Project(a.service.SurfaceChatID(event.SurfaceSessionID), event)
	if len(ops) != 1 || ops[0].Kind != feishu.OperationSendCard {
		return nil
	}
	return &feishu.ActionResult{ReplaceCurrentCard: &ops[0]}
}

func shouldInlineReplaceCurrentCard(action control.Action) bool {
	// Current Feishu cards still use the legacy message-card envelope on send.
	// The synchronous callback replacement path is being rejected by Feishu at
	// runtime (observed as code 200672), so keep card clicks on the append-only
	// path until these cards are migrated end-to-end to the newer card schema.
	return false
}

func (a *App) ensureSurfaceRouteForNotice(action control.Action) {
	if strings.TrimSpace(action.SurfaceSessionID) == "" {
		return
	}
	_ = a.service.ApplySurfaceAction(control.Action{
		Kind:             control.ActionStatus,
		GatewayID:        action.GatewayID,
		SurfaceSessionID: action.SurfaceSessionID,
		ChatID:           action.ChatID,
		ActorUserID:      action.ActorUserID,
	})
}

func (a *App) Service() *orchestrator.Service {
	return a.service
}

func (a *App) onHello(ctx context.Context, hello agentproto.Hello) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.shuttingDown {
		return
	}
	now := time.Now().UTC()

	inst := a.service.Instance(hello.Instance.InstanceID)
	if inst == nil {
		inst = &state.InstanceRecord{
			InstanceID: hello.Instance.InstanceID,
			Threads:    map[string]*state.ThreadRecord{},
		}
	}
	inst.DisplayName = hello.Instance.DisplayName
	inst.WorkspaceRoot = state.NormalizeWorkspaceKey(hello.Instance.WorkspaceRoot)
	inst.WorkspaceKey = state.ResolveWorkspaceKey(hello.Instance.WorkspaceKey, inst.WorkspaceRoot)
	inst.ShortName = strings.TrimSpace(hello.Instance.ShortName)
	if inst.ShortName == "" {
		inst.ShortName = state.WorkspaceShortName(inst.WorkspaceKey)
	}
	inst.Source = firstNonEmpty(strings.TrimSpace(hello.Instance.Source), "vscode")
	inst.Managed = hello.Instance.Managed
	inst.PID = hello.Instance.PID
	inst.Online = true
	a.service.UpsertInstance(inst)
	a.observeManagedHeadless(inst)
	log.Printf(
		"relay instance connected: id=%s workspace=%s display=%s source=%s managed=%t pid=%d",
		inst.InstanceID,
		inst.WorkspaceKey,
		inst.DisplayName,
		inst.Source,
		inst.Managed,
		inst.PID,
	)
	connectEvents := a.service.ApplyInstanceConnected(inst.InstanceID)
	a.recordHeadlessRestoreOutcomeEventsLocked(connectEvents, now)
	a.handleUIEvents(ctx, connectEvents)

	command := agentproto.Command{
		CommandID: a.nextCommandID(),
		Kind:      agentproto.CommandThreadsRefresh,
	}
	refreshSent := false
	if err := a.sendAgentCommand(hello.Instance.InstanceID, command); err != nil {
		log.Printf("relay send command failed: instance=%s kind=%s err=%v", hello.Instance.InstanceID, command.Kind, err)
		if managed := a.managedHeadless[hello.Instance.InstanceID]; managed != nil {
			managed.LastError = "daemon 无法向本地 wrapper 发送初始化 threads.refresh。"
		}
		a.handleUIEvents(ctx, a.service.HandleProblem(hello.Instance.InstanceID, agentproto.ErrorInfoFromError(err, agentproto.ErrorInfo{
			Code:      "relay_send_command_failed",
			Layer:     "daemon",
			Stage:     "send_threads_refresh",
			Operation: string(command.Kind),
			Message:   "daemon 无法向本地 wrapper 发送初始化命令。",
			CommandID: command.CommandID,
			Retryable: true,
		})))
	} else {
		refreshSent = true
		if a.managedHeadless[hello.Instance.InstanceID] != nil {
			a.markManagedThreadsRefreshRequestedLocked(hello.Instance.InstanceID, command.CommandID, now)
		}
	}
	if refreshSent {
		a.markStartupThreadsRefreshRequestedLocked(hello.Instance.InstanceID)
	}
	a.refreshHeadlessRestoreHintsLocked()
	a.syncHeadlessRestoreStateLocked()
	vscodePromptEvents, vscodeBlocked := a.maybePromptVSCodeCompatibilityLocked("")
	a.handleUIEvents(ctx, vscodePromptEvents)
	vscodeRecoveryEvents := []control.UIEvent{}
	if !vscodeBlocked {
		vscodeRecoveryEvents = a.maybeRecoverVSCodeSurfacesLocked(now)
		vscodeRecoveryEvents = append(vscodeRecoveryEvents, a.maybePromptDetachedVSCodeSurfacesLocked()...)
	}
	a.handleUIEvents(ctx, vscodeRecoveryEvents)
	normalRecoveryEvents := a.maybeRecoverNormalSurfacesLocked(now)
	a.handleUIEvents(ctx, normalRecoveryEvents)
	recoveryEvents := a.maybeRecoverHeadlessSurfacesLocked(now)
	a.recordHeadlessRestoreOutcomeEventsLocked(recoveryEvents, now)
	a.handleUIEvents(ctx, recoveryEvents)
	a.maybeShutdownExternalAccessIdleLocked(now)
	a.syncSurfaceResumeStateLocked(nil)
}

func (a *App) onEvents(ctx context.Context, instanceID string, events []agentproto.Event) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.shuttingDown {
		return
	}
	for _, event := range events {
		now := time.Now().UTC()
		log.Printf(
			"agent event: instance=%s kind=%s thread=%s turn=%s item=%s initiator=%s traffic=%s status=%s",
			instanceID,
			event.Kind,
			event.ThreadID,
			event.TurnID,
			event.ItemID,
			event.Initiator.Kind,
			event.TrafficClass,
			event.Status,
		)
		uiEvents := a.service.ApplyAgentEvent(instanceID, event)
		if event.Kind == agentproto.EventThreadsSnapshot {
			a.markStartupThreadsRefreshSettledLocked(instanceID)
			a.noteManagedThreadsSnapshotLocked(instanceID, now)
			a.syncManagedHeadlessLocked(now)
		}
		switch event.Kind {
		case agentproto.EventThreadsSnapshot, agentproto.EventThreadDiscovered, agentproto.EventThreadFocused:
			vscodePromptEvents, vscodeBlocked := a.maybePromptVSCodeCompatibilityLocked("")
			uiEvents = append(uiEvents, vscodePromptEvents...)
			if !vscodeBlocked {
				uiEvents = append(uiEvents, a.maybeRecoverVSCodeSurfacesLocked(now)...)
				uiEvents = append(uiEvents, a.maybePromptDetachedVSCodeSurfacesLocked()...)
			}
			uiEvents = append(uiEvents, a.maybeRecoverNormalSurfacesLocked(now)...)
			uiEvents = append(uiEvents, a.maybeRecoverHeadlessSurfacesLocked(now)...)
		}
		a.recordHeadlessRestoreOutcomeEventsLocked(uiEvents, now)
		a.handleUIEvents(ctx, uiEvents)
	}
	a.refreshHeadlessRestoreHintsLocked()
	a.syncHeadlessRestoreStateLocked()
	a.syncSurfaceResumeStateLocked(nil)
}

func (a *App) onCommandAck(ctx context.Context, instanceID string, ack agentproto.CommandAck) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.shuttingDown {
		return
	}
	log.Printf("relay command ack: instance=%s command=%s accepted=%t error=%s", instanceID, ack.CommandID, ack.Accepted, ack.Error)
	a.debugf(
		"command ack state: instance=%s command=%s accepted=%t pending=%s active=%s",
		instanceID,
		ack.CommandID,
		ack.Accepted,
		summarizeRemoteStatuses(a.service.PendingRemoteTurns()),
		summarizeRemoteStatuses(a.service.ActiveRemoteTurns()),
	)
	if a.noteManagedRefreshAckLocked(instanceID, ack) {
		return
	}
	if ack.Accepted {
		a.handleUIEvents(ctx, a.service.HandleCommandAccepted(instanceID, ack))
		return
	}
	a.handleUIEvents(ctx, a.service.HandleCommandRejected(instanceID, ack))
}

func (a *App) onDisconnect(ctx context.Context, instanceID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.shuttingDown {
		return
	}
	now := time.Now().UTC()
	a.markStartupThreadsRefreshSettledLocked(instanceID)
	inst := a.service.Instance(instanceID)
	if inst == nil {
		a.noteManagedHeadlessDisconnectedLocked(instanceID)
		return
	}
	uiEvents := a.service.ApplyInstanceDisconnected(instanceID)
	a.noteManagedHeadlessDisconnectedLocked(instanceID)
	log.Printf(
		"relay instance disconnected: id=%s workspace=%s display=%s source=%s managed=%t pid=%d",
		inst.InstanceID,
		inst.WorkspaceKey,
		inst.DisplayName,
		inst.Source,
		inst.Managed,
		inst.PID,
	)
	a.handleUIEvents(ctx, uiEvents)
	vscodePromptEvents, vscodeBlocked := a.maybePromptVSCodeCompatibilityLocked("")
	a.handleUIEvents(ctx, vscodePromptEvents)
	vscodeRecoveryEvents := []control.UIEvent{}
	if !vscodeBlocked {
		vscodeRecoveryEvents = a.maybeRecoverVSCodeSurfacesLocked(now)
		vscodeRecoveryEvents = append(vscodeRecoveryEvents, a.maybePromptDetachedVSCodeSurfacesLocked()...)
	}
	a.handleUIEvents(ctx, vscodeRecoveryEvents)
	normalRecoveryEvents := a.maybeRecoverNormalSurfacesLocked(now)
	a.handleUIEvents(ctx, normalRecoveryEvents)
	a.syncSurfaceResumeStateLocked(nil)
}

func (a *App) onTick(ctx context.Context, now time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.shuttingDown {
		return
	}
	uiEvents := a.service.Tick(now)
	uiEvents = append(uiEvents, a.maybeFlushUpgradeResultLocked(now)...)
	uiEvents = append(uiEvents, a.maybePromptPendingUpgradeLocked(now)...)
	a.recordHeadlessRestoreOutcomeEventsLocked(uiEvents, now)
	a.handleUIEvents(ctx, uiEvents)
	a.syncManagedHeadlessLocked(now)
	a.maybeRefreshIdleManagedHeadlessLocked(now)
	a.reapIdleHeadless(now)
	a.syncManagedHeadlessLocked(now)
	a.ensureMinIdleManagedHeadlessLocked(now)
	a.maybeStartAutoUpgradeCheckLocked(now)
	vscodePromptEvents, vscodeBlocked := a.maybePromptVSCodeCompatibilityLocked("")
	a.handleUIEvents(ctx, vscodePromptEvents)
	vscodeRecoveryEvents := []control.UIEvent{}
	if !vscodeBlocked {
		vscodeRecoveryEvents = a.maybeRecoverVSCodeSurfacesLocked(now)
		vscodeRecoveryEvents = append(vscodeRecoveryEvents, a.maybePromptDetachedVSCodeSurfacesLocked()...)
	}
	a.handleUIEvents(ctx, vscodeRecoveryEvents)
	normalRecoveryEvents := a.maybeRecoverNormalSurfacesLocked(now)
	a.handleUIEvents(ctx, normalRecoveryEvents)
	recoveryEvents := a.maybeRecoverHeadlessSurfacesLocked(now)
	a.recordHeadlessRestoreOutcomeEventsLocked(recoveryEvents, now)
	a.handleUIEvents(ctx, recoveryEvents)
	a.maybeShutdownExternalAccessIdleLocked(now)
	a.syncSurfaceResumeStateLocked(nil)
}
