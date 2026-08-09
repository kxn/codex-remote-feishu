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
	"sync"
	"testing"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/kxn/codex-remote-feishu/internal/app/opencodeprofile"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/modelcontextprotocol/go-sdk/mcp"
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
	var requestMu sync.Mutex
	var requestAuths []string
	var requestModels []string
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		requestMu.Lock()
		requestAuths = append(requestAuths, strings.TrimSpace(r.Header.Get("Authorization")))
		if model, _ := body["model"].(string); model != "" {
			requestModels = append(requestModels, model)
		}
		requestMu.Unlock()
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
	cmd.Env = realOpenCodeSmokeEnv(t, home, workspace, llm.URL+"/v1")
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
	requestMu.Lock()
	seenAuths := append([]string(nil), requestAuths...)
	seenModels := append([]string(nil), requestModels...)
	requestMu.Unlock()
	if !containsString(seenModels, "test-model") {
		t.Fatalf("fake provider did not receive overlaid model, got %#v", seenModels)
	}
	if !anyStringContains(seenAuths, "test-key") {
		t.Fatalf("fake provider did not receive auth overlay key, got %#v", seenAuths)
	}
	_ = stdin.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatalf("opencode exited with error: %v; stderr=%s", err, <-stderrDone)
	}
}

func TestRealOpenCodeACPToolPermissionEditSmoke(t *testing.T) {
	binary := strings.TrimSpace(os.Getenv("OPENCODE_ACP_SMOKE_BIN"))
	if binary == "" {
		t.Skip("set OPENCODE_ACP_SMOKE_BIN to run the real OpenCode ACP smoke")
	}
	home := t.TempDir()
	workspace := t.TempDir()
	targetPath := filepath.Join(workspace, "target.txt")
	if err := os.WriteFile(targetPath, []byte("old line\n"), 0o644); err != nil {
		t.Fatalf("write workspace file: %v", err)
	}

	var requestMu sync.Mutex
	nonTitleRequests := 0
	var sawWriteToolOffered bool
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if strings.Contains(mustCompactJSON(t, body), "Generate a title for this conversation") {
			writeSSE(t, w, chatChunk(map[string]any{"role": "assistant"}, "", nil))
			writeSSE(t, w, chatChunk(map[string]any{"content": "Edit Title"}, "", nil))
			writeSSE(t, w, chatChunk(map[string]any{}, "stop", map[string]int{"input": 1, "output": 1}))
			_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}

		requestMu.Lock()
		nonTitleRequests++
		currentRequest := nonTitleRequests
		if anyStringContains(toolNamesFromRequest(body), "edit") {
			sawWriteToolOffered = true
		}
		requestMu.Unlock()

		writeSSE(t, w, chatChunk(map[string]any{"role": "assistant"}, "", nil))
		switch currentRequest {
		case 1:
			args := `{"filePath":"target.txt","oldString":"old line\n","newString":"new line\n"}`
			writeSSE(t, w, toolCallStartChunk("call_edit_1", "edit"))
			writeSSE(t, w, toolCallArgsChunk(args))
			writeSSE(t, w, chatChunk(map[string]any{}, "tool_calls", map[string]int{"input": 12, "output": 3}))
		default:
			writeSSE(t, w, chatChunk(map[string]any{"content": "edit complete"}, "", nil))
			writeSSE(t, w, chatChunk(map[string]any{}, "stop", map[string]int{"input": 13, "output": 2}))
		}
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer llm.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "acp", "--cwd", workspace)
	cmd.Dir = workspace
	cmd.Env = realOpenCodeSmokeEnv(t, home, workspace, llm.URL+"/v1")
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
	tr := NewTranslator("inst-edit-smoke", workspace)
	initializeFrame, err := tr.BuildInitializeFrame()
	if err != nil {
		t.Fatalf("BuildInitializeFrame: %v", err)
	}
	writeFrame(t, stdin, initializeFrame)
	observeUntil(t, scanner, tr, func(result Result) bool { return false }, "initialize response")

	start, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-edit-smoke",
		Kind:      agentproto.CommandPromptSend,
		Origin:    agentproto.Origin{Surface: "surface-smoke"},
		Target: agentproto.Target{
			ExecutionMode: agentproto.PromptExecutionModeStartNew,
			CWD:           workspace,
		},
		Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "Edit target.txt."}}},
	})
	if err != nil {
		t.Fatalf("TranslateCommand(prompt): %v", err)
	}
	for _, frame := range start.OutboundToChild {
		writeFrame(t, stdin, frame)
	}

	var text string
	var sawPermission, sawFileChange, sawTool bool
	var eventSummaries []string
	completed := observeUntil(t, scanner, tr, func(result Result) bool {
		for _, frame := range result.OutboundToChild {
			writeFrame(t, stdin, frame)
		}
		for _, event := range result.Events {
			eventSummaries = append(eventSummaries, fmt.Sprintf("%s item=%s kind=%s req=%s status=%s meta=%s", event.Kind, event.ItemID, event.ItemKind, event.RequestID, event.Status, mustCompactJSON(t, event.Metadata)))
			switch event.Kind {
			case agentproto.EventItemStarted:
				if strings.Contains(fmt.Sprint(event.Metadata), "edit") {
					sawTool = true
				}
			case agentproto.EventRequestStarted:
				sawPermission = true
				if event.RequestPrompt == nil || event.RequestPrompt.Type != agentproto.RequestTypeApproval {
					t.Fatalf("unexpected permission event: %#v", event)
				}
				responded, err := tr.TranslateCommand(agentproto.Command{
					CommandID: "cmd-edit-approve",
					Kind:      agentproto.CommandRequestRespond,
					Origin:    agentproto.Origin{Surface: "surface-smoke"},
					Target:    agentproto.Target{ThreadID: event.ThreadID, TurnID: event.TurnID, CWD: workspace},
					Request:   agentproto.Request{RequestID: event.RequestID, Response: map[string]any{"optionId": "once"}},
				})
				if err != nil {
					t.Fatalf("TranslateCommand(permission response): %v", err)
				}
				for _, frame := range responded.OutboundToChild {
					writeFrame(t, stdin, frame)
				}
			case agentproto.EventItemFileChangePatchUpdated:
				sawFileChange = true
			case agentproto.EventItemDelta:
				text += event.Delta
			case agentproto.EventTurnCompleted:
				return true
			}
		}
		return false
	}, "edit tool completion")
	if !completed {
		t.Fatal("edit prompt did not complete")
	}
	requestMu.Lock()
	toolOffered := sawWriteToolOffered
	requestCount := nonTitleRequests
	requestMu.Unlock()
	if !sawTool {
		t.Fatalf("OpenCode edit tool call was not projected; requests=%d offered=%t events=%#v text=%q", requestCount, toolOffered, eventSummaries, text)
	}
	if !sawPermission {
		t.Fatalf("OpenCode edit tool did not request permission; requests=%d offered=%t events=%#v text=%q", requestCount, toolOffered, eventSummaries, text)
	}
	if !sawFileChange {
		t.Fatalf("OpenCode edit tool did not produce a file change preview event; requests=%d offered=%t events=%#v text=%q", requestCount, toolOffered, eventSummaries, text)
	}
	if !strings.Contains(text, "edit complete") {
		t.Fatalf("assistant text = %q", text)
	}
	content, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("read edited file: %v", err)
	}
	if string(content) != "new line\n" {
		t.Fatalf("edited file content = %q", string(content))
	}
	if !toolOffered {
		t.Fatalf("fake provider request did not include edit tool")
	}
	_ = stdin.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatalf("opencode exited with error: %v; stderr=%s", err, <-stderrDone)
	}
}

