package externalaccess

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/execlaunch"
	"github.com/kxn/codex-remote-feishu/internal/externalaccess/cloudflaredembed"
)

var tryCloudflareURLPattern = regexp.MustCompile(`https://[a-zA-Z0-9.-]+\.trycloudflare\.com`)

const defaultTryCloudflareLaunchTimeout = 60 * time.Second

type TryCloudflareOptions struct {
	BinaryPath          string
	CurrentBinary       string
	LaunchTimeout       time.Duration
	MetricsPort         int
	LogPath             string
	Now                 func() time.Time
	WaitReady           func(context.Context, int) error
	CommandFactory      func(context.Context, string, ...string) *exec.Cmd
	EnsureBundledBinary func(string) (string, bool, error)
}

type TryCloudflareProvider struct {
	now                 func() time.Time
	binaryPath          string
	currentBinary       string
	launchTimeout       time.Duration
	metricsPort         int
	logPath             string
	waitReady           func(context.Context, int) error
	commandFactory      func(context.Context, string, ...string) *exec.Cmd
	ensureBundledBinary func(string) (string, bool, error)

	mu             sync.Mutex
	cmd            *exec.Cmd
	cmdCancel      context.CancelFunc
	configDir      string
	readyPort      int
	publicBase     PublicBase
	boundTargetURL string
	lastError      string
	startWait      chan struct{}
	doneCh         chan struct{}
	stopping       bool
}

type tryCloudflareRuntime struct {
	cmd    *exec.Cmd
	cancel context.CancelFunc
	doneCh chan struct{}
}

func NewTryCloudflareProvider(opts TryCloudflareOptions) *TryCloudflareProvider {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	factory := opts.CommandFactory
	if factory == nil {
		factory = execlaunch.CommandContext
	}
	waitReadyFn := opts.WaitReady
	if waitReadyFn == nil {
		waitReadyFn = waitReady
	}
	ensureBundledBinary := opts.EnsureBundledBinary
	if ensureBundledBinary == nil {
		ensureBundledBinary = cloudflaredembed.EnsureSibling
	}
	if opts.LaunchTimeout <= 0 {
		opts.LaunchTimeout = defaultTryCloudflareLaunchTimeout
	}
	return &TryCloudflareProvider{
		now:                 now,
		binaryPath:          strings.TrimSpace(opts.BinaryPath),
		currentBinary:       strings.TrimSpace(opts.CurrentBinary),
		launchTimeout:       opts.LaunchTimeout,
		metricsPort:         opts.MetricsPort,
		logPath:             strings.TrimSpace(opts.LogPath),
		waitReady:           waitReadyFn,
		commandFactory:      factory,
		ensureBundledBinary: ensureBundledBinary,
	}
}

func (p *TryCloudflareProvider) Kind() string { return "trycloudflare" }

func (p *TryCloudflareProvider) Snapshot() ProviderStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	status := ProviderStatus{
		Kind:      p.Kind(),
		BaseURL:   p.publicBase.BaseURL,
		StartedAt: p.publicBase.StartedAt,
		Ready:     p.cmd != nil && p.cmd.Process != nil && p.doneCh != nil,
		LastError: p.lastError,
	}
	return status
}

