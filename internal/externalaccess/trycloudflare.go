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
)

var tryCloudflareURLPattern = regexp.MustCompile(`https://[a-zA-Z0-9.-]+\.trycloudflare\.com`)

type TryCloudflareOptions struct {
	BinaryPath     string
	CurrentBinary  string
	LaunchTimeout  time.Duration
	MetricsPort    int
	LogPath        string
	Now            func() time.Time
	CommandFactory func(context.Context, string, ...string) *exec.Cmd
}

type TryCloudflareProvider struct {
	now            func() time.Time
	binaryPath     string
	currentBinary  string
	launchTimeout  time.Duration
	metricsPort    int
	logPath        string
	commandFactory func(context.Context, string, ...string) *exec.Cmd

	mu         sync.Mutex
	cmd        *exec.Cmd
	configDir  string
	publicBase PublicBase
	lastError  string
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
	if opts.LaunchTimeout <= 0 {
		opts.LaunchTimeout = 20 * time.Second
	}
	return &TryCloudflareProvider{
		now:            now,
		binaryPath:     strings.TrimSpace(opts.BinaryPath),
		currentBinary:  strings.TrimSpace(opts.CurrentBinary),
		launchTimeout:  opts.LaunchTimeout,
		metricsPort:    opts.MetricsPort,
		logPath:        strings.TrimSpace(opts.LogPath),
		commandFactory: factory,
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
		Ready:     p.cmd != nil && p.cmd.Process != nil,
		LastError: p.lastError,
	}
	return status
}

func (p *TryCloudflareProvider) EnsurePublicBase(ctx context.Context, localListenerURL string) (PublicBase, error) {
	p.mu.Lock()
	if p.cmd != nil && p.cmd.Process != nil && strings.TrimSpace(p.publicBase.BaseURL) != "" {
		base := p.publicBase
		p.mu.Unlock()
		return base, nil
	}
	p.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, p.launchTimeout)
	defer cancel()

	binaryPath, err := p.resolveBinaryPath()
	if err != nil {
		p.recordError(err)
		return PublicBase{}, err
	}
	metricsAddr, metricsPort, err := chooseMetricsAddr(p.metricsPort)
	if err != nil {
		p.recordError(err)
		return PublicBase{}, err
	}
	configDir, err := os.MkdirTemp("", "codex-remote-cloudflared-*")
	if err != nil {
		p.recordError(err)
		return PublicBase{}, err
	}

	args := []string{
		"tunnel",
		"--url", localListenerURL,
		"--no-autoupdate",
		"--metrics", metricsAddr,
	}
	cmd := p.commandFactory(ctx, binaryPath, args...)
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
		_ = os.RemoveAll(configDir)
		p.recordError(err)
		return PublicBase{}, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = os.RemoveAll(configDir)
		p.recordError(err)
		return PublicBase{}, err
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
		_ = os.RemoveAll(configDir)
		p.recordError(err)
		return PublicBase{}, err
	}
	go pump(stdout)
	go pump(stderr)

	var publicURL string
	for {
		select {
		case <-ctx.Done():
			_ = cmd.Process.Kill()
			_ = os.RemoveAll(configDir)
			err := fmt.Errorf("start trycloudflare provider: %w", ctx.Err())
			p.recordError(err)
			return PublicBase{}, err
		case candidate := <-urlCh:
			if candidate == "" {
				continue
			}
			if err := waitReady(ctx, metricsPort); err != nil {
				_ = cmd.Process.Kill()
				_ = os.RemoveAll(configDir)
				p.recordError(err)
				return PublicBase{}, err
			}
			publicURL = candidate
		}
		if publicURL != "" {
			break
		}
	}

	base := PublicBase{BaseURL: publicURL, StartedAt: p.now().UTC()}
	p.mu.Lock()
	_ = p.closeLocked()
	p.cmd = cmd
	p.configDir = configDir
	p.publicBase = base
	p.lastError = ""
	p.mu.Unlock()
	return base, nil
}

func (p *TryCloudflareProvider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closeLocked()
}

func (p *TryCloudflareProvider) closeLocked() error {
	var errs []error
	if p.cmd != nil && p.cmd.Process != nil {
		if err := p.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			errs = append(errs, err)
		}
		_, _ = p.cmd.Process.Wait()
	}
	if strings.TrimSpace(p.configDir) != "" {
		if err := os.RemoveAll(p.configDir); err != nil {
			errs = append(errs, err)
		}
	}
	p.cmd = nil
	p.configDir = ""
	p.publicBase = PublicBase{}
	return errors.Join(errs...)
}

func (p *TryCloudflareProvider) resolveBinaryPath() (string, error) {
	if value := strings.TrimSpace(p.binaryPath); value != "" {
		return value, nil
	}
	if sibling := ResolveBundledCloudflaredPath(p.currentBinary); sibling != "" {
		if _, err := os.Stat(sibling); err == nil {
			return sibling, nil
		}
	}
	pathValue, err := exec.LookPath(executableName("cloudflared"))
	if err != nil {
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
