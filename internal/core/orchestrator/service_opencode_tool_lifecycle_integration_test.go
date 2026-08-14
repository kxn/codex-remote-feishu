package orchestrator

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/acp"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestOpenCodeToolLifecycleReachesProgressOnlyWhenDisplayable(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	translator, turnID := startOpenCodeTranslatorTurn(t)
	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "搜索代码", turnID)

	pending := observeOpenCodeToolUpdate(t, translator, map[string]any{
		"sessionUpdate": "tool_call",
		"toolCallId":    "grep_1",
		"title":         "grep",
		"kind":          "search",
		"status":        "pending",
		"rawInput":      map[string]any{},
	})
	if len(pending) != 0 {
		t.Fatalf("pending emitted shared events: %#v", pending)
	}
	if surface.ActiveExecProgress != nil {
		t.Fatalf("pending created shared progress: %#v", surface.ActiveExecProgress)
	}

	running := observeOpenCodeToolUpdate(t, translator, map[string]any{
		"sessionUpdate": "tool_call_update",
		"toolCallId":    "grep_1",
		"title":         "grep",
		"kind":          "search",
		"status":        "in_progress",
		"rawInput": map[string]any{
			"pattern": "Displayable",
			"path":    "internal",
		},
	})
	if len(running) != 1 || running[0].Kind != agentproto.EventItemStarted {
		t.Fatalf("running events = %#v, want one started event", running)
	}
	progressEvents := svc.ApplyAgentEvent("inst-1", running[0])
	if len(progressEvents) != 1 || progressEvents[0].ExecCommandProgress == nil {
		t.Fatalf("running did not create progress: %#v", progressEvents)
	}
	rows := progressEvents[0].ExecCommandProgress.Timeline
	if len(rows) != 1 || rows[0].Kind != "search" || rows[0].Summary != "Displayable" || rows[0].Secondary != "internal" {
		t.Fatalf("running progress row = %#v", rows)
	}

	completed := observeOpenCodeToolUpdate(t, translator, map[string]any{
		"sessionUpdate": "tool_call_update",
		"toolCallId":    "grep_1",
		"title":         "2 matches",
		"status":        "completed",
		"rawOutput":     map[string]any{"output": "RAW_SEARCH_RESULTS"},
	})
	if len(completed) != 1 || completed[0].Kind != agentproto.EventItemCompleted {
		t.Fatalf("completed events = %#v, want one completed event", completed)
	}
	completionEvents := svc.ApplyAgentEvent("inst-1", completed[0])
	if len(completionEvents) != 1 || completionEvents[0].ExecCommandProgress == nil {
		t.Fatalf("completion did not update progress: %#v", completionEvents)
	}
	completedRow := completionEvents[0].ExecCommandProgress.Timeline[0]
	if completedRow.Status != "completed" || strings.Contains(openCodeProgressRowText(completedRow), "RAW_SEARCH_RESULTS") {
		t.Fatalf("completion row exposed output or stayed running: %#v", completedRow)
	}
}