func (p *TryCloudflareProvider) EnsurePublicBase(ctx context.Context, localListenerURL string) (PublicBase, error) {
	localListenerURL = strings.TrimSpace(localListenerURL)
	for {
		p.mu.Lock()
		if base, readyPort, reuseCandidate := p.reuseCandidateLocked(localListenerURL); reuseCandidate {
			p.mu.Unlock()
			probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			err := p.waitReady(probeCtx, readyPort)
			cancel()
			if err == nil {
				return base, nil
			}
			if ctx.Err() != nil {
				p.recordError(fmt.Errorf("probe trycloudflare provider: %w", err))
				return PublicBase{}, err
			}
			p.recordError(fmt.Errorf("probe trycloudflare provider: %w", err))
			p.mu.Lock()
			if waitCh := p.startWait; waitCh != nil {
				p.mu.Unlock()
				select {
				case <-ctx.Done():
					err := fmt.Errorf("restart trycloudflare provider: %w", ctx.Err())
					p.recordError(err)
					return PublicBase{}, err
				case <-waitCh:
					continue
				}
			}
			if err := p.restartLocked(ctx, localListenerURL); err != nil {
				return PublicBase{}, err
			}
			continue
		}
		if p.runtimeTargetMismatchLocked(localListenerURL) {
			p.lastError = ""
			if err := p.restartLocked(ctx, localListenerURL); err != nil {
				return PublicBase{}, err
			}
			continue
		}
		if waitCh := p.startWait; waitCh != nil {
			p.mu.Unlock()
			select {
			case <-ctx.Done():
				err := fmt.Errorf("start trycloudflare provider: %w", ctx.Err())
				p.recordError(err)
				return PublicBase{}, err
			case <-waitCh:
				continue
			}
		}
		base, err := p.startLocked(ctx, localListenerURL)
		if err == nil {
			return base, nil
		}
		return PublicBase{}, err
	}
}

func (p *TryCloudflareProvider) startPublicBase(ctx context.Context, localListenerURL string) (PublicBase, *exec.Cmd, context.CancelFunc, string, int, error) {
	launchCtx, cancel := context.WithTimeout(ctx, p.launchTimeout)
	defer cancel()

	binaryPath, err := p.resolveBinaryPath()
	if err != nil {
		return PublicBase{}, nil, nil, "", 0, err
	}
	metricsAddr, metricsPort, err := chooseMetricsAddr(p.metricsPort)
	if err != nil {
		return PublicBase{}, nil, nil, "", 0, err
	}
	configDir, err := os.MkdirTemp("", "codex-remote-cloudflared-*")
	if err != nil {
		return PublicBase{}, nil, nil, "", 0, err
	}

	args := []string{
		"tunnel",
		"--url", localListenerURL,
		"--no-autoupdate",
		"--metrics", metricsAddr,
	}
	procCtx, procCancel := context.WithCancel(context.Background())
	cmd := execlaunch.Prepare(p.commandFactory(procCtx, binaryPath, args...))
	cmd.Env = append(os.Environ(),
		"HOME="+configDir,
		"XDG_CONFIG_HOME="+configDir,
	)
	var logWriter io.WriteCloser = ioDiscardCloser{}
	if p.logPath != "" {
		if err := os.MkdirAll(filepath.Dir(p.logPath), 0o755); err == nil {
			if file, openErr := os.OpenFile(p.logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); openErr == nil {
				logWriter = ioFileCloser{File: file}
			}
		}
	}
	defer logWriter.Close()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		procCancel()
		_ = os.RemoveAll(configDir)
		return PublicBase{}, nil, nil, "", 0, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		procCancel()
		_ = os.RemoveAll(configDir)
		return PublicBase{}, nil, nil, "", 0, err
	}
	urlCh := make(chan string, 2)
	pump := func(reader io.Reader) {
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			line := scanner.Text()
			_, _ = logWriter.Write([]byte(line + "\n"))
			if match := tryCloudflareURLPattern.FindString(line); match != "" {
				select {
				case urlCh <- match:
				default:
				}
			}
		}
	}

	if err := cmd.Start(); err != nil {
		procCancel()
		_ = os.RemoveAll(configDir)
		return PublicBase{}, nil, nil, "", 0, err
	}
	go pump(stdout)
	go pump(stderr)

	var publicURL string
	for {
		select {
		case <-launchCtx.Done():
			procCancel()
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			_ = cmd.Wait()
			_ = os.RemoveAll(configDir)
			return PublicBase{}, nil, nil, "", 0, fmt.Errorf("start trycloudflare provider: %w", launchCtx.Err())
		case candidate := <-urlCh:
			if candidate == "" {
				continue
			}
			if err := p.waitReady(launchCtx, metricsPort); err != nil {
				procCancel()
				if cmd.Process != nil {
					_ = cmd.Process.Kill()
				}
				_ = cmd.Wait()
				_ = os.RemoveAll(configDir)
				return PublicBase{}, nil, nil, "", 0, err
			}
			publicURL = candidate
		}
		if publicURL != "" {
			break
		}
	}

	base := PublicBase{BaseURL: publicURL, StartedAt: p.now().UTC()}
	return base, cmd, procCancel, configDir, metricsPort, nil
}

