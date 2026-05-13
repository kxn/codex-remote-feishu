package daemon

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	previewpkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/preview"
	toolruntime "github.com/kxn/codex-remote-feishu/internal/app/daemon/toolruntime"
	"github.com/kxn/codex-remote-feishu/internal/app/install"
	"github.com/kxn/codex-remote-feishu/internal/codexstate"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/conversationtrace"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/debuglog"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
	"github.com/kxn/codex-remote-feishu/internal/shutdownctx"
)

type runnableDaemon interface {
	Bind() error
	Run(context.Context) error
	PprofURL() string
}

func RunMain(ctx context.Context, version, branch string) error {
	return RunMainWithArgs(ctx, nil, version, branch)
}

func RunMainWithArgs(ctx context.Context, args []string, version, branch string) error {
	if err := applyDaemonStartupArgs(args); err != nil {
		return err
	}
	loadedConfig, err := config.LoadAppConfig()
	if err != nil {
		return err
	}
	cfg, err := config.LoadServicesConfig()
	if err != nil {
		return err
	}
	capturedProxyEnv := config.CaptureProxyEnv()
	if !cfg.FeishuUseSystemProxy {
		capturedProxyEnv = config.CaptureAndClearProxyEnv()
	}

	paths, err := relayruntime.DefaultPaths()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(paths.LogsDir, 0o755); err != nil {
		return fmt.Errorf("create logs directory: %w", err)
	}
	logFile, err := os.OpenFile(paths.DaemonLogFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open daemon log file: %w", err)
	}
	defer logFile.Close()
	log.SetOutput(io.MultiWriter(os.Stderr, logFile))

	controller := feishu.NewMultiGatewayController()
	for _, app := range runtimeGatewayApps(loadedConfig.Config, cfg, paths) {
		if err := controller.UpsertApp(ctx, app); err != nil {
			return err
		}
	}
	var gateway feishu.Gateway = controller
	var finalBlockPreviewer previewpkg.FinalBlockPreviewService = controller
	lock, err := relayruntime.AcquireLock(ctx, paths.DaemonLockFile, false)
	if err != nil {
		return fmt.Errorf("acquire service runtime lock: %w", err)
	}
	defer lock.Release()

	startedAt := time.Now().UTC()
	identity, err := relayruntime.NewServerIdentityWithBranch(version, branch, cfg.ConfigPath, startedAt)
	if err != nil {
		return err
	}
	if err := relayruntime.WritePID(paths.PIDFile, identity.PID); err != nil {
		return err
	}
	defer os.Remove(paths.PIDFile)
	if err := relayruntime.WriteServerIdentity(paths.IdentityFile, identity); err != nil {
		return err
	}
	defer os.Remove(paths.IdentityFile)
	repairInstallStateOnStartup(paths, identity)

	env := envMap(os.Environ())
	startup := buildStartupAccessPlan(loadedConfig, cfg, identity.BinaryPath, env)
	envOverrideActive := (strings.TrimSpace(os.Getenv("FEISHU_APP_ID")) != "" || strings.TrimSpace(os.Getenv("FEISHU_APP_SECRET")) != "") &&
		strings.TrimSpace(cfg.FeishuGatewayID) != ""

	app := New(
		net.JoinHostPort(cfg.RelayHost, cfg.RelayPort),
		net.JoinHostPort(startup.AdminBindHost, cfg.RelayAPIPort),
		gateway,
		identity,
	)
	baseEnv := buildDaemonHeadlessBaseEnv(os.Environ(), capturedProxyEnv)
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		BinaryPath: identity.BinaryPath,
		ConfigPath: cfg.ConfigPath,
		BaseEnv:    baseEnv,
		Paths:      paths,
		MinIdle:    1,
	})
	app.SetToolRuntime(toolruntime.Config{
		ListenAddr: net.JoinHostPort(loadedConfig.Config.Tool.ListenHost, strconv.Itoa(loadedConfig.Config.Tool.ListenPort)),
		StateFile:  paths.ToolServiceFile,
	})
	app.SetFinalBlockPreviewer(finalBlockPreviewer)
	app.SetDebugRelayFlow(cfg.DebugRelayFlow)
	app.SetExternalAccess(ExternalAccessRuntimeConfig{
		Settings:      externalAccessSettingsViewFromConfig(loadedConfig.Config.ExternalAccess),
		CurrentBinary: identity.BinaryPath,
	})
	if catalog, err := newDaemonPersistedThreadCatalog(log.Printf); err != nil {
		log.Printf("persisted thread catalog disabled: %v", err)
	} else if catalog != nil {
		app.service.SetPersistedThreadCatalog(catalog)
		if catalog.codex != nil {
			if storage, err := codexstate.NewDefaultTurnPatchStorage(codexstate.TurnPatchStorageOptions{
				SQLiteCatalog: catalog.codex,
				Logf:          log.Printf,
			}); err != nil {
				log.Printf("turn patch storage disabled: %v", err)
			} else if storage != nil {
				app.SetTurnPatchStorage(storage)
			}
		}
	}
	app.ConfigureAdmin(AdminRuntimeOptions{
		ConfigPath:           loadedConfig.Path,
		Services:             cfg,
		AdminListenHost:      startup.AdminBindHost,
		AdminListenPort:      cfg.RelayAPIPort,
		AdminURL:             startup.AdminURL,
		SetupURL:             startup.SetupURL,
		SSHSession:           startup.SSHSession,
		SetupRequired:        startup.SetupRequired,
		EnvOverrideActive:    envOverrideActive,
		EnvOverrideGatewayID: cfg.FeishuGatewayID,
	})
	app.ConfigurePprof(pprofBindAddrForDebugSettings(loadedConfig.Config.Debug))
	if cfg.DebugRelayRaw {
		rawLogger, err := debuglog.OpenRaw(paths.DaemonRawLogFile, "daemon", "", os.Getpid())
		if err != nil {
			log.Printf("relay raw daemon log disabled: %v", err)
		} else {
			app.SetRawLogger(rawLogger)
		}
	}
	traceLogger, err := conversationtrace.Open(filepath.Join(paths.LogsDir, "codex-remote-conversation-trace.ndjson"))
	if err != nil {
		log.Printf("conversation trace disabled: %v", err)
	} else {
		app.SetConversationTrace(traceLogger)
	}
	if startup.SetupRequired {
		token, expiresAt, err := app.EnableSetupAccess(20 * time.Minute)
		if err != nil {
			return err
		}
		startup.SetupToken = token
		startup.SetupTokenExpiry = expiresAt
	}
	_ = shutdownctx.SetConsoleCloseHandler(ctx, func() {
		_ = app.shutdownForConsoleClose()
	})
	return runConfiguredDaemon(ctx, app, startup, cfg, env)
}

