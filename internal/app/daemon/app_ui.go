package daemon

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/render"
)

func (a *App) handleUIEvents(ctx context.Context, events []control.UIEvent) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.handleUIEventsLocked(ctx, events)
}

func (a *App) handleUIEventsLocked(ctx context.Context, events []control.UIEvent) {
	_ = ctx
	for _, event := range events {
		if event.DaemonCommand != nil {
			a.mu.Unlock()
			followup := a.handleDaemonCommand(*event.DaemonCommand)
			a.mu.Lock()
			a.handleUIEventsLocked(context.Background(), followup)
			continue
		}
		if event.Command != nil {
			if event.Command.CommandID == "" {
				event.Command.CommandID = a.nextCommandID()
			}
			instanceID := a.service.AttachedInstanceID(event.SurfaceSessionID)
			snapshot := a.service.SurfaceSnapshot(event.SurfaceSessionID)
			a.debugf(
				"dispatch prepare: surface=%s instance=%s command=%s kind=%s selectedThread=%s route=%s promptThread=%s promptCreate=%t pending=%s active=%s sourceMessage=%s",
				event.SurfaceSessionID,
				instanceID,
				event.Command.CommandID,
				event.Command.Kind,
				snapshotSelectedThreadID(snapshot),
				snapshotRouteMode(snapshot),
				snapshotPromptThreadID(snapshot),
				snapshotPromptCreateThread(snapshot),
				summarizeRemoteStatuses(a.service.PendingRemoteTurns()),
				summarizeRemoteStatuses(a.service.ActiveRemoteTurns()),
				event.Command.Origin.MessageID,
			)
			a.service.BindPendingRemoteCommand(event.SurfaceSessionID, event.Command.CommandID)
			a.debugf(
				"dispatch bound: surface=%s instance=%s command=%s pending=%s",
				event.SurfaceSessionID,
				instanceID,
				event.Command.CommandID,
				summarizeRemoteStatuses(a.service.PendingRemoteTurns()),
			)
			log.Printf(
				"ui command: surface=%s instance=%s kind=%s thread=%s turn=%s sourceMessage=%s",
				event.SurfaceSessionID,
				instanceID,
				event.Command.Kind,
				event.Command.Target.ThreadID,
				event.Command.Target.TurnID,
				event.Command.Origin.MessageID,
			)
			if instanceID == "" {
				log.Printf("ui command skipped: surface=%s kind=%s err=no attached instance", event.SurfaceSessionID, event.Command.Kind)
				rollback := a.service.HandleCommandDispatchFailure(event.SurfaceSessionID, event.Command.CommandID, agentproto.ErrorInfo{
					Code:             "no_attached_instance",
					Layer:            "daemon",
					Stage:            "dispatch_prepare",
					Operation:        string(event.Command.Kind),
					Message:          "当前飞书会话还没有接管实例。",
					SurfaceSessionID: event.SurfaceSessionID,
					CommandID:        event.Command.CommandID,
				})
				a.handleUIEventsLocked(context.Background(), rollback)
				continue
			}
			a.traceSteerCommand(event.SurfaceSessionID, instanceID, *event.Command)
			a.mu.Unlock()
			err := a.sendAgentCommand(instanceID, *event.Command)
			a.mu.Lock()
			if err != nil {
				log.Printf("relay send command failed: instance=%s kind=%s err=%v", instanceID, event.Command.Kind, err)
				rollback := a.service.HandleCommandDispatchFailure(event.SurfaceSessionID, event.Command.CommandID, agentproto.ErrorInfoFromError(err, agentproto.ErrorInfo{
					Code:             "relay_send_command_failed",
					Layer:            "daemon",
					Stage:            "relay_send_command",
					Operation:        string(event.Command.Kind),
					Message:          "服务无法把消息发送到本地 wrapper。",
					SurfaceSessionID: event.SurfaceSessionID,
					CommandID:        event.Command.CommandID,
					Retryable:        true,
				}))
				a.handleUIEventsLocked(context.Background(), rollback)
			} else {
				a.debugf(
					"dispatch sent: surface=%s instance=%s command=%s kind=%s pending=%s active=%s",
					event.SurfaceSessionID,
					instanceID,
					event.Command.CommandID,
					event.Command.Kind,
					summarizeRemoteStatuses(a.service.PendingRemoteTurns()),
					summarizeRemoteStatuses(a.service.ActiveRemoteTurns()),
				)
			}
			continue
		}
		a.flushPendingGlobalRuntimeNoticesLocked(event.SurfaceSessionID)
		event, isGlobalRuntimeNotice := normalizeGlobalRuntimeNoticeEvent(event)
		if isGlobalRuntimeNotice && a.shouldSuppressGlobalRuntimeNoticeLocked(event, time.Now()) {
			continue
		}
		event = a.routeVSCodeMigrationFlowNoticeLocked(event)
		if err := a.deliverUIEventLocked(context.Background(), event); err != nil {
			chatID := a.service.SurfaceChatID(event.SurfaceSessionID)
			log.Printf("gateway apply failed: chat=%s event=%s err=%v", chatID, event.Kind, err)
			a.queueGatewayFailureNotice(event, err)
		} else if isGlobalRuntimeNotice {
			a.recordGlobalRuntimeNoticeLocked(event, time.Now())
		}
	}
}

