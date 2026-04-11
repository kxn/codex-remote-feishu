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

	mu         sync.Mutex
	cmd        *exec.Cmd
	cmdCancel  context.CancelFunc
	configDir  string
	readyURL   string
	publicBase PublicBase
	lastError  string
	startWait  chan struct{}
	doneCh     chan struct{}
	stopping   bool
}

func NewTryCloudflareProvider(opts TryCloudflareOptions) *TryCloudflareProvider {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	factory := opts.CommandFactory
	if factory == nil {
		factory = exec.CommandContext
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
	for {
		p.mu.Lock()
		if p.cmd != nil && p.cmd.Process != nil && strings.TrimSpace(p.publicBase.BaseURL) != "" {
			base := p.publicBase
			p.mu.Unlock()
			return base, nil
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
		p.startWait = make(chan struct{})
		waitCh := p.startWait
		p.mu.Unlock()

		base, cmd, cmdCancel, configDir, readyURL, err := p.startPublicBase(ctx, localListenerURL)
		var doneCh chan struct{}
		if err == nil {
			doneCh = make(chan struct{})
		}
		p.mu.Lock()
		if err == nil {
			p.cmd = cmd
			p.cmdCancel = cmdCancel
			p.configDir = configDir
			p.readyURL = readyURL
			p.publicBase = base
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
		}
		return base, err
	}
}

func (p *TryCloudflareProvider) startPublicBase(ctx context.Context, localListenerURL string) (PublicBase, *exec.Cmd, context.CancelFunc, string, string, error) {
	launchCtx, cancel := context.WithTimeout(ctx, p.launchTimeout)
	defer cancel()

	binaryPath, err := p.resolveBinaryPath()
	if err != nil {
		return PublicBase{}, nil, nil, "", "", err
	}
	metricsAddr, metricsPort, err := chooseMetricsAddr(p.metricsPort)
	if err != nil {
		return PublicBase{}, nil, nil, "", "", err
	}
	configDir, err := os.MkdirTemp("", "codex-remote-cloudflared-*")
	if err != nil {
		return PublicBase{}, nil, nil, "", "", err
	}

	args := []string{
		"tunnel",
		"--url", localListenerURL,
		"--no-autoupdate",
		"--metrics", metricsAddr,
	}
	procCtx, procCancel := context.WithCancel(context.Background())
	cmd := p.commandFactory(procCtx, binaryPath, args...)
	cmd.Env = append(os.Environ(),
		"HOME="+configDir,
		"XDG_CONFIG_HOME="+configDir,
	)
	configureTryCloudflareLaunch(cmd)
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
		return PublicBase{}, nil, nil, "", "", err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		procCancel()
		_ = os.RemoveAll(configDir)
		return PublicBase{}, nil, nil, "", "", err
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
		return PublicBase{}, nil, nil, "", "", err
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
			return PublicBase{}, nil, nil, "", "", fmt.Errorf("start trycloudflare provider: %w", launchCtx.Err())
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
				return PublicBase{}, nil, nil, "", "", err
			}
			publicURL = candidate
		}
		if publicURL != "" {
			break
		}
	}

	base := PublicBase{BaseURL: publicURL, StartedAt: p.now().UTC()}
	return base, cmd, procCancel, configDir, fmt.Sprintf("http://127.0.0.1:%d/ready", metricsPort), nil
}

func (p *TryCloudflareProvider) Close() error {
	p.mu.Lock()
	waitCh := p.startWait
	if waitCh != nil {
		p.mu.Unlock()
		<-waitCh
		p.mu.Lock()
	}
	cmd := p.cmd
	cancel := p.cmdCancel
	doneCh := p.doneCh
	p.stopping = true
	p.mu.Unlock()

	var errs []error
	if cancel != nil {
		cancel()
	}
	if cmd != nil && cmd.Process != nil {
		if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			errs = append(errs, err)
		}
	}
	if doneCh != nil {
		<-doneCh
	}

	p.mu.Lock()
	if p.cmd == nil {
		p.cmdCancel = nil
		p.configDir = ""
		p.readyURL = ""
		p.publicBase = PublicBase{}
		p.doneCh = nil
		p.lastError = ""
	}
	p.stopping = false
	p.mu.Unlock()
	return errors.Join(errs...)
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
		p.readyURL = ""
		p.publicBase = PublicBase{}
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