func applyDaemonStartupArgs(args []string) error {
	flagSet := flag.NewFlagSet("daemon", flag.ContinueOnError)
	flagSet.SetOutput(io.Discard)
	configPath := flagSet.String("config", "", "config path")
	xdgConfigHome := flagSet.String("xdg-config-home", "", "XDG config home")
	xdgDataHome := flagSet.String("xdg-data-home", "", "XDG data home")
	xdgStateHome := flagSet.String("xdg-state-home", "", "XDG state home")
	if err := flagSet.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if flagSet.NArg() > 0 {
		return fmt.Errorf("unsupported daemon arguments: %s", strings.Join(flagSet.Args(), " "))
	}
	if value := strings.TrimSpace(*configPath); value != "" {
		if err := os.Setenv(config.UnifiedConfigEnvPath, value); err != nil {
			return err
		}
	}
	if value := strings.TrimSpace(*xdgConfigHome); value != "" {
		if err := os.Setenv("XDG_CONFIG_HOME", value); err != nil {
			return err
		}
	}
	if value := strings.TrimSpace(*xdgDataHome); value != "" {
		if err := os.Setenv("XDG_DATA_HOME", value); err != nil {
			return err
		}
	}
	if value := strings.TrimSpace(*xdgStateHome); value != "" {
		if err := os.Setenv("XDG_STATE_HOME", value); err != nil {
			return err
		}
	}
	return nil
}

func buildDaemonHeadlessBaseEnv(currentEnv, proxyEnv []string) []string {
	baseEnv := config.BuildCodexChildEnv(currentEnv, proxyEnv, nil)
	if resolved, err := config.ResolveClaudeBinary(baseEnv); err == nil && strings.TrimSpace(resolved) != "" {
		baseEnv = config.UpsertEnvValue(baseEnv, config.ClaudeBinaryEnv, resolved)
	}
	return baseEnv
}

func runConfiguredDaemon(ctx context.Context, app runnableDaemon, startup startupAccessPlan, services config.ServicesConfig, env map[string]string) error {
	if err := app.Bind(); err != nil {
		return fmt.Errorf("bind service listeners: %w", err)
	}
	logStartupState(startup, services, app.PprofURL())
	if err := maybeOpenSetupBrowser(startup, env); err != nil {
		switch {
		case err == errBrowserUnavailable:
			log.Printf("setup browser auto-open skipped: no local desktop opener available")
		default:
			log.Printf("setup browser auto-open failed: %v", err)
		}
	}
	if err := app.Run(ctx); err != nil && err != context.Canceled {
		return fmt.Errorf("run service: %w", err)
	}
	return nil
}

