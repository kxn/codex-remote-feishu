package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
)

func TestPrivateNonblockingInputNativePeer(t *testing.T) {
	if os.Getenv("PRIVATE_NONBLOCKING_PEER") != "1" {
		return
	}
	cwd, _ := os.Getwd()
	turn := 0
	questionTurn := 2
	if os.Getenv("PRIVATE_INPUT_FIRST_TURN") == "1" {
		questionTurn = 1
	}
	questionTurnID := fmt.Sprintf("input-turn-%d", questionTurn)
	scan := bufio.NewScanner(os.Stdin)
	for scan.Scan() {
		var frame struct {
			ID     any            `json:"id"`
			Method string         `json:"method"`
			Result map[string]any `json:"result"`
		}
		if json.Unmarshal(scan.Bytes(), &frame) != nil {
			continue
		}
		if frame.Method == "" && frame.ID == "question-1" {
			answers, ok := frame.Result["answers"].(map[string]any)
			choice, yes := answers["choice"].(map[string]any)
			values, valid := choice["answers"].([]any)
			if !ok || !yes || !valid || len(values) != 1 || values[0] != "One" {
				os.Exit(53)
			}
			fmt.Printf(`{"method":"serverRequest/resolved","params":{"requestId":"question-1","threadId":"input-thread","turnId":%q}}`+"\n", questionTurnID)
			fmt.Printf(`{"method":"item/completed","params":{"threadId":"input-thread","turnId":%q,"item":{"id":"answer","type":"agentMessage","text":"input dialogue complete"}}}`+"\n", questionTurnID)
			fmt.Printf(`{"method":"turn/completed","params":{"threadId":"input-thread","turn":{"id":%q,"status":"completed"}}}`+"\n", questionTurnID)
			continue
		}
		switch frame.Method {
		case "initialize":
			fmt.Printf(`{"id":%q,"result":{}}`+"\n", frame.ID)
		case "thread/start", "thread/resume":
			fmt.Printf(`{"id":%q,"result":{"model":"fixture-model","thread":{"id":"input-thread","cwd":%q}}}`+"\n", frame.ID, cwd)
			if frame.Method == "thread/start" {
				fmt.Printf(`{"method":"thread/started","params":{"thread":{"id":"input-thread","cwd":%q,"source":"cli"}}}`+"\n", cwd)
			}
		case "turn/start":
			turn++
			id := fmt.Sprintf("input-turn-%d", turn)
			fmt.Printf(`{"id":%q,"result":{"turn":{"id":%q}}}`+"\n", frame.ID, id)
			fmt.Printf(`{"method":"turn/started","params":{"threadId":"input-thread","turn":{"id":%q}}}`+"\n", id)
			if turn == questionTurn {
				blocking := os.Getenv("PRIVATE_INPUT_BLOCKING") == "1"
				fmt.Printf(`{"id":"question-1","method":"item/tool/requestUserInput","params":{"threadId":"input-thread","turnId":%q,"itemId":"question-item","isBlocking":%t,"questions":[{"id":"choice","header":"Choice","question":"Choose an option","options":[{"label":"One","description":"First"},{"label":"Two","description":"Second"}]}]}}`+"\n", id, blocking)
				continue
			}
			fmt.Printf(`{"method":"item/completed","params":{"threadId":"input-thread","turnId":%q,"item":{"id":"answer","type":"agentMessage","text":"input dialogue complete"}}}`+"\n", id)
			fmt.Printf(`{"method":"turn/completed","params":{"threadId":"input-thread","turn":{"id":%q,"status":"completed"}}}`+"\n", id)
		case "thread/read":
			fmt.Printf(`{"id":%q,"result":{"thread":{"id":"input-thread","cwd":%q,"status":{"type":"idle"}}}}`+"\n", frame.ID, cwd)
		case "thread/goal/get":
			fmt.Printf(`{"id":%q,"result":{"goal":null}}`+"\n", frame.ID)
		case "turn/interrupt":
			fmt.Printf(`{"id":%q,"result":{}}`+"\n", frame.ID)
			fmt.Println(`{"method":"serverRequest/resolved","params":{"requestId":"question-1","threadId":"input-thread","turnId":"input-turn-2"}}`)
			fmt.Println(`{"method":"turn/completed","params":{"threadId":"input-thread","turn":{"id":"input-turn-2","status":"interrupted"}}}`)
		case "thread/list", "model/list":
			fmt.Printf(`{"id":%q,"result":{"data":[]}}`+"\n", frame.ID)
		}
	}
	os.Exit(0)
}

