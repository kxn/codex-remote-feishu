package daemon

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
)

func TestApplicationDrainKeepsStoresOpenForSDKCallback(t *testing.T) {
	a := &App{applications: &applicationRuntime{}}
	r := a.applications
	entered, release := make(chan struct{}), make(chan struct{})
	h := a.guardApplicationIngress(feishu.RawIngressHook{Receive: func(context.Context, platformingress.RawIngressEnvelope) error {
		close(entered)
		<-release
		return nil
	}})
	finished := make(chan error, 1)
	go func() { finished <- h.Receive(t.Context(), platformingress.RawIngressEnvelope{}) }()
	<-entered
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := r.drainNativeDeliveries(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("drain while callback is active: %v", err)
	}
	if err := r.close(); err == nil {
		t.Fatal("closed stores while SDK callback still uses them")
	}
	if err := h.Receive(t.Context(), platformingress.RawIngressEnvelope{}); !errors.Is(err, errApplicationClosing) {
		t.Fatalf("new callback admitted during shutdown: %v", err)
	}
	close(release)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if err := r.drainNativeDeliveries(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := r.close(); err != nil {
		t.Fatal(err)
	}
}

func TestApplicationDrainRejectsAllPlatformAndHTTPEntries(t *testing.T) {
	a := &App{applications: &applicationRuntime{}}
	called := false
	fail := func() error { called = true; return nil }
	h := a.guardApplicationIngress(feishu.RawIngressHook{
		Authorize:          func(context.Context, platformingress.RawIngressEnvelope) (bool, error) { return true, fail() },
		Receive:            func(context.Context, platformingress.RawIngressEnvelope) error { return fail() },
		ReceiveInteraction: func(context.Context, platformingress.HumanInteractionEnvelope) (string, error) { return "", fail() },
		ReceiveInteractionResponse: func(context.Context, platformingress.HumanInteractionEnvelope) (feishu.InteractionResponse, error) {
			return feishu.InteractionResponse{}, fail()
		},
		ReceiveMenu:          func(context.Context, feishu.MenuIngressEvent) error { return fail() },
		ReceivePlatformEvent: func(context.Context, feishu.PlatformIngressEvent) error { return fail() },
	})
	if err := a.applications.drainNativeDeliveries(t.Context()); err != nil {
		t.Fatal(err)
	}
	_, authErr := h.Authorize(t.Context(), platformingress.RawIngressEnvelope{})
	_, interactionErr := h.ReceiveInteraction(t.Context(), platformingress.HumanInteractionEnvelope{})
	_, responseErr := h.ReceiveInteractionResponse(t.Context(), platformingress.HumanInteractionEnvelope{})
	for _, err := range []error{authErr, interactionErr, responseErr,
		h.Receive(t.Context(), platformingress.RawIngressEnvelope{}),
		h.ReceiveMenu(t.Context(), feishu.MenuIngressEvent{}),
		h.ReceivePlatformEvent(t.Context(), feishu.PlatformIngressEvent{}),
	} {
		if !errors.Is(err, errApplicationClosing) {
			t.Errorf("entry escaped shutdown guard: %v", err)
		}
	}
	w := httptest.NewRecorder()
	a.applicationRequestGuard(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/status", nil))
	if w.Code != http.StatusServiceUnavailable || called {
		t.Fatalf("code=%d handler called=%v", w.Code, called)
	}
}

func TestApplicationCloseWaitsForBackgroundWorkers(t *testing.T) {
	r := &applicationRuntime{done: make(chan struct{})}
	if err := r.close(); err == nil {
		t.Fatal("closed before worker joined")
	}
	close(r.done)
	if err := r.close(); err != nil {
		t.Fatal(err)
	}
}

func TestApplicationPlatformDrainKeepsNativeReceiptsAvailable(t *testing.T) {
	a := &App{applications: &applicationRuntime{}}
	called := false
	hook := a.guardApplicationIngress(feishu.RawIngressHook{Receive: func(context.Context, platformingress.RawIngressEnvelope) error {
		called = true
		return nil
	}})
	if err := a.applications.platformDeliveries.drain(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := hook.Receive(t.Context(), platformingress.RawIngressEnvelope{}); !errors.Is(err, errApplicationClosing) || called {
		t.Fatalf("new input during drain: %v", err)
	}
	end, ok := a.beginApplicationNativeDelivery()
	if !ok {
		t.Fatal("native stop receipt blocked before carrier drain")
	}
	end()
	if err := a.applications.drainNativeDeliveries(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.beginApplicationNativeDelivery(); ok {
		t.Fatal("native receipt admitted after final drain")
	}
}

func TestApplicationFinishShutdownKeepsSharedStoreForActiveRequest(t *testing.T) {
	a, _ := newApplicationRuntimeTestApp(t)
	end, ok := a.beginApplicationNativeDelivery()
	if !ok {
		t.Fatal("request not admitted")
	}
	defer end()
	if err := a.finishShutdown(); err == nil {
		t.Fatal("shutdown concealed active request")
	}
	// A private migration/request can use the team store after its scheduler
	// joined. Closing only that store early would violate the shared lifetime.
	if err := a.teamRuntime.store.Within(t.Context(), func(*workstore.Tx) error { return nil }); err != nil {
		t.Fatalf("shared store closed underneath active application request: %v", err)
	}
}

func TestApplicationCloseKeepsRegistryForUnjoinedTeamWorker(t *testing.T) {
	a, _ := newApplicationRuntimeTestApp(t)
	a.teamRuntime.runDone = make(chan struct{})
	defer close(a.teamRuntime.runDone)
	if err := a.applications.close(); err == nil {
		t.Fatal("private shutdown closed stores still owned by team worker")
	}
	if err := a.teamRuntime.store.Within(t.Context(), func(*workstore.Tx) error { return nil }); err != nil {
		t.Fatalf("shared store closed: %v", err)
	}
}
