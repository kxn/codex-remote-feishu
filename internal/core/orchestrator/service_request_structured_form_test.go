package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestRequestPromptViewIncludesStructuredFormAndMultiValueDrafts(t *testing.T) {
	svc := newServiceForTest(nil)
	record := &state.RequestPromptRecord{
		RequestID:    "req-1",
		RequestType:  "approval",
		SemanticKind: control.RequestSemanticPlanConfirmation,
		Title:        "需要确认计划",
		StructuredForm: &state.RequestPromptStructuredFormRecord{
			SubmitLabel: "按以上授权继续",
			Fields: []state.RequestPromptFormFieldRecord{
				{
					Name:  "grant_level",
					Kind:  state.RequestPromptFormFieldSelectStatic,
					Label: "授权级别",
					Options: []state.RequestPromptFormFieldOptionRecord{
						{Label: "仅按选中范围自动允许", Value: "scoped_rules"},
					},
					DefaultValues: []string{"scoped_rules"},
				},
				{
					Name:  "directories",
					Kind:  state.RequestPromptFormFieldMultiSelectStatic,
					Label: "目录范围",
					Options: []state.RequestPromptFormFieldOptionRecord{
						{Label: "internal/core/orchestrator", Value: "/data/dl/droid/internal/core/orchestrator"},
						{Label: "internal/adapter/feishu", Value: "/data/dl/droid/internal/adapter/feishu"},
					},
					DefaultValues: []string{
						"/data/dl/droid/internal/core/orchestrator",
						"/data/dl/droid/internal/adapter/feishu",
					},
				},
			},
		},
		StructuredDraftAnswers: map[string][]string{
			"grant_level": {"scoped_rules"},
			"directories": {
				"/data/dl/droid/internal/core/orchestrator",
				"/data/dl/droid/internal/adapter/feishu",
			},
		},
	}

	view := svc.requestPromptView(record, "")
	if view.StructuredForm == nil {
		t.Fatalf("expected structured form in request view")
	}
	if got := view.StructuredForm.Fields[1].DefaultValues; len(got) != 2 {
		t.Fatalf("expected multi-value defaults, got %#v", got)
	}
}

func TestPlanConfirmationAcceptForSessionEntersStructuredPermissionPanel(t *testing.T) {
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Backend:       agentproto.BackendClaude,
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionModeCommand, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", Text: "/mode claude"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", ThreadID: "thread-1"})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})

	started := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-plan-1",
		Metadata: map[string]any{
			"requestType":   "approval",
			"requestMethod": "tool/ExitPlanMode",
			"body": strings.Join([]string{
				"Claude 计划如下，请确认是否继续。",
				"- 修改 internal/core/orchestrator/service_request.go",
				"- 修改 internal/adapter/feishu/projector/request.go",
				"- 更新 docs/general/relay-protocol-spec.md",
			}, "\n"),
			"options": []map[string]any{
				{"id": "accept", "label": "批准"},
				{"id": "decline", "label": "拒绝"},
			},
		},
	})
	if len(started) != 1 {
		t.Fatalf("expected one request prompt event, got %#v", started)
	}
	initial := requestPromptFromEvent(t, started[0])
	if len(initial.Options) != 4 {
		t.Fatalf("expected quick decision card to expose four options, got %#v", initial.Options)
	}
	if initial.Options[1].OptionID != "acceptForSession" || initial.Options[1].Label != "配置本会话授权" {
		t.Fatalf("expected second option to enter permission panel, got %#v", initial.Options)
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionRespondRequest,
		SurfaceSessionID: "surface-1",
		MessageID:        "om-card-2",
		Request:          testRequestAction("req-plan-1", "approval", "acceptForSession", nil, initial.RequestRevision),
	})
	if len(events) != 1 {
		t.Fatalf("expected inline panel refresh without dispatch command, got %#v", events)
	}
	panel := requestPromptFromEvent(t, events[0])
	if panel.StructuredForm == nil {
		t.Fatalf("expected plan confirmation to enter structured panel, got %#v", panel)
	}
	if panel.Phase != frontstagecontract.PhaseEditing || panel.Sealed {
		t.Fatalf("expected editable panel state, got %#v", panel)
	}
	if len(panel.StructuredForm.Fields) != 3 {
		t.Fatalf("expected grant/directory/rule fields, got %#v", panel.StructuredForm)
	}
	if panel.StructuredForm.Fields[0].Kind != control.RequestPromptFormFieldSelectStatic {
		t.Fatalf("expected first field to be select_static, got %#v", panel.StructuredForm.Fields[0])
	}
	if panel.StructuredForm.Fields[1].Kind != control.RequestPromptFormFieldMultiSelectStatic {
		t.Fatalf("expected directory field to be multi_select_static, got %#v", panel.StructuredForm.Fields[1])
	}
	if panel.StructuredForm.Fields[2].Kind != control.RequestPromptFormFieldMultiSelectStatic {
		t.Fatalf("expected rule field to be multi_select_static, got %#v", panel.StructuredForm.Fields[2])
	}
	if len(panel.Options) != 2 || panel.Options[0].OptionID != frontstagecontract.RequestPromptOptionStepPrevious || panel.Options[1].OptionID != "decline" {
		t.Fatalf("expected panel footer to expose return/decline, got %#v", panel.Options)
	}
	if !containsRequestSectionLine(panel.Sections, "仅当前会话有效") {
		t.Fatalf("expected session-only warning in panel sections, got %#v", panel.Sections)
	}
	record := svc.root.Surfaces["surface-1"].PendingRequests["req-plan-1"]
	if record == nil || record.PendingDispatchCommandID != "" {
		t.Fatalf("expected pending request to stay editable, got %#v", record)
	}
}

