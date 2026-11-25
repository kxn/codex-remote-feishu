package daemon

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	gatewaypkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/gateway"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func TestPrivateSDKCleanRestartNewThreadReadyTwoTurns(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX child fixture")
	}
	old, gateway, lock := newApplicationRuntimeTestAppWithLock(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := old.headlessRuntime.Paths.StateDir
	cwd := t.TempDir()
	trace := filepath.Join(t.TempDir(), "trace")
	release := filepath.Join(t.TempDir(), "release-turn")
	native := filepath.Join(t.TempDir(), "codex")
	script := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then printf 'codex-cli 0.159.3-dev.1\\n'; exit; fi\nPRIVATE_CLEAN_CODEX=1 exec %q -test.run '^TestPrivateCleanRestartCodexProcess$'\n", exe)
	if err = os.WriteFile(native, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	imagePath := filepath.Join(t.TempDir(), "image.png")
	imageBytes, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/a9sAAAAASUVORK5CYII=")
	if err = os.WriteFile(imagePath, imageBytes, 0600); err != nil {
		t.Fatal(err)
	}
	old.headlessRuntime.BaseEnv = []string{"HOME=" + t.TempDir(), "PATH=" + os.Getenv("PATH"), "FIXTURE_TRACE=" + trace, "CLEAN_TURN_RELEASE=" + release}
	var mu sync.Mutex
	var children []*exec.Cmd
	var childWait sync.WaitGroup
	startServer := func(app *App) (*httptest.Server, func(relayruntime.HeadlessLaunchOptions) (int, error)) {
		app.relay.SetServerIdentity(agentproto.ServerIdentity{BinaryIdentity: agentproto.BinaryIdentity{Product: relayruntime.ProductName, Version: "fixture", BuildFingerprint: "fixture"}})
		server := httptest.NewServer(app.relay)
		url := "ws" + strings.TrimPrefix(server.URL, "http")
		start := func(v relayruntime.HeadlessLaunchOptions) (int, error) {
			child := exec.Command(exe, "-test.run=^TestPrivateOpenCodeRelayFixtureProcess$")
			child.Env = append(v.Env, "PRIVATE_OPENCODE_FIXTURE=wrapper", "FIXTURE_RELAY_URL="+url, "FIXTURE_STATE_DIR="+root, "FIXTURE_CODEX_BIN="+native)
			child.Dir = v.WorkDir
			child.Stdout = io.Discard
			child.Stderr = os.Stderr
			if e := child.Start(); e != nil {
				return 0, e
			}
			mu.Lock()
			children = append(children, child)
			childWait.Add(1)
			mu.Unlock()
			go func() { defer childWait.Done(); _ = child.Wait() }()
			return child.Process.Pid, nil
		}
		return server, start
	}
	defer func() {
		mu.Lock()
		for _, c := range children {
			if c.Process != nil {
				_ = c.Process.Kill()
			}
		}
		mu.Unlock()
		childWait.Wait()
	}()
	server, start := startServer(old)
	defer server.Close()
	old.applications.native.startHeadless = start
	r := old.applications
	r.ingress.stager = controlledSDKImageStager{path: imagePath}
	errs := make(chan error, 8)
	if err = r.startWorkers(ctx, errs); err != nil {
		t.Fatal(err)
	}
	raw := platformingress.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", SenderTenant: "tenant", ChatID: "private-chat", ChatType: "p2p", SenderType: "user", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, MessageType: "text", ReceivedAt: time.Now()}
	send := func(app *App, id, text string) {
		t.Helper()
		v := raw
		v.EventID = id
		v.MessageID = id
		v.RawContent = fmt.Sprintf(`{"text":%q}`, text)
		if e := app.applications.hook.Receive(ctx, v); e != nil {
			t.Fatal(e)
		}
	}
	send(old, "mode", "/mode codex")
	surface := gatewaypkg.SurfaceIDForInbound("gateway", "", "p2p", "user")
	o := privatechat.Owner{GatewayID: "gateway", SurfaceID: surface, ChatID: "private-chat", UserID: "user", TenantID: "tenant", AppID: "app"}
	var bindingID string
	var epoch int64
	wait := func(label string, check func() bool) {
		t.Helper()
		deadline := time.NewTimer(10 * time.Second)
		defer deadline.Stop()
		for !check() {
			select {
			case e := <-errs:
				t.Fatal(e)
			case <-ctx.Done():
				t.Fatal(label)
			case <-deadline.C:
				if cp, e := r.journal.LoadCheckpoint(context.Background(), o, epoch); e == nil {
					for id, item := range cp.Core.QueueItems {
						if item != nil {
							t.Logf("queue id=%s source=%s sources=%v status=%s", id, item.SourceMessageID, item.SourceMessageIDs, item.Status)
						}
					}
					t.Logf("route=%s active=%s queued=%v holds=%v", cp.Core.RouteMode, cp.Core.ActiveQueueItemID, cp.Core.QueuedQueueItemIDs, cp.Core.Holds)
				}
				data, _ := os.ReadFile(trace)
				t.Logf("native trace=%s", data)
				t.Fatal(label)
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
	wait("owner binding", func() bool {
		b, e := r.ingress.privatePublicationBindingFor(ctx, o, 1)
		if e == nil {
			bindingID, epoch = b.Owner.ID, b.Owner.Epoch
		}
		return e == nil
	})
	n := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: bindingID, Epoch: epoch, CarrierIncarnation: "clean-old", InstanceID: "clean-old-instance", HostID: r.native.hostID, InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex}
	rec := privateCarrierRecord{Snapshot: agentproto.PrivateNativeLaunchSnapshot{Version: 1, Owner: n, CWD: cwd, JournalPath: filepath.Join(root, "clean-old.db")}, Surface: o, SurfaceGeneration: epoch}
	if _, err = old.launchPrivateNative(ctx, r.native, privateNativeLaunchRequest{Record: rec, CodexProfileID: "cp_native"}); err != nil {
		t.Fatal(err)
	}
	wait("old wrapper hello", func() bool {
		r.native.mu.Lock()
		defer r.native.mu.Unlock()
		return r.native.connections[n.InstanceID] != 0
	})
	send(old, "list", "/list")
	var card feishu.Operation
	var callback map[string]any
	wait("picker", func() bool {
		for _, op := range gateway.snapshotOperations() {
			if op.Kind != feishu.OperationSendCard || op.MessageID == "" {
				continue
			}
			b, e := feishu.FreezePrivateOperation(op, old.daemonLifecycleID)
			var doc any
			if e == nil && json.Unmarshal(b, &doc) == nil {
				if found := findPrivateCallback(doc, "target_picker_confirm"); found != nil {
					card = op
					callback = found
					return true
				}
			}
		}
		return false
	})
	wait("picker receipt", func() bool {
		_, e := r.router.PublishedMessage(ctx, "tenant", "app", "private-chat", card.MessageID)
		return e == nil
	})
	click := platformingress.HumanInteractionEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", EventID: "attach", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, ChatID: "private-chat", MessageID: card.MessageID, Kind: "card", Values: callback, ReceivedAt: time.Now()}
	if _, err = r.hook.ReceiveInteractionResponse(ctx, click); err != nil {
		t.Fatal(err)
	}
	wait("new workspace ready before shutdown", func() bool {
		cp, e := r.journal.LoadCheckpoint(ctx, o, epoch)
		return e == nil && cp.Core.RouteMode == "new_thread_ready" && cp.Core.PreparedThreadCWD == cwd && cp.Core.AttachedInstanceID == n.InstanceID && cp.Core.SelectedThreadID == "" && cp.Core.ActiveQueueItemID == "" && len(cp.Core.QueuedQueueItemIDs) == 0
	})

	shutdown, cancelShutdown := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancelShutdown()
	if err = old.shutdownApplications(shutdown); err != nil {
		if db, e := sql.Open("sqlite", filepath.Join(root, "private-carriers.db")); e == nil {
			var state string
			var raw []byte
			if db.QueryRow(`SELECT state,record FROM carriers WHERE instance=?`, n.InstanceID).Scan(&state, &raw) == nil {
				var record privateCarrierRecord
				_ = json.Unmarshal(raw, &record)
				t.Logf("carrier state=%s inspection-id=%t inspection-seq=%d stop-id=%t", state, record.StopInspectionID != "", record.Inspection.Sequence, record.StopEffectID != "")
			}
			_ = db.Close()
		}
		if db, e := sql.Open("sqlite", filepath.Join(root, "private-chat.db")); e == nil {
			rows, q := db.Query(`SELECT payload FROM native_events ORDER BY sequence DESC LIMIT 8`)
			if q == nil {
				for rows.Next() {
					var raw []byte
					var event agentproto.Event
					if rows.Scan(&raw) == nil && json.Unmarshal(raw, &event) == nil {
						t.Logf("native kind=%s status=%s error=%s", event.Kind, event.Status, event.ErrorMessage)
					}
				}
				rows.Close()
			}
			rows, q = db.Query(`SELECT n.payload,d.state FROM native_commands n JOIN dispatch d ON d.id=n.dispatch_id ORDER BY d.rowid DESC LIMIT 8`)
			if q == nil {
				for rows.Next() {
					var raw []byte
					var status string
					var command agentproto.Command
					if rows.Scan(&raw, &status) == nil && json.Unmarshal(raw, &command) == nil {
						t.Logf("native command=%s state=%s", command.Kind, status)
					}
				}
				rows.Close()
			}
			_ = db.Close()
		}
		t.Fatalf("clean shutdown: %v", err)
	}
	server.Close()
	retiredDB, err := sql.Open("sqlite", "file:"+filepath.Join(root, "private-carriers.db")+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	var retiredState string
	err = retiredDB.QueryRow(`SELECT state FROM carriers WHERE instance=? AND incarnation=?`, n.InstanceID, n.CarrierIncarnation).Scan(&retiredState)
	_ = retiredDB.Close()
	if err != nil || retiredState != "retired" {
		t.Fatalf("old carrier was not durably retired: state=%q err=%v", retiredState, err)
	}
	snapshot, err := captureTeamLegacyFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	next := New("127.0.0.1:0", "127.0.0.1:0", gateway, agentproto.ServerIdentity{})
	if err = next.prepareApplicationCore(); err != nil {
		t.Fatal(err)
	}
	next.SetHeadlessRuntime(old.headlessRuntime)
	next.ConfigureAdmin(AdminRuntimeOptions{ConfigPath: old.headlessRuntime.Paths.ConfigFile, Services: defaultFeishuServices()})
	if err = next.configureWorkRuntime(ctx, teamPrivateCommands{app: next}, next.authorizeTeamControl); err != nil {
		t.Fatal(err)
	}
	if err = next.activateWorkRuntime(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	if err = next.configureApplicationIngress(ctx, feishu.NewMultiGatewayController(), lock); err != nil {
		t.Fatal(err)
	}
	nextServer, nextStart := startServer(next)
	defer nextServer.Close()
	r2 := next.applications
	r2.native.startHeadless = nextStart
	r2.ingress.stager = controlledSDKImageStager{path: imagePath}
	if err = r2.startWorkers(ctx, errs); err != nil {
		t.Fatal(err)
	}
	defer func() {
		finish, c := context.WithTimeout(context.Background(), 10*time.Second)
		defer c()
		_ = next.shutdownApplications(finish)
	}()
	before, err := r2.journal.LoadCheckpoint(ctx, o, epoch)
	if err != nil {
		t.Fatal(err)
	}
	if before.Core.RouteMode != "new_thread_ready" || before.Core.PreparedThreadCWD != cwd || before.Core.SelectedThreadID != "" || before.Core.AttachedInstanceID != n.InstanceID || before.Core.ActiveQueueItemID != "" || len(before.Core.QueuedQueueItemIDs) != 0 {
		t.Fatalf("unexpected reopened checkpoint: route=%s cwd=%q selected=%q attached=%q active=%q queued=%v", before.Core.RouteMode, before.Core.PreparedThreadCWD, before.Core.SelectedThreadID, before.Core.AttachedInstanceID, before.Core.ActiveQueueItemID, before.Core.QueuedQueueItemIDs)
	}
	beforeOps := len(gateway.snapshotOperations())
	send(next, "restart-first-hi", "hi")
	wait("first input did not finish", func() bool {
		cp, e := r2.journal.LoadCheckpoint(ctx, o, epoch)
		if e != nil {
			return false
		}
		for _, item := range cp.Core.QueueItems {
			if item != nil && item.SourceMessageID == "restart-first-hi" && item.Status == "completed" {
				return cp.Core.SelectedThreadID == "session" && cp.Core.ActiveQueueItemID == "" && cp.Core.AttachedInstanceID != "" && cp.Core.AttachedInstanceID != n.InstanceID
			}
		}
		return false
	})
	wait("first final did not reach gateway", func() bool {
		for _, op := range gateway.snapshotOperations()[beforeOps:] {
			raw, e := feishu.FreezePrivateOperation(op, next.daemonLifecycleID)
			if e == nil && strings.Contains(string(raw), "done") {
				return true
			}
		}
		return false
	})
	beforeOps = len(gateway.snapshotOperations())
	send(next, "restart-second-hi", "hello again")
	wait("second input did not finish", func() bool {
		cp, e := r2.journal.LoadCheckpoint(ctx, o, epoch)
		if e != nil {
			return false
		}
		for _, item := range cp.Core.QueueItems {
			if item != nil && item.SourceMessageID == "restart-second-hi" && item.Status == "completed" {
				return cp.Core.SelectedThreadID == "session" && cp.Core.ActiveQueueItemID == ""
			}
		}
		return false
	})
	wait("second final did not reach gateway", func() bool {
		for _, op := range gateway.snapshotOperations()[beforeOps:] {
			raw, e := feishu.FreezePrivateOperation(op, next.daemonLifecycleID)
			if e == nil && strings.Contains(string(raw), "done") {
				return true
			}
		}
		return false
	})
	frames, _ := os.ReadFile(trace)
	if bytes.Count(frames, []byte(`"method":"turn/start"`)) != 2 || !bytes.Contains(frames, []byte(`"text":"hi"`)) || !bytes.Contains(frames, []byte(`"text":"hello again"`)) {
		t.Fatalf("unexpected native turn evidence: %s", frames)
	}
}
