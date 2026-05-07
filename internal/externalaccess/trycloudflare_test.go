package externalaccess

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTryCloudflareProviderDefaultLaunchTimeout(t *testing.T) {
	provider := NewTryCloudflareProvider(TryCloudflareOptions{})
	if provider.launchTimeout != defaultTryCloudflareLaunchTimeout {
		t.Fatalf("launchTimeout = %v, want %v", provider.launchTimeout, defaultTryCloudflareLaunchTimeout)
	}
}

func TestTryCloudflareProviderEnsurePublicBase(t *testing.T) {
	metricsPort := reserveLocalPort(t)
	provider := NewTryCloudflareProvider(TryCloudflareOptions{
		BinaryPath:  "cloudflared",
		MetricsPort: metricsPort,
		WaitReady: func(context.Context, int) error {
			return nil
		},
		CommandFactory: func(ctx context.Context, _ string, args ...string) *exec.Cmd {
			return mockTryCloudflareCommand(t, ctx, mockTryCloudflareProcess{
				URL: "https://example.trycloudflare.com",
			})
		},
	})
	defer provider.Close()

	base, err := provider.EnsurePublicBase(t.Context(), "http://127.0.0.1:9512")
	if err != nil {
		t.Fatalf("EnsurePublicBase: %v", err)
	}
	if !strings.HasPrefix(base.BaseURL, "https://") || !strings.Contains(base.BaseURL, ".trycloudflare.com") {
		t.Fatalf("base = %#v, want trycloudflare url", base)
	}

	snapshot := provider.Snapshot()
	if !snapshot.Ready || snapshot.BaseURL != base.BaseURL {
		t.Fatalf("snapshot = %#v, want ready base=%q", snapshot, base.BaseURL)
	}
}

func TestTryCloudflareProviderKeepsTunnelAliveAfterStartupContextEnds(t *testing.T) {
	metricsPort := reserveLocalPort(t)
	probeFile := filepath.Join(t.TempDir(), "alive.txt")
	provider := NewTryCloudflareProvider(TryCloudflareOptions{
		BinaryPath:  "cloudflared",
		MetricsPort: metricsPort,
		WaitReady: func(context.Context, int) error {
			return nil
		},
		CommandFactory: func(ctx context.Context, _ string, args ...string) *exec.Cmd {
			return mockTryCloudflareCommand(t, ctx, mockTryCloudflareProcess{
				URL:        "https://example.trycloudflare.com",
				AliveFile:  probeFile,
				AliveDelay: 300 * time.Millisecond,
			})
		},
	})
	defer provider.Close()

	ctx, cancel := context.WithCancel(t.Context())
	if _, err := provider.EnsurePublicBase(ctx, "http://127.0.0.1:9512"); err != nil {
		t.Fatalf("EnsurePublicBase: %v", err)
	}
	cancel()

	content, err := waitForFileContent(probeFile, 3*time.Second)
	if err != nil {
		t.Fatalf("expected tunnel child process to outlive startup context, read probe file: %v", err)
	}
	if strings.TrimSpace(string(content)) != "alive" {
		t.Fatalf("probe content = %q, want alive", string(content))
	}
}

func TestTryCloudflareProviderClearsSnapshotWhenTunnelExits(t *testing.T) {
	metricsPort := reserveLocalPort(t)
	provider := NewTryCloudflareProvider(TryCloudflareOptions{
		BinaryPath:  "cloudflared",
		MetricsPort: metricsPort,
		WaitReady: func(context.Context, int) error {
			return nil
		},
		CommandFactory: func(ctx context.Context, _ string, args ...string) *exec.Cmd {
			return mockTryCloudflareCommand(t, ctx, mockTryCloudflareProcess{
				URL:       "https://example.trycloudflare.com",
				ExitDelay: 200 * time.Millisecond,
			})
		},
	})
	defer provider.Close()

	base, err := provider.EnsurePublicBase(t.Context(), "http://127.0.0.1:9512")
	if err != nil {
		t.Fatalf("EnsurePublicBase: %v", err)
	}
	if base.BaseURL == "" {
		t.Fatalf("base = %#v, want non-empty base", base)
	}

	snapshot, err := waitForSnapshot(3*time.Second, func(status ProviderStatus) bool {
		return !status.Ready && status.BaseURL == ""
	}, provider.Snapshot)
	if err != nil {
		t.Fatalf("wait for cleared snapshot: %v", err)
	}
	if snapshot.Ready || snapshot.BaseURL != "" {
		t.Fatalf("snapshot = %#v, want cleared stale tunnel state", snapshot)
	}
	if !strings.Contains(snapshot.LastError, "exited") {
		t.Fatalf("snapshot = %#v, want exit detail", snapshot)
	}
}

