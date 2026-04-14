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
	_ = ctx
	for _, event := range events {
		if event.DaemonCommand != nil {
			followup := a.handleDaemonCommandLocked(*event.DaemonCommand)
			a.handleUIEvents(context.Background(), followup)
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
				a.handleUIEvents(context.Background(), rollback)
				continue
			}
			a.traceSteerCommand(event.SurfaceSessionID, instanceID, *event.Command)
			if err := a.sendAgentCommand(instanceID, *event.Command); err != nil {
				log.Printf("relay send command failed: instance=%s kind=%s err=%v", instanceID, event.Command.Kind, err)
				rollback := a.service.HandleCommandDispatchFailure(event.SurfaceSessionID, event.Command.CommandID, agentproto.ErrorInfoFromError(err, agentproto.ErrorInfo{
					Code:             "relay_send_command_failed",
					Layer:            "daemon",
					Stage:            "relay_send_command",
					Operation:        string(event.Command.Kind),
					Message:          "daemon 无法把消息发送到本地 wrapper。",
					SurfaceSessionID: event.SurfaceSessionID,
					CommandID:        event.Command.CommandID,
					Retryable:        true,
				}))
				a.handleUIEvents(context.Background(), rollback)
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
		a.flushPendingGatewayNotices(event.SurfaceSessionID)
		if err := a.deliverUIEvent(event); err != nil {
			chatID := a.service.SurfaceChatID(event.SurfaceSessionID)
			log.Printf("gateway apply failed: chat=%s event=%s err=%v", chatID, event.Kind, err)
			a.queueGatewayFailureNotice(event, err)
		}
	}
}

func (a *App) deliverUIEvent(event control.UIEvent) error {
	return a.deliverUIEventWithContext(context.Background(), event)
}

func (a *App) deliverUIEventWithContext(ctx context.Context, event control.UIEvent) error {
	chatID := a.service.SurfaceChatID(event.SurfaceSessionID)
	actorUserID := a.service.SurfaceActorUserID(event.SurfaceSessionID)
	gatewayID := firstNonEmpty(event.GatewayID, a.service.SurfaceGatewayID(event.SurfaceSessionID))
	receiveID, receiveIDType := feishu.ResolveReceiveTarget(chatID, actorUserID)
	if receiveID == "" || receiveIDType == "" {
		return nil
	}
	log.Printf("ui event: surface=%s chat=%s actor=%s kind=%s", event.SurfaceSessionID, chatID, actorUserID, event.Kind)
	var previewSupplementOps []feishu.Operation
	if a.finalBlockPreviewer != nil && event.Kind == control.UIEventBlockCommitted && event.Block != nil {
		previewCtx, previewCancel := a.newTimeoutContext(ctx, a.finalPreviewTimeout)
		previewResult, err := a.finalBlockPreviewer.RewriteFinalBlock(previewCtx, feishu.FinalBlockPreviewRequest{
			GatewayID:        gatewayID,
			SurfaceSessionID: event.SurfaceSessionID,
			ChatID:           chatID,
			ActorUserID:      actorUserID,
			WorkspaceRoot:    a.previewWorkspaceRoot(event.SurfaceSessionID, *event.Block),
			ThreadCWD:        a.previewThreadCWD(event.SurfaceSessionID, *event.Block),
			Block:            *event.Block,
		})
		previewCancel()
		event.Block = &previewResult.Block
		previewSupplementOps = a.projector.ProjectPreviewSupplements(gatewayID, event.SurfaceSessionID, chatID, event.SourceMessageID, previewResult.Supplements)
		if err != nil {
			log.Printf(
				"final block preview rewrite failed (continuing without preview rewrite or supplements): surface=%s instance=%s thread=%s item=%s err=%v",
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
	operations = append(operations, previewSupplementOps...)
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
	if err := a.gateway.Apply(applyCtx, operations); err != nil {
		if a.observeFeishuPermissionError(gatewayID, err) {
			log.Printf("feishu permission gap observed during ui delivery: gateway=%s surface=%s event=%s err=%v", gatewayID, event.SurfaceSessionID, event.Kind, err)
			return nil
		}
		return err
	}
	a.recordUIEventDelivery(event, operations)
	a.traceAssistantBlock(event)
	return nil
}

func (a *App) recordUIEventDelivery(event control.UIEvent, operations []feishu.Operation) {
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
	if event.ExecCommandProgress == nil {
		return
	}
	for _, operation := range operations {
		if operation.Kind != feishu.OperationSendCard {
			continue
		}
		if strings.TrimSpace(operation.MessageID) == "" {
			continue
		}
		a.service.RecordExecCommandProgressMessage(
			event.SurfaceSessionID,
			event.ExecCommandProgress.ThreadID,
			event.ExecCommandProgress.TurnID,
			event.ExecCommandProgress.ItemID,
			operation.MessageID,
		)
		return
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

func (a *App) flushPendingGatewayNotices(surfaceID string) {
	if strings.TrimSpace(surfaceID) == "" {
		return
	}
	pending := a.pendingGatewayNotices[surfaceID]
	if len(pending) == 0 {
		return
	}
	for _, event := range pending {
		if err := a.deliverUIEvent(event); err != nil {
			return
		}
	}
	delete(a.pendingGatewayNotices, surfaceID)
}

func (a *App) queueGatewayFailureNotice(event control.UIEvent, err error) {
	if strings.TrimSpace(event.SurfaceSessionID) == "" {
		return
	}
	if event.Notice != nil && event.Notice.Code == "gateway_apply_failed" {
		return
	}
	notice := orchestrator.NoticeForProblem(agentproto.ErrorInfoFromError(err, agentproto.ErrorInfo{
		Code:             "gateway_apply_failed",
		Layer:            "daemon",
		Stage:            "gateway_apply",
		Operation:        string(event.Kind),
		Message:          "daemon 无法把消息发送到飞书。",
		SurfaceSessionID: event.SurfaceSessionID,
		Retryable:        true,
	}))
	notice.Code = "gateway_apply_failed"
	queued := control.UIEvent{
		Kind:             control.UIEventNotice,
		SurfaceSessionID: event.SurfaceSessionID,
		Notice:           &notice,
	}
	pending := a.pendingGatewayNotices[event.SurfaceSessionID]
	if len(pending) > 0 {
		last := pending[len(pending)-1]
		if last.Notice != nil && last.Notice.Code == queued.Notice.Code && last.Notice.Text == queued.Notice.Text {
			return
		}
	}
	a.pendingGatewayNotices[event.SurfaceSessionID] = append(pending, queued)
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
