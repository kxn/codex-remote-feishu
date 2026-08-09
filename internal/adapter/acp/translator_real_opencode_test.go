package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestRealOpenCodeACPPromptSmoke(t *testing.T) {
	binary := strings.TrimSpace(os.Getenv("OPENCODE_ACP_SMOKE_BIN"))
	if binary == "" {
		t.Skip("set OPENCODE_ACP_SMOKE_BIN to run the real OpenCode ACP smoke")
	}
	home := t.TempDir()
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "README.md"), []byte("# OpenCode ACP smoke\n"), 0o644); err != nil {
		t.Fatalf("write workspace file: %v", err)
	}
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if strings.Contains(mustCompactJSON(t, body), "Generate a title for this conversation") {
			writeSSE(t, w, chatChunk(map[string]any{"role": "assistant"}, "", nil))
			writeSSE(t, w, chatChunk(map[string]any{"content": "Smoke Title"}, "", nil))
			writeSSE(t, w, chatChunk(map[string]any{}, "stop", map[string]int{"input": 1, "output": 1}))
			_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		writeSSE(t, w, chatChunk(map[string]any{"role": "assistant"}, "", nil))
		writeSSE(t, w, chatChunk(map[string]any{"reasoning_content": "think "}, "", nil))
		writeSSE(t, w, chatChunk(map[string]any{"reasoning_content": "twice"}, "", nil))
		writeSSE(t, w, chatChunk(map[string]any{"content": "hello "}, "", nil))
		writeSSE(t, w, chatChunk(map[string]any{"content": "world"}, "", nil))
		writeSSE(t, w, chatChunk(map[string]any{}, "stop", map[string]int{"input": 11, "output": 7}))
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer llm.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "acp", "--cwd", workspace)
	cmd.Dir = workspace
	cmd.Env = realOpenCodeSmokeEnv(home, llm.URL+"/v1")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("StdinPipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("StderrPipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start opencode: %v", err)
	}
	stderrDone := make(chan string, 1)
	go func() {
		var builder strings.Builder
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			builder.WriteString(scanner.Text())
			builder.WriteByte('\n')
		}
		stderrDone <- builder.String()
	}()
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	tr := NewTranslator("inst-smoke", workspace)
	initializeFrame, err := tr.BuildInitializeFrame()
	if err != nil {
		t.Fatalf("BuildInitializeFrame: %v", err)
	}
	writeFrame(t, stdin, initializeFrame)
	observeUntil(t, scanner, tr, func(result Result) bool { return false }, "initialize response")

	start, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-smoke",
		Kind:      agentproto.CommandPromptSend,
		Origin:    agentproto.Origin{Surface: "surface-smoke"},
		Target: agentproto.Target{
			ExecutionMode: agentproto.PromptExecutionModeStartNew,
			CWD:           workspace,
		},
		Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "Say hello."}}},
	})
	if err != nil {
		t.Fatalf("TranslateCommand(prompt): %v", err)
	}
	for _, frame := range start.OutboundToChild {
		writeFrame(t, stdin, frame)
	}

	var text, thought string
	completed := observeUntil(t, scanner, tr, func(result Result) bool {
		for _, frame := range result.OutboundToChild {
			writeFrame(t, stdin, frame)
		}
		for _, event := range result.Events {
			switch event.Kind {
			case agentproto.EventItemDelta:
				text += event.Delta
			case agentproto.EventItemReasoningSummaryPartAdded:
				thought += event.Delta
			case agentproto.EventTurnCompleted:
				return true
			}
		}
		return false
	}, "prompt completion")
	if !completed {
		t.Fatal("prompt did not complete")
	}
	if text != "hello world" {
		t.Fatalf("assistant text = %q", text)
	}
	if thought != "think twice" {
		t.Fatalf("thought text = %q", thought)
	}
	sessionID := tr.currentSessionID
	if strings.TrimSpace(sessionID) == "" {
		t.Fatal("translator did not retain current OpenCode session id")
	}

	historyRequest, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-history-smoke",
		Kind:      agentproto.CommandThreadHistoryRead,
		Target:    agentproto.Target{ThreadID: sessionID, CWD: workspace},
	})
	if err != nil {
		t.Fatalf("TranslateCommand(history): %v", err)
	}
	for _, frame := range historyRequest.OutboundToChild {
		writeFrame(t, stdin, frame)
	}
	var history *agentproto.ThreadHistoryRecord
	observeUntil(t, scanner, tr, func(result Result) bool {
		for _, event := range result.Events {
			switch event.Kind {
			case agentproto.EventItemDelta, agentproto.EventItemReasoningSummaryPartAdded:
				t.Fatalf("session/load replay leaked live event: %#v", event)
			case agentproto.EventThreadHistoryRead:
				history = event.ThreadHistory
				return true
			}
		}
		return false
	}, "history load")
	if history == nil || history.Thread.ThreadID != sessionID {
		t.Fatalf("history load result = %#v", history)
	}
	if !historyContainsItemText(history, "agent_message", "hello world") {
		t.Fatalf("history did not include replayed assistant text: %#v", history)
	}
	_ = stdin.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatalf("opencode exited with error: %v; stderr=%s", err, <-stderrDone)
	}
}

