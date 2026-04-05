package daemon

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/adapter/relayws"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/render"
	"github.com/kxn/codex-remote-feishu/internal/core/renderer"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/debuglog"
)

type App struct {
	service           *orchestrator.Service
	projector         *feishu.Projector
	gateway           feishu.Gateway
	markdownPreviewer feishu.MarkdownPreviewService
	relay             *relayws.Server
	debugRelayFlow    bool
	rawLogger         *debuglog.RawLogger

	relayServer *http.Server
	apiServer   *http.Server

	commandSeq uint64
	mu         sync.Mutex

	pendingGatewayNotices map[string][]control.UIEvent
}

func New(relayAddr, apiAddr string, gateway feishu.Gateway, serverIdentity agentproto.ServerIdentity) *App {
	if gateway == nil {
		gateway = feishu.NopGateway{}
	}
	app := &App{
		service:               orchestrator.NewService(time.Now, orchestrator.Config{TurnHandoffWait: 800 * time.Millisecond}, renderer.NewPlanner()),
		projector:             feishu.NewProjector(),
		gateway:               gateway,
		pendingGatewayNotices: map[string][]control.UIEvent{},
	}
	app.relay = relayws.NewServer(relayws.ServerCallbacks{
		OnHello:      app.onHello,
		OnEvents:     app.onEvents,
		OnCommandAck: app.onCommandAck,
		OnDisconnect: app.onDisconnect,
	})
	app.relay.SetServerIdentity(serverIdentity)

	relayMux := http.NewServeMux()
	relayMux.Handle("/ws/agent", app.relay)
	relayMux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	app.relayServer = &http.Server{Addr: relayAddr, Handler: relayMux}

	apiMux := http.NewServeMux()
	apiMux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	apiMux.HandleFunc("/v1/status", app.handleStatus)
	app.apiServer = &http.Server{Addr: apiAddr, Handler: apiMux}
	return app
}

func (a *App) SetMarkdownPreviewer(previewer feishu.MarkdownPreviewService) {
	a.markdownPreviewer = previewer
}

func (a *App) SetDebugRelayFlow(enabled bool) {
	a.debugRelayFlow = enabled
}

func (a *App) SetRawLogger(logger *debuglog.RawLogger) {
	a.rawLogger = logger
	a.relay.SetRawLogger(logger)
}

func (a *App) debugf(format string, args ...any) {
	if a.debugRelayFlow {
		log.Printf("relay flow daemon: "+format, args...)
	}
}

func (a *App) Run(ctx context.Context) error {
	errCh := make(chan error, 3)

	go func() {
		if err := a.relayServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()
	go func() {
		if err := a.apiServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()
	go func() {
		if err := a.gateway.Start(ctx, a.HandleAction); err != nil && err != context.Canceled {
			errCh <- err
		}
	}()
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				a.onTick(ctx, now)
			}
		}
	}()

	select {
	case <-ctx.Done():
		_ = a.Shutdown(context.Background())
		return nil
	case err := <-errCh:
		_ = a.Shutdown(context.Background())
		return err
	}
}

func (a *App) Shutdown(ctx context.Context) error {
	_ = a.relay.Close()
	_ = a.relayServer.Shutdown(ctx)
	_ = a.apiServer.Shutdown(ctx)
	if a.rawLogger != nil {
		_ = a.rawLogger.Close()
	}
	return nil
}

func (a *App) HandleAction(ctx context.Context, action control.Action) {
	a.mu.Lock()
	defer a.mu.Unlock()
	log.Printf(
		"surface action: surface=%s chat=%s actor=%s kind=%s message=%s instance=%s thread=%s text=%q",
		action.SurfaceSessionID,
		action.ChatID,
		action.ActorUserID,
		action.Kind,
		action.MessageID,
		action.InstanceID,
		action.ThreadID,
		actionTextPreview(action.Text),
	)
	events := a.service.ApplySurfaceAction(action)
	a.handleUIEvents(ctx, events)
}

func (a *App) Service() *orchestrator.Service {
	return a.service
}