func (a *App) deliverUIEvent(event control.UIEvent) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.deliverUIEventLocked(context.Background(), event)
}

func (a *App) deliverUIEventLocked(ctx context.Context, event control.UIEvent) error {
	return a.deliverUIEventWithContextMode(ctx, event, true)
}

func (a *App) deliverUIEventWithContext(ctx context.Context, event control.UIEvent) error {
	return a.deliverUIEventWithContextMode(ctx, event, false)
}

func (a *App) deliverUIEventWithContextMode(ctx context.Context, event control.UIEvent, appLocked bool) error {
	chatID := a.service.SurfaceChatID(event.SurfaceSessionID)
	actorUserID := a.service.SurfaceActorUserID(event.SurfaceSessionID)
	gatewayID := firstNonEmpty(event.GatewayID, a.service.SurfaceGatewayID(event.SurfaceSessionID))
	receiveID, receiveIDType := feishu.ResolveReceiveTarget(chatID, actorUserID)
	if receiveID == "" || receiveIDType == "" {
		return nil
	}
	log.Printf("ui event: surface=%s chat=%s actor=%s kind=%s", event.SurfaceSessionID, chatID, actorUserID, event.Kind)
	var (
		previewReq feishu.FinalBlockPreviewRequest
		previewErr error
		didPreview bool
	)
	if a.finalBlockPreviewer != nil && event.Kind == control.UIEventBlockCommitted && event.Block != nil {
		previewCtx, previewCancel := a.newTimeoutContext(ctx, a.finalPreviewTimeout)
		previewReq = feishu.FinalBlockPreviewRequest{
			GatewayID:        gatewayID,
			SurfaceSessionID: event.SurfaceSessionID,
			ChatID:           chatID,
			ActorUserID:      actorUserID,
			WorkspaceRoot:    a.previewWorkspaceRoot(event.SurfaceSessionID, *event.Block),
			ThreadCWD:        a.previewThreadCWD(event.SurfaceSessionID, *event.Block),
			PreviewGrantKey:  a.previewGrantKey(gatewayID, event.SurfaceSessionID, *event.Block),
			Block:            *event.Block,
		}
		var (
			previewResult feishu.FinalBlockPreviewResult
			err           error
		)
		didPreview = true
		if appLocked {
			a.mu.Unlock()
			previewResult, err = a.finalBlockPreviewer.RewriteFinalBlock(previewCtx, previewReq)
			a.mu.Lock()
		} else {
			previewResult, err = a.finalBlockPreviewer.RewriteFinalBlock(previewCtx, previewReq)
		}
		previewCancel()
		previewErr = err
		event.Block = &previewResult.Block
		if err != nil {
			log.Printf(
				"final block preview rewrite failed (continuing without preview rewrite): surface=%s instance=%s thread=%s item=%s err=%v",
				event.SurfaceSessionID,
				previewResult.Block.InstanceID,
				previewResult.Block.ThreadID,
				previewResult.Block.ItemID,
				err,
			)
		}
	}
	event.DaemonLifecycleID = a.daemonLifecycleID
	if event.Snapshot != nil {
		a.populateSnapshotFeishuPermissionGaps(event.Snapshot, event.SurfaceSessionID)
	}
	operations := a.projector.Project(chatID, event)
	for i := range operations {
		if operations[i].GatewayID == "" {
			operations[i].GatewayID = gatewayID
		}
		if operations[i].SurfaceSessionID == "" {
			operations[i].SurfaceSessionID = event.SurfaceSessionID
		}
		if operations[i].ReceiveID == "" {
			operations[i].ReceiveID = receiveID
		}
		if operations[i].ReceiveIDType == "" {
			operations[i].ReceiveIDType = receiveIDType
		}
	}
	applyCtx, applyCancel := a.newTimeoutContext(ctx, a.gatewayApplyTimeout)
	defer applyCancel()
	var err error
	if appLocked {
		a.mu.Unlock()
		err = a.gateway.Apply(applyCtx, operations)
		a.mu.Lock()
	} else {
		err = a.gateway.Apply(applyCtx, operations)
	}
	if err != nil {
		if a.observeFeishuPermissionError(gatewayID, err) {
			log.Printf("feishu permission gap observed during ui delivery: gateway=%s surface=%s event=%s err=%v", gatewayID, event.SurfaceSessionID, event.Kind, err)
			return nil
		}
		return err
	}
	a.recordUIEventDelivery(event, operations)
	if didPreview {
		a.maybeScheduleSecondChanceFinalPatchLocked(gatewayID, chatID, event, operations, previewReq, previewErr)
	}
	a.traceAssistantBlock(event)
	return nil
}