func TestTryCloudflareProviderResolveBinaryPathUsesBundledExtractor(t *testing.T) {
	dir := t.TempDir()
	currentBinary := filepath.Join(dir, executableName("codex-remote"))
	if err := os.WriteFile(currentBinary, []byte("codex-remote"), 0o755); err != nil {
		t.Fatalf("seed current binary: %v", err)
	}
	bundledPath := filepath.Join(dir, executableName("cloudflared"))
	provider := NewTryCloudflareProvider(TryCloudflareOptions{
		CurrentBinary: currentBinary,
		EnsureBundledBinary: func(path string) (string, bool, error) {
			if path != currentBinary {
				t.Fatalf("currentBinary = %q, want %q", path, currentBinary)
			}
			if err := os.WriteFile(bundledPath, []byte("cloudflared"), 0o755); err != nil {
				t.Fatalf("seed bundled asset: %v", err)
			}
			return bundledPath, true, nil
		},
	})

	pathValue, err := provider.resolveBinaryPath()
	if err != nil {
		t.Fatalf("resolveBinaryPath: %v", err)
	}
	if pathValue != bundledPath {
		t.Fatalf("pathValue = %q, want %q", pathValue, bundledPath)
	}
}

func TestTryCloudflareProviderResolveBinaryPathReportsBundledError(t *testing.T) {
	dir := t.TempDir()
	currentBinary := filepath.Join(dir, executableName("codex-remote"))
	if err := os.WriteFile(currentBinary, []byte("codex-remote"), 0o755); err != nil {
		t.Fatalf("seed current binary: %v", err)
	}
	provider := NewTryCloudflareProvider(TryCloudflareOptions{
		CurrentBinary: currentBinary,
		EnsureBundledBinary: func(string) (string, bool, error) {
			return "", false, errors.New("extract embedded cloudflared failed")
		},
	})

	_, err := provider.resolveBinaryPath()
	if err == nil {
		t.Fatal("resolveBinaryPath succeeded unexpectedly")
	}
	message := err.Error()
	if !strings.Contains(message, "extract embedded cloudflared failed") {
		t.Fatalf("error = %q, want bundled extraction detail", message)
	}
	if !strings.Contains(message, "path fallback failed") {
		t.Fatalf("error = %q, want path fallback detail", message)
	}
}

func TestTryCloudflareProviderEnsurePublicBaseCoalescesConcurrentStart(t *testing.T) {
	metricsPort := reserveLocalPort(t)
	var factoryCalls atomic.Int32
	provider := NewTryCloudflareProvider(TryCloudflareOptions{
		BinaryPath:  "cloudflared",
		MetricsPort: metricsPort,
		WaitReady: func(context.Context, int) error {
			return nil
		},
		CommandFactory: func(ctx context.Context, _ string, args ...string) *exec.Cmd {
			factoryCalls.Add(1)
			return mockTryCloudflareCommand(t, ctx, mockTryCloudflareProcess{
				URL:         "https://example.trycloudflare.com",
				PreURLDelay: 200 * time.Millisecond,
			})
		},
	})
	defer provider.Close()

	results := make([]PublicBase, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func(idx int) {
			defer wg.Done()
			results[idx], errs[idx] = provider.EnsurePublicBase(t.Context(), "http://127.0.0.1:9512")
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("EnsurePublicBase[%d]: %v", i, err)
		}
	}
	if results[0].BaseURL == "" || results[1].BaseURL == "" {
		t.Fatalf("results = %#v, want populated base URLs", results)
	}
	if results[0].BaseURL != results[1].BaseURL {
		t.Fatalf("results = %#v, want shared public base", results)
	}
	if got := factoryCalls.Load(); got != 1 {
		t.Fatalf("factoryCalls = %d, want 1", got)
	}
}