func TestRealOpenCodeACPCancelSmoke(t *testing.T) {
	binary := strings.TrimSpace(os.Getenv("OPENCODE_ACP_SMOKE_BIN"))
	if binary == "" {
		t.Skip("set OPENCODE_ACP_SMOKE_BIN to run the real OpenCode ACP smoke")
	}
	home := t.TempDir()
	workspace := t.TempDir()
	cancelStarted := make(chan struct{})
	cancelObserved := make(chan struct{})
	var hangOnce sync.Once
	var observeCancelOnce sync.Once
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if strings.Contains(mustCompactJSON(t, body), "Generate a title for this conversation") {
			writeSSE(t, w, chatChunk(map[string]any{"role": "assistant"}, "", nil))
			writeSSE(t, w, chatChunk(map[string]any{"content": "Cancel Title"}, "", nil))
			writeSSE(t, w, chatChunk(map[string]any{}, "stop", map[string]int{"input": 1, "output": 1}))
			_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		hangOnce.Do(func() { close(cancelStarted) })
		writeSSE(t, w, chatChunk(map[string]any{"role": "assistant"}, "", nil))
		<-r.Context().Done()
		observeCancelOnce.Do(func() { close(cancelObserved) })
	}))
	defer llm.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "acp", "--cwd", workspace)
	cmd.Dir = workspace
	cmd.Env = realOpenCodeSmokeEnv(t, home, workspace, llm.URL+"/v1")
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
	tr := NewTranslator("inst-cancel-smoke", workspace)
	initializeFrame, err := tr.BuildInitializeFrame()
	if err != nil {
		t.Fatalf("BuildInitializeFrame: %v", err)
	}
	writeFrame(t, stdin, initializeFrame)
	observeUntil(t, scanner, tr, func(result Result) bool { return false }, "initialize response")

	start, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-cancel-smoke",
		Kind:      agentproto.CommandPromptSend,
		Origin:    agentproto.Origin{Surface: "surface-smoke"},
		Target: agentproto.Target{
			ExecutionMode: agentproto.PromptExecutionModeStartNew,
			CWD:           workspace,
		},
		Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "Hang until cancelled."}}},
	})
	if err != nil {
		t.Fatalf("TranslateCommand(prompt): %v", err)
	}
	for _, frame := range start.OutboundToChild {
		writeFrame(t, stdin, frame)
	}

	var sentCancel bool
	var completionStatus string
	completed := observeUntil(t, scanner, tr, func(result Result) bool {
		for _, frame := range result.OutboundToChild {
			writeFrame(t, stdin, frame)
		}
		for _, event := range result.Events {
			if event.Kind == agentproto.EventTurnStarted && !sentCancel {
				select {
				case <-cancelStarted:
				case <-time.After(10 * time.Second):
					t.Fatal("fake provider was not entered before cancel")
				}
				sentCancel = true
				interrupted, err := tr.TranslateCommand(agentproto.Command{
					CommandID: "cmd-cancel-interrupt",
					Kind:      agentproto.CommandTurnInterrupt,
					Origin:    agentproto.Origin{Surface: "surface-smoke"},
					Target:    agentproto.Target{ThreadID: event.ThreadID, TurnID: event.TurnID, CWD: workspace},
				})
				if err != nil {
					t.Fatalf("TranslateCommand(cancel): %v", err)
				}
				for _, frame := range interrupted.OutboundToChild {
					writeFrame(t, stdin, frame)
				}
			}
			if event.Kind == agentproto.EventTurnCompleted {
				completionStatus = event.Status
				return true
			}
		}
		return false
	}, "cancel completion")
	if !completed {
		t.Fatal("cancel prompt did not complete")
	}
	if !sentCancel {
		t.Fatal("cancel command was not sent")
	}
	select {
	case <-cancelStarted:
	default:
		t.Fatal("fake provider was not entered")
	}
	select {
	case <-cancelObserved:
	case <-time.After(5 * time.Second):
		t.Fatal("fake provider request was not cancelled")
	}
	if completionStatus != "cancelled" {
		t.Fatalf("turn completion status = %q, want cancelled", completionStatus)
	}
	_ = stdin.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatalf("opencode exited with error: %v; stderr=%s", err, <-stderrDone)
	}
}

