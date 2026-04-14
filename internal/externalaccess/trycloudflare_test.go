package externalaccess

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
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
			return exec.CommandContext(ctx, "bash", "-lc", "printf 'https://example.trycloudflare.com\\n'; sleep 60")
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
			script := fmt.Sprintf("printf 'https://example.trycloudflare.com\\n'; sleep 0.3; printf 'alive\\n' > %s; sleep 60", bashSingleQuotedPath(probeFile))
			return exec.CommandContext(ctx, "bash", "-lc", script)
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
			return exec.CommandContext(ctx, "bash", "-lc", "printf 'https://example.trycloudflare.com\\n'; sleep 0.2")
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

	time.Sleep(500 * time.Millisecond)
	snapshot := provider.Snapshot()
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
			return exec.CommandContext(ctx, "bash", "-lc", "sleep 0.2; printf 'https://example.trycloudflare.com\\n'; sleep 60")
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

func bashSingleQuotedPath(path string) string {
	value := filepath.ToSlash(path)
	value = strings.ReplaceAll(value, `'`, `'"'"'`)
	return "'" + value + "'"
}