func findInputChoice(v any) map[string]any {
	switch x := v.(type) {
	case map[string]any:
		if x["kind"] == "request_respond" && x["private_view"] != nil {
			if a, ok := x["request_answers"].(map[string]any); ok {
				if v, ok := a["choice"].([]any); ok && len(v) == 1 && v[0] == "One" {
					return x
				}
			}
		}
		for _, child := range x {
			if found := findInputChoice(child); found != nil {
				return found
			}
		}
	case []any:
		for _, child := range x {
			if found := findInputChoice(child); found != nil {
				return found
			}
		}
	}
	return nil
}

func TestPrivateSDKNonblockingInputKeepsDialogueUsable(t *testing.T) {
	for _, first := range []bool{false, true} {
		t.Run(fmt.Sprintf("first-turn=%t", first), func(t *testing.T) { runNonblockingInputJourney(t, first) })
	}
}

func runNonblockingInputJourney(t *testing.T, first bool) {
	f := newContinuityFixtureBare(t, nil)
	t.Cleanup(func() {
		if t.Failed() {
			status, err := f.app.applications.processor.ReadRuntimeStatus(context.Background())
			t.Logf("runtime status: %+v, err=%v", status, err)
			trace, _ := os.ReadFile(f.trace)
			t.Logf("peer trace: %s", trace)
		}
	})
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	firstEnv := "0"
	if first {
		firstEnv = "1"
	}
	script := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then printf 'codex-cli 0.159.3-dev.1\\n'; exit; fi\nPRIVATE_NONBLOCKING_PEER=1 PRIVATE_INPUT_FIRST_TURN=%s exec %q -test.run '^TestPrivateNonblockingInputNativePeer$'\n", firstEnv, exe)
	if err = os.WriteFile(f.app.headlessRuntime.CodexRealBinary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	u := f.user
	n := len(f.gateway.snapshotOperations())
	u.send(f.app, "input-cold", "hi")
	u.waitVisible(f.app, n, "/list")
	u.chooseNew(f.app, "input-new")
	n = len(f.gateway.snapshotOperations())
	if !first {
		u.send(f.app, "input-established", "establish session")
		u.waitFinal(f.app, n, "input-established", "input dialogue complete")
		n = len(f.gateway.snapshotOperations())
	}
	u.send(f.app, "input-first", "first")
	u.waitVisible(f.app, n, "Choose an option")
	var choice map[string]any
	var message string
	for choice == nil {
		for _, op := range f.gateway.snapshotOperations()[n:] {
			if op.MessageID == "" {
				continue
			}
			raw, e := feishu.FreezePrivateOperation(op, f.app.daemonLifecycleID)
			var doc any
			if e == nil && json.Unmarshal(raw, &doc) == nil {
				if choice = findInputChoice(doc); choice != nil {
					message = op.MessageID
					break
				}
			}
		}
		if choice == nil {
			select {
			case <-f.ctx.Done():
				t.Fatal("answer card absent")
			case <-time.After(20 * time.Millisecond):
			}
		}
	}
	queued := len(f.gateway.snapshotOperations())
	u.send(f.app, "input-queued", "continue")
	u.waitVisible(f.app, queued, "OneSecond")
	click := platformingress.HumanInteractionEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", EventID: "input-choice", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, ChatID: "private-chat", MessageID: message, Kind: "card", Values: choice, ReceivedAt: time.Now()}
	if _, err = f.app.applications.hook.ReceiveInteractionResponse(f.ctx, click); err != nil {
		t.Fatal(err)
	}
	u.waitFinal(f.app, n, "input-first", "input dialogue complete")
	u.waitFinal(f.app, queued, "input-queued", "input dialogue complete")
	n = len(f.gateway.snapshotOperations())
	u.send(f.app, "input-next", "next")
	u.waitFinal(f.app, n, "input-next", "input dialogue complete")
}
