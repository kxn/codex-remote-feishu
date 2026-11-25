package daemon

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

// TestPrivateMCPOAuthNativePeer implements the 0.171 login protocol. Its first
// login remains pending while a normal turn runs; the second lacks an attempt ID.
func TestPrivateMCPOAuthNativePeer(t *testing.T) {
	if os.Getenv("PRIVATE_OAUTH_PEER") != "1" {
		return
	}
	cwd, _ := os.Getwd()
	turns, logins, historyReads := 0, 0, 0
	pending := false
	scan := bufio.NewScanner(os.Stdin)
	for scan.Scan() {
		var frame struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
			Params struct {
				IncludeTurns bool `json:"includeTurns"`
			} `json:"params"`
		}
		if json.Unmarshal(scan.Bytes(), &frame) != nil {
			continue
		}
		switch frame.Method {
		case "initialize":
			fmt.Printf(`{"id":%q,"result":{}}`+"\n", frame.ID)
		case "thread/start", "thread/resume":
			fmt.Printf(`{"id":%q,"result":{"model":"fixture-model","thread":{"id":"oauth-thread","cwd":%q}}}`+"\n", frame.ID, cwd)
		case "turn/start":
			turns++
			id := fmt.Sprintf("oauth-turn-%d", turns)
			fmt.Printf(`{"id":%q,"result":{"turn":{"id":%q}}}`+"\n", frame.ID, id)
			fmt.Printf(`{"method":"turn/started","params":{"threadId":"oauth-thread","turn":{"id":%q}}}`+"\n", id)
			fmt.Printf(`{"method":"item/completed","params":{"threadId":"oauth-thread","turnId":%q,"item":{"id":"answer","type":"agentMessage","text":"oauth dialogue complete"}}}`+"\n", id)
			fmt.Printf(`{"method":"turn/completed","params":{"threadId":"oauth-thread","turn":{"id":%q,"status":"completed"}}}`+"\n", id)
			if pending {
				pending = false
				fmt.Println(`{"method":"mcpServer/oauthLogin/completed","params":{"name":"docs","loginId":"attempt-B","success":true}}`)
			}
		case "mcpServer/oauth/login":
			logins++
			if logins == 1 {
				fmt.Printf(`{"id":%q,"result":{"authorizationUrl":"https://example.invalid/consent","loginId":"attempt-A"}}`+"\n", frame.ID)
				pending = true
			} else if logins == 2 {
				fmt.Printf(`{"id":%q,"result":{"authorizationUrl":"https://example.invalid/second","loginId":"attempt-B"}}`+"\n", frame.ID)
				fmt.Println(`{"method":"mcpServer/oauthLogin/completed","params":{"name":"docs","loginId":"attempt-A","success":true}}`)
			} else if logins == 3 {
				fmt.Printf(`{"id":%q,"error":{"code":-32603,"message":"controlled login failure"}}`+"\n", frame.ID)
			} else {
				fmt.Printf(`{"id":%q,"result":{"authorizationUrl":"https://example.invalid/retry"}}`+"\n", frame.ID)
			}
		case "thread/read":
			if os.Getenv("PRIVATE_HISTORY_FAILURE") == "1" && frame.Params.IncludeTurns {
				historyReads++
				if historyReads == 1 {
					fmt.Printf(`{"id":%q,"error":{"code":-32603,"message":"controlled history failure"}}`+"\n", frame.ID)
					continue
				}
			}
			fmt.Printf(`{"id":%q,"result":{"thread":{"id":"oauth-thread","cwd":%q,"status":{"type":"idle"},"turns":[{"id":"historic","status":"completed","items":[{"id":"message","type":"agentMessage","text":"history recovered"}]}]}}}`+"\n", frame.ID, cwd)
		case "thread/goal/get":
			fmt.Printf(`{"id":%q,"result":{"goal":null}}`+"\n", frame.ID)
		case "thread/list", "model/list":
			fmt.Printf(`{"id":%q,"result":{"data":[]}}`+"\n", frame.ID)
		}
	}
	os.Exit(0)
}

func TestPrivateSDKMCPOAuthDoesNotBlockDialogueAndUnverifiedRetry(t *testing.T) {
	f := newContinuityFixtureBare(t, nil)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	script := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then printf 'codex-cli 0.159.3-dev.1\\n'; exit; fi\nPRIVATE_OAUTH_PEER=1 exec %q -test.run '^TestPrivateMCPOAuthNativePeer$'\n", exe)
	if err = os.WriteFile(f.app.headlessRuntime.CodexRealBinary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	u := f.user
	n := len(f.gateway.snapshotOperations())
	u.send(f.app, "oauth-cold", "hi")
	u.waitVisible(f.app, n, "/list")
	u.chooseNew(f.app, "oauth-start")
	n = len(f.gateway.snapshotOperations())
	u.send(f.app, "oauth-first", "hello")
	u.waitFinal(f.app, n, "oauth-first", "oauth dialogue complete")
	n = len(f.gateway.snapshotOperations())
	u.send(f.app, "oauth-login", "/mcpoauth docs")
	u.waitVisible(f.app, n, "授权链接")
	n = len(f.gateway.snapshotOperations())
	u.send(f.app, "oauth-replace", "/mcpoauth docs")
	u.waitVisible(f.app, n, "example.invalid/second")
	n = len(f.gateway.snapshotOperations())
	u.send(f.app, "oauth-during", "continue")
	u.waitFinal(f.app, n, "oauth-during", "oauth dialogue complete")
	u.waitVisible(f.app, n, "认证已完成")
	n = len(f.gateway.snapshotOperations())
	u.send(f.app, "oauth-retry", "/mcpoauth docs")
	u.waitVisible(f.app, n, "认证失败")
	n = len(f.gateway.snapshotOperations())
	u.send(f.app, "oauth-unverified", "/mcpoauth docs")
	u.waitVisible(f.app, n, "无法自动确认")
	n = len(f.gateway.snapshotOperations())
	u.send(f.app, "oauth-after", "continue again")
	u.waitFinal(f.app, n, "oauth-after", "oauth dialogue complete")
}

func TestPrivateSDKHistoryFailureRetryAndDialogue(t *testing.T) {
	f := newContinuityFixtureBare(t, nil)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	script := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then printf 'codex-cli 0.159.3-dev.1\\n'; exit; fi\nPRIVATE_OAUTH_PEER=1 PRIVATE_HISTORY_FAILURE=1 exec %q -test.run '^TestPrivateMCPOAuthNativePeer$'\n", exe)
	if err = os.WriteFile(f.app.headlessRuntime.CodexRealBinary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	u := f.user
	n := len(f.gateway.snapshotOperations())
	u.send(f.app, "history-cold", "hi")
	u.waitVisible(f.app, n, "/list")
	u.chooseNew(f.app, "history-new")
	n = len(f.gateway.snapshotOperations())
	u.send(f.app, "history-first", "hello")
	u.waitFinal(f.app, n, "history-first", "oauth dialogue complete")
	n = len(f.gateway.snapshotOperations())
	u.send(f.app, "history-failure", "/history")
	u.waitVisible(f.app, n, "controlled history failure")
	n = len(f.gateway.snapshotOperations())
	u.send(f.app, "history-retry", "/history")
	u.waitVisible(f.app, n, "总轮数")
	n = len(f.gateway.snapshotOperations())
	u.send(f.app, "history-after", "continue")
	u.waitFinal(f.app, n, "history-after", "oauth dialogue complete")
}
