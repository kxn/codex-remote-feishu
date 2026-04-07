package daemon

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/adapter/relayws"
	"github.com/kxn/codex-remote-feishu/internal/app/adminauth"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/render"
	"github.com/kxn/codex-remote-feishu/internal/core/renderer"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/debuglog"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

type HeadlessRuntimeConfig struct {
	BinaryPath string
	ConfigPath string
	BaseEnv    []string
	Paths      relayruntime.Paths
	LaunchArgs []string
	IdleTTL    time.Duration
	KillGrace  time.Duration
}

type managedHeadlessProcess struct {
	InstanceID    string
	PID           int
	RequestedAt   time.Time
	StartedAt     time.Time
	IdleSince     time.Time
	ThreadID      string
	ThreadCWD     string
	WorkspaceRoot string
	DisplayName   string
	Status        string
	LastError     string
}

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

	commandSeq    uint64
	mu            sync.Mutex
	adminConfigMu sync.Mutex
	listenMu      sync.Mutex
	ingressRunMu  sync.Mutex
	relayConnMu   sync.Mutex

	pendingGatewayNotices map[string][]control.UIEvent
	headlessRuntime       HeadlessRuntimeConfig
	managedHeadless       map[string]*managedHeadlessProcess
	startHeadless         func(relayruntime.HeadlessLaunchOptions) (int, error)
	stopProcess           func(int, time.Duration) error
	ingress               *ingressPump
	ingressCancel         context.CancelFunc
	ingressStarted        bool
	ingressWG             sync.WaitGroup
	relayConnections      map[string]*relayConnectionState

	adminAuth *adminauth.Manager
	admin     adminRuntimeState

	relayListener net.Listener
	apiListener   net.Listener
}

func New(relayAddr, apiAddr string, gateway feishu.Gateway, serverIdentity agentproto.ServerIdentity) *App {
	if gateway == nil {
		gateway = feishu.NopGateway{}
	}
	authManager, err := adminauth.NewManager(adminauth.ManagerOptions{})
	if err != nil {
		panic(err)
	}
	app := &App{
		service:               orchestrator.NewService(time.Now, orchestrator.Config{TurnHandoffWait: 800 * time.Millisecond}, renderer.NewPlanner()),
		projector:             feishu.NewProjector(),
		gateway:               gateway,
		pendingGatewayNotices: map[string][]control.UIEvent{},
		managedHeadless:       map[string]*managedHeadlessProcess{},
		startHeadless:         relayruntime.StartDetachedWrapper,
		stopProcess:           relayruntime.TerminateProcess,
		ingress:               newIngressPump(),
		relayConnections:      map[string]*relayConnectionState{},
		adminAuth:             authManager,
	}
	app.relay = relayws.NewServer(relayws.ServerCallbacks{
		OnHello:      app.enqueueHello,
		OnEvents:     app.enqueueEvents,
		OnCommandAck: app.enqueueCommandAck,
		OnDisconnect: app.enqueueDisconnect,
	})
	app.relay.SetServerIdentity(serverIdentity)

	relayMux := http.NewServeMux()
	relayMux.Handle("GET /ws/agent", app.relay)
	relayMux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	app.relayServer = &http.Server{Addr: relayAddr, Handler: relayMux}

	apiMux := http.NewServeMux()
	app.registerAPIRoutes(apiMux)
	app.apiServer = &http.Server{Addr: apiAddr, Handler: apiMux}
	return app
}

