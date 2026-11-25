package daemon

import (
	"bufio"
	"context"
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
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func TestPrivateSDKUnboundHiExistingWorkspaceNewSessionJourney(t *testing.T) {
	helloFirst := true
	if runtime.GOOS == "windows" {
		t.Skip("POSIX wrapper fixture")
	}
	app, gateway := newApplicationRuntimeTestApp(t)
	r := app.applications
	cwd := t.TempDir()
	app.service.SetPersistedThreadCatalog(existingWorkspaceJourneyCatalog{cwd: cwd})
	setTestWorkBotSettings(t, app, state.BotCapabilitySettingsRecord{GatewayID: "gateway", Backend: agentproto.BackendCodex, CodexProfileID: state.NativeCodexProfileID})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := app.headlessRuntime.Paths.StateDir
	native := filepath.Join(t.TempDir(), "codex")
	script := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then printf 'codex-cli 0.159.3-dev.1\\n'; exit; fi\nPRIVATE_EXISTING_CODEX=1 exec %q -test.run '^TestPrivateExistingWorkspaceNativeProcess$'\n", exe)
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
	first := raw
	first.EventID, first.MessageID, first.RawContent = "unbound-hi", "unbound-hi", `{"text":"hi"}`
	if err = r.hook.Receive(ctx, first); err != nil {
		t.Fatal(err)
	}
	for {
		found := false
		for _, op := range gateway.snapshotOperations() {
			f, _ := feishu.FreezePrivateOperation(op, app.daemonLifecycleID)
			if strings.Contains(string(f), "/list") {
				found = true
				break
			}
		}
		if found {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("unbound hi did not present /list")
		case <-time.After(20 * time.Millisecond):
		}
	}
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
	selectSession := findPrivateCallback(document, "target_picker_select_session")
	if selectSession == nil {
		t.Fatal("existing session choice missing")
	}
	click.EventID, click.Values, click.Option = "choose-existing-session", selectSession, "thread:session"
	selected, e := r.hook.ReceiveInteractionResponse(ctx, click)
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(selected.RawBody, &document); e != nil {
		t.Fatal(e)
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
	releaseOnce.Do(func() { close(releasePID) })
	for {
		found := false
		for _, op := range gateway.snapshotOperations() {
			f, _ := feishu.FreezePrivateOperation(op, app.daemonLifecycleID)
			if op.MessageID == card.MessageID && strings.Contains(string(f), "已切换会话") {
				found = true
				break
			}
		}
		if found {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("existing session attachment was not visibly confirmed")
		case <-time.After(20 * time.Millisecond):
		}
	}
	beforeNew := len(gateway.snapshotOperations())
	newMessage := raw
	newMessage.EventID, newMessage.MessageID, newMessage.RawContent, newMessage.ReceivedAt = "new-command", "new-command", `{"text":"/new"}`, time.Now()
	if err = r.hook.Receive(ctx, newMessage); err != nil {
		t.Fatal(err)
	}
	for {
		found := false
		ops := gateway.snapshotOperations()
		for _, op := range ops[beforeNew:] {
			f, _ := feishu.FreezePrivateOperation(op, app.daemonLifecycleID)
			if strings.Contains(string(f), "下一条文本") && strings.Contains(string(f), "会话") {
				found = true
				break
			}
		}
		if found {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("new session ready was not visible")
		case <-time.After(20 * time.Millisecond):
		}
	}
	user := sdkJourneyUser{t: t, ctx: ctx, gateway: gateway, errors: errorsCh, cwd: cwd}
	beforeTurn := len(gateway.snapshotOperations())
	raw.EventID = "turn-one"
	raw.MessageID = "turn-one"
	raw.RawContent = `{"text":"hi"}`
	raw.ReceivedAt = time.Now()
	if err = r.hook.Receive(ctx, raw); err != nil {
		t.Fatal(err)
	}
	releaseOnce.Do(func() { close(releasePID) })
	user.waitFinal(app, beforeTurn, "turn-one", "done")
	beforeTurn = len(gateway.snapshotOperations())
	raw.EventID = "turn-two"
	raw.MessageID = "turn-two"
	raw.RawContent = `{"text":"followup"}`
	raw.ReceivedAt = time.Now()
	if err = r.hook.Receive(ctx, raw); err != nil {
		t.Fatal(err)
	}
	user.waitFinal(app, beforeTurn, "turn-two", "done")
	data, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), `"method":"turn/start"`) != 2 {
		t.Fatal("native turn count mismatch")
	}
	if strings.Count(string(data), `"method":"thread/start"`) != 1 || strings.Count(string(data), `"threadId":"new-session"`) < 2 {
		t.Fatalf("two turns did not use the newly created session: %s", data)
	}
	if !helloFirst && strings.Contains(string(data), `"early"`) {
		t.Fatal("input rejected during startup was unexpectedly sent later")
	}
}

