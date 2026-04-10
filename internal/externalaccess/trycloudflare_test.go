package externalaccess

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os/exec"
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

func reserveLocalPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}