func TestOpenCodeToolLifecycleDirectTerminalCreatesSafeCompletedProgress(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	translator, turnID := startOpenCodeTranslatorTurn(t)
	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "读取文件", turnID)

	if events := observeOpenCodeToolUpdate(t, translator, map[string]any{
		"sessionUpdate": "tool_call",
		"toolCallId":    "read_1",
		"title":         "read",
		"kind":          "read",
		"status":        "pending",
		"rawInput":      map[string]any{},
	}); len(events) != 0 {
		t.Fatalf("pending emitted shared events: %#v", events)
	}

	terminal := observeOpenCodeToolUpdate(t, translator, map[string]any{
		"sessionUpdate": "tool_call_update",
		"toolCallId":    "read_1",
		"title":         "README.md",
		"status":        "failed",
		"rawOutput": map[string]any{
			"output": "RAW_FILE_CONTENT",
			"error":  "read failed",
		},
	})
	if len(terminal) != 2 || terminal[0].Kind != agentproto.EventItemStarted || terminal[1].Kind != agentproto.EventItemCompleted {
		t.Fatalf("terminal events = %#v, want ordered started/completed", terminal)
	}
	var rendered []eventcontract.Event
	for _, event := range terminal {
		rendered = append(rendered, svc.ApplyAgentEvent("inst-1", event)...)
	}
	var row *control.ExecCommandProgressTimelineItem
	for _, event := range rendered {
		if event.ExecCommandProgress == nil || len(event.ExecCommandProgress.Timeline) != 1 {
			continue
		}
		candidate := event.ExecCommandProgress.Timeline[0]
		row = &candidate
	}
	if row == nil {
		t.Fatalf("terminal fallback did not render progress: %#v", rendered)
	}
	if row.Kind != "dynamic_tool_call" || row.Status != "failed" || strings.TrimSpace(row.Summary) == "" {
		t.Fatalf("terminal fallback row = %#v", row)
	}
	if strings.Contains(openCodeProgressRowText(*row), "README.md") || strings.Contains(openCodeProgressRowText(*row), "RAW_FILE_CONTENT") {
		t.Fatalf("terminal fallback used result title or raw output: %#v", row)
	}
	for _, event := range rendered {
		if event.Block != nil && (strings.Contains(event.Block.Text, "README.md") || strings.Contains(event.Block.Text, "RAW_FILE_CONTENT")) {
			t.Fatalf("terminal fallback text used result title or raw output: %#v", rendered)
		}
	}
}

func startOpenCodeTranslatorTurn(t *testing.T) (*acp.Translator, string) {
	t.Helper()
	translator := acp.NewTranslator("inst-1", "/data/dl/droid")
	start, err := translator.TranslateCommand(agentproto.Command{
		CommandID: "cmd-opencode",
		Kind:      agentproto.CommandPromptSend,
		Origin:    agentproto.Origin{Surface: "surface-1"},
		Target: agentproto.Target{
			ExecutionMode: agentproto.PromptExecutionModeStartNew,
			CWD:           "/data/dl/droid",
		},
		Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}}},
	})
	if err != nil {
		t.Fatalf("TranslateCommand: %v", err)
	}
	request := decodeOpenCodeFrame(t, start.OutboundToChild[0])
	observed, err := translator.ObserveServer(marshalOpenCodeFrame(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      request["id"],
		"result":  map[string]any{"sessionId": "thread-1"},
	}))
	if err != nil {
		t.Fatalf("ObserveServer(session/new): %v", err)
	}
	for _, event := range observed.Events {
		if event.Kind == agentproto.EventTurnStarted && event.TurnID != "" {
			return translator, event.TurnID
		}
	}
	t.Fatalf("session/new events = %#v, want turn started", observed.Events)
	return nil, ""
}

func observeOpenCodeToolUpdate(t *testing.T, translator *acp.Translator, update map[string]any) []agentproto.Event {
	t.Helper()
	result, err := translator.ObserveServer(marshalOpenCodeFrame(t, map[string]any{
		"jsonrpc": "2.0",
		"method":  "session/update",
		"params": map[string]any{
			"sessionId": "thread-1",
			"update":    update,
		},
	}))
	if err != nil {
		t.Fatalf("ObserveServer(session/update): %v", err)
	}
	return result.Events
}

func marshalOpenCodeFrame(t *testing.T, frame map[string]any) []byte {
	t.Helper()
	data, err := json.Marshal(frame)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return append(data, '\n')
}

func decodeOpenCodeFrame(t *testing.T, frame []byte) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal(frame, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	return decoded
}

func openCodeProgressRowText(row control.ExecCommandProgressTimelineItem) string {
	return strings.Join(append([]string{row.Summary, row.Secondary}, row.Items...), " ")
}