func (a *App) SetHeadlessRuntime(cfg HeadlessRuntimeConfig) {
	if cfg.IdleTTL <= 0 {
		cfg.IdleTTL = 2 * time.Hour
	}
	if cfg.KillGrace <= 0 {
		cfg.KillGrace = 3 * time.Second
	}
	cfg.BaseEnv = append([]string{}, cfg.BaseEnv...)
	cfg.LaunchArgs = append([]string{}, cfg.LaunchArgs...)
	a.headlessRuntime = cfg
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

func (a *App) Bind() error {
	a.listenMu.Lock()
	defer a.listenMu.Unlock()

	if a.relayListener != nil && a.apiListener != nil {
		return nil
	}

	relayListener, err := net.Listen("tcp", a.relayServer.Addr)
	if err != nil {
		return err
	}
	apiListener, err := net.Listen("tcp", a.apiServer.Addr)
	if err != nil {
		_ = relayListener.Close()
		return err
	}

	a.relayListener = relayListener
	a.apiListener = apiListener
	return nil
}

func (a *App) Run(ctx context.Context) error {
	if err := a.Bind(); err != nil {
		return err
	}

	a.listenMu.Lock()
	relayListener := a.relayListener
	apiListener := a.apiListener
	a.listenMu.Unlock()

	errCh := make(chan error, 3)
	a.startIngressPump(ctx, errCh)

	go func() {
		if err := a.relayServer.Serve(relayListener); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()
	go func() {
		if err := a.apiServer.Serve(apiListener); err != nil && err != http.ErrServerClosed {
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
	a.stopIngressPump()
	_ = a.relayServer.Shutdown(ctx)
	_ = a.apiServer.Shutdown(ctx)
	a.listenMu.Lock()
	if a.relayListener != nil {
		_ = a.relayListener.Close()
		a.relayListener = nil
	}
	if a.apiListener != nil {
		_ = a.apiListener.Close()
		a.apiListener = nil
	}
	a.listenMu.Unlock()
	if a.rawLogger != nil {
		_ = a.rawLogger.Close()
	}
	return nil
}

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
	a.rememberRelayConnection(hello.Instance.InstanceID, meta.ConnectionID)
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
}

func (a *App) onTick(ctx context.Context, now time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	uiEvents := a.service.Tick(now)
	a.handleUIEvents(ctx, uiEvents)
	a.reapIdleHeadless(now)
}

func (a *App) handleUIEvents(ctx context.Context, events []control.UIEvent) {
	_ = ctx
	for _, event := range events {
		if event.DaemonCommand != nil {
			followup := a.handleDaemonCommand(*event.DaemonCommand)
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
	gatewayID := firstNonEmpty(event.GatewayID, a.service.SurfaceGatewayID(event.SurfaceSessionID))
	receiveID, receiveIDType := feishu.ResolveReceiveTarget(chatID, actorUserID)
	if receiveID == "" || receiveIDType == "" {
		return nil
	}
	log.Printf("ui event: surface=%s chat=%s actor=%s kind=%s", event.SurfaceSessionID, chatID, actorUserID, event.Kind)
	if a.markdownPreviewer != nil && event.Kind == control.UIEventBlockCommitted && event.Block != nil {
		rewriteCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		rewrittenBlock, err := a.markdownPreviewer.RewriteFinalBlock(rewriteCtx, feishu.MarkdownPreviewRequest{
			GatewayID:        gatewayID,
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

func (a *App) handleDaemonCommand(command control.DaemonCommand) []control.UIEvent {
	switch command.Kind {
	case control.DaemonCommandStartHeadless:
		return a.startManagedHeadless(command)
	case control.DaemonCommandKillHeadless:
		return a.killManagedHeadless(command)
	default:
		return nil
	}
}

func (a *App) startManagedHeadless(command control.DaemonCommand) []control.UIEvent {
	cfg := a.headlessRuntime
	if strings.TrimSpace(cfg.BinaryPath) == "" {
		return a.service.HandleHeadlessLaunchFailed(
			command.SurfaceSessionID,
			command.InstanceID,
			agentproto.ErrorInfo{
				Code:             "headless_binary_missing",
				Layer:            "daemon",
				Stage:            "headless_start",
				Operation:        "new_instance",
				Message:          "headless 启动器未配置可执行文件。",
				SurfaceSessionID: command.SurfaceSessionID,
				ThreadID:         command.ThreadID,
			},
		)
	}

	env := append([]string{}, cfg.BaseEnv...)
	env = append(env,
		"CODEX_REMOTE_INSTANCE_ID="+command.InstanceID,
		"CODEX_REMOTE_INSTANCE_SOURCE=headless",
		"CODEX_REMOTE_INSTANCE_MANAGED=1",
	)
	if strings.TrimSpace(command.ThreadCWD) == "" {
		env = append(env, "CODEX_REMOTE_INSTANCE_DISPLAY_NAME=headless")
	}

	workDir := strings.TrimSpace(command.ThreadCWD)
	if workDir == "" {
		workDir = strings.TrimSpace(cfg.Paths.StateDir)
	}

	pid, err := a.startHeadless(relayruntime.HeadlessLaunchOptions{
		BinaryPath: cfg.BinaryPath,
		ConfigPath: cfg.ConfigPath,
		Env:        env,
		Paths:      cfg.Paths,
		WorkDir:    workDir,
		InstanceID: command.InstanceID,
		Args:       cfg.LaunchArgs,
	})
	if err != nil {
		log.Printf(
			"headless start failed: surface=%s instance=%s thread=%s cwd=%s err=%v",
			command.SurfaceSessionID,
			command.InstanceID,
			command.ThreadID,
			command.ThreadCWD,
			err,
		)
		return a.service.HandleHeadlessLaunchFailed(command.SurfaceSessionID, command.InstanceID, err)
	}

	a.managedHeadless[command.InstanceID] = &managedHeadlessProcess{
		InstanceID:    command.InstanceID,
		PID:           pid,
		RequestedAt:   time.Now().UTC(),
		StartedAt:     time.Now().UTC(),
		ThreadID:      command.ThreadID,
		ThreadCWD:     workDir,
		WorkspaceRoot: workDir,
		DisplayName:   "headless",
		Status:        "starting",
	}
	log.Printf(
		"headless start requested: surface=%s instance=%s pid=%d thread=%s cwd=%s",
		command.SurfaceSessionID,
		command.InstanceID,
		pid,
		command.ThreadID,
		workDir,
	)
	return a.service.HandleHeadlessLaunchStarted(command.SurfaceSessionID, command.InstanceID, pid)
}

func (a *App) killManagedHeadless(command control.DaemonCommand) []control.UIEvent {
	pid := 0
	if managed := a.managedHeadless[command.InstanceID]; managed != nil {
		pid = managed.PID
	}
	if pid == 0 {
		if inst := a.service.Instance(command.InstanceID); inst != nil && strings.EqualFold(strings.TrimSpace(inst.Source), "headless") && inst.Managed {
			pid = inst.PID
		}
	}
	if pid == 0 {
		if strings.TrimSpace(command.SurfaceSessionID) == "" {
			return nil
		}
		return a.service.HandleProblem(command.InstanceID, agentproto.ErrorInfo{
			Code:             "headless_pid_unknown",
			Layer:            "daemon",
			Stage:            "headless_kill",
			Operation:        "kill_instance",
			Message:          "找不到可结束的 headless 进程。",
			SurfaceSessionID: command.SurfaceSessionID,
			ThreadID:         command.ThreadID,
			Retryable:        true,
		})
	}
	if err := a.stopProcess(pid, a.headlessRuntime.KillGrace); err != nil {
		log.Printf(
			"headless kill failed: surface=%s instance=%s pid=%d err=%v",
			command.SurfaceSessionID,
			command.InstanceID,
			pid,
			err,
		)
		if strings.TrimSpace(command.SurfaceSessionID) == "" {
			return nil
		}
		return a.service.HandleProblem(command.InstanceID, agentproto.ErrorInfoFromError(err, agentproto.ErrorInfo{
			Code:             "headless_kill_failed",
			Layer:            "daemon",
			Stage:            "headless_kill",
			Operation:        "kill_instance",
			Message:          "无法结束 headless 实例。",
			SurfaceSessionID: command.SurfaceSessionID,
			ThreadID:         command.ThreadID,
			Retryable:        true,
		}))
	}
	delete(a.managedHeadless, command.InstanceID)
	a.service.RemoveInstance(command.InstanceID)
	log.Printf("headless kill requested: surface=%s instance=%s pid=%d", command.SurfaceSessionID, command.InstanceID, pid)
	return nil
}

func (a *App) observeManagedHeadless(inst *state.InstanceRecord) {
	if inst == nil || !strings.EqualFold(strings.TrimSpace(inst.Source), "headless") || !inst.Managed {
		return
	}
	managed := a.managedHeadless[inst.InstanceID]
	if managed == nil {
		managed = &managedHeadlessProcess{
			InstanceID:  inst.InstanceID,
			RequestedAt: time.Now().UTC(),
			StartedAt:   time.Now().UTC(),
			Status:      "online",
		}
		a.managedHeadless[inst.InstanceID] = managed
	}
	if inst.PID > 0 {
		managed.PID = inst.PID
	}
	if strings.TrimSpace(inst.DisplayName) != "" {
		managed.DisplayName = inst.DisplayName
	}
	if strings.TrimSpace(inst.WorkspaceRoot) != "" {
		managed.WorkspaceRoot = inst.WorkspaceRoot
	}
	managed.Status = "online"
}

func (a *App) reapIdleHeadless(now time.Time) {
	if a.headlessRuntime.IdleTTL <= 0 {
		return
	}
	attached := map[string]bool{}
	for _, surface := range a.service.Surfaces() {
		if surface == nil || surface.AttachedInstanceID == "" {
			continue
		}
		attached[surface.AttachedInstanceID] = true
	}
	for instanceID, managed := range a.managedHeadless {
		if managed == nil {
			delete(a.managedHeadless, instanceID)
			continue
		}
		inst := a.service.Instance(instanceID)
		if inst == nil || !inst.Online || !strings.EqualFold(strings.TrimSpace(inst.Source), "headless") || !inst.Managed {
			managed.IdleSince = time.Time{}
			continue
		}
		if attached[instanceID] || inst.ActiveTurnID != "" {
			managed.IdleSince = time.Time{}
			continue
		}
		if managed.IdleSince.IsZero() {
			managed.IdleSince = now
			continue
		}
		if now.Sub(managed.IdleSince) < a.headlessRuntime.IdleTTL {
			continue
		}
		if managed.PID == 0 && inst.PID > 0 {
			managed.PID = inst.PID
		}
		if managed.PID == 0 {
			log.Printf("headless idle cleanup skipped: instance=%s err=missing pid", instanceID)
			continue
		}
		if err := a.stopProcess(managed.PID, a.headlessRuntime.KillGrace); err != nil {
			log.Printf("headless idle cleanup failed: instance=%s pid=%d err=%v", instanceID, managed.PID, err)
			continue
		}
		log.Printf("headless idle cleanup: instance=%s pid=%d idle_since=%s", instanceID, managed.PID, managed.IdleSince.Format(time.RFC3339))
		delete(a.managedHeadless, instanceID)
		a.service.RemoveInstance(instanceID)
	}
}

func (a *App) nextCommandID() string {
	return "cmd-" + strconv.FormatUint(atomic.AddUint64(&a.commandSeq, 1), 10)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (a *App) handleStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.runtimeStatusPayload())
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
