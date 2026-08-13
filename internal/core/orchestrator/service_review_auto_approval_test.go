package orchestrator

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func startCodexReviewForAutoApprovalTest(t *testing.T, svc *Service, surface *state.SurfaceConsoleRecord, cwd string) {
	t.Helper()
	events := svc.startReview(surface, reviewStartState{
		Ready:           true,
		ParentThreadID:  "thread-main",
		ThreadCWD:       cwd,
		SourceMessageID: "msg-review-start",
		Target:          agentproto.ReviewTarget{Kind: agentproto.ReviewTargetKindUncommittedChanges},
	})
	if len(events) != 2 || events[1].Command == nil || events[1].Command.Kind != agentproto.CommandReviewStart {
		t.Fatalf("expected native Codex review start, got %#v", events)
	}
	activateReviewSessionForTest(t, svc, surface, "msg-review-start", "turn-review-1")
}

func reviewApprovalRequestEvent(threadID, turnID, requestID, semanticKind string) agentproto.Event {
	return agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  threadID,
		TurnID:    turnID,
		RequestID: requestID,
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: "surface-1"},
		Metadata: map[string]any{
			"requestType": "approval",
			"requestKind": semanticKind,
			"requestMethod": map[string]string{
				control.RequestSemanticApprovalCommand:    "item/commandExecution/requestApproval",
				control.RequestSemanticApprovalFileChange: "item/fileChange/requestApproval",
				control.RequestSemanticApprovalNetwork:    "item/commandExecution/requestApproval",
			}[semanticKind],
			"title": "需要确认",
		},
	}
}

func requireSilentReviewApprovalCommand(t *testing.T, events []eventcontract.Event, requestID string) *agentproto.Command {
	t.Helper()
	if len(events) != 1 || events[0].Command == nil || events[0].RequestView != nil {
		t.Fatalf("expected one silent request response command, got %#v", events)
	}
	command := events[0].Command
	if command.Kind != agentproto.CommandRequestRespond || command.Request.RequestID != requestID {
		t.Fatalf("unexpected request response command: %#v", command)
	}
	return command
}

func requireReviewRequestPrompt(t *testing.T, events []eventcontract.Event, requestID string) {
	t.Helper()
	if len(events) != 1 || events[0].RequestView == nil || events[0].Command != nil || events[0].RequestView.RequestID != requestID {
		t.Fatalf("expected one visible request prompt, got %#v", events)
	}
}

func TestFullAccessCodexReviewSilentlyApprovesCommandRequest(t *testing.T) {
	svc, surface, cwd := newReviewSessionService(t)
	startCodexReviewForAutoApprovalTest(t, svc, surface, cwd)

	events := svc.ApplyAgentEvent("inst-1", reviewApprovalRequestEvent(
		"thread-review",
		"turn-review-1",
		"req-review-command",
		control.RequestSemanticApprovalCommand,
	))

	command := requireSilentReviewApprovalCommand(t, events, "req-review-command")
	if command.Request.Response["type"] != "approval" || command.Request.Response["decision"] != "accept" {
		t.Fatalf("expected one-shot accept response, got %#v", command.Request.Response)
	}
	request := surface.PendingRequests["req-review-command"]
	if request == nil || request.PendingDispatchCommandID != command.CommandID || request.LifecycleState != requestLifecycleSubmitting || request.VisibleMessageID != "" {
		t.Fatalf("expected hidden request lifecycle to remain auditable until resolved, got %#v", request)
	}
}

func TestCodexReviewFreezesEffectiveAccessAtStart(t *testing.T) {
	t.Run("confirm remains interactive after surface switches to full access", func(t *testing.T) {
		svc, surface, cwd := newReviewSessionService(t)
		surface.PromptOverride.AccessMode = agentproto.AccessModeConfirm
		startCodexReviewForAutoApprovalTest(t, svc, surface, cwd)
		if surface.ReviewSession.FrozenAccessMode != agentproto.AccessModeConfirm {
			t.Fatalf("frozen access = %q, want confirm", surface.ReviewSession.FrozenAccessMode)
		}
		surface.PromptOverride.AccessMode = agentproto.AccessModeFullAccess

		events := svc.ApplyAgentEvent("inst-1", reviewApprovalRequestEvent(
			"thread-review", "turn-review-1", "req-confirm", control.RequestSemanticApprovalCommand,
		))

		requireReviewRequestPrompt(t, events, "req-confirm")
	})

	t.Run("full access remains silent after surface switches to confirm", func(t *testing.T) {
		svc, surface, cwd := newReviewSessionService(t)
		startCodexReviewForAutoApprovalTest(t, svc, surface, cwd)
		if surface.ReviewSession.FrozenAccessMode != agentproto.AccessModeFullAccess {
			t.Fatalf("frozen access = %q, want full_access", surface.ReviewSession.FrozenAccessMode)
		}
		surface.PromptOverride.AccessMode = agentproto.AccessModeConfirm

		events := svc.ApplyAgentEvent("inst-1", reviewApprovalRequestEvent(
			"thread-review", "turn-review-1", "req-full", control.RequestSemanticApprovalCommand,
		))

		requireSilentReviewApprovalCommand(t, events, "req-full")
	})
}

