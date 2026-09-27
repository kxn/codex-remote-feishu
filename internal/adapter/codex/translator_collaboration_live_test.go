package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

// Opt-in: uses the local Codex login for read-only new-thread and resumed-thread model turns.
func TestLiveNativeDefaultCollaborationSettings(t *testing.T) {
	if os.Getenv("CODEX_REMOTE_LIVE_SETTINGS_TEST") != "1" {
		t.Skip("set CODEX_REMOTE_LIVE_SETTINGS_TEST=1 to use the local Codex runtime")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	process := exec.CommandContext(ctx, "codex", "app-server", "--stdio")
	stdin, err := process.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := process.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := os.CreateTemp(t.TempDir(), "app-server-*.log")
	if err != nil {
		t.Fatal(err)
	}
	defer stderr.Close()
	process.Stderr = stderr
	if err = process.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); _ = process.Wait() }()
	send := func(v any) {
		t.Helper()
		data, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = stdin.Write(append(data, '\n')); err != nil {
			t.Fatal(err)
		}
	}
	send(map[string]any{"id": "verify-init", "method": "initialize", "params": map[string]any{"clientInfo": map[string]any{"name": "remote-settings-verifier", "version": "1"}, "capabilities": map[string]any{"experimentalApi": true}}})
	tr := NewTranslator("verify-native-default")
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 65536), 16*1024*1024)
	reply := ""
	completed := 0
	for scanner.Scan() {
		raw := append([]byte(nil), scanner.Bytes()...)
		var msg map[string]any
		if err = json.Unmarshal(raw, &msg); err != nil {
			continue
		}
		if msg["error"] != nil {
			t.Fatalf("real app-server rejected request %v: %v", msg["id"], msg["error"])
		}
		if msg["id"] == "verify-archive" {
			return
		}
		if msg["id"] == "verify-init" {
			send(map[string]any{"method": "initialized", "params": map[string]any{}})
			frames, err := tr.TranslateCommand(agentproto.Command{Kind: agentproto.CommandPromptSend, Target: agentproto.Target{CWD: t.TempDir(), ExecutionMode: agentproto.PromptExecutionModeStartNew}, Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "Reply with SETTINGS_OK only. Do not call any tools."}}}, Overrides: agentproto.PromptOverrides{PlanMode: "off"}})
			if err != nil {
				t.Fatal(err)
			}
			for _, frame := range frames {
				if _, err = stdin.Write(frame); err != nil {
					t.Fatal(err)
				}
			}
			continue
		}
		if r, ok := msg["result"].(map[string]any); ok && r["model"] != nil {
			t.Logf("resolved model=%v effort=%v", r["model"], r["reasoningEffort"])
		}
		result, err := tr.ObserveServer(raw)
		if err != nil {
			t.Fatal(err)
		}
		for _, frame := range result.OutboundToCodex {
			if _, err = stdin.Write(frame); err != nil {
				t.Fatal(err)
			}
		}
		if msg["method"] == "item/completed" {
			if text := lookupString(msg, "params", "item", "text"); text != "" {
				reply = text
			}
		}
		if msg["method"] == "turn/completed" {
			if lookupString(msg, "params", "turn", "status") != "completed" || !strings.Contains(reply, "SETTINGS_OK") {
				t.Fatalf("turn failed: status=%s reply=%q", lookupString(msg, "params", "turn", "status"), reply)
			}
			completed++
			t.Logf("real turn %d completed: %s", completed, reply)
			threadID := lookupString(msg, "params", "threadId")
			if completed == 2 {
				send(map[string]any{"id": "verify-archive", "method": "thread/archive", "params": map[string]any{"threadId": threadID}})
				continue
			}
			tr = NewTranslator("verify-native-resume")
			frames, err := tr.TranslateCommand(agentproto.Command{Kind: agentproto.CommandPromptSend, Target: agentproto.Target{ThreadID: threadID}, Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "Reply with SETTINGS_OK only. Do not call any tools."}}}, Overrides: agentproto.PromptOverrides{PlanMode: "off"}})
			if err != nil {
				t.Fatal(err)
			}
			for _, frame := range frames {
				if _, err = stdin.Write(frame); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	t.Fatalf("app-server ended before verification completed: scanner=%v context=%v", scanner.Err(), ctx.Err())
}