func TestPlanConfirmationStructuredPermissionPanelSubmitDispatchesSelection(t *testing.T) {
	now := time.Date(2026, 5, 26, 12, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Backend:       agentproto.BackendClaude,
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionModeCommand, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", Text: "/mode claude"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", ThreadID: "thread-1"})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})
	started := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-plan-1",
		Metadata: map[string]any{
			"requestType":   "approval",
			"requestMethod": "tool/ExitPlanMode",
			"body": strings.Join([]string{
				"Claude 计划如下，请确认是否继续。",
				"- 修改 internal/core/orchestrator/service_request.go",
				"- 修改 internal/adapter/feishu/projector/request.go",
			}, "\n"),
			"options": []map[string]any{
				{"id": "accept", "label": "批准"},
				{"id": "decline", "label": "拒绝"},
			},
		},
	})
	initial := requestPromptFromEvent(t, started[0])
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionRespondRequest,
		SurfaceSessionID: "surface-1",
		MessageID:        "om-card-2",
		Request:          testRequestAction("req-plan-1", "approval", "acceptForSession", nil, initial.RequestRevision),
	})

	record := svc.root.Surfaces["surface-1"].PendingRequests["req-plan-1"]
	if record == nil || record.StructuredForm == nil {
		t.Fatalf("expected structured form state, got %#v", record)
	}
	answers := structuredPanelAnswersFromRecord(t, record)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionRespondRequest,
		SurfaceSessionID: "surface-1",
		MessageID:        "om-card-3",
		Request:          testRequestAction("req-plan-1", "approval", "", answers, record.CardRevision),
	})
	if len(events) != 2 || events[1].Command == nil {
		t.Fatalf("expected inline waiting state plus dispatch command, got %#v", events)
	}
	waiting := requestPromptFromEvent(t, events[0])
	if waiting.Phase != frontstagecontract.PhaseWaitingDispatch || !waiting.Sealed {
		t.Fatalf("expected sealed waiting-dispatch view, got %#v", waiting)
	}
	if !containsRequestSectionLine(waiting.Sections, "有效期：当前会话") {
		t.Fatalf("expected sealed summary to mention current-session lifetime, got %#v", waiting.Sections)
	}

	req := events[1].Command.Request
	if req.BridgeKind != string(control.RequestBridgePlanConfirmation) {
		t.Fatalf("expected plan confirmation bridge, got %#v", req)
	}
	if req.Response["decision"] != "accept" {
		t.Fatalf("expected structured panel submit to dispatch accept, got %#v", req.Response)
	}
	selection, ok := req.Response["permissionSelection"].(map[string]any)
	if !ok {
		t.Fatalf("expected permissionSelection payload, got %#v", req.Response)
	}
	if selection["scope"] != "session" {
		t.Fatalf("expected session-only selection, got %#v", selection)
	}
	if selection["grant_level"] == "" {
		t.Fatalf("expected grant level in selection, got %#v", selection)
	}
	if got := stringSliceAny(selection["directories"]); len(got) == 0 {
		t.Fatalf("expected selected directories, got %#v", selection)
	}
	if got := stringSliceAny(selection["rule_classes"]); len(got) == 0 {
		t.Fatalf("expected selected rule classes, got %#v", selection)
	}
	if pending := svc.root.Surfaces["surface-1"].PendingRequests["req-plan-1"]; pending == nil || pending.PendingDispatchCommandID == "" {
		t.Fatalf("expected pending request to enter dispatch-blocked state, got %#v", pending)
	}
}

func structuredPanelAnswersFromRecord(t *testing.T, record *state.RequestPromptRecord) map[string][]string {
	t.Helper()
	if record == nil || record.StructuredForm == nil {
		t.Fatalf("expected structured form record, got %#v", record)
	}
	answers := map[string][]string{}
	for _, field := range record.StructuredForm.Fields {
		name := strings.TrimSpace(field.Name)
		if name == "" {
			continue
		}
		if len(field.DefaultValues) != 0 {
			answers[name] = append([]string(nil), field.DefaultValues...)
			continue
		}
		if len(field.Options) != 0 {
			answers[name] = []string{field.Options[0].Value}
		}
	}
	return answers
}

func containsRequestSectionLine(sections []control.FeishuCardTextSection, want string) bool {
	want = strings.TrimSpace(want)
	for _, section := range sections {
		for _, line := range section.Lines {
			if strings.TrimSpace(line) == want {
				return true
			}
		}
	}
	return false
}

func stringSliceAny(raw any) []string {
	switch typed := raw.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := strings.TrimSpace(item.(string)); text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}
