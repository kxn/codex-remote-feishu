package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestOpenCodePermissionRequestPromptShowsToolTarget(t *testing.T) {
	now := time.Date(2026, 8, 15, 20, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "fido2key",
		WorkspaceRoot: "/data/dl/fido2key",
		WorkspaceKey:  "/data/dl/fido2key",
		ShortName:     "fido2key",
		Backend:       agentproto.BackendOpenCode,
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"ses_1": {ThreadID: "ses_1", Name: "fido2key", CWD: "/data/dl/fido2key", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionModeCommand, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", Text: "/mode opencode"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", ThreadID: "ses_1"})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "ses_1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})

	started := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "ses_1",
		TurnID:    "turn-1",
		RequestID: "req-read-1",
		Metadata: map[string]any{
			"requestType":   "approval",
			"requestKind":   "approval_can_use_tool",
			"requestMethod": "session/request_permission",
			"toolName":      "read",
			"blockedPath":   "/data/dl/fido2key/docs/README.md",
			"body":          "OpenCode 请求读取文件：\n/data/dl/fido2key/docs/README.md",
			"options": []map[string]any{
				{"id": "once", "label": "Allow once"},
				{"id": "reject", "label": "Reject"},
			},
		},
	})
	if len(started) != 1 {
		t.Fatalf("expected one request prompt event, got %#v", started)
	}
	prompt := requestPromptFromEvent(t, started[0])
	if prompt.SemanticKind != control.RequestSemanticApprovalCanUseTool {
		t.Fatalf("semantic kind = %q, want %q", prompt.SemanticKind, control.RequestSemanticApprovalCanUseTool)
	}
	var lines []string
	for _, section := range prompt.Sections {
		lines = append(lines, section.Lines...)
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "/data/dl/fido2key/docs/README.md") {
		t.Fatalf("request prompt must show tracked tool target, got %#v", prompt.Sections)
	}
	if !strings.Contains(joined, "read") {
		t.Fatalf("request prompt must show tool name, got %#v", prompt.Sections)
	}
}
