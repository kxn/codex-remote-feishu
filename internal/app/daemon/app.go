package daemon

import (
	"context"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/adapter/relayws"
	"github.com/kxn/codex-remote-feishu/internal/app/adminauth"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/renderer"
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
	StartTTL   time.Duration

	IdleRefreshInterval time.Duration
	IdleRefreshTimeout  time.Duration
	MinIdle             int
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
	LastHelloAt   time.Time

	RefreshCommandID       string
	RefreshInFlight        bool
	LastRefreshRequestedAt time.Time
	LastRefreshCompletedAt time.Time
}

type headlessRestoreRecoveryState struct {
	Hint            HeadlessRestoreHint
	NextAttemptAt   time.Time
	LastAttemptAt   time.Time
	LastFailureCode string
}

type App struct {
	service             *orchestrator.Service
	projector           *feishu.Projector
	gateway             feishu.Gateway
	finalBlockPreviewer feishu.FinalBlockPreviewService
	relay               *relayws.Server
	serverIdentity      agentproto.ServerIdentity
	debugRelayFlow      bool
	rawLogger           *debuglog.RawLogger

	relayServer *http.Server
	apiServer   *http.Server

	daemonStartedAt   time.Time
	daemonLifecycleID string
	shutdownStarted   bool
	shuttingDown      bool

	commandSeq    uint64
	mu            sync.Mutex
	shutdownMu    sync.Mutex
	adminConfigMu sync.Mutex
	adminFeishuMu sync.RWMutex
	listenMu      sync.Mutex
	ingressRunMu  sync.Mutex
	relayConnMu   sync.Mutex

	pendingGatewayNotices map[string][]control.UIEvent
	headlessRuntime       HeadlessRuntimeConfig
	headlessRestoreHints  *headlessRestoreHintStore
	headlessRestoreState  map[string]*headlessRestoreRecoveryState
	startupRefreshPending map[string]bool
	startupRefreshSeen    bool
	managedHeadless       map[string]*managedHeadlessProcess
	startHeadless         func(relayruntime.HeadlessLaunchOptions) (int, error)
	stopProcess           func(int, time.Duration) error
	sendAgentCommand      func(string, agentproto.Command) error
	ingress               *ingressPump
	ingressCancel         context.CancelFunc
	ingressStarted        bool
	ingressWG             sync.WaitGroup
	gatewayRunCancel      context.CancelFunc
	gatewayRunDone        chan struct{}
	relayConnections      map[string]*relayConnectionState
	feishuRuntimeApply    map[string]feishuRuntimeApplyPendingState

	adminAuth *adminauth.Manager
	admin     adminRuntimeState

	relayListener net.Listener
	apiListener   net.Listener

	shutdownGracePeriod   time.Duration
	shutdownNoticeTimeout time.Duration
	gatewayStopTimeout    time.Duration

	upgradeLookup          releaseLookupFunc
	upgradeCheckInterval   time.Duration
	upgradeStartupDelay    time.Duration
	upgradePromptScanEvery time.Duration
	upgradeCheckInFlight   bool
	upgradeNextCheckAt     time.Time
	upgradeNextPromptScan  time.Time
}

func New(relayAddr, apiAddr string, gateway feishu.Gateway, serverIdentity agentproto.ServerIdentity) *App {
	if gateway == nil {
		gateway = feishu.NopGateway{}
	}
	daemonStartedAt := serverIdentity.StartedAt.UTC()
	if daemonStartedAt.IsZero() {
		daemonStartedAt = time.Now().UTC()
	}
	authManager, err := adminauth.NewManager(adminauth.ManagerOptions{})
	if err != nil {
		panic(err)
	}
	app := &App{
		service:                orchestrator.NewService(time.Now, orchestrator.Config{TurnHandoffWait: 800 * time.Millisecond}, renderer.NewPlanner()),
		projector:              feishu.NewProjector(),
		gateway:                gateway,
		serverIdentity:         serverIdentity,
		daemonStartedAt:        daemonStartedAt,
		daemonLifecycleID:      daemonLifecycleID(serverIdentity, daemonStartedAt),
		pendingGatewayNotices:  map[string][]control.UIEvent{},
		headlessRestoreState:   map[string]*headlessRestoreRecoveryState{},
		startupRefreshPending:  map[string]bool{},
		managedHeadless:        map[string]*managedHeadlessProcess{},
		startHeadless:          relayruntime.StartDetachedWrapper,
		stopProcess:            relayruntime.TerminateProcess,
		ingress:                newIngressPump(),
		relayConnections:       map[string]*relayConnectionState{},
		feishuRuntimeApply:     map[string]feishuRuntimeApplyPendingState{},
		adminAuth:              authManager,
		shutdownGracePeriod:    5 * time.Second,
		shutdownNoticeTimeout:  2 * time.Second,
		gatewayStopTimeout:     3 * time.Second,
		upgradeCheckInterval:   3 * time.Hour,
		upgradeStartupDelay:    1 * time.Minute,
		upgradePromptScanEvery: 5 * time.Second,
	}
	app.upgradeLookup = app.defaultReleaseLookup
	app.relay = relayws.NewServer(relayws.ServerCallbacks{
		OnHello:      app.enqueueHello,
		OnEvents:     app.enqueueEvents,
		OnCommandAck: app.enqueueCommandAck,
		OnDisconnect: app.enqueueDisconnect,
	})
	app.sendAgentCommand = app.relay.SendCommand
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
	if cfg.StartTTL <= 0 {
		cfg.StartTTL = 45 * time.Second
	}
	if cfg.IdleRefreshInterval <= 0 {
		cfg.IdleRefreshInterval = 10 * time.Minute
	}
	if cfg.IdleRefreshTimeout <= 0 {
		cfg.IdleRefreshTimeout = 30 * time.Second
	}
	if cfg.MinIdle < 0 {
		cfg.MinIdle = 0
	}
	cfg.BaseEnv = append([]string{}, cfg.BaseEnv...)
	cfg.LaunchArgs = append([]string{}, cfg.LaunchArgs...)
	a.headlessRuntime = cfg
	a.mu.Lock()
	defer a.mu.Unlock()
	a.configureHeadlessRestoreHintsLocked(cfg.Paths.StateDir)
}

func (a *App) SetFinalBlockPreviewer(previewer feishu.FinalBlockPreviewService) {
	a.finalBlockPreviewer = previewer
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
	gatewayCtx, gatewayCancel := context.WithCancel(context.Background())
	gatewayDone := make(chan struct{})
	a.setGatewayRuntime(gatewayCancel, gatewayDone)
	go func() {
		defer close(gatewayDone)
		if err := a.gateway.Start(gatewayCtx, a.HandleAction); err != nil && err != context.Canceled {
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