func repairInstallStateOnStartup(paths relayruntime.Paths, identity agentproto.ServerIdentity) {
	statePath := filepath.Join(paths.DataDir, "install-state.json")
	state, err := loadInstallStateIfPresent(statePath)
	if err != nil || state == nil {
		return
	}
	state.StatePath = statePath
	if !install.RepairRuntimeState(state, install.RuntimeStateRepairOptions{
		CurrentBinaryPath: identity.BinaryPath,
		CurrentVersion:    identity.Version,
		ConfigPath:        identity.ConfigPath,
		PID:               identity.PID,
	}) {
		return
	}
	if err := install.WriteState(statePath, *state); err != nil {
		log.Printf("startup install-state repair skipped: %v", err)
	}
}

func runtimeGatewayApps(appConfig config.AppConfig, services config.ServicesConfig, paths relayruntime.Paths) []feishu.GatewayAppConfig {
	runtimeApps := make([]config.FeishuAppConfig, 0, len(appConfig.Feishu.Apps))
	for _, app := range appConfig.Feishu.Apps {
		if strings.TrimSpace(app.ID) == "" {
			continue
		}
		runtimeApps = append(runtimeApps, app)
	}

	if strings.TrimSpace(services.FeishuAppID) != "" || strings.TrimSpace(services.FeishuAppSecret) != "" {
		gatewayID := strings.TrimSpace(services.FeishuGatewayID)
		if gatewayID == "" {
			goto build
		}
		found := false
		for i := range runtimeApps {
			currentID := strings.TrimSpace(runtimeApps[i].ID)
			if currentID != gatewayID {
				continue
			}
			runtimeApps[i].ID = gatewayID
			runtimeApps[i].AppID = services.FeishuAppID
			runtimeApps[i].AppSecret = services.FeishuAppSecret
			enabled := true
			runtimeApps[i].Enabled = &enabled
			found = true
			break
		}
		if !found {
			enabled := true
			runtimeApps = append(runtimeApps, config.FeishuAppConfig{
				ID:        gatewayID,
				Name:      "Runtime Override",
				AppID:     services.FeishuAppID,
				AppSecret: services.FeishuAppSecret,
				Enabled:   &enabled,
			})
		}
	}

build:
	values := make([]feishu.GatewayAppConfig, 0, len(runtimeApps))
	for _, app := range runtimeApps {
		gatewayID := strings.TrimSpace(app.ID)
		if gatewayID == "" {
			continue
		}
		enabled := app.Enabled == nil || *app.Enabled
		values = append(values, feishu.GatewayAppConfig{
			GatewayID:             gatewayID,
			Name:                  strings.TrimSpace(app.Name),
			AppID:                 strings.TrimSpace(app.AppID),
			AppSecret:             strings.TrimSpace(app.AppSecret),
			Enabled:               enabled,
			UseSystemProxy:        services.FeishuUseSystemProxy,
			ImageTempDir:          filepath.Join(paths.StateDir, "image-staging", sanitizeGatewayPath(gatewayID)),
			PreviewStatePath:      filepath.Join(paths.StateDir, "feishu-md-preview-"+sanitizeGatewayPath(gatewayID)+".json"),
			PreviewCacheDir:       filepath.Join(paths.DataDir, "preview-cache", sanitizeGatewayPath(gatewayID)),
			PreviewRootFolderName: strings.TrimSpace(appConfig.Storage.PreviewRootFolderName),
		})
	}
	return values
}

func sanitizeGatewayPath(gatewayID string) string {
	value := strings.NewReplacer(":", "-", "/", "-", "\\", "-", " ", "-").Replace(strings.TrimSpace(gatewayID))
	value = strings.Trim(value, "-")
	if value == "" {
		return "missing-gateway"
	}
	return value
}

func logStartupState(startup startupAccessPlan, services config.ServicesConfig, pprofURL string) {
	relayEndpoint := net.JoinHostPort(strings.TrimSpace(services.RelayHost), strings.TrimSpace(services.RelayPort))
	log.Printf("relay daemon listening: relay=%s admin=%s", relayEndpoint, startup.AdminURL)
	if strings.TrimSpace(pprofURL) != "" {
		log.Printf("pprof (local only): %s", pprofURL)
	}
	if startup.SetupRequired {
		setupURL := startup.SetupURL
		if startup.SSHSession && strings.TrimSpace(startup.SetupToken) != "" {
			setupURL += "?token=" + url.QueryEscape(startup.SetupToken)
		}
		log.Printf("startup state: setup required; configured_feishu_apps=%d ssh=%t", startup.ConfiguredAppCount, startup.SSHSession)
		log.Printf("web setup: %s", setupURL)
		if !startup.SetupTokenExpiry.IsZero() {
			log.Printf("setup token expires at: %s", startup.SetupTokenExpiry.Format(time.RFC3339))
		}
		return
	}

	phase := "ready"
	if startup.ConfiguredAppCount == 0 {
		phase = "ready_degraded"
	}
	log.Printf("startup state: %s; configured_feishu_apps=%d admin=%s", phase, startup.ConfiguredAppCount, startup.AdminURL)
	log.Printf("web admin: %s", startup.AdminURL)
}
