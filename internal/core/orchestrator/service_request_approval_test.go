package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestApprovalCommandRequestPromptAddsCancelOption(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-cmd-1",
		Metadata: map[string]any{
			"requestType": "approval",
			"requestKind": "approval_command",
			"title":       "需要确认执行命令",
			"body":        "本地 Codex 想执行 `npm install`。",
		},
	})
	if len(events) != 1 {
		t.Fatalf("expected one request prompt event, got %#v", events)
	}
	prompt := requestPromptFromEvent(t, events[0])
	if prompt.SemanticKind != control.RequestSemanticApprovalCommand {
		t.Fatalf("expected approval command semantic kind, got %#v", prompt)
	}
	if !strings.Contains(prompt.HintText, "命令或参数") {
		t.Fatalf("expected approval command hint text, got %#v", prompt)
	}
	if len(prompt.Options) != 5 {
		t.Fatalf("expected command approval prompt to expose cancel + feedback, got %#v", prompt.Options)
	}
	if prompt.Options[1].OptionID != "acceptForSession" || prompt.Options[3].OptionID != "cancel" || prompt.Options[4].OptionID != "captureFeedback" {
		t.Fatalf("unexpected command approval options: %#v", prompt.Options)
	}
}

func TestRespondRequestCancelDispatchesDecision(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-cmd-1",
		Metadata: map[string]any{
			"requestType": "approval",
			"requestKind": "approval_command",
			"options": []map[string]any{
				{"id": "accept", "label": "允许一次"},
				{"id": "cancel", "label": "取消"},
			},
		},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionRespondRequest,
		SurfaceSessionID: "surface-1",
		MessageID:        "om-card-1",
		Request:          testRequestAction("req-cmd-1", "", "cancel", nil, 0),
	})
	if len(events) != 2 || !events[0].InlineReplaceCurrentCard || events[1].Command == nil {
		t.Fatalf("expected sealed request replacement plus one agent command event, got %#v", events)
	}
	prompt := requestPromptFromEvent(t, events[0])
	if !prompt.Sealed || prompt.Phase != "waiting_dispatch" {
		t.Fatalf("expected approval request to seal before dispatch, got %#v", prompt)
	}
	if events[1].Command.Request.Response["decision"] != "cancel" {
		t.Fatalf("unexpected request cancel payload: %#v", events[1].Command.Request.Response)
	}
}

func TestApprovalFileChangeRequestPromptUsesSemanticContext(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-file-1",
		Metadata: map[string]any{
			"requestType": "approval",
			"requestKind": "approval_file_change",
			"title":       "需要确认",
			"body":        "本地 Codex 即将修改仓库文件。",
			"grantRoot":   "/data/dl/droid",
		},
	})
	prompt := requestPromptFromEvent(t, events[0])
	if prompt.SemanticKind != control.RequestSemanticApprovalFileChange {
		t.Fatalf("expected approval file-change semantic kind, got %#v", prompt)
	}
	if !strings.Contains(prompt.HintText, "写入范围") {
		t.Fatalf("expected file-change hint text, got %#v", prompt)
	}
	foundGrantRoot := false
	for _, section := range prompt.Sections {
		if section.Label == "写入范围" && containsPromptSectionLine(section, "/data/dl/droid") {
			foundGrantRoot = true
		}
	}
	if !foundGrantRoot {
		t.Fatalf("expected file-change prompt to render write scope, got %#v", prompt.Sections)
	}
}

func TestApprovalNetworkRequestPromptUsesSemanticContext(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-net-1",
		Metadata: map[string]any{
			"requestType": "approval",
			"requestKind": "approval_network",
			"title":       "需要确认",
			"body":        "本地 Codex 即将访问外部网络。",
			"networkApprovalContext": map[string]any{
				"host":     "registry.npmjs.org",
				"protocol": "https",
				"port":     443,
			},
		},
	})
	prompt := requestPromptFromEvent(t, events[0])
	if prompt.SemanticKind != control.RequestSemanticApprovalNetwork {
		t.Fatalf("expected approval network semantic kind, got %#v", prompt)
	}
	if !strings.Contains(prompt.HintText, "联网目标") {
		t.Fatalf("expected network hint text, got %#v", prompt)
	}
	foundTarget := false
	for _, section := range prompt.Sections {
		if section.Label == "网络目标" && containsPromptSectionLine(section, "registry.npmjs.org") {
			foundTarget = true
		}
	}
	if !foundTarget {
		t.Fatalf("expected network prompt to render target section, got %#v", prompt.Sections)
	}
}