func (p *TryCloudflareProvider) Close() error {
	p.mu.Lock()
	waitCh := p.startWait
	if waitCh != nil {
		p.mu.Unlock()
		<-waitCh
		p.mu.Lock()
	}
	p.stopping = true
	runtime := p.detachRuntimeLocked(true)
	p.mu.Unlock()

	err := p.closeDetachedRuntime(runtime)

	p.mu.Lock()
	if p.cmd == nil {
		p.lastError = ""
	}
	p.stopping = false
	p.mu.Unlock()
	return err
}

func (p *TryCloudflareProvider) watchCommand(cmd *exec.Cmd, configDir string, doneCh chan struct{}) {
	err := cmd.Wait()
	_ = os.RemoveAll(configDir)

	p.mu.Lock()
	if p.cmd == cmd {
		stopping := p.stopping
		p.cmd = nil
		p.cmdCancel = nil
		p.configDir = ""
		p.readyPort = 0
		p.publicBase = PublicBase{}
		p.boundTargetURL = ""
		p.doneCh = nil
		if stopping {
			p.lastError = ""
		} else if err != nil {
			p.lastError = fmt.Sprintf("trycloudflare exited: %v", err)
		} else {
			p.lastError = "trycloudflare exited unexpectedly"
		}
		p.stopping = false
	}
	p.mu.Unlock()

	close(doneCh)
}

func (p *TryCloudflareProvider) reuseCandidateLocked(localListenerURL string) (PublicBase, int, bool) {
	if p.cmd == nil || p.cmd.Process == nil || p.doneCh == nil || strings.TrimSpace(p.publicBase.BaseURL) == "" {
		return PublicBase{}, 0, false
	}
	if p.runtimeTargetMismatchLocked(localListenerURL) || p.readyPort <= 0 {
		return PublicBase{}, 0, false
	}
	return p.publicBase, p.readyPort, true
}

func (p *TryCloudflareProvider) runtimeTargetMismatchLocked(localListenerURL string) bool {
	if p.cmd == nil || p.cmd.Process == nil || p.doneCh == nil || strings.TrimSpace(p.publicBase.BaseURL) == "" {
		return false
	}
	return strings.TrimSpace(p.boundTargetURL) != strings.TrimSpace(localListenerURL)
}

func (p *TryCloudflareProvider) startLocked(ctx context.Context, localListenerURL string) (PublicBase, error) {
	p.startWait = make(chan struct{})
	waitCh := p.startWait
	p.mu.Unlock()

	base, cmd, cmdCancel, configDir, readyPort, err := p.startPublicBase(ctx, localListenerURL)
	var doneCh chan struct{}
	if err == nil {
		doneCh = make(chan struct{})
	}

	p.mu.Lock()
	if err == nil {
		p.cmd = cmd
		p.cmdCancel = cmdCancel
		p.configDir = configDir
		p.readyPort = readyPort
		p.publicBase = base
		p.boundTargetURL = localListenerURL
		p.doneCh = doneCh
		p.stopping = false
		p.lastError = ""
	} else {
		p.lastError = err.Error()
	}
	close(waitCh)
	p.startWait = nil
	p.mu.Unlock()

	if err == nil {
		go p.watchCommand(cmd, configDir, doneCh)
		return base, nil
	}
	return PublicBase{}, err
}