func (a *App) onHello(ctx context.Context, hello agentproto.Hello) {
	a.mu.Lock()
	defer a.mu.Unlock()

	inst := a.service.Instance(hello.Instance.InstanceID)
	if inst == nil {
		inst = &state.InstanceRecord{
			InstanceID: hello.Instance.InstanceID,
			Threads:    map[string]*state.ThreadRecord{},
		}
	}
	inst.DisplayName = hello.Instance.DisplayName
	inst.WorkspaceRoot = hello.Instance.WorkspaceRoot
	inst.WorkspaceKey = hello.Instance.WorkspaceKey
	inst.ShortName = hello.Instance.ShortName
	inst.Online = true
	a.service.UpsertInstance(inst)
	log.Printf("relay instance connected: id=%s workspace=%s display=%s", inst.InstanceID, inst.WorkspaceKey, inst.DisplayName)
	a.handleUIEvents(ctx, a.service.ApplyInstanceConnected(inst.InstanceID))

	command := agentproto.Command{
		CommandID: a.nextCommandID(),
		Kind:      agentproto.CommandThreadsRefresh,
	}
	if err := a.relay.SendCommand(hello.Instance.InstanceID, command); err != nil {
		log.Printf("relay send command failed: instance=%s kind=%s err=%v", hello.Instance.InstanceID, command.Kind, err)
		a.handleUIEvents(ctx, a.service.HandleProblem(hello.Instance.InstanceID, agentproto.ErrorInfoFromError(err, agentproto.ErrorInfo{
			Code:      "relay_send_command_failed",
			Layer:     "daemon",
			Stage:     "send_threads_refresh",
			Operation: string(command.Kind),
			Message:   "daemon 无法向本地 wrapper 发送初始化命令。",
			CommandID: command.CommandID,
			Retryable: true,
		})))
	}
}

func (a *App) onEvents(ctx context.Context, instanceID string, events []agentproto.Event) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, event := range events {
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
		a.handleUIEvents(ctx, uiEvents)
	}
}

func (a *App) onCommandAck(ctx context.Context, instanceID string, ack agentproto.CommandAck) {
	a.mu.Lock()
	defer a.mu.Unlock()
	log.Printf("relay command ack: instance=%s command=%s accepted=%t error=%s", instanceID, ack.CommandID, ack.Accepted, ack.Error)
	a.debugf(
		"command ack state: instance=%s command=%s accepted=%t pending=%s active=%s",
		instanceID,
		ack.CommandID,
		ack.Accepted,
		summarizeRemoteStatuses(a.service.PendingRemoteTurns()),
		summarizeRemoteStatuses(a.service.ActiveRemoteTurns()),
	)
	if ack.Accepted {
		return
	}
	a.handleUIEvents(ctx, a.service.HandleCommandRejected(instanceID, ack))
}

func (a *App) onDisconnect(ctx context.Context, instanceID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	inst := a.service.Instance(instanceID)
	if inst == nil {
		return
	}
	uiEvents := a.service.ApplyInstanceDisconnected(instanceID)
	log.Printf("relay instance disconnected: id=%s workspace=%s display=%s", inst.InstanceID, inst.WorkspaceKey, inst.DisplayName)
	a.handleUIEvents(ctx, uiEvents)
}

func (a *App) onTick(ctx context.Context, now time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	uiEvents := a.service.Tick(now)
	a.handleUIEvents(ctx, uiEvents)
}

