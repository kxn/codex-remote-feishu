package daemon

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/codexprofile"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func TestOAuthBackgroundProbeKeepsApplicationAliveUntilDurableCompletion(t *testing.T) {
	a := New(":0", ":0", nil, agentproto.ServerIdentity{})
	a.SetHeadlessRuntime(HeadlessRuntimeConfig{CodexRealBinary: "fake-codex", BaseEnv: []string{"HOME=/tmp/test"}, Paths: relayruntime.Paths{StateDir: t.TempDir()}})
	a.applications = &applicationRuntime{}
	entered, release := make(chan struct{}), make(chan struct{})
	a.runCodexOAuthProbe = func(context.Context, codexprofile.OAuthProbeOptions) (codexprofile.OAuthProbeObservation, error) {
		close(entered)
		<-release
		return testDetectedOAuthObservation(), nil
	}
	if !a.requestCodexOAuthProbe(t.Context(), false) {
		t.Fatal("probe not admitted")
	}
	<-entered
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := a.applications.drainNativeDeliveries(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("async probe not counted: %v", err)
	}
	if err := a.applications.close(); err == nil {
		t.Fatal("stores closed before background probe result")
	}
	if a.requestCodexOAuthProbe(t.Context(), true) {
		t.Fatal("new probe admitted during drain")
	}
	close(release)
	deadline, stop := context.WithTimeout(t.Context(), 3*time.Second)
	defer stop()
	if err := a.applications.drainNativeDeliveries(deadline); err != nil {
		t.Fatal(err)
	}
	if err := a.applications.close(); err != nil {
		t.Fatal(err)
	}
}

func TestBackgroundCatalogAndFactsRejectNewWorkAfterApplicationDrain(t *testing.T) {
	a := New(":0", ":0", nil, agentproto.ServerIdentity{})
	a.applications = &applicationRuntime{}
	if err := a.applications.drainNativeDeliveries(t.Context()); err != nil {
		t.Fatal(err)
	}
	a.maybeStartFeishuFactsRefreshLocked(time.Now())
	if a.feishuFactsState.refreshInFlight {
		t.Fatal("facts refresh started after drain")
	}
	if _, err := a.startProfileCatalogRefresh(t.Context(), teamProfileCatalogTarget{}); !errors.Is(err, errApplicationClosing) {
		t.Fatalf("catalog used closed source store: %v", err)
	}
}
