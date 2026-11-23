package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestPrivateHostStoreProtectsCarrierAcrossReopen(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "host.db")
	s, err := openPrivateNativeHostStore(path)
	if err != nil {
		t.Fatal(err)
	}
	owner := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "owner", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "history"}
	rec := privateCarrierRecord{Snapshot: agentproto.PrivateNativeLaunchSnapshot{Version: 1, Owner: owner, CWD: root, JournalPath: filepath.Join(root, "wrapper.db")}, Surface: privatechat.Owner{GatewayID: "gateway", SurfaceID: "surface", ChatID: "chat", UserID: "user", TenantID: "tenant", AppID: "app"}, SurfaceGeneration: 1}
	secret, err := s.prepare(ctx, rec)
	if err != nil {
		t.Fatal(err)
	}
	if secret == "" {
		t.Fatal("missing credential")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("mode: %v %v", info, err)
	}
	h := agentproto.Hello{PrivateNativeProtocolVersion: 1, CarrierIncarnation: "inc", Instance: agentproto.InstanceHello{InstanceID: "instance", Backend: agentproto.BackendCodex}}
	if err = s.authorize(h, secret); err != nil {
		t.Fatal(err)
	}
	if err = s.authorize(h, "wrong"); err == nil {
		t.Fatal("bad credential accepted")
	}
	h.PrivateNativeProtocolVersion = 0
	if err = s.authorize(h, ""); err == nil {
		t.Fatal("legacy takeover accepted")
	}
	h.PrivateNativeProtocolVersion = 1
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = openPrivateNativeHostStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.authorize(h, secret); err != nil {
		t.Fatal(err)
	}
	got, err := s.lookup(ctx, "instance")
	if err == nil {
		recovered, e := s.credential(ctx, got)
		if e != nil || recovered != secret {
			t.Fatalf("credential recovery: %v", e)
		}
		info, e := os.Stat(got.CredentialPath)
		if e != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("credential mode: %v", e)
		}
	}
	if err != nil || got.Snapshot != rec.Snapshot || got.Surface != rec.Surface {
		t.Fatalf("reopen: %+v %v", got, err)
	}
	if _, err = s.prepare(ctx, rec); err == nil {
		t.Fatal("duplicate carrier accepted")
	}
}

func TestAuthenticatedHelloRecoversSendingPIDAndRejectsDifferentProcess(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	s, err := openPrivateNativeHostStore(filepath.Join(root, "host.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	owner := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "o", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "history"}
	rec := privateCarrierRecord{Snapshot: agentproto.PrivateNativeLaunchSnapshot{Version: 1, Owner: owner, CWD: root, JournalPath: filepath.Join(root, "native.db")}, Surface: privatechat.Owner{"gateway", "surface", "chat", "user", "tenant", "app"}, SurfaceGeneration: 1}
	secret, err := s.prepare(ctx, rec)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.transition(ctx, "instance", "prepared", "sending", hostinventory.ProcessIdentity{}); err != nil {
		t.Fatal(err)
	}
	rec, err = s.lookup(ctx, "instance")
	if err != nil {
		t.Fatal(err)
	}
	h := &privateNativeHost{store: s, hostID: "host"}
	hello := agentproto.Hello{PrivateNativeProtocolVersion: 1, CarrierIncarnation: "inc", Instance: agentproto.InstanceHello{InstanceID: "instance", Backend: agentproto.BackendCodex, Managed: true, Source: "headless", WorkspaceRoot: root, PID: os.Getpid()}}
	if err = s.authorize(hello, secret); err != nil {
		t.Fatal(err)
	}
	rec, err = h.recordAuthenticatedProcess(ctx, rec, hello)
	if err != nil || rec.State != "running" || rec.Process.PID != os.Getpid() {
		t.Fatalf("cannot recover authenticated PID: %+v %v", rec, err)
	}
	hello.Instance.PID = 0
	if _, err = h.recordAuthenticatedProcess(ctx, rec, hello); err == nil {
		t.Fatal("unknown PID accepted")
	}
}