type existingWorkspaceJourneyCatalog struct{ cwd string }

func (c existingWorkspaceJourneyCatalog) RecentThreads(int) ([]state.ThreadRecord, error) {
	return []state.ThreadRecord{{ThreadID: "session", CWD: c.cwd, Name: "Previous session"}}, nil
}
func (c existingWorkspaceJourneyCatalog) RecentWorkspaces(int) (map[string]time.Time, error) {
	return map[string]time.Time{c.cwd: time.Now()}, nil
}
func (c existingWorkspaceJourneyCatalog) ThreadByID(id string) (*state.ThreadRecord, error) {
	if id != "session" {
		return nil, nil
	}
	return &state.ThreadRecord{ThreadID: id, CWD: c.cwd, Name: "Previous session"}, nil
}

// This subprocess is a controlled native protocol peer; selection, ownership,
// launch and routing are all driven by the real SDK journey above.
func TestPrivateExistingWorkspaceNativeProcess(t *testing.T) {
	if os.Getenv("PRIVATE_EXISTING_CODEX") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	thread := "session"
	turns := 0
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		var req struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
		}
		if json.Unmarshal(line, &req) != nil {
			continue
		}
		if req.Method == "thread/start" || req.Method == "thread/resume" || req.Method == "turn/start" {
			if f, e := os.OpenFile(os.Getenv("FIXTURE_TRACE"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600); e == nil {
				_, _ = f.Write(append(line, '\n'))
				_ = f.Close()
			}
		}
		switch req.Method {
		case "initialize":
			fmt.Printf(`{"id":%q,"result":{}}`+"\n", req.ID)
		case "thread/start":
			thread = "new-session"
			cwd, _ := os.Getwd()
			fmt.Printf(`{"id":%q,"result":{"model":"fixture-model","thread":{"id":%q,"cwd":%q}}}`+"\n", req.ID, thread, cwd)
		case "thread/resume":
			thread = "session"
			cwd, _ := os.Getwd()
			fmt.Printf(`{"id":%q,"result":{"model":"fixture-model","thread":{"id":%q,"cwd":%q}}}`+"\n", req.ID, thread, cwd)
		case "turn/start":
			turns++
			turn := fmt.Sprintf("turn-%d", turns)
			fmt.Printf(`{"id":%q,"result":{"turn":{"id":%q}}}`+"\n", req.ID, turn)
			fmt.Printf(`{"method":"turn/started","params":{"threadId":%q,"turn":{"id":%q}}}`+"\n", thread, turn)
			fmt.Printf(`{"method":"item/completed","params":{"threadId":%q,"turnId":%q,"item":{"id":"answer","type":"agentMessage","text":"done"}}}`+"\n", thread, turn)
			fmt.Printf(`{"method":"turn/completed","params":{"threadId":%q,"turn":{"id":%q,"status":"completed"}}}`+"\n", thread, turn)
		case "thread/read":
			cwd, _ := os.Getwd()
			fmt.Printf(`{"id":%q,"result":{"thread":{"id":%q,"cwd":%q,"status":{"type":"idle"}}}}`+"\n", req.ID, thread, cwd)
		case "thread/goal/get":
			fmt.Printf(`{"id":%q,"result":{"goal":null}}`+"\n", req.ID)
		case "thread/list", "model/list":
			fmt.Printf(`{"id":%q,"result":{"data":[]}}`+"\n", req.ID)
		case "turn/interrupt":
			fmt.Printf(`{"id":%q,"result":{}}`+"\n", req.ID)
		}
	}
	os.Exit(0)
}
