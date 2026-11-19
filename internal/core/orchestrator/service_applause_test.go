package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func applauseFixture(t *testing.T) (*Service, *state.SurfaceConsoleRecord) {
	t.Helper()
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:           "inst-1",
		Backend:              agentproto.BackendCodex,
		Online:               true,
		WorkspaceRoot:        "/tmp/workspace",
		WorkspaceKey:         "/tmp/workspace",
		ActiveThreadID:       "thread-1",
		ActiveTurnID:         "turn-1",
		Capabilities:         agentproto.Capabilities{ThreadShellCommand: true},
		CapabilitiesDeclared: true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", CWD: "/tmp/workspace"},
		},
	})
	svc.MaterializeSurface("surface-1", "gateway-1", "chat-1", "user-1")
	surface := svc.root.Surfaces["surface-1"]
	surface.AttachedInstanceID = "inst-1"
	surface.StagedImages = map[string]*state.StagedImageRecord{}
	surface.QueueItems = map[string]*state.QueueItemRecord{}
	return svc, surface
}

func TestApplauseReactionBuildsStructuredPayloadAndClaimsOnlyMainMessage(t *testing.T) {
	svc, surface := applauseFixture(t)
	surface.StagedImages["img-1"] = &state.StagedImageRecord{
		ImageID:         "img-1",
		SourceMessageID: "msg-image",
		LocalPath:       "/tmp/image with spaces.png",
		MIMEType:        "image/png",
		State:           state.ImageBound,
	}
	surface.QueueItems["queue-1"] = &state.QueueItemRecord{
		ID:               "queue-1",
		SourceMessageID:  "msg-text",
		SourceMessageIDs: []string{"msg-text", "msg-image"},
		Inputs: []agentproto.Input{
			{Type: agentproto.InputLocalImage, Path: "/tmp/image with spaces.png", MIMEType: "image/png"},
			{Type: agentproto.InputText, Text: "$(printf unsafe)\n请继续处理"},
		},
		FrozenDispatchPlan: agentproto.PromptDispatchPlan{ExecutionThreadID: "thread-1"},
		Status:             state.QueueItemQueued,
	}
	surface.QueuedQueueItemIDs = []string{"queue-1"}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReactionCreated,
		SurfaceSessionID: "surface-1",
		ReactionType:     "APPLAUSE",
		TargetMessageID:  "msg-text",
	})
	if len(events) != 2 || events[1].Command == nil {
		t.Fatalf("expected pending status and shell command, got %#v", events)
	}
	item := surface.QueueItems["queue-1"]
	if item.Status != state.QueueItemShellCommanding || len(surface.QueuedQueueItemIDs) != 0 {
		t.Fatalf("unexpected shell claim state: item=%#v queue=%#v", item, surface.QueuedQueueItemIDs)
	}
	command := events[1].Command
	if command.Kind != agentproto.CommandThreadShellCommand || command.Target.TurnID != "turn-1" {
		t.Fatalf("unexpected shell command: %#v", command)
	}
	if !strings.Contains(command.ShellCommand.Payload, "queued_input_bundle.v1") || !strings.Contains(command.ShellCommand.Payload, "$(printf unsafe)") || !strings.Contains(command.ShellCommand.Payload, `"type": "image"`) || !strings.Contains(command.ShellCommand.Payload, "/tmp/image with spaces.png") {
		t.Fatalf("payload lost structured content: %s", command.ShellCommand.Payload)
	}
	if !strings.Contains(command.ShellCommand.Payload, `"mime_type": "image/png"`) {
		t.Fatalf("payload lost MIME type: %s", command.ShellCommand.Payload)
	}

	if got := svc.ApplySurfaceAction(control.Action{Kind: control.ActionReactionCreated, SurfaceSessionID: "surface-1", ReactionType: "ThumbsUp", TargetMessageID: "msg-text"}); len(got) != 0 {
		t.Fatalf("steer must lose the queue-item claim race, got %#v", got)
	}

	svc.BindPendingRemoteCommand("surface-1", "cmd-shell-1")
	accepted := svc.HandleCommandAccepted("inst-1", agentproto.CommandAck{CommandID: "cmd-shell-1", Accepted: true})
	if item.Status != state.QueueItemShellCommanded || svc.pendingShellBinding("queue-1") != nil {
		t.Fatalf("unexpected accepted state: item=%#v binding=%#v", item, svc.pendingShellBinding("queue-1"))
	}
	foundApplause := false
	for _, event := range accepted {
		if event.PendingInput != nil && event.PendingInput.Applause && event.PendingInput.SourceMessageID == "msg-text" && event.PendingInput.QueueOff {
			foundApplause = true
		}
		if event.PendingInput != nil && event.PendingInput.SourceMessageID == "msg-image" && event.PendingInput.Applause {
			t.Fatal("image source must not receive the bot acknowledgement reaction")
		}
	}
	if !foundApplause {
		t.Fatalf("accepted shell command did not project APPLAUSE: %#v", accepted)
	}
}

