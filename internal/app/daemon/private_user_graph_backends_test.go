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
	"strings"
	"sync"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/wrapper"
	"github.com/kxn/codex-remote-feishu/internal/config"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

// TestPrivateGraphBackendProcess is a controllable external native peer. It
// supplies protocol facts only; SDK selection, launch, and routing stay real.
func TestPrivateGraphBackendProcess(t *testing.T) {
	kind := os.Getenv("GRAPH_NATIVE_BACKEND")
	if kind == "" {
		return
	}
	scan := bufio.NewScanner(os.Stdin)
	for scan.Scan() {
		raw := append([]byte(nil), scan.Bytes()...)
		var frame map[string]any
		if json.Unmarshal(raw, &frame) != nil {
			continue
		}
		if file, e := os.OpenFile(os.Getenv("GRAPH_BACKEND_TRACE"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600); e == nil {
			fmt.Fprintf(file, "%s profile=%s %s\n", kind, os.Getenv(config.ClaudeRuntimeProfileIDEnv), raw)
			file.Close()
		}
		if kind == "claude" {
			switch frame["type"] {
			case "control_request":
				id, _ := frame["request_id"].(string)
				fmt.Println(`{"type":"system","subtype":"init","session_id":"claude-session","permissionMode":"default"}`)
				fmt.Printf(`{"type":"control_response","response":{"request_id":%q,"subtype":"success","response":{}}}`+"\n", id)
			case "user":
				uuid, _ := frame["uuid"].(string)
				fmt.Printf(`{"type":"user","uuid":%q,"session_id":"claude-session","message":{"role":"user","content":[{"type":"text","text":"hello"}]}}`+"\n", uuid)
				fmt.Println(`{"type":"assistant","session_id":"claude-session","message":{"id":"answer","content":[{"type":"text","text":"done-claude"}]}}`)
				fmt.Println(`{"type":"result","subtype":"success","session_id":"claude-session","result":"done-claude","is_error":false}`)
			}
			continue
		}
		id := frame["id"]
		switch frame["method"] {
		case "initialize":
			fmt.Printf(`{"id":%q,"result":{"protocolVersion":1,"agentInfo":{"name":"OpenCode","version":"2.1.4"}}}`+"\n", id)
		case "session/new":
			fmt.Printf(`{"id":%q,"result":{"sessionId":"opencode-session","configOptions":[]}}`+"\n", id)
		case "session/prompt":
			fmt.Println(`{"method":"session/update","params":{"sessionId":"opencode-session","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"done-opencode"}}}}`)
			fmt.Printf(`{"id":%q,"result":{"stopReason":"end_turn"}}`+"\n", id)
		case "session/set_config_option":
			fmt.Printf(`{"id":%q,"result":{}}`+"\n", id)
		}
	}
	os.Exit(0)
}

func TestPrivateGraphBackendWrapperProcess(t *testing.T) {
	if os.Getenv("GRAPH_BACKEND_WRAPPER") != "1" {
		return
	}
	cfg, err := wrapper.LoadConfig([]string{os.Getenv("GRAPH_LAUNCH_MODE")}, "fixture", "fixture")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(71)
	}
	cfg.RelayServerURL = os.Getenv("FIXTURE_RELAY_URL")
	cfg.CodexRealBinary = os.Getenv("FIXTURE_CODEX_BIN")
	cfg.RuntimePaths = relayruntime.Paths{StateDir: os.Getenv("FIXTURE_STATE_DIR"), ManagerLockFile: filepath.Join(os.Getenv("FIXTURE_STATE_DIR"), "manager.lock")}
	cfg.BuildFingerprint = "fixture"
	code, err := wrapper.New(cfg).Run(context.Background(), os.Stdin, io.Discard, os.Stderr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	os.Exit(code)
}

func TestPrivateSDKUserGraphBackendsAndInvalidProfileRecovery(t *testing.T) {
	a := filepath.Join(t.TempDir(), "a")
	b := filepath.Join(t.TempDir(), "b")
	if e := os.MkdirAll(a, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.MkdirAll(b, 0700); e != nil {
		t.Fatal(e)
	}
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	trace := filepath.Join(dir, "backend-trace")
	var extraEnv []string
	for _, backend := range []string{"claude", "opencode"} {
		bin := filepath.Join(dir, backend)
		script := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then printf '2.1.4\\n'; exit; fi\nGRAPH_NATIVE_BACKEND=%s exec %q -test.run '^TestPrivateGraphBackendProcess$'\n", backend, exe)
		if e = os.WriteFile(bin, []byte(script), 0700); e != nil {
			t.Fatal(e)
		}
		key := config.ClaudeBinaryEnv
		if backend == "opencode" {
			key = config.OpenCodeBinaryEnv
		}
		extraEnv = append(extraEnv, key+"="+bin)
	}
	extraEnv = append(extraEnv, "GRAPH_BACKEND_TRACE="+trace)
	g := newGraphEnv(t, a, b, func(app *App, _ *messageIDAssigningGateway) {
		app.headlessRuntime.BaseEnv = append(app.headlessRuntime.BaseEnv, extraEnv...)
		// Keep the same real launcher and controlled relay established by newGraphEnv;
		// only the wrapper subprocess entry is swapped for a backend-neutral loader.
		relay := httptest.NewServer(app.relay)
		t.Cleanup(relay.Close)
		orig := app.applications.native.startHeadless
		var mu sync.Mutex
		var children []*exec.Cmd
		var wg sync.WaitGroup
		app.applications.native.startHeadless = func(v relayruntime.HeadlessLaunchOptions) (int, error) {
			if v.LaunchMode == relayruntime.HeadlessLaunchModeAppServer {
				return orig(v)
			}
			child := exec.Command(exe, "-test.run=^TestPrivateGraphBackendWrapperProcess$")
			relayURL := "ws" + strings.TrimPrefix(relay.URL, "http")
			child.Env = append(v.Env, "GRAPH_BACKEND_WRAPPER=1", "GRAPH_LAUNCH_MODE="+v.LaunchMode, "FIXTURE_RELAY_URL="+relayURL, "FIXTURE_STATE_DIR="+app.headlessRuntime.Paths.StateDir, "FIXTURE_CODEX_BIN="+app.headlessRuntime.CodexRealBinary)
			child.Dir = v.WorkDir
			child.Stdout = io.Discard
			child.Stderr = os.Stderr
			if err := child.Start(); err != nil {
				return 0, err
			}
			mu.Lock()
			children = append(children, child)
			wg.Add(1)
			mu.Unlock()
			go func() { defer wg.Done(); _ = child.Wait() }()
			return child.Process.Pid, nil
		}
		t.Cleanup(func() {
			mu.Lock()
			for _, child := range children {
				if child.Process != nil {
					_ = child.Process.Kill()
				}
			}
			mu.Unlock()
			wg.Wait()
		})
	})
	u := g.user
	before := len(u.gateway.snapshotOperations())
	u.send(g.app, "first-hi", "hi")
	u.waitVisible(g.app, before, "/list")
	u.chooseNew(g.app, "codex-a")
	before = len(u.gateway.snapshotOperations())
	u.send(g.app, "codex-first", "hello codex")
	u.waitFinal(g.app, before, "codex-first", "done-graph-a")
	for _, backend := range []string{"claude", "opencode"} {
		before = len(u.gateway.snapshotOperations())
		u.send(g.app, "mode-"+backend, "/mode "+backend)
		graphSettingsWait(t, g, before, "已切换到 "+backend)
		graphSettingsWait(t, g, before, "下一条")
		before = len(u.gateway.snapshotOperations())
		u.send(g.app, "prompt-"+backend, "hello "+backend)
		u.waitFinal(g.app, before, "prompt-"+backend, "done-"+backend)
	}
	before = len(u.gateway.snapshotOperations())
	u.send(g.app, "bad-profile", "/opencodeprofile nonexistent-profile")
	graphSettingsWait(t, g, before, "找不到这个 OpenCode Profile")
	before = len(u.gateway.snapshotOperations())
	u.send(g.app, "repair-profile", "/opencodeprofile op_default")
	graphSettingsWait(t, g, before, "已切换到 OpenCode Profile")
	graphSettingsWait(t, g, before, "下一条")
	before = len(u.gateway.snapshotOperations())
	u.send(g.app, "repaired-opencode", "profile repaired")
	u.waitFinal(g.app, before, "repaired-opencode", "done-opencode")
	before = len(u.gateway.snapshotOperations())
	u.send(g.app, "back-codex", "/mode codex")
	graphSettingsWait(t, g, before, "已切换到 codex")
	graphSettingsWait(t, g, before, "下一条")
	before = len(u.gateway.snapshotOperations())
	u.send(g.app, "codex-final", "back to codex")
	u.waitFinal(g.app, before, "codex-final", "done-graph-a")
	data, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "claude profile=default ") || !strings.Contains(string(data), "opencode ") {
		t.Fatal("backend native frames missing")
	}
}