func (a *App) recordUIEventDelivery(event control.UIEvent, operations []feishu.Operation) {
	if event.Kind == control.UIEventBlockCommitted && event.Block != nil && event.Block.Final {
		for _, operation := range operations {
			if operation.Kind != feishu.OperationSendCard {
				continue
			}
			if strings.TrimSpace(operation.MessageID) == "" {
				continue
			}
			a.service.RecordFinalCardMessage(
				event.SurfaceSessionID,
				*event.Block,
				event.SourceMessageID,
				operation.MessageID,
				event.DaemonLifecycleID,
			)
			break
		}
	}
	if event.FeishuThreadHistoryView != nil {
		for _, operation := range operations {
			if operation.Kind != feishu.OperationSendCard {
				continue
			}
			if strings.TrimSpace(operation.MessageID) == "" {
				continue
			}
			a.service.RecordThreadHistoryMessage(
				event.SurfaceSessionID,
				event.FeishuThreadHistoryView.PickerID,
				operation.MessageID,
			)
			break
		}
	}
	if event.FeishuTargetPickerView != nil {
		for _, operation := range operations {
			if operation.Kind != feishu.OperationSendCard {
				continue
			}
			if strings.TrimSpace(operation.MessageID) == "" {
				continue
			}
			a.service.RecordTargetPickerMessage(
				event.SurfaceSessionID,
				event.FeishuTargetPickerView.PickerID,
				operation.MessageID,
			)
			break
		}
	}
	if event.FeishuPathPickerView != nil {
		for _, operation := range operations {
			if operation.Kind != feishu.OperationSendCard {
				continue
			}
			if strings.TrimSpace(operation.MessageID) == "" {
				continue
			}
			a.service.RecordPathPickerMessage(
				event.SurfaceSessionID,
				event.FeishuPathPickerView.PickerID,
				operation.MessageID,
			)
			break
		}
	}
	if event.FeishuPageContext != nil && strings.TrimSpace(event.FeishuPageContext.MenuFlowID) != "" {
		for _, operation := range operations {
			if operation.Kind != feishu.OperationSendCard {
				continue
			}
			if strings.TrimSpace(operation.MessageID) == "" {
				continue
			}
			a.service.RecordMenuFlowMessage(
				event.SurfaceSessionID,
				event.FeishuPageContext.MenuFlowID,
				operation.MessageID,
			)
			break
		}
	}
	if event.FeishuPageView != nil && strings.TrimSpace(event.FeishuPageView.TrackingKey) != "" {
		for _, operation := range operations {
			if operation.Kind != feishu.OperationSendCard {
				continue
			}
			if strings.TrimSpace(operation.MessageID) == "" {
				continue
			}
			a.service.RecordOwnerCardFlowMessage(
				event.SurfaceSessionID,
				event.FeishuPageView.TrackingKey,
				operation.MessageID,
			)
			a.recordUpgradeOwnerCardMessageLocked(
				event.FeishuPageView.TrackingKey,
				operation.MessageID,
			)
			a.recordVSCodeMigrationFlowMessageLocked(event.FeishuPageView.TrackingKey, operation.MessageID)
			break
		}
	}
	if event.ExecCommandProgress == nil {
		return
	}
	for _, operation := range operations {
		if operation.Kind != feishu.OperationSendCard && operation.Kind != feishu.OperationUpdateCard {
			continue
		}
		if strings.TrimSpace(operation.MessageID) == "" {
			continue
		}
		a.service.RecordExecCommandProgressMessageStartSeq(
			event.SurfaceSessionID,
			event.ExecCommandProgress.ThreadID,
			event.ExecCommandProgress.TurnID,
			event.ExecCommandProgress.ItemID,
			operation.MessageID,
			operation.ProgressCardStartSeq,
		)
	}
}