func TestApplauseRejectedRestoresQueueOrderAndUnknownDoesNotRetry(t *testing.T) {
	svc, surface := applauseFixture(t)
	surface.QueueItems["queue-1"] = &state.QueueItemRecord{ID: "queue-1", SourceMessageID: "msg-1", Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "one"}}, FrozenDispatchPlan: agentproto.PromptDispatchPlan{ExecutionThreadID: "thread-1"}, Status: state.QueueItemQueued}
	surface.QueueItems["queue-2"] = &state.QueueItemRecord{ID: "queue-2", SourceMessageID: "msg-2", Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "two"}}, FrozenDispatchPlan: agentproto.PromptDispatchPlan{ExecutionThreadID: "thread-1"}, Status: state.QueueItemQueued}
	surface.QueuedQueueItemIDs = []string{"queue-1", "queue-2"}
	if events := svc.ApplySurfaceAction(control.Action{Kind: control.ActionReactionCreated, SurfaceSessionID: "surface-1", ReactionType: "APPLAUSE", TargetMessageID: "msg-2"}); len(events) != 2 {
		t.Fatalf("expected shell command dispatch, got %#v", events)
	}
	svc.BindPendingRemoteCommand("surface-1", "cmd-shell-reject")
	restored := svc.HandleCommandRejected("inst-1", agentproto.CommandAck{CommandID: "cmd-shell-reject", Error: "rejected"})
	if got := surface.QueuedQueueItemIDs; len(got) != 2 || got[0] != "queue-1" || got[1] != "queue-2" || surface.QueueItems["queue-2"].Status != state.QueueItemQueued {
		t.Fatalf("rejection did not restore order: queue=%#v item=%#v", got, surface.QueueItems["queue-2"])
	}
	if len(restored) == 0 || restored[len(restored)-1].Notice == nil || restored[len(restored)-1].Notice.Code != "shell_command_failed" {
		t.Fatalf("expected shell failure notice, got %#v", restored)
	}

	if events := svc.ApplySurfaceAction(control.Action{Kind: control.ActionReactionCreated, SurfaceSessionID: "surface-1", ReactionType: "APPLAUSE", TargetMessageID: "msg-2"}); len(events) != 2 {
		t.Fatalf("expected second shell command dispatch, got %#v", events)
	}
	svc.BindPendingRemoteCommand("surface-1", "cmd-shell-unknown")
	unknown := svc.HandleCommandRejected("inst-1", agentproto.CommandAck{CommandID: "cmd-shell-unknown", Problem: &agentproto.ErrorInfo{Code: "shell_command_response_timeout", Message: "timeout"}})
	if surface.QueueItems["queue-2"].Status != state.QueueItemShellUnknown || len(surface.QueuedQueueItemIDs) != 1 || surface.QueuedQueueItemIDs[0] != "queue-1" {
		t.Fatalf("unknown result must not requeue: queue=%#v item=%#v", surface.QueuedQueueItemIDs, surface.QueueItems["queue-2"])
	}
	if len(unknown) == 0 || unknown[len(unknown)-1].Notice == nil || unknown[len(unknown)-1].Notice.Code != "shell_command_unknown" {
		t.Fatalf("expected unknown result notice, got %#v", unknown)
	}
}

func TestApplauseReactionBlockedByActiveRequestCapture(t *testing.T) {
	svc, surface := applauseFixture(t)
	surface.ActiveRequestCapture = &state.RequestCaptureRecord{RequestID: "request-1"}
	surface.QueueItems["queue-1"] = &state.QueueItemRecord{
		ID:                 "queue-1",
		SourceMessageID:    "msg-1",
		Inputs:             []agentproto.Input{{Type: agentproto.InputText, Text: "one"}},
		FrozenDispatchPlan: agentproto.PromptDispatchPlan{ExecutionThreadID: "thread-1"},
		Status:             state.QueueItemQueued,
	}
	surface.QueuedQueueItemIDs = []string{"queue-1"}

	if events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReactionCreated,
		SurfaceSessionID: "surface-1",
		ReactionType:     "APPLAUSE",
		TargetMessageID:  "msg-1",
	}); len(events) != 0 {
		t.Fatalf("applause must be ignored while request feedback is captured, got %#v", events)
	}
	if item := surface.QueueItems["queue-1"]; item.Status != state.QueueItemQueued {
		t.Fatalf("request capture must leave queue item untouched, got %#v", item)
	}
}

func TestApplauseShellBindingsMatchPendingCommandsInCreationOrder(t *testing.T) {
	svc, surface := applauseFixture(t)
	for _, item := range []*state.QueueItemRecord{
		{ID: "queue-1", SourceMessageID: "msg-1", Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "one"}}, FrozenDispatchPlan: agentproto.PromptDispatchPlan{ExecutionThreadID: "thread-1"}, Status: state.QueueItemQueued},
		{ID: "queue-2", SourceMessageID: "msg-2", Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "two"}}, FrozenDispatchPlan: agentproto.PromptDispatchPlan{ExecutionThreadID: "thread-1"}, Status: state.QueueItemQueued},
	} {
		surface.QueueItems[item.ID] = item
	}
	surface.QueuedQueueItemIDs = []string{"queue-1", "queue-2"}
	if events := svc.ApplySurfaceAction(control.Action{Kind: control.ActionReactionCreated, SurfaceSessionID: "surface-1", ReactionType: "APPLAUSE", TargetMessageID: "msg-1"}); len(events) != 2 {
		t.Fatalf("expected first shell command dispatch, got %#v", events)
	}
	if events := svc.ApplySurfaceAction(control.Action{Kind: control.ActionReactionCreated, SurfaceSessionID: "surface-1", ReactionType: "APPLAUSE", TargetMessageID: "msg-2"}); len(events) != 2 {
		t.Fatalf("expected second shell command dispatch, got %#v", events)
	}

	svc.BindPendingShellCommand("surface-1", "cmd-first")
	if binding := svc.pendingShellBinding("queue-1"); binding == nil || binding.CommandID != "cmd-first" {
		t.Fatalf("first command was not bound to the first pending shell item: %#v", binding)
	}
	svc.BindPendingShellCommand("surface-1", "cmd-second")
	if binding := svc.pendingShellBinding("queue-2"); binding == nil || binding.CommandID != "cmd-second" {
		t.Fatalf("second command was not bound to the second pending shell item: %#v", binding)
	}
}
