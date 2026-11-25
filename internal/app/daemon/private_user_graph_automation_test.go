package daemon

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// External native peer: returns a genuine protocol interruption, then a
// completed automatic continuation. It never writes application state.
func TestPrivateAutomationNativeProcess(t *testing.T) {
	if os.Getenv("PRIVATE_AUTOMATION_NATIVE") != "1" {
		return
	}
	cwd, _ := os.Getwd()
	turn := 0
	scan := bufio.NewScanner(os.Stdin)
	for scan.Scan() {
		var f struct {
			ID     any             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(scan.Bytes(), &f) != nil {
			continue
		}
		if file, err := os.OpenFile(os.Getenv("GRAPH_TRACE"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600); err == nil {
			fmt.Fprintln(file, scan.Text())
			file.Close()
		}
		respond := func(body string) { fmt.Printf(`{"id":%q,"result":%s}`+"\n", f.ID, body) }
		switch f.Method {
		case "initialize":
			respond(`{}`)
		case "thread/start", "thread/resume":
			respond(fmt.Sprintf(`{"model":"fixture-model","thread":{"id":"graph-a","cwd":%q}}`, cwd))
		case "thread/list", "model/list":
			respond(`{"data":[]}`)
		case "thread/read":
			respond(fmt.Sprintf(`{"thread":{"id":"graph-a","cwd":%q,"status":{"type":"idle"}}}`, cwd))
		case "thread/goal/get":
			respond(`{"goal":null}`)
		case "turn/start":
			turn++
			id := fmt.Sprintf("automation-%d", turn)
			respond(fmt.Sprintf(`{"turn":{"id":%q}}`, id))
			fmt.Printf(`{"method":"turn/started","params":{"threadId":"graph-a","turn":{"id":%q}}}`+"\n", id)
			if strings.Contains(string(f.Params), "trigger-stream-failure") {
				fmt.Printf(`{"method":"error","params":{"threadId":"graph-a","turnId":%q,"willRetry":false,"error":{"message":"stream closed","codexErrorInfo":"responseStreamDisconnected"}}}`+"\n", id)
				fmt.Printf(`{"method":"turn/completed","params":{"threadId":"graph-a","turn":{"id":%q,"status":"interrupted","error":{"message":"stream closed"}}}}`+"\n", id)
				continue
			}
			text := "automation-user-final"
			if strings.Contains(string(f.Params), "上一次响应因上游推理中断") {
				text = "automation-continued-final"
			}
			if strings.Contains(string(f.Params), "trigger-whip") {
				text = "automation-needs-more"
			}
			if strings.Contains(string(f.Params), "你看还有没有别的任务需要完成") {
				text = "老板不要再打我了，真的没有事情干了"
			}
			fmt.Printf(`{"method":"item/completed","params":{"threadId":"graph-a","turnId":%q,"item":{"id":%q,"type":"agentMessage","text":%q}}}`+"\n", id, id, text)
			fmt.Printf(`{"method":"turn/completed","params":{"threadId":"graph-a","turn":{"id":%q,"status":"completed"}}}`+"\n", id)
		}
	}
	os.Exit(0)
}

func TestPrivateSDKUserGraphAutomationAndNextMessage(t *testing.T) {
	a, b := filepath.Join(t.TempDir(), "a"), filepath.Join(t.TempDir(), "b")
	for _, cwd := range []string{a, b} {
		if err := os.MkdirAll(cwd, 0700); err != nil {
			t.Fatal(err)
		}
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	script := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then printf 'codex-cli 0.159.3-dev.1\\n'; exit; fi\nPRIVATE_AUTOMATION_NATIVE=1 exec %q -test.run '^TestPrivateAutomationNativeProcess$'\n", exe)
	g := newGraphEnv(t, a, b, func(app *App, _ *messageIDAssigningGateway) {
		if e := os.WriteFile(app.headlessRuntime.CodexRealBinary, []byte(script), 0700); e != nil {
			t.Fatal(e)
		}
	})
	u := g.user
	before := len(u.gateway.snapshotOperations())
	u.send(g.app, "automation-unbound-hi", "hi")
	u.waitVisible(g.app, before, "/list")
	u.chooseNew(g.app, "automation-target")
	before = len(u.gateway.snapshotOperations())
	u.send(g.app, "continue-on", "/autocontinue on")
	u.waitVisible(g.app, before, "自动继续")
	before = len(u.gateway.snapshotOperations())
	u.send(g.app, "stream-failure", "trigger-stream-failure")
	u.waitFinal(g.app, before, "stream-failure", "automation-continued-final")
	before = len(u.gateway.snapshotOperations())
	u.send(g.app, "continue-off", "/autocontinue off")
	u.waitVisible(g.app, before, "自动继续")
	before = len(u.gateway.snapshotOperations())
	u.send(g.app, "whip-on", "/autowhip on")
	u.waitVisible(g.app, before, "AutoWhip")
	before = len(u.gateway.snapshotOperations())
	u.send(g.app, "whip-trigger", "trigger-whip")
	u.waitFinal(g.app, before, "whip-trigger", "老板不要再打我了，真的没有事情干了")
	before = len(u.gateway.snapshotOperations())
	u.send(g.app, "whip-off", "/autowhip off")
	u.waitVisible(g.app, before, "AutoWhip")
	before = len(u.gateway.snapshotOperations())
	u.send(g.app, "automation-next", "ordinary after automation")
	u.waitFinal(g.app, before, "automation-next", "automation-user-final")
	trace, err := os.ReadFile(g.trace)
	if err != nil {
		t.Fatal(err)
	}
	starts := 0
	for _, line := range strings.Split(string(trace), "\n") {
		if strings.Contains(line, `"method":"thread/start"`) {
			starts++
		}
		if strings.Contains(line, `"method":"turn/start"`) && strings.Contains(line, `"threadId":"graph-a"`) == false {
			t.Errorf("turn did not target original session: %s", line)
		}
	}
	if starts != 1 {
		t.Errorf("thread/start count=%d, want exactly one", starts)
	}
	for _, text := range []string{"trigger-stream-failure", "上一次响应因上游推理中断", "trigger-whip", "你看还有没有别的任务需要完成", "ordinary after automation"} {
		count := 0
		for _, line := range strings.Split(string(trace), "\n") {
			if strings.Contains(line, `"method":"turn/start"`) && strings.Contains(line, text) {
				count++
			}
		}
		if count != 1 {
			t.Errorf("native input %q occurred %d times, want once", text, count)
		}
	}
}
