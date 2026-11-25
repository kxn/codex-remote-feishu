package daemon

import (
	"context"
	"database/sql"
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
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func TestPrivateSDKStructuredMediaThroughNativeAndNextTurn(t *testing.T) {
	helloFirst := true
	if runtime.GOOS == "windows" {
		t.Skip("POSIX wrapper fixture")
	}
	app, gateway := newApplicationRuntimeTestApp(t)
	r := app.applications
	stager := newStructuredUATStager(t)
	r.ingress.stager = stager
	cwd := t.TempDir()
	app.service.SetPersistedThreadCatalog(newWorkspaceJourneyCatalog{cwd: cwd})
	setTestWorkBotSettings(t, app, state.BotCapabilitySettingsRecord{GatewayID: "gateway", Backend: agentproto.BackendCodex, CodexProfileID: state.NativeCodexProfileID})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := app.headlessRuntime.Paths.StateDir
	native := filepath.Join(t.TempDir(), "codex")
	script := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then printf 'codex-cli 0.159.3-dev.1\\n'; exit; fi\nPRIVATE_CLEAN_CODEX=1 exec %q -test.run '^TestPrivateCleanRestartCodexProcess$'\n", exe)
	if err = os.WriteFile(native, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	managedRoot := filepath.Join(root, "codex-runtime")
	if err = os.MkdirAll(managedRoot, 0700); err != nil {
		t.Fatal(err)
	}
	pin, _ := json.Marshal(map[string]string{"binary": native, "version": "0.159.3-dev.1"})
	if err = os.WriteFile(filepath.Join(managedRoot, "exact-version.json"), pin, 0600); err != nil {
		t.Fatal(err)
	}
	trace := filepath.Join(t.TempDir(), "trace")
	releaseTurn := filepath.Join(t.TempDir(), "release-turn")
	app.headlessRuntime.BaseEnv = []string{"HOME=" + t.TempDir(), "PATH=" + os.Getenv("PATH"), "FIXTURE_TRACE=" + trace, "CLEAN_TURN_RELEASE=" + releaseTurn}
	app.headlessRuntime.CodexRealBinary = native
	app.relay.SetServerIdentity(agentproto.ServerIdentity{BinaryIdentity: agentproto.BinaryIdentity{Product: relayruntime.ProductName, Version: "fixture", BuildFingerprint: "fixture"}})
	server := httptest.NewServer(app.relay)
	defer server.Close()
	relayURL := "ws" + strings.TrimPrefix(server.URL, "http")
	spawned := make(chan struct{})
	helloGate := filepath.Join(t.TempDir(), "release-hello")
	releasePID := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(releasePID) })
	var child *exec.Cmd
	var childWait sync.WaitGroup
	r.native.startHeadless = func(v relayruntime.HeadlessLaunchOptions) (int, error) {
		child = exec.Command(exe, "-test.run=^TestPrivateOrderDelayedHelloWrapper$")
		child.Env = append(v.Env, "PRIVATE_OPENCODE_FIXTURE=wrapper", "FIXTURE_RELAY_URL="+relayURL, "FIXTURE_STATE_DIR="+root, "FIXTURE_CODEX_BIN="+native, "PRIVATE_ORDER_WRAPPER=1", fmt.Sprintf("PRIVATE_ORDER_HELLO_FIRST=%t", helloFirst), "PRIVATE_ORDER_HELLO_GATE="+helloGate)
		child.Dir = v.WorkDir
		child.Stdout = io.Discard
		child.Stderr = os.Stderr
		if e := child.Start(); e != nil {
			return 0, e
		}
		pid := child.Process.Pid
		childWait.Add(1)
		go func() { defer childWait.Done(); _ = child.Wait() }()
		close(spawned)
		if !helloFirst {
			return pid, nil
		}
		select {
		case <-releasePID:
			return pid, nil
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
	defer func() {
		releaseOnce.Do(func() { close(releasePID) })
		if child != nil && child.Process != nil {
			_ = child.Process.Kill()
		}
		childWait.Wait()
	}()
	errorsCh := make(chan error, 8)
	if err = r.startWorkers(ctx, errorsCh); err != nil {
		t.Fatal(err)
	}
	defer func() {
		stop, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = r.stopWorkers(stop)
		_ = r.drainNativeDeliveries(stop)
	}()
	raw := platformingress.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", SenderTenant: "tenant", EventID: "list", MessageID: "source", ChatID: "private-chat", ChatType: "p2p", SenderType: "user", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, MessageType: "text", RawContent: `{"text":"/list"}`, ReceivedAt: time.Now()}
	if err = r.hook.Receive(ctx, raw); err != nil {
		t.Fatal(err)
	}
	var card feishu.Operation
	for card.MessageID == "" {
		for _, op := range gateway.snapshotOperations() {
			if op.Kind == feishu.OperationSendCard && op.MessageID != "" {
				f, e := feishu.FreezePrivateOperation(op, app.daemonLifecycleID)
				var d any
				if e == nil && json.Unmarshal(f, &d) == nil && findPrivateCallback(d, "target_picker_confirm") != nil {
					card = op
					break
				}
			}
		}
		select {
		case <-ctx.Done():
			t.Fatal("picker was not published")
		case <-time.After(10 * time.Millisecond):
		}
	}
	for {
		if _, e := r.router.PublishedMessage(ctx, "tenant", "app", "private-chat", card.MessageID); e == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("picker publication was not registered")
		case <-time.After(10 * time.Millisecond):
		}
	}
	frozen, err := feishu.FreezePrivateOperation(card, app.daemonLifecycleID)
	if err != nil {
		t.Fatal(err)
	}
	var document any
	if err = json.Unmarshal(frozen, &document); err != nil {
		t.Fatal(err)
	}
	click := platformingress.HumanInteractionEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, ChatID: "private-chat", MessageID: card.MessageID, Kind: "card", ReceivedAt: time.Now()}
	if selectValue := findPrivateCallback(document, "target_picker_select_workspace"); selectValue != nil {
		click.EventID = "choose-workspace"
		click.Values = selectValue
		click.Option = cwd
		response, e := r.hook.ReceiveInteractionResponse(ctx, click)
		if e != nil {
			t.Fatal(e)
		}
		if e = json.Unmarshal(response.RawBody, &document); e != nil {
			t.Fatal(e)
		}
	}
	confirm := findPrivateCallback(document, "target_picker_confirm")
	if confirm == nil {
		t.Fatal("new-session confirm missing")
	}
	click.EventID = "confirm-new"
	click.Values = confirm
	click.Option = ""
	confirmResponse, confirmErr := r.hook.ReceiveInteractionResponse(ctx, click)
	if confirmErr != nil {
		t.Fatal(confirmErr)
	}
	_ = confirmResponse

	surface := gatewaypkg.SurfaceIDForInbound("gateway", "", "p2p", "user")
	readyInstance := ""
	for readyInstance == "" {
		status, e := r.processor.ReadRuntimeStatus(ctx)
		if e != nil {
			t.Fatal(e)
		}
		for _, s := range status.Surfaces {
			if s.SurfaceSessionID == surface && s.RouteMode == state.RouteModeNewThreadReady {
				if s.AttachedInstanceID == "" || s.PreparedThreadCWD != cwd || s.PendingHeadless != nil {
					t.Fatalf("incorrect new workspace: %+v", s)
				}
				for _, inst := range status.Instances {
					if inst.InstanceID == s.AttachedInstanceID && inst.CodexProfileID == state.NativeCodexProfileID && inst.Online {
						readyInstance = inst.InstanceID
						break
					}
				}
				if readyInstance == "" {
					t.Fatal("connected instance missing oauth profile")
				}
				break
			}
		}
		if readyInstance != "" {
			break
		}
		select {
		case e := <-errorsCh:
			t.Fatal(e)
		case <-ctx.Done():
			for _, v := range status.Surfaces {
				t.Logf("surface route=%s pending=%+v attached=%q profile=%q", v.RouteMode, v.PendingHeadless, v.AttachedInstanceID, v.CodexProfileID)
			}
			for _, v := range status.Instances {
				t.Logf("instance online=%v backend=%s profile=%q cwd=%q", v.Online, v.Backend, v.CodexProfileID, v.WorkspaceRoot)
			}
			for _, op := range gateway.snapshotOperations() {
				rawOp, _ := feishu.FreezePrivateOperation(op, app.daemonLifecycleID)
				t.Logf("operation=%s document=%s", op.Kind, rawOp)
			}
			t.Fatal("new session did not become ready")
		case <-time.After(20 * time.Millisecond):
		}
	}
	for {
		success := false
		for _, op := range gateway.snapshotOperations() {
			if op.Kind != feishu.OperationUpdateCard || op.MessageID != card.MessageID {
				continue
			}
			rawOp, e := feishu.FreezePrivateOperation(op, app.daemonLifecycleID)
			if e == nil && strings.Contains(string(rawOp), "已进入新会话待命") {
				success = true
				break
			}
		}
		if success {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("same-card success was not published")
		case <-time.After(20 * time.Millisecond):
		}
	}
	select {
	case <-spawned:
	default:
		t.Fatal("success without spawned carrier")
	}

	// In hello-first mode the visible success precedes PID return; in the
	// opposite order the earlier receipt must not claim the carrier is ready.
	if readyInstance == "" {
		t.Fatal("missing ready instance")
	}
	prepareStructuredUATInputs(t, ctx, r, raw)
	raw.EventID = "turn-one"
	raw.MessageID = "turn-one"
	raw.MessageType = "post"
	raw.ParentID = "quoted-original"
	raw.RawContent = `{"title":"title","content":[[{"tag":"text","text":"rich text"},{"tag":"img","image_key":"post-image"}]]}`
	raw.ReceivedAt = time.Now()
	if err = r.hook.Receive(ctx, raw); err != nil {
		t.Fatal(err)
	}
	releaseOnce.Do(func() { close(releasePID) })
	owner := privatechat.Owner{GatewayID: "gateway", SurfaceID: surface, ChatID: "private-chat", UserID: "user", TenantID: "tenant", AppID: "app"}
	waitDone := func(id string) {
		t.Helper()
		for {
			cp, e := r.journal.LoadCheckpoint(ctx, owner, 1)
			if e == nil {
				for _, item := range cp.Core.QueueItems {
					if item != nil && item.SourceMessageID == id && item.Status == state.QueueItemCompleted {
						return
					}
				}
			}
			select {
			case e := <-errorsCh:
				t.Fatal(e)
			case <-ctx.Done():
				t.Fatalf("turn %s did not complete", id)
			case <-time.After(20 * time.Millisecond):
			}
		}
	}
	waitFinals := func(want int) {
		t.Helper()
		for {
			count := 0
			for _, op := range gateway.snapshotOperations() {
				raw, e := feishu.FreezePrivateOperation(op, app.daemonLifecycleID)
				if e == nil && strings.Contains(string(raw), "done") {
					count++
				}
			}
			if count >= want {
				return
			}
			select {
			case e := <-errorsCh:
				t.Fatal(e)
			case <-ctx.Done():
				diagnostic, err := sql.Open("sqlite", "file:"+filepath.Join(root, "private-chat.db")+"?mode=ro")
				if err == nil {
					rows, queryErr := diagnostic.Query(`SELECT e.id,e.revision,e.ordinal,e.state,coalesce(json_extract(e.body,'$.event.Kind'),''),coalesce(json_extract(e.body,'$.event.Notice.Code'),''),coalesce(group_concat(o.order_key),'') FROM private_effect e LEFT JOIN private_effect_applied a ON a.effect_id=e.id LEFT JOIN private_publication_order o ON o.effect_id=e.id WHERE a.effect_id IS NULL GROUP BY e.id ORDER BY e.revision,e.ordinal LIMIT 10`)
					if queryErr == nil {
						for rows.Next() {
							var id, state, kind, notice, keys string
							var rev, ordinal int64
							if rows.Scan(&id, &rev, &ordinal, &state, &kind, &notice, &keys) == nil {
								t.Logf("pending effect id=%s revision=%d ordinal=%d state=%s kind=%s notice=%s keys=%s", id, rev, ordinal, state, kind, notice, keys)
							}
						}
						rows.Close()
					} else {
						t.Logf("diagnostic query: %v", queryErr)
					}
					diagnostic.Close()
				}
				t.Fatalf("only %d visible finals, want %d", count, want)
			case <-time.After(20 * time.Millisecond):
			}
		}
	}
	waitDone("turn-one")
	waitFinals(1)
	db, e := sql.Open("sqlite", "file:"+filepath.Join(root, "private-chat.db")+"?mode=ro")
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	for {
		var count int
		e = db.QueryRowContext(ctx, `SELECT count(*) FROM private_effect e JOIN private_effect_applied a ON a.effect_id=e.id JOIN private_local_receipts r ON r.effect_id=e.id WHERE json_extract(e.body,'$.event.DaemonCommand.Kind')='headless.start' AND e.state='confirmed'`).Scan(&count)
		if e != nil {
			t.Fatal(e)
		}
		if count > 0 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("late PID receipt was not applied")
		case <-time.After(20 * time.Millisecond):
		}
	}
	status, e := r.processor.ReadRuntimeStatus(ctx)
	if e != nil {
		t.Fatal(e)
	}
	for _, v := range status.Surfaces {
		if v.SurfaceSessionID == surface && (v.AttachedInstanceID != readyInstance || v.PendingHeadless != nil) {
			t.Fatal("late launch receipt changed ready carrier")
		}
	}
	raw.MessageType = "text"
	raw.ParentID = ""
	raw.EventID = "turn-two"
	raw.MessageID = "turn-two"
	raw.RawContent = `{"text":"followup"}`
	raw.ReceivedAt = time.Now()
	if err = r.hook.Receive(ctx, raw); err != nil {
		t.Fatal(err)
	}
	waitDone("turn-two")
	waitFinals(2)
	raw.EventID, raw.MessageID, raw.MessageType = "failed-image", "failed-image", "image"
	raw.RawContent, raw.ReceivedAt = `{"image_key":"bad"}`, time.Now()
	if err = r.hook.Receive(ctx, raw); err != nil {
		t.Fatal(err)
	}
	select {
	case <-stager.started:
	case <-ctx.Done():
		t.Fatal("controlled download did not start")
	}
	raw.EventID, raw.MessageID, raw.MessageType = "after-failure", "after-failure", "text"
	raw.RawContent, raw.ReceivedAt = `{"text":"continue after failure"}`, time.Now()
	if err = r.hook.Receive(ctx, raw); err != nil {
		t.Fatal(err)
	}
	close(stager.release)
	waitDone("after-failure")
	waitFinals(3)
	data, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), `"method":"turn/start"`) != 3 {
		t.Fatal("native turn count mismatch")
	}
	for _, expected := range []string{"quoted original", "rich text", "a.txt", "b.txt", "continue after failure"} {
		if !strings.Contains(string(data), expected) {
			t.Fatalf("native inputs omitted %q: %s", expected, data)
		}
	}
	if strings.Count(string(data), `"type":"localImage"`) < 3 {
		t.Fatalf("native image cluster missing: %s", data)
	}

}
