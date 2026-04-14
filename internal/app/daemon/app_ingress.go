package daemon

import (
	"context"
	"errors"
	"fmt"
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
	a.traceUserTextAction(action)
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
	events := a.applyIngressActionLocked(action)
	appendEvents := events
	inlineResult := a.inlineCardActionResultLocked(action, events)
	commandSubmissionAnchorReplace := false
	if inlineResult == nil {
		inlineResult, appendEvents = a.bareCommandContinuationResultLocked(action, events)
	}
	if inlineResult == nil {
		inlineResult = a.commandSubmissionAnchorResultLocked(action)
		commandSubmissionAnchorReplace = inlineResult != nil
	}
	inlineNavigationReplace := inlineResult != nil && control.AllowsInlineCardReplacement(action)
	if !inlineNavigationReplace {
		a.handleUIEvents(ctx, appendEvents)
	}
	if commandSubmissionAnchorReplace {
		a.scheduleCommandSubmissionAnchorRecall(action)
	}
	var clearTargets map[string]bool
	if a.shouldClearSurfaceResumeTargetLocked(action, before) {
		clearTargets = map[string]bool{strings.TrimSpace(action.SurfaceSessionID): true}
	}
	a.syncSurfaceResumeStateLocked(clearTargets)
	a.syncWorkspaceSurfaceContextFilesLocked()
	if action.Kind == control.ActionModeCommand {
		after := a.service.SurfaceSnapshot(action.SurfaceSessionID)
		switchedIntoVSCode := after != nil &&
			state.NormalizeProductMode(state.ProductMode(after.ProductMode)) == state.ProductModeVSCode &&
			(before == nil || state.NormalizeProductMode(state.ProductMode(before.ProductMode)) != state.ProductModeVSCode)
		if !switchedIntoVSCode {
			return inlineResult
		}
		a.invalidateVSCodeCompatibilityCacheLocked()
		promptEvents, _ := a.maybePromptVSCodeCompatibilityLocked(action.SurfaceSessionID)
		if inlineResult == nil {
			a.handleUIEvents(ctx, promptEvents)
		}
	}
	return inlineResult
}

func (a *App) applyIngressActionLocked(action control.Action) []control.UIEvent {
	return a.service.ApplySurfaceAction(action)
}

func (a *App) inlineCardActionResultLocked(action control.Action, events []control.UIEvent) *feishu.ActionResult {
	if !control.AllowsInlineCardReplacement(action) || len(events) != 1 {
		return nil
	}
	event := events[0]
	if !event.InlineReplaceCurrentCard || event.Command != nil || event.DaemonCommand != nil {
		return nil
	}
	event.DaemonLifecycleID = a.daemonLifecycleID
	ops := a.projector.Project(a.service.SurfaceChatID(event.SurfaceSessionID), event)
	if len(ops) != 1 || ops[0].Kind != feishu.OperationSendCard {
		return nil
	}
	return &feishu.ActionResult{ReplaceCurrentCard: &ops[0]}
}

func (a *App) commandSubmissionAnchorResultLocked(action control.Action) *feishu.ActionResult {
	if !control.AllowsCommandSubmissionAnchorReplacement(action) {
		return nil
	}
	commandText := commandSubmissionAnchorCommandText(action)
	if commandText == "" {
		return nil
	}
	catalog := control.FeishuDirectCommandCatalog{
		Title:        "命令已提交",
		Summary:      fmt.Sprintf("已执行 `%s`，结果会显示在下方。", commandText),
		Interactive:  true,
		DisplayStyle: control.CommandCatalogDisplayCompactButtons,
		RelatedButtons: []control.CommandCatalogButton{{
			Label:       "重新打开菜单",
			Kind:        control.CommandCatalogButtonRunCommand,
			CommandText: "/menu",
		}},
	}
	event := control.UIEvent{
		Kind:                       control.UIEventFeishuDirectCommandCatalog,
		GatewayID:                  action.GatewayID,
		SurfaceSessionID:           action.SurfaceSessionID,
		DaemonLifecycleID:          a.daemonLifecycleID,
		FeishuDirectCommandCatalog: &catalog,
	}
	ops := a.projector.Project(a.service.SurfaceChatID(action.SurfaceSessionID), event)
	if len(ops) != 1 || ops[0].Kind != feishu.OperationSendCard {
		return nil
	}
	return &feishu.ActionResult{ReplaceCurrentCard: &ops[0]}
}