func TestRealOpenCodeACPMCPServerSmoke(t *testing.T) {
	binary := strings.TrimSpace(os.Getenv("OPENCODE_ACP_SMOKE_BIN"))
	if binary == "" {
		t.Skip("set OPENCODE_ACP_SMOKE_BIN to run the real OpenCode ACP smoke")
	}
	home := t.TempDir()
	workspace := t.TempDir()
	var mcpMu sync.Mutex
	var mcpAuths []string
	mcpServer := newRealOpenCodeFakeMCPServer(t, func(auth string) {
		mcpMu.Lock()
		mcpAuths = append(mcpAuths, auth)
		mcpMu.Unlock()
	})
	defer mcpServer.Close()

	var requestMu sync.Mutex
	var seenTools []string
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if strings.Contains(mustCompactJSON(t, body), "Generate a title for this conversation") {
			writeSSE(t, w, chatChunk(map[string]any{"role": "assistant"}, "", nil))
			writeSSE(t, w, chatChunk(map[string]any{"content": "MCP Title"}, "", nil))
			writeSSE(t, w, chatChunk(map[string]any{}, "stop", map[string]int{"input": 1, "output": 1}))
			_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		requestMu.Lock()
		seenTools = append(seenTools, toolNamesFromRequest(body)...)
		requestMu.Unlock()
		writeSSE(t, w, chatChunk(map[string]any{"role": "assistant"}, "", nil))
		writeSSE(t, w, chatChunk(map[string]any{"content": "mcp visible"}, "", nil))
		writeSSE(t, w, chatChunk(map[string]any{}, "stop", map[string]int{"input": 11, "output": 3}))
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer llm.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "acp", "--cwd", workspace)
	cmd.Dir = workspace
	cmd.Env = realOpenCodeSmokeEnv(t, home, workspace, llm.URL+"/v1")
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
	tr := NewTranslator("inst-mcp-smoke", workspace)
	tr.SetMCPServers([]MCPServer{{
		Name: feishuMCPServerIDForSmoke,
		Type: "http",
		URL:  mcpServer.URL,
		Headers: []MCPNameValue{{
			Name:  "Authorization",
			Value: "Bearer mcp-secret",
		}},
	}})
	initializeFrame, err := tr.BuildInitializeFrame()
	if err != nil {
		t.Fatalf("BuildInitializeFrame: %v", err)
	}
	writeFrame(t, stdin, initializeFrame)
	observeUntil(t, scanner, tr, func(result Result) bool { return false }, "initialize response")

	start, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-mcp-smoke",
		Kind:      agentproto.CommandPromptSend,
		Origin:    agentproto.Origin{Surface: "surface-smoke"},
		Target: agentproto.Target{
			ExecutionMode: agentproto.PromptExecutionModeStartNew,
			CWD:           workspace,
		},
		Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "Use available tools if needed."}}},
	})
	if err != nil {
		t.Fatalf("TranslateCommand(prompt): %v", err)
	}
	for _, frame := range start.OutboundToChild {
		writeFrame(t, stdin, frame)
	}
	completed := observeUntil(t, scanner, tr, func(result Result) bool {
		for _, frame := range result.OutboundToChild {
			writeFrame(t, stdin, frame)
		}
		for _, event := range result.Events {
			if event.Kind == agentproto.EventTurnCompleted {
				return true
			}
		}
		return false
	}, "mcp prompt completion")
	if !completed {
		t.Fatal("mcp prompt did not complete")
	}
	requestMu.Lock()
	tools := append([]string(nil), seenTools...)
	requestMu.Unlock()
	if !containsString(tools, feishuMCPServerIDForSmoke+"_demo_echo") {
		t.Fatalf("fake provider request did not include MCP tool, got %#v", tools)
	}
	mcpMu.Lock()
	auths := append([]string(nil), mcpAuths...)
	mcpMu.Unlock()
	if !containsString(auths, "Bearer mcp-secret") {
		t.Fatalf("fake MCP server did not receive Authorization header, got %#v", auths)
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

const feishuMCPServerIDForSmoke = "codex_remote_feishu"

func newRealOpenCodeFakeMCPServer(t *testing.T, recordAuth func(string)) *httptest.Server {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "opencode-acp-mcp-smoke",
		Version: "1.0.0",
	}, nil)
	server.AddTool(&mcp.Tool{
		Name:        "demo_echo",
		Description: "Echoes a small smoke value.",
		InputSchema: &jsonschema.Schema{Type: "object"},
	}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "mcp ok"}},
		}, nil
	})
	handler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		if recordAuth != nil {
			recordAuth(strings.TrimSpace(r.Header.Get("Authorization")))
		}
		return server
	}, &mcp.StreamableHTTPOptions{JSONResponse: true})
	return httptest.NewServer(handler)
}