func (a *App) handleUIEvents(ctx context.Context, events []control.UIEvent) {
	_ = ctx
	for _, event := range events {
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
				rollback := a.service.HandleCommandDispatchFailure(event.SurfaceSessionID, agentproto.ErrorInfo{
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
			if err := a.relay.SendCommand(instanceID, *event.Command); err != nil {
				log.Printf("relay send command failed: instance=%s kind=%s err=%v", instanceID, event.Command.Kind, err)
				rollback := a.service.HandleCommandDispatchFailure(event.SurfaceSessionID, agentproto.ErrorInfoFromError(err, agentproto.ErrorInfo{
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
	chatID := a.service.SurfaceChatID(event.SurfaceSessionID)
	actorUserID := a.service.SurfaceActorUserID(event.SurfaceSessionID)
	receiveID, receiveIDType := feishu.ResolveReceiveTarget(chatID, actorUserID)
	if receiveID == "" || receiveIDType == "" {
		return nil
	}
	log.Printf("ui event: surface=%s chat=%s actor=%s kind=%s", event.SurfaceSessionID, chatID, actorUserID, event.Kind)
	if a.markdownPreviewer != nil && event.Kind == control.UIEventBlockCommitted && event.Block != nil {
		rewriteCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		rewrittenBlock, err := a.markdownPreviewer.RewriteFinalBlock(rewriteCtx, feishu.MarkdownPreviewRequest{
			SurfaceSessionID: event.SurfaceSessionID,
			ChatID:           chatID,
			ActorUserID:      actorUserID,
			WorkspaceRoot:    a.previewWorkspaceRoot(event.SurfaceSessionID, *event.Block),
			ThreadCWD:        a.previewThreadCWD(event.SurfaceSessionID, *event.Block),
			Block:            *event.Block,
		})
		cancel()
		event.Block = &rewrittenBlock
		if err != nil {
			log.Printf(
				"markdown preview rewrite failed: surface=%s instance=%s thread=%s item=%s err=%v",
				event.SurfaceSessionID,
				rewrittenBlock.InstanceID,
				rewrittenBlock.ThreadID,
				rewrittenBlock.ItemID,
				err,
			)
		}
	}
	operations := a.projector.Project(chatID, event)
	for i := range operations {
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
	applyCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	err := a.gateway.Apply(applyCtx, operations)
	cancel()
	return err
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

func (a *App) nextCommandID() string {
	return "cmd-" + strconv.FormatUint(atomic.AddUint64(&a.commandSeq, 1), 10)
}

func (a *App) handleStatus(w http.ResponseWriter, _ *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	payload := struct {
		Instances          []*state.InstanceRecord         `json:"instances"`
		Surfaces           []*state.SurfaceConsoleRecord   `json:"surfaces"`
		PendingRemoteTurns []orchestrator.RemoteTurnStatus `json:"pendingRemoteTurns"`
		ActiveRemoteTurns  []orchestrator.RemoteTurnStatus `json:"activeRemoteTurns"`
	}{
		Instances:          a.service.Instances(),
		Surfaces:           a.service.Surfaces(),
		PendingRemoteTurns: a.service.PendingRemoteTurns(),
		ActiveRemoteTurns:  a.service.ActiveRemoteTurns(),
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

func summarizeRemoteStatuses(values []orchestrator.RemoteTurnStatus) string {
	if len(values) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, strings.Join([]string{
			"instance=" + value.InstanceID,
			"surface=" + value.SurfaceSessionID,
			"queue=" + value.QueueItemID,
			"command=" + value.CommandID,
			"thread=" + value.ThreadID,
			"turn=" + value.TurnID,
			"status=" + value.Status,
		}, ","))
	}
	return strings.Join(parts, "; ")
}

func snapshotSelectedThreadID(snapshot *control.Snapshot) string {
	if snapshot == nil {
		return ""
	}
	return snapshot.Attachment.SelectedThreadID
}

func snapshotRouteMode(snapshot *control.Snapshot) string {
	if snapshot == nil {
		return ""
	}
	return snapshot.Attachment.RouteMode
}

func snapshotPromptThreadID(snapshot *control.Snapshot) string {
	if snapshot == nil {
		return ""
	}
	return snapshot.NextPrompt.ThreadID
}

func snapshotPromptCreateThread(snapshot *control.Snapshot) bool {
	if snapshot == nil {
		return false
	}
	return snapshot.NextPrompt.CreateThread
}

func actionTextPreview(text string) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
	if len(text) <= 120 {
		return text
	}
	return text[:117] + "..."
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