func (a *App) bareCommandContinuationResultLocked(action control.Action, events []control.UIEvent) (*feishu.ActionResult, []control.UIEvent) {
	if !allowsBareCommandContinuation(action) || len(events) != 1 || events[0].DaemonCommand == nil {
		return nil, events
	}
	daemonCommand := *events[0].DaemonCommand
	if !daemonCommandMatchesBareContinuation(action, daemonCommand) {
		return nil, events
	}
	followup := a.handleDaemonCommandLocked(daemonCommand)
	if len(followup) == 0 {
		return nil, nil
	}
	replace := a.projectFirstCardAsReplacementLocked(action, followup[0])
	if replace == nil {
		return nil, followup
	}
	if len(followup) == 1 {
		return replace, nil
	}
	return replace, followup[1:]
}

func allowsBareCommandContinuation(action control.Action) bool {
	if action.Inbound == nil || strings.TrimSpace(action.Inbound.CardDaemonLifecycleID) == "" {
		return false
	}
	fields := strings.Fields(strings.TrimSpace(action.Text))
	if len(fields) != 1 {
		return false
	}
	switch action.Kind {
	case control.ActionUpgradeCommand, control.ActionDebugCommand:
		return true
	default:
		return false
	}
}

func daemonCommandMatchesBareContinuation(action control.Action, command control.DaemonCommand) bool {
	switch action.Kind {
	case control.ActionUpgradeCommand:
		return command.Kind == control.DaemonCommandUpgrade
	case control.ActionDebugCommand:
		return command.Kind == control.DaemonCommandDebug
	default:
		return false
	}
}

func (a *App) projectFirstCardAsReplacementLocked(action control.Action, event control.UIEvent) *feishu.ActionResult {
	if event.Command != nil || event.DaemonCommand != nil {
		return nil
	}
	if strings.TrimSpace(event.GatewayID) == "" {
		event.GatewayID = action.GatewayID
	}
	if strings.TrimSpace(event.SurfaceSessionID) == "" {
		event.SurfaceSessionID = action.SurfaceSessionID
	}
	event.DaemonLifecycleID = a.daemonLifecycleID
	ops := a.projector.Project(a.service.SurfaceChatID(event.SurfaceSessionID), event)
	if len(ops) != 1 || ops[0].Kind != feishu.OperationSendCard {
		return nil
	}
	return &feishu.ActionResult{ReplaceCurrentCard: &ops[0]}
}

func commandSubmissionAnchorCommandText(action control.Action) string {
	switch action.Kind {
	case control.ActionListInstances:
		return "/list"
	case control.ActionShowThreads:
		return "/use"
	case control.ActionShowAllThreads:
		return "/useall"
	case control.ActionStatus:
		return "/status"
	case control.ActionStop:
		return "/stop"
	case control.ActionNewThread:
		return "/new"
	case control.ActionFollowLocal:
		return "/follow"
	case control.ActionDetach:
		return "/detach"
	case control.ActionUpgradeCommand, control.ActionDebugCommand:
		fields := strings.Fields(strings.TrimSpace(action.Text))
		if len(fields) == 1 {
			return fields[0]
		}
		return ""
	default:
		return ""
	}
}

func (a *App) scheduleCommandSubmissionAnchorRecall(action control.Action) {
	messageID := strings.TrimSpace(action.MessageID)
	if messageID == "" || a.commandAnchorRecallDelay < 0 {
		return
	}
	delay := a.commandAnchorRecallDelay
	surfaceID := strings.TrimSpace(action.SurfaceSessionID)
	gatewayID := strings.TrimSpace(action.GatewayID)
	go func() {
		if delay > 0 {
			time.Sleep(delay)
		}
		a.mu.Lock()
		shuttingDown := a.shuttingDown
		a.mu.Unlock()
		if shuttingDown {
			return
		}
		ctx, cancel := a.newTimeoutContext(context.Background(), a.gatewayApplyTimeout)
		defer cancel()
		err := a.gateway.Apply(ctx, []feishu.Operation{{
			Kind:             feishu.OperationDeleteMessage,
			GatewayID:        gatewayID,
			SurfaceSessionID: surfaceID,
			MessageID:        messageID,
		}})
		if err != nil {
			log.Printf("command submission anchor recall skipped: surface=%s message=%s err=%v", surfaceID, messageID, err)
		}
	}()
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
	if inst.Source == "vscode" {
		a.invalidateVSCodeCompatibilityCacheLocked()
	}

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
	a.syncWorkspaceSurfaceContextFilesLocked()
}

