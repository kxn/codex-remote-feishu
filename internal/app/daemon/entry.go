package daemon

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/debuglog"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func RunMain(ctx context.Context, version string) error {
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

	var gateway feishu.Gateway = feishu.NopGateway{}
	var markdownPreviewer feishu.MarkdownPreviewService
	if cfg.FeishuAppID != "" && cfg.FeishuAppSecret != "" {
		liveGateway := feishu.NewLiveGateway(feishu.LiveGatewayConfig{
			GatewayID:      cfg.FeishuGatewayID,
			AppID:          cfg.FeishuAppID,
			AppSecret:      cfg.FeishuAppSecret,
			TempDir:        os.TempDir(),
			UseSystemProxy: cfg.FeishuUseSystemProxy,
		})
		gateway = liveGateway
		markdownPreviewer = feishu.NewDriveMarkdownPreviewer(
			feishu.NewLarkDrivePreviewAPI(liveGateway.Client()),
			feishu.MarkdownPreviewConfig{
				StatePath: filepath.Join(paths.StateDir, "feishu-md-preview.json"),
			},
		)
	}
	lock, err := relayruntime.AcquireLock(ctx, paths.DaemonLockFile, false)
	if err != nil {
		return fmt.Errorf("acquire daemon runtime lock: %w", err)
	}
	defer lock.Release()

	startedAt := time.Now().UTC()
	identity, err := relayruntime.NewServerIdentity(version, cfg.ConfigPath, startedAt)
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

	app := New(
		net.JoinHostPort(cfg.RelayHost, cfg.RelayPort),
		net.JoinHostPort(cfg.RelayAPIHost, cfg.RelayAPIPort),
		gateway,
		identity,
	)
	baseEnv := config.FilterEnvWithoutProxy(os.Environ())
	baseEnv = append(baseEnv, capturedProxyEnv...)
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		BinaryPath: identity.BinaryPath,
		ConfigPath: cfg.ConfigPath,
		BaseEnv:    baseEnv,
		Paths:      paths,
	})
	app.SetMarkdownPreviewer(markdownPreviewer)
	app.SetDebugRelayFlow(cfg.DebugRelayFlow)
	if cfg.DebugRelayRaw {
		rawLogger, err := debuglog.OpenRaw(paths.DaemonRawLogFile, "daemon", "", os.Getpid())
		if err != nil {
			log.Printf("relay raw daemon log disabled: %v", err)
		} else {
			app.SetRawLogger(rawLogger)
		}
	}
	if err := app.Run(ctx); err != nil && err != context.Canceled {
		return fmt.Errorf("run daemon: %w", err)
	}
	return nil
}