func TestCodexReviewFreezesWorkspaceDefaultAccess(t *testing.T) {
	svc, surface, cwd := newReviewSessionService(t)
	svc.root.WorkspaceDefaults[state.WorkspaceDefaultsStorageKey(cwd, state.InstanceBackendContract{
		Backend:        agentproto.BackendCodex,
		CodexProfileID: state.DefaultCodexProfileID,
	})] = state.ModelConfigRecord{AccessMode: agentproto.AccessModeConfirm}

	startCodexReviewForAutoApprovalTest(t, svc, surface, cwd)

	if surface.ReviewSession.FrozenAccessMode != agentproto.AccessModeConfirm {
		t.Fatalf("frozen access = %q, want workspace default confirm", surface.ReviewSession.FrozenAccessMode)
	}
}

func TestFullAccessCodexReviewAutoApprovalWhitelist(t *testing.T) {
	for _, semanticKind := range []string{
		control.RequestSemanticApprovalCommand,
		control.RequestSemanticApprovalFileChange,
		control.RequestSemanticApprovalNetwork,
	} {
		t.Run(semanticKind, func(t *testing.T) {
			svc, surface, cwd := newReviewSessionService(t)
			startCodexReviewForAutoApprovalTest(t, svc, surface, cwd)
			requestID := "req-" + semanticKind

			events := svc.ApplyAgentEvent("inst-1", reviewApprovalRequestEvent(
				"thread-review", "turn-review-1", requestID, semanticKind,
			))

			requireSilentReviewApprovalCommand(t, events, requestID)
		})
	}
}

func TestFullAccessCodexReviewSilentlyApprovesLegacyPermissionRequests(t *testing.T) {
	for _, method := range []string{"execCommandApproval", "applyPatchApproval"} {
		t.Run(method, func(t *testing.T) {
			svc, surface, cwd := newReviewSessionService(t)
			startCodexReviewForAutoApprovalTest(t, svc, surface, cwd)
			requestID := "req-" + method
			request := reviewApprovalRequestEvent(
				"thread-review", "turn-review-1", requestID, control.RequestSemanticApproval,
			)
			request.Metadata["requestKind"] = map[string]string{
				"execCommandApproval": "exec_command_approval",
				"applyPatchApproval":  "apply_patch_approval",
			}[method]
			request.Metadata["requestMethod"] = method

			events := svc.ApplyAgentEvent("inst-1", request)

			command := requireSilentReviewApprovalCommand(t, events, requestID)
			if command.Request.Response["decision"] != "accept" {
				t.Fatalf("legacy response = %#v, want accept", command.Request.Response)
			}
		})
	}
}

func TestFullAccessCodexReviewPermissionsRequestUsesTurnScope(t *testing.T) {
	svc, surface, cwd := newReviewSessionService(t)
	startCodexReviewForAutoApprovalTest(t, svc, surface, cwd)
	permissions := []map[string]any{{"name": "network", "host": "127.0.0.1"}}

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-review",
		TurnID:    "turn-review-1",
		RequestID: "req-review-permissions",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surface.SurfaceSessionID},
		RequestPrompt: &agentproto.RequestPrompt{
			Type: agentproto.RequestTypePermissionsRequestApproval,
			Permissions: &agentproto.PermissionsRequestPrompt{
				Permissions: permissions,
			},
		},
		Metadata: map[string]any{"requestType": "permissions_request_approval"},
	})

	command := requireSilentReviewApprovalCommand(t, events, "req-review-permissions")
	if command.Request.Response["scope"] != "turn" {
		t.Fatalf("permission scope = %#v, want turn", command.Request.Response["scope"])
	}
	granted, ok := command.Request.Response["permissions"].([]map[string]any)
	if !ok || len(granted) != 1 || granted[0]["host"] != "127.0.0.1" {
		t.Fatalf("permissions = %#v, want original request permissions", command.Request.Response["permissions"])
	}
}

func TestFullAccessCodexReviewDoesNotAutoApproveEmptyPermissionsRequest(t *testing.T) {
	svc, surface, cwd := newReviewSessionService(t)
	startCodexReviewForAutoApprovalTest(t, svc, surface, cwd)

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-review",
		TurnID:    "turn-review-1",
		RequestID: "req-empty-permissions",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surface.SurfaceSessionID},
		RequestPrompt: &agentproto.RequestPrompt{
			Type:        agentproto.RequestTypePermissionsRequestApproval,
			Permissions: &agentproto.PermissionsRequestPrompt{},
		},
		Metadata: map[string]any{"requestType": "permissions_request_approval"},
	})

	requireReviewRequestPrompt(t, events, "req-empty-permissions")
}

