package daemon

import (
	"context"
	"log"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/adapter/relayws"
	"github.com/kxn/codex-remote-feishu/internal/app/adminauth"
	"github.com/kxn/codex-remote-feishu/internal/app/cronrepo"
	"github.com/kxn/codex-remote-feishu/internal/app/install"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/renderer"
	"github.com/kxn/codex-remote-feishu/internal/debuglog"
	"github.com/kxn/codex-remote-feishu/internal/externalaccess"
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

type ExternalAccessRuntimeConfig struct {
	Settings      externalAccessSettingsView
	CurrentBinary string
}

type ToolRuntimeConfig struct {
	ListenAddr string
	StateFile  string
}

type externalAccessSettingsView struct {
	ListenHost                 string
	ListenPort                 int
	DefaultLinkTTL             time.Duration
	DefaultSessionTTL          time.Duration
	ProviderKind               string
	ProviderLazyStart          bool
	TryCloudflareBinaryPath    string
	TryCloudflareLaunchTimeout time.Duration
	TryCloudflareMetricsPort   int
	TryCloudflareLogPath       string
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

type pendingThreadHistoryRead struct {
	SurfaceSessionID string
	InstanceID       string
	ThreadID         string
}

type vscodeCompatibilityCacheState struct {
	Checked         bool
	Issue           *vscodeCompatibilityIssue
	RefreshInFlight bool
	NextRetryAt     time.Time
	RefreshToken    uint64
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
	conversationTrace   conversationTracer

	relayServer *http.Server
	apiServer   *http.Server
	pprofServer *http.Server
	toolRuntime toolRuntimeState

	daemonStartedAt   time.Time
	daemonLifecycleID string
	shutdownStarted   bool
	shuttingDown      bool

	commandSeq         uint64
	mu                 sync.Mutex
	shutdownMu         sync.Mutex
	adminConfigMu      sync.Mutex
	adminFeishuMu      sync.RWMutex
	listenMu           sync.Mutex
	ingressRunMu       sync.Mutex
	relayConnMu        sync.Mutex
	feishuPermissionMu sync.RWMutex
	upgradeStateIOMu   sync.Mutex
	cronStateIOMu      sync.Mutex

	pendingGlobalRuntimeNotices     map[string][]control.UIEvent
	recentGlobalRuntimeNotices      map[string]map[string]time.Time
	headlessRuntime                 HeadlessRuntimeConfig
	vscodeDetect                    func() (vscodeDetectResponse, error)
	detectPlatformDefaults          func() (install.PlatformDefaults, error)
	vscodeCompatibility             vscodeCompatibilityCacheState
	managedHeadless                 map[string]*managedHeadlessProcess
	pendingThreadHistoryReads       map[string]pendingThreadHistoryRead
	gitWorkspaceImports             map[string]*gitWorkspaceImportRuntime
	startHeadless                   func(relayruntime.HeadlessLaunchOptions) (int, error)
	stopProcess                     func(int, time.Duration) error
	sendAgentCommand                func(string, agentproto.Command) error
	ingress                         *ingressPump
	ingressCancel                   context.CancelFunc
	ingressStarted                  bool
	ingressWG                       sync.WaitGroup
	gatewayRunCancel                context.CancelFunc
	gatewayRunDone                  chan struct{}
	relayConnections                map[string]*relayConnectionState
	feishuRuntimeApply              map[string]feishuRuntimeApplyPendingState
	feishuTimeSensitive             map[string]feishuTimeSensitiveState
	feishuPermissionGaps            map[string]map[string]*feishuPermissionGapRecord
	feishuPermissionRefreshEvery    time.Duration
	feishuPermissionNextRefresh     time.Time
	feishuPermissionRefreshInFlight bool
	feishuOnboarding                map[string]*feishuOnboardingSession
	feishuSetup                     feishuSetupClient
	cronLoaded                      bool
	cronSyncInFlight                bool
	cronState                       *cronStateFile
	cronRuns                        map[string]*cronRunState
	cronJobActiveRuns               map[string]map[string]struct{}
	cronExitTargets                 map[string]*cronExitTarget
	cronBitableFactory              func(string) (feishu.BitableAPI, error)
	cronGatewayIdentityLookup       func(string) (cronGatewayIdentity, bool, error)
	cronNextScheduleScan            time.Time
	cronRepoManager                 *cronrepo.Manager

	adminAuth                  *adminauth.Manager
	admin                      adminRuntimeState
	externalAccess             *externalaccess.Service
	externalAccessRuntime      ExternalAccessRuntimeConfig
	externalAccessShutdownWait chan struct{}
	webPreviewGrants           map[string]*previewGrantRecord
	surfaceResumeRuntime       surfaceResumeRuntimeState
	upgradeRuntime             upgradeRuntimeState

	relayListener          net.Listener
	apiListener            net.Listener
	pprofListener          net.Listener
	externalAccessListener net.Listener
	externalAccessServer   *http.Server

	shutdownGracePeriod      time.Duration
	shutdownNoticeTimeout    time.Duration
	gatewayStopTimeout       time.Duration
	shutdownDrainTimeout     time.Duration
	shutdownDrainPoll        time.Duration
	shutdownForceKillGrace   time.Duration
	gatewayApplyTimeout      time.Duration
	finalPreviewTimeout      time.Duration
	commandAnchorRecallDelay time.Duration
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
		service:                      orchestrator.NewService(time.Now, orchestrator.Config{TurnHandoffWait: 800 * time.Millisecond, GitAvailable: gitExecutableAvailable()}, renderer.NewPlanner()),
		projector:                    feishu.NewProjector(),
		gateway:                      gateway,
		serverIdentity:               serverIdentity,
		daemonStartedAt:              daemonStartedAt,
		daemonLifecycleID:            daemonLifecycleID(serverIdentity, daemonStartedAt),
		pendingGlobalRuntimeNotices:  map[string][]control.UIEvent{},
		recentGlobalRuntimeNotices:   map[string]map[string]time.Time{},
		surfaceResumeRuntime:         newSurfaceResumeRuntimeState(),
		upgradeRuntime:               newUpgradeRuntimeState(),
		managedHeadless:              map[string]*managedHeadlessProcess{},
		pendingThreadHistoryReads:    map[string]pendingThreadHistoryRead{},
		gitWorkspaceImports:          map[string]*gitWorkspaceImportRuntime{},
		startHeadless:                relayruntime.StartDetachedWrapper,
		stopProcess:                  relayruntime.TerminateProcess,
		ingress:                      newIngressPump(),
		relayConnections:             map[string]*relayConnectionState{},
		feishuRuntimeApply:           map[string]feishuRuntimeApplyPendingState{},
		feishuTimeSensitive:          map[string]feishuTimeSensitiveState{},
		feishuPermissionGaps:         map[string]map[string]*feishuPermissionGapRecord{},
		feishuPermissionRefreshEvery: defaultFeishuPermissionRefreshEvery,
		feishuOnboarding:             map[string]*feishuOnboardingSession{},
		feishuSetup:                  newLiveFeishuSetupClient(),
		cronRuns:                     map[string]*cronRunState{},
		cronJobActiveRuns:            map[string]map[string]struct{}{},
		cronExitTargets:              map[string]*cronExitTarget{},
		adminAuth:                    authManager,
		webPreviewGrants:             map[string]*previewGrantRecord{},
		shutdownGracePeriod:          5 * time.Second,
		shutdownNoticeTimeout:        2 * time.Second,
		gatewayStopTimeout:           3 * time.Second,
		shutdownDrainTimeout:         3 * time.Second,
		shutdownDrainPoll:            50 * time.Millisecond,
		shutdownForceKillGrace:       0,
		gatewayApplyTimeout:          30 * time.Second,
		finalPreviewTimeout:          90 * time.Second,
		commandAnchorRecallDelay:     8 * time.Second,
	}
	app.projector.SetSnapshotBinary(formatStatusSnapshotBinary(serverIdentity))
	app.upgradeRuntime.lookup = app.defaultReleaseLookup
	app.upgradeRuntime.devManifest = app.defaultDevManifestLookup
	app.cronBitableFactory = app.defaultCronBitableFactory
	app.cronGatewayIdentityLookup = app.defaultCronGatewayIdentityLookup
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

	app.apiServer = &http.Server{
		Addr: apiAddr,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.URL.Path == "/admin":
				http.Redirect(w, r, "/admin/", http.StatusFound)
				return
			case strings.HasPrefix(r.URL.Path, "/admin/"):
				app.adminPrefixMux(apiMux).ServeHTTP(w, r)
				return
			default:
				apiMux.ServeHTTP(w, r)
			}
		}),
	}
	return app
}