func observeUntil(t *testing.T, scanner *bufio.Scanner, tr *Translator, done func(Result) bool, label string) bool {
	t.Helper()
	deadline := time.After(20 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("timeout waiting for %s", label)
		default:
		}
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				t.Fatalf("read opencode stdout: %v", err)
			}
			t.Fatalf("opencode stdout closed before %s", label)
		}
		result, err := tr.ObserveServer(append([]byte(scanner.Text()), '\n'))
		if err != nil {
			t.Fatalf("ObserveServer(%s): %v", scanner.Text(), err)
		}
		if done(result) {
			return true
		}
		if label == "initialize response" {
			return true
		}
	}
}

func realOpenCodeSmokeEnv(home, baseURL string) []string {
	configContent := map[string]any{
		"formatter": false,
		"lsp":       false,
		"model":     "test/test-model",
		"provider": map[string]any{
			"test": map[string]any{
				"name": "Test",
				"id":   "test",
				"env":  []any{},
				"npm":  "@ai-sdk/openai-compatible",
				"models": map[string]any{
					"test-model": map[string]any{
						"id":           "test-model",
						"name":         "Test Model",
						"attachment":   false,
						"reasoning":    true,
						"temperature":  false,
						"tool_call":    true,
						"release_date": "2025-01-01",
						"limit":        map[string]any{"context": 100000, "output": 10000},
						"cost":         map[string]any{"input": 0, "output": 0},
						"options":      map[string]any{},
					},
				},
				"options": map[string]any{"apiKey": "test-key", "baseURL": baseURL},
			},
		},
	}
	env := append([]string{}, os.Environ()...)
	env = upsertTestEnv(env, "OPENCODE_TEST_HOME", home)
	env = upsertTestEnv(env, "HOME", home)
	env = upsertTestEnv(env, "XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	env = upsertTestEnv(env, "XDG_DATA_HOME", filepath.Join(home, ".local/share"))
	env = upsertTestEnv(env, "XDG_STATE_HOME", filepath.Join(home, ".local/state"))
	env = upsertTestEnv(env, "XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	env = upsertTestEnv(env, "OPENCODE_CONFIG_CONTENT", mustCompactJSONForEnv(configContent))
	env = upsertTestEnv(env, "OPENCODE_AUTH_CONTENT", "{}")
	env = upsertTestEnv(env, "OPENCODE_DISABLE_PROJECT_CONFIG", "1")
	env = upsertTestEnv(env, "OPENCODE_PURE", "1")
	env = upsertTestEnv(env, "OPENCODE_DISABLE_AUTOUPDATE", "1")
	env = upsertTestEnv(env, "OPENCODE_DISABLE_AUTOCOMPACT", "1")
	env = upsertTestEnv(env, "OPENCODE_DISABLE_MODELS_FETCH", "1")
	return env
}

func chatChunk(delta map[string]any, finish string, usage map[string]int) map[string]any {
	chunk := map[string]any{
		"id":     "chatcmpl-test",
		"object": "chat.completion.chunk",
		"choices": []any{map[string]any{
			"delta": delta,
		}},
	}
	if finish != "" {
		chunk["choices"].([]any)[0].(map[string]any)["finish_reason"] = finish
	}
	if usage != nil {
		chunk["usage"] = map[string]any{
			"prompt_tokens":     usage["input"],
			"completion_tokens": usage["output"],
			"total_tokens":      usage["input"] + usage["output"],
		}
	}
	return chunk
}

func writeSSE(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal SSE: %v", err)
	}
	_, _ = fmt.Fprintf(w, "data: %s\n\n", string(data))
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func writeFrame(t *testing.T, stdin interface{ Write([]byte) (int, error) }, frame []byte) {
	t.Helper()
	if _, err := stdin.Write(frame); err != nil {
		t.Fatalf("write frame: %v", err)
	}
}

func historyContainsItemText(history *agentproto.ThreadHistoryRecord, kind, text string) bool {
	if history == nil {
		return false
	}
	for _, turn := range history.Turns {
		for _, item := range turn.Items {
			if item.Kind == kind && strings.Contains(item.Text, text) {
				return true
			}
		}
	}
	return false
}

func mustFrame(t *testing.T, frame []byte, err error) []byte {
	t.Helper()
	if err != nil {
		t.Fatalf("build frame: %v", err)
	}
	return frame
}

func mustCompactJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}
	return string(data)
}

func mustCompactJSONForEnv(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(data)
}

func upsertTestEnv(env []string, key, value string) []string {
	entry := key + "=" + value
	out := make([]string, 0, len(env)+1)
	replaced := false
	for _, item := range env {
		currentKey, _, ok := strings.Cut(item, "=")
		if ok && currentKey == key {
			if !replaced {
				out = append(out, entry)
				replaced = true
			}
			continue
		}
		out = append(out, item)
	}
	if !replaced {
		out = append(out, entry)
	}
	return out
}