func TestFullAccessCodexReviewDoesNotAutoApproveNonPermissionInteractions(t *testing.T) {
	tests := []struct {
		name     string
		request  agentproto.Event
		semantic string
	}{
		{
			name: "generic approval",
			request: reviewApprovalRequestEvent(
				"thread-review", "turn-review-1", "req-generic", control.RequestSemanticApproval,
			),
		},
		{
			name: "plan confirmation",
			request: agentproto.Event{
				Kind:      agentproto.EventRequestStarted,
				ThreadID:  "thread-review",
				TurnID:    "turn-review-1",
				RequestID: "req-plan",
				Metadata: map[string]any{
					"requestType":   "approval",
					"requestMethod": "tool/ExitPlanMode",
					"options":       []map[string]any{{"id": "accept"}, {"id": "decline"}},
				},
			},
		},
		{
			name: "request user input",
			request: agentproto.Event{
				Kind:      agentproto.EventRequestStarted,
				ThreadID:  "thread-review",
				TurnID:    "turn-review-1",
				RequestID: "req-input",
				RequestPrompt: &agentproto.RequestPrompt{
					Type: agentproto.RequestTypeRequestUserInput,
				},
				Metadata: map[string]any{
					"requestType": "request_user_input",
					"questions": []map[string]any{{
						"id": "choice", "header": "选择", "question": "继续吗？", "options": []map[string]any{{"label": "继续"}},
					}},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, surface, cwd := newReviewSessionService(t)
			startCodexReviewForAutoApprovalTest(t, svc, surface, cwd)
			tt.request.Initiator = agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surface.SurfaceSessionID}

			events := svc.ApplyAgentEvent("inst-1", tt.request)

			requireReviewRequestPrompt(t, events, tt.request.RequestID)
		})
	}
}

func TestFullAccessCodexReviewAutoApprovalFailsClosedOnIdentityMismatch(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(*state.SurfaceConsoleRecord)
		threadID string
		turnID   string
	}{
		{
			name: "legacy session without frozen access",
			mutate: func(surface *state.SurfaceConsoleRecord) {
				surface.ReviewSession.FrozenAccessMode = ""
			},
			threadID: "thread-review",
			turnID:   "turn-review-1",
		},
		{
			name:     "parent thread",
			threadID: "thread-main",
			turnID:   "turn-review-1",
		},
		{
			name:     "different turn",
			threadID: "thread-review",
			turnID:   "turn-other",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, surface, cwd := newReviewSessionService(t)
			startCodexReviewForAutoApprovalTest(t, svc, surface, cwd)
			if tt.mutate != nil {
				tt.mutate(surface)
			}

			events := svc.ApplyAgentEvent("inst-1", reviewApprovalRequestEvent(
				tt.threadID, tt.turnID, "req-mismatch", control.RequestSemanticApprovalCommand,
			))

			requireReviewRequestPrompt(t, events, "req-mismatch")
		})
	}
}

func TestFullAccessCodexReviewRequiresAcceptCapability(t *testing.T) {
	svc, surface, cwd := newReviewSessionService(t)
	startCodexReviewForAutoApprovalTest(t, svc, surface, cwd)
	request := reviewApprovalRequestEvent(
		"thread-review", "turn-review-1", "req-no-accept", control.RequestSemanticApprovalCommand,
	)
	request.Metadata["options"] = []map[string]any{{"id": "decline", "label": "拒绝"}}

	events := svc.ApplyAgentEvent("inst-1", request)

	requireReviewRequestPrompt(t, events, "req-no-accept")
}

func TestFullAccessCodexReviewAutoApprovalDispatchFailureRestoresVisiblePrompt(t *testing.T) {
	svc, surface, cwd := newReviewSessionService(t)
	startCodexReviewForAutoApprovalTest(t, svc, surface, cwd)

	dispatch := svc.ApplyAgentEvent("inst-1", reviewApprovalRequestEvent(
		"thread-review", "turn-review-1", "req-dispatch-failure", control.RequestSemanticApprovalCommand,
	))
	command := requireSilentReviewApprovalCommand(t, dispatch, "req-dispatch-failure")

	restored := svc.HandleCommandRejected("inst-1", agentproto.CommandAck{
		CommandID: command.CommandID,
		Accepted:  false,
		Error:     "request dispatch failed",
	})

	if len(restored) != 2 || restored[0].RequestView == nil || restored[1].Notice == nil {
		t.Fatalf("expected visible retry prompt plus failure notice, got %#v", restored)
	}
	if restored[0].RequestView.RequestID != "req-dispatch-failure" || restored[0].RequestView.Sealed {
		t.Fatalf("expected editable restored request prompt, got %#v", restored[0].RequestView)
	}
	request := surface.PendingRequests["req-dispatch-failure"]
	if request == nil || request.PendingDispatchCommandID != "" || request.LifecycleState != requestLifecycleEditingVisible {
		t.Fatalf("expected restored interactive lifecycle, got %#v", request)
	}
}
