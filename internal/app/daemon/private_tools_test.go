package daemon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/executionowner"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/toolservicecontract"
)

func TestPrivateToolAuthenticationUsesCarrierCapabilityAndDrain(t *testing.T) {
	ctx, root := t.Context(), t.TempDir()
	h := &privateNativeHost{hostID: "host", stateDir: root}
	var err error
	h.store, err = openPrivateNativeHostStore(filepath.Join(root, "host.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer h.store.Close()
	h.registry, err = executionowner.Open(filepath.Join(root, "owners.db"), h)
	if err != nil {
		t.Fatal(err)
	}
	defer h.registry.Close()
	h.outbound, err = executionowner.NewOutbound(h.registry)
	if err != nil {
		t.Fatal(err)
	}
	n := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "owner", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "history"}
	rec := privateCarrierRecord{Snapshot: agentproto.PrivateNativeLaunchSnapshot{Version: 1, Owner: n, CWD: root, JournalPath: filepath.Join(root, "wrapper.db")}, Surface: privatechat.Owner{"gateway", "surface", "chat", "user", "tenant", "app"}, SurfaceGeneration: 1}
	secret, err := h.store.prepare(ctx, rec)
	if err != nil {
		t.Fatal(err)
	}
	if err = h.registry.Register(ctx, privateRegistration(n)); err != nil {
		t.Fatal(err)
	}
	process, err := hostinventory.CaptureProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if err = h.store.transition(ctx, "instance", "prepared", "sending", hostinventory.ProcessIdentity{}); err != nil {
		t.Fatal(err)
	}
	if err = h.store.transition(ctx, "instance", "sending", "running", process); err != nil {
		t.Fatal(err)
	}
	runtime := &privateToolRuntime{host: h}
	app := New(":0", ":0", &privatePublishGateway{}, agentproto.ServerIdentity{})
	app.privateTools = runtime
	app.toolRuntime.BearerToken = "global-legacy-token"
	called := 0
	handler := app.requireToolAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := r.Context().Value(privateToolCallerKey{}).(*privateToolCaller); !ok {
			t.Error("missing authenticated caller")
		}
		called++
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, tc := range []struct {
		instance, token string
		status          int
	}{
		{"instance", toolservicecontract.PrivateBearer(secret), http.StatusNoContent},
		{"instance", secret, http.StatusUnauthorized},
		{"instance", "global-legacy-token", http.StatusUnauthorized},
		{"other", toolservicecontract.PrivateBearer(secret), http.StatusUnauthorized},
	} {
		r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/?"+toolservicecontract.CallerInstanceIDQueryParam+"="+tc.instance, nil)
		r.RemoteAddr = "127.0.0.1:1234"
		r.Header.Set("Authorization", "Bearer "+tc.token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("identity %s: status %d", tc.instance, w.Code)
		}
	}
	if called != 1 {
		t.Fatal("wrong identity reached handler")
	}
	caller, err := runtime.authenticate(ctx, "instance", toolservicecontract.PrivateBearer(secret))
	if err != nil {
		t.Fatal(err)
	}
	if err = h.outbound.Drain(ctx, privateRegistration(n)); err != nil {
		t.Fatal(err)
	}
	if err = caller.run(ctx, func(_ context.Context) error { t.Fatal("drained tool published"); return nil }); err == nil {
		t.Fatal("drain ignored")
	}
}
