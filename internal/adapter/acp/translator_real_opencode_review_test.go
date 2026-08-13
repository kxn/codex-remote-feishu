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

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestRealOpenCodeACPReviewForkSmoke(t *testing.T) {
	binary := strings.TrimSpace(os.Getenv("OPENCODE_ACP_SMOKE_BIN"))
	if binary == "" {
		t.Skip("set OPENCODE_ACP_SMOKE_BIN to run the real OpenCode ACP smoke")
	}
	home := t.TempDir()
	workspace := t.TempDir()
	targetPath := filepath.Join(workspace, "target.txt")
	const originalContent = "review fixture must remain unchanged\n"
	if err := os.WriteFile(targetPath, []byte(originalContent), 0o644); err != nil {
		t.Fatalf("write review fixture: %v", err)
	}

	var requestMu sync.Mutex
	reviewPhase := false
	var reviewTools []string
	var reviewBodies []string
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		compact := mustCompactJSON(t, body)
		if strings.Contains(compact, "Generate a title for this conversation") {
			writeSSE(t, w, chatChunk(map[string]any{"role": "assistant"}, "", nil))
			writeSSE(t, w, chatChunk(map[string]any{"content": "Review Smoke"}, "", nil))
			writeSSE(t, w, chatChunk(map[string]any{}, "stop", map[string]int{"input": 1, "output": 1}))
			_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}

		requestMu.Lock()
		isReview := reviewPhase
		if isReview {
			reviewTools = append(reviewTools, toolNamesFromRequest(body)...)
			reviewBodies = append(reviewBodies, compact)
		}
		requestMu.Unlock()

		writeSSE(t, w, chatChunk(map[string]any{"role": "assistant"}, "", nil))
		response := "parent ready"
		if isReview {
			response = "review complete"
		}
		writeSSE(t, w, chatChunk(map[string]any{"content": response}, "", nil))
		writeSSE(t, w, chatChunk(map[string]any{}, "stop", map[string]int{"input": 5, "output": 2}))
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
	tr := NewTranslator("inst-review-smoke", workspace)
	initializeFrame, err := tr.BuildInitializeFrame()
	if err != nil {
		t.Fatalf("BuildInitializeFrame: %v", err)
	}
	writeFrame(t, stdin, initializeFrame)
	observeUntil(t, scanner, tr, func(Result) bool { return false }, "initialize response")

	runPrompt := func(command agentproto.Command, label string) string {
		t.Helper()
		result, err := tr.TranslateCommand(command)
		if err != nil {
			t.Fatalf("TranslateCommand(%s): %v", label, err)
		}
		for _, frame := range result.OutboundToChild {
			writeFrame(t, stdin, frame)
		}
		var threadID string
		observeUntil(t, scanner, tr, func(result Result) bool {
			for _, frame := range result.OutboundToChild {
				writeFrame(t, stdin, frame)
			}
			for _, event := range result.Events {
				if event.Kind == agentproto.EventSystemError {
					t.Fatalf("%s failed: %#v", label, event.Problem)
				}
				if event.Kind == agentproto.EventTurnStarted {
					threadID = event.ThreadID
				}
				if event.Kind == agentproto.EventTurnCompleted {
					if event.Status != "completed" {
						t.Fatalf("%s status=%q problem=%#v", label, event.Status, event.Problem)
					}
					return true
				}
			}
			return false
		}, label)
		return threadID
	}

	parentID := runPrompt(agentproto.Command{
		CommandID: "cmd-review-parent-smoke",
		Kind:      agentproto.CommandPromptSend,
		Origin:    agentproto.Origin{Surface: "surface-review-smoke"},
		Target: agentproto.Target{
			ExecutionMode: agentproto.PromptExecutionModeStartNew,
			CWD:           workspace,
		},
		Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "Prepare for review."}}},
	}, "parent prompt")
	if parentID == "" {
		t.Fatal("parent prompt did not produce a session id")
	}

	requestMu.Lock()
	reviewPhase = true
	requestMu.Unlock()
	reviewID := runPrompt(agentproto.Command{
		CommandID: "cmd-review-fork-smoke",
		Kind:      agentproto.CommandPromptSend,
		Origin:    agentproto.Origin{Surface: "surface-review-smoke"},
		Target: agentproto.Target{
			ExecutionMode:  agentproto.PromptExecutionModeForkEphemeral,
			SourceThreadID: parentID,
			CWD:            workspace,
			Purpose:        agentproto.PromptPurposeReview,
		},
		Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "Review target.txt without modifying it."}}},
	}, "review fork")
	if reviewID == "" || reviewID == parentID {
		t.Fatalf("review fork session = %q, parent = %q", reviewID, parentID)
	}

	requestMu.Lock()
	tools := append([]string(nil), reviewTools...)
	bodies := append([]string(nil), reviewBodies...)
	requestMu.Unlock()
	if len(bodies) == 0 {
		t.Fatal("fake provider did not receive a review request")
	}
	for _, forbidden := range []string{"bash", "edit", "write", "task"} {
		if containsString(tools, forbidden) {
			t.Fatalf("review request exposed forbidden tool %q in %#v", forbidden, tools)
		}
	}
	for _, required := range []string{"read", "glob", "grep"} {
		if !containsString(tools, required) {
			t.Fatalf("review request missing read-only tool %q in %#v", required, tools)
		}
	}
	content, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("read review fixture: %v", err)
	}
	if string(content) != originalContent {
		t.Fatalf("review changed fixture: %q", string(content))
	}

	_ = stdin.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatalf("opencode exited with error: %v; stderr=%s", err, <-stderrDone)
	}
}
