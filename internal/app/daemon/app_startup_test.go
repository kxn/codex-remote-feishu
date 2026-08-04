package daemon

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/codexprofile"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestRunServesAPIWhileCodexStartupProbeIsBlocked(t *testing.T) {
	app := New("127.0.0.1:0", "127.0.0.1:0", &recordingGateway{}, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{CodexRealBinary: "/tmp/codex-real"})
	if err := app.Bind(); err != nil {
		t.Fatalf("Bind: %v", err)
	}

	probeStarted := make(chan struct{})
	releaseProbe := make(chan struct{})
	app.runCodexCapabilityPreflight = func(context.Context, codexprofile.CapabilityPreflightOptions) (codexprofile.CapabilityPreflightObservation, error) {
		close(probeStarted)
		<-releaseProbe
		return codexprofile.CapabilityPreflightObservation{}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runDone := make(chan error, 1)
	go func() {
		runDone <- app.Run(ctx)
	}()
	defer func() {
		close(releaseProbe)
		cancel()
		select {
		case err := <-runDone:
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("Run did not stop")
		}
	}()

	select {
	case <-probeStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("Codex startup probe did not start")
	}

	client := &http.Client{Timeout: time.Second}
	response, err := client.Get("http://" + app.apiListener.Addr().String() + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz while startup probe is blocked: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET /healthz status = %d, want %d", response.StatusCode, http.StatusOK)
	}
}
