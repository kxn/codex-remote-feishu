package orchestrator

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/acp"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestOpenCodeEmptyResponseCompletionProjectsActionableTurnFailure(t *testing.T) {
	now := time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)
	workspace := t.TempDir()
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResumeWithOpenCodeProfile(
		"surface-1",
		"",
		"chat-1",
		"user-1",
		state.ProductModeNormal,
		agentproto.BackendOpenCode,
		state.DefaultOpenCodeProfileID,
		state.SurfaceVerbosityNormal,
		state.PlanModeSettingOff,
	)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		WorkspaceRoot:           workspace,
		WorkspaceKey:            workspace,
		Backend:                 agentproto.BackendOpenCode,
		OpenCodeProfileID:       state.DefaultOpenCodeProfileID,
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "OpenCode session", CWD: workspace},
		},
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "你好",
	})

	translator := acp.NewTranslator("inst-1", workspace)
	start, err := translator.TranslateCommand(agentproto.Command{
		CommandID: "cmd-1",
		Kind:      agentproto.CommandPromptSend,
		Origin:    agentproto.Origin{Surface: "surface-1"},
		Target: agentproto.Target{
			ExecutionMode: agentproto.PromptExecutionModeStartNew,
			CWD:           workspace,
		},
		Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "你好"}}},
	})
	if err != nil {
		t.Fatalf("TranslateCommand: %v", err)
	}
	newFrame := decodeACPTestFrame(t, start.OutboundToChild[0])
	ready := observeACPTestFrame(t, translator, map[string]any{
		"jsonrpc": "2.0",
		"id":      newFrame["id"],
		"result":  map[string]any{"sessionId": "thread-1"},
	})
	for _, event := range ready.Events {
		svc.ApplyAgentEvent("inst-1", event)
	}
	if len(ready.OutboundToChild) != 1 {
		t.Fatalf("session ready did not start prompt: %#v", ready)
	}
	promptFrame := decodeACPTestFrame(t, ready.OutboundToChild[0])
	completed := observeACPTestFrame(t, translator, map[string]any{
		"jsonrpc": "2.0",
		"id":      promptFrame["id"],
		"result":  map[string]any{"stopReason": "unknown"},
	})

	var projected []string
	for _, event := range completed.Events {
		for _, output := range svc.ApplyAgentEvent("inst-1", event) {
			if output.Notice != nil {
				projected = append(projected, output.Notice.Code+"\n"+output.Notice.Text)
			}
		}
	}
	joined := strings.Join(projected, "\n")
	if !strings.Contains(joined, "turn_failed") ||
		!strings.Contains(joined, "opencode_empty_response") ||
		!strings.Contains(joined, "空响应") ||
		!strings.Contains(joined, "端点") ||
		!strings.Contains(joined, "协议") {
		t.Fatalf("adapter failure did not reach actionable orchestrator notice: %q", joined)
	}
}

func observeACPTestFrame(t *testing.T, translator *acp.Translator, payload map[string]any) acp.Result {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal ACP frame: %v", err)
	}
	result, err := translator.ObserveServer(append(data, '\n'))
	if err != nil {
		t.Fatalf("ObserveServer: %v", err)
	}
	return result
}

func decodeACPTestFrame(t *testing.T, frame []byte) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(frame, &payload); err != nil {
		t.Fatalf("decode ACP frame: %v", err)
	}
	return payload
}
