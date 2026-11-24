package daemon

import (
	"bufio"
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

// The fake Codex process responds to real wrapper frames. Gateway SDK input,
// owner journal, shutdown, carrier retirement and restart are all production.
func TestPrivateSDKCleanShutdownRestoresSelectedSession(t *testing.T) {
	testPrivateSDKCleanShutdownRestoresSelectedSession(t, false, false)
}
func TestPrivateSDKCleanShutdownRecoversQueuedInputWithoutNewMessage(t *testing.T) {
	testPrivateSDKCleanShutdownRestoresSelectedSession(t, true, false)
}

func TestPrivateSDKCleanShutdownRecoversFrozenUnstartedPrompt(t *testing.T) {
	testPrivateSDKCleanShutdownRestoresSelectedSession(t, false, true)
}

func testPrivateSDKCleanShutdownRestoresSelectedSession(t *testing.T, queuedAtShutdown, frozenUnstarted bool) {
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
	send(old, "seed", "establish session")
	wait("initial selected turn", func() bool {
		cp, e := r.journal.LoadCheckpoint(ctx, o, epoch)
		return e == nil && cp.Core.SelectedThreadID == "session" && cp.Core.ActiveQueueItemID == ""
	})
	send(old, "active", "active turn")
	wait("second turn", func() bool { b, _ := os.ReadFile(trace); return bytes.Count(b, []byte(`"method":"turn/start"`)) >= 2 })
	image := raw
	image.EventID = "queued-image"
	image.MessageID = image.EventID
	image.MessageType = "image"
	image.RawContent = `{"image_key":"fixture"}`
	if err = r.hook.Receive(ctx, image); err != nil {
		t.Fatal(err)
	}
	wait("queued image staged", func() bool {
		cp, e := r.journal.LoadCheckpoint(ctx, o, epoch)
		return e == nil && len(cp.Core.StagedImages) > 0
	})
	send(old, "queued", "queued text")
	wait("queued item", func() bool {
		cp, e := r.journal.LoadCheckpoint(ctx, o, epoch)
		return e == nil && len(cp.Core.QueuedQueueItemIDs) > 0
	})
	if !queuedAtShutdown {
		if err = os.WriteFile(release, []byte("finish"), 0600); err != nil {
			t.Fatal(err)
		}
		wait("queued turn completed", func() bool {
			cp, e := r.journal.LoadCheckpoint(ctx, o, epoch)
			if e != nil {
				return false
			}
			for _, item := range cp.Core.QueueItems {
				if item != nil && item.SourceMessageID == "queued" && item.Status == "completed" {
					return true
				}
			}
			return false
		})
	}
	image.EventID = "restart-image"
	image.MessageID = image.EventID
	if err = r.hook.Receive(ctx, image); err != nil {
		t.Fatal(err)
	}
	wait("staged image", func() bool {
		cp, e := r.journal.LoadCheckpoint(ctx, o, epoch)
		return e == nil && len(cp.Core.StagedImages) > 0
	})
	if frozenUnstarted {
		if err = r.stopWorkers(ctx); err != nil {
			t.Fatal(err)
		}
		send(old, "frozen-before-shutdown", "frozen unsent")
		page, e := r.journal.PendingIngressWorkPage(ctx, 128, 0)
		if e != nil {
			t.Fatal(e)
		}
		var seq int64
		for _, item := range page {
			if item.Owner == o && item.Generation == epoch && item.Lane == "input" {
				seq = item.Sequence
			}
		}
		if seq == 0 {
			t.Fatal("missing durable frozen input")
		}
		effects, e := r.processor.ApplyIngress(ctx, o, epoch, seq)
		if e != nil {
			t.Fatal(e)
		}
		found := false
		for _, effect := range effects {
			event, e := privatechat.DecodeEffectEvent(effect.Effect.Body)
			if e != nil {
				t.Fatal(e)
			}
			if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend {
				found = true
				dispatched, e := r.journal.HasNativeDispatch(ctx, effect.Effect.ID)
				if e != nil || dispatched {
					t.Fatalf("prompt already dispatched: %v %v", dispatched, e)
				}
			}
		}
		if !found {
			t.Fatal("missing frozen prompt effect")
		}
	}
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
	if !frozenUnstarted && len(before.Core.StagedImages) == 0 {
		t.Fatal("staged image lost after clean restart")
	}
	queuedPreserved := false
	for _, item := range before.Core.QueueItems {
		if item != nil && item.SourceMessageID == "queued" {
			queuedPreserved = true
		}
	}
	if !queuedPreserved {
		t.Fatal("queued input evidence lost after clean restart")
	}
	if frozenUnstarted {
		wait("frozen prompt did not resume without new message", func() bool {
			cp, e := r2.journal.LoadCheckpoint(ctx, o, epoch)
			if e != nil {
				return false
			}
			for _, item := range cp.Core.QueueItems {
				if item != nil && item.SourceMessageID == "frozen-before-shutdown" && item.Status == "completed" {
					return cp.Core.SelectedThreadID == "session"
				}
			}
			return false
		})
	}
	if queuedAtShutdown {
		t.Logf("post-shutdown checkpoint route=%s active=%s queued=%v holds=%+v", before.Core.RouteMode, before.Core.ActiveQueueItemID, before.Core.QueuedQueueItemIDs, before.Core.Holds)
		for id, item := range before.Core.QueueItems {
			if item != nil {
				t.Logf("post-shutdown queue id=%s source=%s status=%s", id, item.SourceMessageID, item.Status)
			}
		}
		wait("queued input did not recover without new message", func() bool {
			cp, e := r2.journal.LoadCheckpoint(ctx, o, epoch)
			if e != nil {
				return false
			}
			for _, item := range cp.Core.QueueItems {
				if item != nil && item.SourceMessageID == "queued" && item.Status == "completed" {
					return cp.Core.SelectedThreadID == "session"
				}
			}
			return false
		})
	}
	send(next, "after-restart", "continue same session")
	wait("replacement did not resume original session", func() bool {
		cp, e := r2.journal.LoadCheckpoint(ctx, o, epoch)
		if e != nil {
			return false
		}
		completed := false
		for _, item := range cp.Core.QueueItems {
			if item != nil && item.SourceMessageID == "after-restart" && item.Status == "completed" {
				completed = true
			}
		}
		return completed && cp.Core.SelectedThreadID == "session" && cp.Core.AttachedInstanceID != "" && cp.Core.AttachedInstanceID != n.InstanceID && cp.Core.ActiveQueueItemID == ""
	})
	data, _ := os.ReadFile(trace)
	wantTurns := 4
	if frozenUnstarted {
		wantTurns = 5
	}
	if bytes.Count(data, []byte(`"method":"turn/start"`)) != wantTurns {
		t.Fatalf("unexpected native input count: %d", bytes.Count(data, []byte(`"method":"turn/start"`)))
	}
	lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
	last := lines[len(lines)-1]
	queuedTurn := lines[2]
	if !bytes.Contains(queuedTurn, []byte("queued text")) || !bytes.Contains(queuedTurn, []byte(imagePath)) || !bytes.Contains(queuedTurn, []byte(`"threadId":"session"`)) {
		t.Fatal("queued text/image lost original session or ordering")
	}
	if !bytes.Contains(last, []byte("continue same session")) || (!frozenUnstarted && !bytes.Contains(last, []byte(imagePath))) || !bytes.Contains(last, []byte(`"threadId":"session"`)) {
		t.Fatal("post-restart text/image did not reach the same native turn")
	}
}

// Controlled native protocol process; turn 2 remains active until the test
// explicitly releases it, allowing a real queued image/text input to form.
func TestPrivateCleanRestartCodexProcess(t *testing.T) {
	if os.Getenv("PRIVATE_CLEAN_CODEX") != "1" {
		return
	}
	var out sync.Mutex
	write := func(format string, args ...any) { out.Lock(); defer out.Unlock(); fmt.Printf(format, args...) }
	scanner := bufio.NewScanner(os.Stdin)
	turns := 0
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		var f struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
		}
		if json.Unmarshal(line, &f) != nil {
			continue
		}
		if f.Method == "turn/start" {
			if file, e := os.OpenFile(os.Getenv("FIXTURE_TRACE"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600); e == nil {
				_, _ = file.Write(append(line, '\n'))
				_ = file.Close()
			}
		}
		switch f.Method {
		case "initialize":
			write(`{"id":%q,"result":{}}`+"\n", f.ID)
		case "thread/start", "thread/resume":
			cwd, _ := os.Getwd()
			write(`{"id":%q,"result":{"thread":{"id":"session","cwd":%q}}}`+"\n", f.ID, cwd)
		case "turn/start":
			turns++
			turn := fmt.Sprintf("turn-%d", turns)
			write(`{"id":%q,"result":{"turn":{"id":%q}}}`+"\n", f.ID, turn)
			write(`{"method":"turn/started","params":{"threadId":"session","turn":{"id":%q}}}`+"\n", turn)
			finish := func() {
				write(`{"method":"item/completed","params":{"threadId":"session","turnId":%q,"item":{"id":"answer","type":"agentMessage","text":"done"}}}`+"\n", turn)
				write(`{"method":"turn/completed","params":{"threadId":"session","turn":{"id":%q,"status":"completed"}}}`+"\n", turn)
			}
			if bytes.Contains(line, []byte(`"text":"active turn"`)) {
				go func() {
					for {
						if _, e := os.Stat(os.Getenv("CLEAN_TURN_RELEASE")); e == nil {
							finish()
							return
						}
						time.Sleep(10 * time.Millisecond)
					}
				}()
			} else {
				finish()
			}
		case "thread/read":
			cwd, _ := os.Getwd()
			write(`{"id":%q,"result":{"thread":{"id":"session","cwd":%q,"status":{"type":"idle"}}}}`+"\n", f.ID, cwd)
		case "thread/goal/get":
			write(`{"id":%q,"result":{"goal":null}}`+"\n", f.ID)
		case "thread/list":
			write(`{"id":%q,"result":{"data":[]}}`+"\n", f.ID)
		case "model/list":
			write(`{"id":%q,"result":{"data":[]}}`+"\n", f.ID)
		case "turn/interrupt":
			write(`{"id":%q,"result":{}}`+"\n", f.ID)
			write(`{"method":"turn/completed","params":{"threadId":"session","turn":{"id":"turn-2","status":"interrupted"}}}` + "\n")
		}
	}
	os.Exit(0)
}