func gitExecutableAvailable() bool {
	_, err := exec.LookPath("git")
	return err == nil
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
	a.cronRepoManager = cronrepo.NewManager(cfg.Paths.StateDir)
	a.mu.Lock()
	defer a.mu.Unlock()
	a.configureSurfaceResumeStateLocked(cfg.Paths.StateDir)
	a.migrateLegacyHeadlessRestoreHintsLocked(cfg.Paths.StateDir)
	a.syncSurfaceResumeStateLocked(nil)
}

func (a *App) SetFinalBlockPreviewer(previewer feishu.FinalBlockPreviewService) {
	a.finalBlockPreviewer = previewer
	if configurable, ok := previewer.(feishu.WebPreviewConfigurable); ok {
		configurable.SetWebPreviewPublisher(daemonWebPreviewPublisher{app: a})
	}
}

func (a *App) SetDebugRelayFlow(enabled bool) {
	a.debugRelayFlow = enabled
}

func (a *App) SetRawLogger(logger *debuglog.RawLogger) {
	a.rawLogger = logger
	a.relay.SetRawLogger(logger)
}

func (a *App) SetConversationTrace(logger conversationTracer) {
	a.conversationTrace = logger
}

func (a *App) debugf(format string, args ...any) {
	if a.debugRelayFlow {
		log.Printf("relay flow daemon: "+format, args...)
	}
}