func (p *TryCloudflareProvider) restartLocked(ctx context.Context, localListenerURL string) error {
	p.startWait = make(chan struct{})
	waitCh := p.startWait
	runtime := p.detachRuntimeLocked(false)
	p.mu.Unlock()

	closeErr := p.closeDetachedRuntime(runtime)
	base, cmd, cmdCancel, configDir, readyPort, err := p.startPublicBase(ctx, localListenerURL)
	var doneCh chan struct{}
	if err == nil {
		doneCh = make(chan struct{})
	}

	p.mu.Lock()
	if err == nil {
		p.cmd = cmd
		p.cmdCancel = cmdCancel
		p.configDir = configDir
		p.readyPort = readyPort
		p.publicBase = base
		p.boundTargetURL = localListenerURL
		p.doneCh = doneCh
		p.stopping = false
		p.lastError = ""
	} else if closeErr != nil {
		p.lastError = errors.Join(closeErr, err).Error()
	} else {
		p.lastError = err.Error()
	}
	close(waitCh)
	p.startWait = nil
	p.mu.Unlock()

	if err == nil {
		go p.watchCommand(cmd, configDir, doneCh)
		return nil
	}
	if closeErr != nil {
		return errors.Join(closeErr, err)
	}
	return err
}

func (p *TryCloudflareProvider) detachRuntimeLocked(clearLastError bool) tryCloudflareRuntime {
	runtime := tryCloudflareRuntime{
		cmd:    p.cmd,
		cancel: p.cmdCancel,
		doneCh: p.doneCh,
	}
	p.cmd = nil
	p.cmdCancel = nil
	p.configDir = ""
	p.readyPort = 0
	p.publicBase = PublicBase{}
	p.boundTargetURL = ""
	p.doneCh = nil
	if clearLastError {
		p.lastError = ""
	}
	return runtime
}

func (p *TryCloudflareProvider) closeDetachedRuntime(runtime tryCloudflareRuntime) error {
	var errs []error
	if runtime.cancel != nil {
		runtime.cancel()
	}
	if runtime.cmd != nil && runtime.cmd.Process != nil {
		if err := runtime.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			errs = append(errs, err)
		}
	}
	if runtime.doneCh != nil {
		<-runtime.doneCh
	}
	return errors.Join(errs...)
}

func (p *TryCloudflareProvider) resolveBinaryPath() (string, error) {
	if value := strings.TrimSpace(p.binaryPath); value != "" {
		return value, nil
	}
	if sibling := ResolveBundledCloudflaredPath(p.currentBinary); sibling != "" {
		if info, err := os.Stat(sibling); err == nil && info.Mode().IsRegular() {
			return sibling, nil
		}
	}
	var bundledErr error
	if p.ensureBundledBinary != nil {
		pathValue, ok, err := p.ensureBundledBinary(p.currentBinary)
		if err != nil {
			bundledErr = err
		} else if ok && strings.TrimSpace(pathValue) != "" {
			return pathValue, nil
		}
	}
	pathValue, err := exec.LookPath(executableName("cloudflared"))
	if err != nil {
		if bundledErr != nil {
			return "", fmt.Errorf("resolve cloudflared binary: %v; path fallback failed: %w", bundledErr, err)
		}
		return "", fmt.Errorf("resolve cloudflared binary: %w", err)
	}
	return pathValue, nil
}

func (p *TryCloudflareProvider) recordError(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err == nil {
		p.lastError = ""
		return
	}
	p.lastError = err.Error()
}

func chooseMetricsAddr(preferredPort int) (string, int, error) {
	addr := net.JoinHostPort("127.0.0.1", "0")
	if preferredPort > 0 {
		addr = net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", preferredPort))
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return "", 0, err
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	return net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", port)), port, nil
}

func waitReady(ctx context.Context, metricsPort int) error {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	readyURL := fmt.Sprintf("http://127.0.0.1:%d/ready", metricsPort)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, readyURL, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

type ioFileCloser struct {
	*os.File
}

type ioDiscardCloser struct{}

func (ioDiscardCloser) Write(p []byte) (int, error) { return len(p), nil }
func (ioDiscardCloser) Close() error                { return nil }