func realOpenCodeSmokeEnv(t *testing.T, home, workspace, baseURL string) []string {
	t.Helper()
	env := append([]string{}, os.Environ()...)
	env = upsertTestEnv(env, "OPENCODE_TEST_HOME", home)
	env = upsertTestEnv(env, "HOME", home)
	env = upsertTestEnv(env, "XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	env = upsertTestEnv(env, "XDG_DATA_HOME", filepath.Join(home, ".local/share"))
	env = upsertTestEnv(env, "XDG_STATE_HOME", filepath.Join(home, ".local/state"))
	env = upsertTestEnv(env, "XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	env = upsertTestEnv(env, "OPENCODE_PURE", "1")
	env = upsertTestEnv(env, "OPENCODE_DISABLE_AUTOUPDATE", "1")
	env = upsertTestEnv(env, "OPENCODE_DISABLE_AUTOCOMPACT", "1")
	env = upsertTestEnv(env, "OPENCODE_DISABLE_MODELS_FETCH", "1")
	material, err := opencodeprofile.CompileLaunchMaterial(opencodeprofile.CompileInput{
		Profile: config.OpenCodeProfile{
			OpenCodeAPIProfileSecretConfig: config.OpenCodeAPIProfileSecretConfig{
				ID:                "op_smoke",
				Revision:          7,
				Name:              "Smoke OpenCode",
				BaseURL:           baseURL,
				APIKey:            "test-key",
				Model:             "test-model",
				ReasoningEffort:   "high",
				ProjectConfigMode: config.OpenCodeProjectConfigDisable,
				PermissionMode:    "ask",
			},
		},
		WorkspaceRoot: workspace,
		RuntimeDir:    filepath.Join(home, "runtime"),
		BaseEnv:       env,
	})
	if err != nil {
		t.Fatalf("compile OpenCode smoke launch material: %v", err)
	}
	return material.Env
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func anyStringContains(values []string, needle string) bool {
	for _, value := range values {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func toolNamesFromRequest(body map[string]any) []string {
	rawTools, _ := body["tools"].([]any)
	names := make([]string, 0, len(rawTools))
	for _, raw := range rawTools {
		tool, _ := raw.(map[string]any)
		if tool == nil {
			continue
		}
		if function, _ := tool["function"].(map[string]any); function != nil {
			if name, _ := function["name"].(string); name != "" {
				names = append(names, name)
			}
		}
	}
	return names
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

func toolCallStartChunk(id, name string) map[string]any {
	return chatChunk(map[string]any{
		"tool_calls": []any{map[string]any{
			"index": 0,
			"id":    id,
			"type":  "function",
			"function": map[string]any{
				"name":      name,
				"arguments": "",
			},
		}},
	}, "", nil)
}

func toolCallArgsChunk(args string) map[string]any {
	return chatChunk(map[string]any{
		"tool_calls": []any{map[string]any{
			"index": 0,
			"function": map[string]any{
				"arguments": args,
			},
		}},
	}, "", nil)
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
