package externalaccess

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestTryCloudflareProviderEnsurePublicBase(t *testing.T) {
	metricsPort := reserveLocalPort(t)
	readyServer := &http.Server{
		Addr: net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", metricsPort)),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/ready" {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("ok"))
				return
			}
			http.NotFound(w, r)
		}),
	}
	go func() {
		_ = readyServer.ListenAndServe()
	}()
	t.Cleanup(func() {
		_ = readyServer.Close()
	})

	provider := NewTryCloudflareProvider(TryCloudflareOptions{
		BinaryPath:  "cloudflared",
		MetricsPort: metricsPort,
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

func reserveLocalPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}