func (a *App) onEvents(ctx context.Context, instanceID string, events []agentproto.Event) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.shuttingDown {
		return
	}
	syncSurfaceResumeState := false
	for _, event := range events {
		now := time.Now().UTC()
		if historyEvents, handled := a.handleThreadHistoryEventLocked(instanceID, event); handled {
			a.handleUIEvents(ctx, historyEvents)
			continue
		}
		if event.Kind == agentproto.EventTurnCompleted {
			a.traceTurnLifecycle(instanceID, event)
		}
		if shouldLogAgentEvent(event) {
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
		}
		uiEvents := a.service.ApplyAgentEvent(instanceID, event)
		a.logThreadRefreshCommand(instanceID, event, uiEvents)
		if event.Kind == agentproto.EventTurnStarted {
			a.traceTurnLifecycle(instanceID, event)
		}
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
		if eventAffectsSurfaceResumeState(event) {
			syncSurfaceResumeState = true
		}
	}
	if syncSurfaceResumeState {
		a.syncSurfaceResumeStateForInstanceLocked(instanceID, nil)
	}
}

func (a *App) logThreadRefreshCommand(instanceID string, event agentproto.Event, uiEvents []control.UIEvent) {
	inst := a.service.Instance(instanceID)
	activeTurnID := ""
	if inst != nil {
		activeTurnID = inst.ActiveTurnID
	}
	var refreshSurfaceIDs []string
	for _, uiEvent := range uiEvents {
		if uiEvent.Command != nil && uiEvent.Command.Kind == agentproto.CommandThreadsRefresh {
			refreshSurfaceIDs = append(refreshSurfaceIDs, uiEvent.SurfaceSessionID)
		}
	}
	if len(refreshSurfaceIDs) == 0 {
		if event.Kind == agentproto.EventThreadDiscovered && event.FocusSource == "remote_created_thread" {
			log.Printf(
				"thread discovered from remote create: instance=%s thread=%s pending=%s active=%s activeTurn=%s",
				instanceID,
				event.ThreadID,
				summarizeRemoteStatuses(a.service.PendingRemoteTurns()),
				summarizeRemoteStatuses(a.service.ActiveRemoteTurns()),
				activeTurnID,
			)
		}
		return
	}
	log.Printf(
		"auto thread refresh queued: instance=%s causeEvent=%s thread=%s focusSource=%s surfaces=%s pending=%s active=%s activeTurn=%s",
		instanceID,
		event.Kind,
		event.ThreadID,
		event.FocusSource,
		strings.Join(refreshSurfaceIDs, ","),
		summarizeRemoteStatuses(a.service.PendingRemoteTurns()),
		summarizeRemoteStatuses(a.service.ActiveRemoteTurns()),
		activeTurnID,
	)
}

func shouldLogAgentEvent(event agentproto.Event) bool {
	switch event.Kind {
	case agentproto.EventItemDelta:
		return false
	default:
		return true
	}
}

func eventAffectsSurfaceResumeState(event agentproto.Event) bool {
	switch event.Kind {
	case agentproto.EventThreadsSnapshot,
		agentproto.EventThreadDiscovered,
		agentproto.EventThreadFocused,
		agentproto.EventItemCompleted,
		agentproto.EventTurnCompleted:
		return true
	default:
		return false
	}
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
	if historyEvents, handled := a.handleThreadHistoryCommandAckLocked(instanceID, ack); handled {
		a.handleUIEvents(ctx, historyEvents)
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
	if inst.Source == "vscode" {
		a.invalidateVSCodeCompatibilityCacheLocked()
	}
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
	a.syncWorkspaceSurfaceContextFilesLocked()
}

// onTick runs on the daemon's 100ms heartbeat.
// Only maintenance that cannot be tied to a specific ingress event belongs
// here, and anything non-trivial must already have its own interval/backoff.
func (a *App) onTick(ctx context.Context, now time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.shuttingDown {
		return
	}
	uiEvents := a.service.Tick(now)
	uiEvents = append(uiEvents, a.maybeFlushUpgradeResultLocked(now)...)
	a.recordHeadlessRestoreOutcomeEventsLocked(uiEvents, now)
	a.handleUIEvents(ctx, uiEvents)
	a.syncManagedHeadlessLocked(now)
	a.maybeRefreshIdleManagedHeadlessLocked(now)
	a.reapIdleHeadless(now)
	a.syncManagedHeadlessLocked(now)
	a.ensureMinIdleManagedHeadlessLocked(now)
	vscodeBlocked := false
	if a.vscodeStartupCheckDue || a.vscodeCompatibility.Checked {
		a.vscodeStartupCheckDue = false
		vscodePromptEvents, blocked := a.maybePromptVSCodeCompatibilityLocked("")
		vscodeBlocked = blocked
		a.handleUIEvents(ctx, vscodePromptEvents)
	}
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
	a.syncFeishuTimeSensitiveLocked(ctx)
	a.maybeStartFeishuPermissionRefreshLocked(now)
	a.maybeShutdownExternalAccessIdleLocked(now)
}