func TestTryCloudflareProviderRestartsWhenListenerTargetChanges(t *testing.T) {
	metricsPort := reserveLocalPort(t)
	var factoryCalls atomic.Int32
	provider := NewTryCloudflareProvider(TryCloudflareOptions{
		BinaryPath:  "cloudflared",
		MetricsPort: metricsPort,
		WaitReady: func(context.Context, int) error {
			return nil
		},
		CommandFactory: func(ctx context.Context, _ string, args ...string) *exec.Cmd {
			call := factoryCalls.Add(1)
			return mockTryCloudflareCommand(t, ctx, mockTryCloudflareProcess{
				URL: fmt.Sprintf("https://example-%d.trycloudflare.com", call),
			})
		},
	})
	defer provider.Close()

	first, err := provider.EnsurePublicBase(t.Context(), "http://127.0.0.1:9512")
	if err != nil {
		t.Fatalf("EnsurePublicBase first: %v", err)
	}
	second, err := provider.EnsurePublicBase(t.Context(), "http://127.0.0.1:9513")
	if err != nil {
		t.Fatalf("EnsurePublicBase second: %v", err)
	}
	if got := factoryCalls.Load(); got != 2 {
		t.Fatalf("factoryCalls = %d, want 2", got)
	}
	if first.BaseURL == second.BaseURL {
		t.Fatalf("expected different public base after target change, got %q", first.BaseURL)
	}
}

func TestTryCloudflareProviderRestartsWhenReadyProbeFails(t *testing.T) {
	metricsPort := reserveLocalPort(t)
	var factoryCalls atomic.Int32
	var readyCalls atomic.Int32
	provider := NewTryCloudflareProvider(TryCloudflareOptions{
		BinaryPath:  "cloudflared",
		MetricsPort: metricsPort,
		WaitReady: func(context.Context, int) error {
			call := readyCalls.Add(1)
			switch call {
			case 1, 3:
				return nil
			case 2:
				return errors.New("ready probe failed")
			default:
				return nil
			}
		},
		CommandFactory: func(ctx context.Context, _ string, args ...string) *exec.Cmd {
			call := factoryCalls.Add(1)
			return mockTryCloudflareCommand(t, ctx, mockTryCloudflareProcess{
				URL: fmt.Sprintf("https://example-%d.trycloudflare.com", call),
			})
		},
	})
	defer provider.Close()

	first, err := provider.EnsurePublicBase(t.Context(), "http://127.0.0.1:9512")
	if err != nil {
		t.Fatalf("EnsurePublicBase first: %v", err)
	}
	second, err := provider.EnsurePublicBase(t.Context(), "http://127.0.0.1:9512")
	if err != nil {
		t.Fatalf("EnsurePublicBase second: %v", err)
	}
	if got := factoryCalls.Load(); got != 2 {
		t.Fatalf("factoryCalls = %d, want 2", got)
	}
	if got := readyCalls.Load(); got != 4 {
		t.Fatalf("readyCalls = %d, want 4", got)
	}
	if first.BaseURL == second.BaseURL {
		t.Fatalf("expected restarted public base after failed probe, got %q", first.BaseURL)
	}
}

func TestTryCloudflareProviderCoalescesConcurrentRestartOnTargetChange(t *testing.T) {
	metricsPort := reserveLocalPort(t)
	var factoryCalls atomic.Int32
	provider := NewTryCloudflareProvider(TryCloudflareOptions{
		BinaryPath:  "cloudflared",
		MetricsPort: metricsPort,
		WaitReady: func(context.Context, int) error {
			return nil
		},
		CommandFactory: func(ctx context.Context, _ string, args ...string) *exec.Cmd {
			call := factoryCalls.Add(1)
			return mockTryCloudflareCommand(t, ctx, mockTryCloudflareProcess{
				URL:         fmt.Sprintf("https://example-%d.trycloudflare.com", call),
				PreURLDelay: 200 * time.Millisecond,
			})
		},
	})
	defer provider.Close()

	if _, err := provider.EnsurePublicBase(t.Context(), "http://127.0.0.1:9512"); err != nil {
		t.Fatalf("EnsurePublicBase first: %v", err)
	}

	results := make([]PublicBase, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func(idx int) {
			defer wg.Done()
			results[idx], errs[idx] = provider.EnsurePublicBase(t.Context(), "http://127.0.0.1:9513")
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("EnsurePublicBase concurrent[%d]: %v", i, err)
		}
	}
	if got := factoryCalls.Load(); got != 2 {
		t.Fatalf("factoryCalls = %d, want 2", got)
	}
	if results[0].BaseURL == "" || results[1].BaseURL == "" {
		t.Fatalf("results = %#v, want populated base URLs", results)
	}
	if results[0].BaseURL != results[1].BaseURL {
		t.Fatalf("results = %#v, want shared restarted base", results)
	}
}

func reserveLocalPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func waitForFileContent(path string, timeout time.Duration) ([]byte, error) {
	deadline := time.Now().Add(timeout)
	for {
		raw, err := os.ReadFile(path)
		if err == nil && len(strings.TrimSpace(string(raw))) != 0 {
			return raw, nil
		}
		if time.Now().After(deadline) {
			if err != nil {
				return nil, err
			}
			return raw, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func waitForSnapshot(timeout time.Duration, predicate func(ProviderStatus) bool, snapshot func() ProviderStatus) (ProviderStatus, error) {
	deadline := time.Now().Add(timeout)
	for {
		status := snapshot()
		if predicate(status) {
			return status, nil
		}
		if time.Now().After(deadline) {
			return status, fmt.Errorf("timed out waiting for snapshot to match predicate")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

type mockTryCloudflareProcess struct {
	URL         string
	PreURLDelay time.Duration
	AliveFile   string
	AliveDelay  time.Duration
	ExitDelay   time.Duration
}

func TestHelperProcessTryCloudflare(t *testing.T) {
	args, ok := mockTryCloudflareArgsFromCommandLine()
	if !ok {
		return
	}

	if err := runMockTryCloudflareProcess(args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func mockTryCloudflareCommand(t *testing.T, ctx context.Context, process mockTryCloudflareProcess) *exec.Cmd {
	t.Helper()

	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	cmd := exec.CommandContext(ctx,
		executable,
		"-test.run", "^TestHelperProcessTryCloudflare$",
		"--",
		"--mock-trycloudflare",
		process.URL,
		strconv.FormatInt(process.PreURLDelay.Milliseconds(), 10),
		process.AliveFile,
		strconv.FormatInt(process.AliveDelay.Milliseconds(), 10),
		strconv.FormatInt(process.ExitDelay.Milliseconds(), 10),
	)
	return cmd
}

func runMockTryCloudflareProcess(args []string) error {
	if len(args) != 5 {
		return fmt.Errorf("mock trycloudflare args = %q, want 5 items", args)
	}

	preURLDelay, err := durationMillisArg("pre-url-delay", args[1])
	if err != nil {
		return err
	}
	aliveDelay, err := durationMillisArg("alive-delay", args[3])
	if err != nil {
		return err
	}
	exitDelay, err := durationMillisArg("exit-delay", args[4])
	if err != nil {
		return err
	}

	if preURLDelay > 0 {
		time.Sleep(preURLDelay)
	}
	if _, err := fmt.Fprintln(os.Stdout, args[0]); err != nil {
		return fmt.Errorf("write mock trycloudflare url: %w", err)
	}
	if aliveDelay > 0 {
		time.Sleep(aliveDelay)
	}
	if aliveFile := args[2]; aliveFile != "" {
		if err := os.WriteFile(aliveFile, []byte("alive\n"), 0o644); err != nil {
			return fmt.Errorf("write mock trycloudflare alive file: %w", err)
		}
	}
	if exitDelay > 0 {
		time.Sleep(exitDelay)
		return nil
	}

	select {}
}

func mockTryCloudflareArgsFromCommandLine() ([]string, bool) {
	for i := 0; i < len(os.Args); i++ {
		if os.Args[i] != "--mock-trycloudflare" {
			continue
		}
		return os.Args[i+1:], true
	}
	return nil, false
}

func durationMillisArg(name, value string) (time.Duration, error) {
	millis, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %s=%q: %w", name, value, err)
	}
	return time.Duration(millis) * time.Millisecond, nil
}
