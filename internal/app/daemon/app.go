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
	previewpkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/preview"
	"github.com/kxn/codex-remote-feishu/internal/adapter/relayws"
	"github.com/kxn/codex-remote-feishu/internal/app/adminauth"
	"github.com/kxn/codex-remote-feishu/internal/app/codexupgrade"
	"github.com/kxn/codex-remote-feishu/internal/app/cronrepo"
	codexupgraderuntime "github.com/kxn/codex-remote-feishu/internal/app/daemon/codexupgraderuntime"
	headlessruntime "github.com/kxn/codex-remote-feishu/internal/app/daemon/headlessruntime"
	toolruntime "github.com/kxn/codex-remote-feishu/internal/app/daemon/toolruntime"
	turnpatchruntime "github.com/kxn/codex-remote-feishu/internal/app/daemon/turnpatchruntime"
	upgraderuntime "github.com/kxn/codex-remote-feishu/internal/app/daemon/upgraderuntime"
	"github.com/kxn/codex-remote-feishu/internal/app/install"
	"github.com/kxn/codex-remote-feishu/internal/codexstate"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
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
	finalBlockPreviewer previewpkg.FinalBlockPreviewService
	relay               *relayws.Server
	serverIdentity      agentproto.ServerIdentity
	debugRelayFlow      bool
	rawLogger           *debuglog.RawLogger
	conversationTrace   conversationTracer

	relayServer *http.Server
	apiServer   *http.Server
	pprofServer *http.Server
	toolRuntime toolruntime.State

	daemonStartedAt   time.Time
	daemonLifecycleID string
	shutdownStarted   bool
	shuttingDown      bool

	commandSeq       uint64
	mu               sync.Mutex
	shutdownMu       sync.Mutex
	adminConfigMu    sync.Mutex
	listenMu         sync.Mutex
	ingressRunMu     sync.Mutex
	relayConnMu      sync.Mutex
	upgradeStateIOMu sync.Mutex

	pendingGlobalRuntimeNotices map[string][]eventcontract.Event
	recentGlobalRuntimeNotices  map[string]map[string]time.Time
	headlessRuntime             HeadlessRuntimeConfig
	vscodeDetect                func() (vscodeDetectResponse, error)
	detectPlatformDefaults      func() (install.PlatformDefaults, error)
	vscodeCompatibility         vscodeCompatibilityCacheState
	managedHeadlessRuntime      headlessruntime.State
	pendingThreadHistoryReads   map[string]pendingThreadHistoryRead
	childRestartWaiters         map[string]*childRestartWaiter
	gitWorkspaceImports         map[string]*gitWorkspaceImportRuntime
	gitWorkspaceWorktrees       map[string]*gitWorkspaceWorktreeRuntime
	startHeadless               func(relayruntime.HeadlessLaunchOptions) (int, error)
	stopProcess                 func(int, time.Duration) error
	sendAgentCommand            func(string, agentproto.Command) error
	ingress                     *ingressPump
	ingressCancel               context.CancelFunc
	ingressStarted              bool
	ingressWG                   sync.WaitGroup
	gatewayRunCancel            context.CancelFunc
	gatewayRunDone              chan struct{}
	relayConnections            map[string]*relayConnectionState
	feishuRuntime               feishuRuntimeState
	cronRuntime                 cronRuntimeState
	claudeWorkspaceProfileState claudeWorkspaceProfileRuntimeState

	adminAuth                  *adminauth.Manager
	admin                      adminRuntimeState
	externalAccess             *externalaccess.Service
	externalAccessRuntime      ExternalAccessRuntimeConfig
	externalAccessShutdownWait chan struct{}
	webPreviewGrants           map[string]*previewGrantRecord
	surfaceResumeRuntime       surfaceResumeRuntimeState
	codexUpgradeRuntime        codexupgraderuntime.State
	upgradeRuntime             upgraderuntime.State
	turnPatchRuntime           turnpatchruntime.State

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
		service:                     orchestrator.NewService(time.Now, orchestrator.Config{TurnHandoffWait: 800 * time.Millisecond, GitAvailable: gitExecutableAvailable()}, renderer.NewPlanner()),
		projector:                   feishu.NewProjector(),
		gateway:                     gateway,
		serverIdentity:              serverIdentity,
		daemonStartedAt:             daemonStartedAt,
		daemonLifecycleID:           daemonLifecycleID(serverIdentity, daemonStartedAt),
		pendingGlobalRuntimeNotices: map[string][]eventcontract.Event{},
		recentGlobalRuntimeNotices:  map[string]map[string]time.Time{},
		managedHeadlessRuntime:      headlessruntime.NewState(),
		claudeWorkspaceProfileState: claudeWorkspaceProfileRuntimeState{},
		surfaceResumeRuntime:        newSurfaceResumeRuntimeState(),
		childRestartWaiters:         map[string]*childRestartWaiter{},
		codexUpgradeRuntime:         codexupgraderuntime.NewState(),
		upgradeRuntime:              upgraderuntime.NewState(),
		turnPatchRuntime:            turnpatchruntime.NewState(),
		cronRuntime:                 newCronRuntimeState(),
		feishuRuntime:               newFeishuRuntimeState(),
		pendingThreadHistoryReads:   map[string]pendingThreadHistoryRead{},
		gitWorkspaceImports:         map[string]*gitWorkspaceImportRuntime{},
		gitWorkspaceWorktrees:       map[string]*gitWorkspaceWorktreeRuntime{},
		startHeadless:               relayruntime.StartDetachedWrapper,
		stopProcess:                 relayruntime.TerminateProcess,
		ingress:                     newIngressPump(),
		relayConnections:            map[string]*relayConnectionState{},
		adminAuth:                   authManager,
		webPreviewGrants:            map[string]*previewGrantRecord{},
		shutdownGracePeriod:         5 * time.Second,
		shutdownNoticeTimeout:       2 * time.Second,
		gatewayStopTimeout:          3 * time.Second,
		shutdownDrainTimeout:        3 * time.Second,
		shutdownDrainPoll:           50 * time.Millisecond,
		shutdownForceKillGrace:      0,
		gatewayApplyTimeout:         30 * time.Second,
		finalPreviewTimeout:         90 * time.Second,
		commandAnchorRecallDelay:    8 * time.Second,
	}
	app.codexUpgradeRuntime.Inspect = func(ctx context.Context, opts codexupgrade.InspectOptions) (codexupgrade.Installation, error) {
		return codexupgrade.Inspect(ctx, opts), nil
	}
	app.codexUpgradeRuntime.LatestLookup = func(ctx context.Context) (string, error) {
		return codexupgrade.LookupLatestVersion(ctx, codexupgrade.LatestVersionOptions{})
	}
	app.codexUpgradeRuntime.Install = func(ctx context.Context, installation codexupgrade.Installation, version string) error {
		return codexupgrade.InstallGlobal(ctx, version, codexupgrade.InstallOptions{
			NPMCommand: installation.NPMCommand,
		})
	}
	app.projector.SetSnapshotBinary(formatStatusSnapshotBinary(serverIdentity))
	app.projector.SetMenuHomeVersion(serverIdentity.Version)
	app.upgradeRuntime.Lookup = app.defaultReleaseLookup
	app.upgradeRuntime.DevManifest = app.defaultDevManifestLookup
	app.cronRuntime.bitableFactory = app.defaultCronBitableFactory
	app.cronRuntime.gatewayIdentityLookup = app.defaultCronGatewayIdentityLookup
	app.feishuRuntime.setup = newLiveFeishuSetupClient()
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

func (a *App) SetTurnPatchStorage(storage *codexstate.TurnPatchStorage) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.turnPatchRuntime.Storage = storage
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
	a.cronRuntime.repoManager = cronrepo.NewManager(cfg.Paths.StateDir)
	a.mu.Lock()
	defer a.mu.Unlock()
	a.configureClaudeWorkspaceProfileStateLocked(cfg.Paths.StateDir)
	a.configureSurfaceResumeStateLocked(cfg.Paths.StateDir)
	a.syncClaudeWorkspaceProfileStateLocked()
	a.syncSurfaceResumeStateLocked(nil)
}

func (a *App) SetToolRuntime(cfg toolruntime.Config) {
	a.toolRuntime.Configure(cfg, a.newToolRuntimeHandler())
}

func (a *App) SetFinalBlockPreviewer(previewer previewpkg.FinalBlockPreviewService) {
	a.finalBlockPreviewer = previewer
	if configurable, ok := previewer.(previewpkg.WebPreviewConfigurable); ok {
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

	if err := a.toolRuntime.BindLocked(); err != nil {
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
	toolListener := a.toolRuntime.Listener
	toolServer := a.toolRuntime.Server
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