func (a *App) Bind() error {
	a.listenMu.Lock()
	defer a.listenMu.Unlock()

	var createdRelay bool
	if a.relayListener == nil {
		relayListener, err := net.Listen("tcp", a.relayServer.Addr)
		if err != nil {
			return err
		}
		a.relayListener = relayListener
		createdRelay = true
	}

	if a.apiListener == nil {
		apiListener, err := net.Listen("tcp", a.apiServer.Addr)
		if err != nil {
			if createdRelay {
				_ = a.relayListener.Close()
				a.relayListener = nil
			}
			return err
		}
		a.apiListener = apiListener
	}

	if err := a.bindToolListenerLocked(); err != nil {
		if createdRelay {
			_ = a.relayListener.Close()
			a.relayListener = nil
		}
		if a.apiListener != nil {
			_ = a.apiListener.Close()
			a.apiListener = nil
		}
		return err
	}

	a.bindPprofListenerLocked()
	return nil
}

func (a *App) Run(ctx context.Context) error {
	if err := a.Bind(); err != nil {
		return err
	}

	a.listenMu.Lock()
	relayListener := a.relayListener
	apiListener := a.apiListener
	pprofListener := a.pprofListener
	pprofServer := a.pprofServer
	toolListener := a.toolRuntime.listener
	toolServer := a.toolRuntime.server
	a.listenMu.Unlock()

	errCh := make(chan error, 4)
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
	if toolServer != nil && toolListener != nil {
		go func() {
			if err := toolServer.Serve(toolListener); err != nil && err != http.ErrServerClosed {
				errCh <- err
			}
		}()
	}
	if pprofServer != nil && pprofListener != nil {
		go func() {
			if err := pprofServer.Serve(pprofListener); err != nil && err != http.ErrServerClosed {
				log.Printf("pprof server stopped: %v", err)
			}
		}()
	}
	gatewayCtx, gatewayCancel := context.WithCancel(context.Background())
	gatewayDone := make(chan struct{})
	a.setGatewayRuntime(gatewayCancel, gatewayDone)
	go func() {
		defer close(gatewayDone)
		if err := a.gateway.Start(gatewayCtx, a.HandleGatewayAction); err != nil && err != context.Canceled {
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