func (a *App) newTimeoutContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	base := context.Background()
	if parent != nil {
		base = parent
	}
	if timeout <= 0 {
		return context.WithCancel(base)
	}
	return context.WithTimeout(base, timeout)
}

func (a *App) queueGatewayFailureNotice(event control.UIEvent, err error) {
	if strings.TrimSpace(event.SurfaceSessionID) == "" {
		return
	}
	if event.Notice != nil && event.Notice.Code == "gateway_apply_failed" {
		return
	}
	notice := orchestrator.GlobalRuntimeGatewayApplyFailureNotice(agentproto.ErrorInfoFromError(err, agentproto.ErrorInfo{
		Code:             "gateway_apply_failed",
		Layer:            "daemon",
		Stage:            "gateway_apply",
		Operation:        string(event.Kind),
		Message:          "服务无法把消息发送到飞书。",
		SurfaceSessionID: event.SurfaceSessionID,
		Retryable:        true,
	}))
	a.queueGlobalRuntimeNoticeLocked(control.UIEvent{
		Kind:             control.UIEventNotice,
		SurfaceSessionID: event.SurfaceSessionID,
		Notice:           &notice,
	})
}

func (a *App) previewWorkspaceRoot(surfaceID string, block render.Block) string {
	instanceID := strings.TrimSpace(block.InstanceID)
	if instanceID == "" {
		instanceID = a.service.AttachedInstanceID(surfaceID)
	}
	if instanceID == "" {
		return ""
	}
	inst := a.service.Instance(instanceID)
	if inst == nil {
		return ""
	}
	return inst.WorkspaceRoot
}

func (a *App) previewThreadCWD(surfaceID string, block render.Block) string {
	instanceID := strings.TrimSpace(block.InstanceID)
	if instanceID == "" {
		instanceID = a.service.AttachedInstanceID(surfaceID)
	}
	if instanceID == "" {
		return ""
	}
	inst := a.service.Instance(instanceID)
	if inst == nil {
		return ""
	}
	if thread := inst.Threads[block.ThreadID]; thread != nil {
		return thread.CWD
	}
	return ""
}
