package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestDetachDoesNotReleaseManagedHeadlessUsedByAnotherSurface(t *testing.T) {
	now := time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-headless-1",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		Source:        "headless",
		Managed:       true,
		Online:        true,
	})
	first := svc.ensureSurface(control.Action{SurfaceSessionID: "surface-1"})
	second := svc.ensureSurface(control.Action{SurfaceSessionID: "surface-2"})
	// The first surface has already finalized its route; the second still owns
	// the instance, so releasing the managed carrier would be unsafe.
	first.AttachedInstanceID = ""
	second.AttachedInstanceID = "inst-headless-1"

	if events := svc.releaseManagedHeadlessAfterDetach(first, "inst-headless-1"); len(events) != 0 {
		t.Fatalf("expected shared managed headless to remain alive, got %#v", events)
	}
}

func TestDetachTimeoutWatchdogForcesFinalizeAfterRunningTurn(t *testing.T) {
	now := time.Date(2026, 4, 5, 11, 30, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Source:                  "headless",
		Managed:                 true,
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, SurfaceSessionID: "surface-1", MessageID: "msg-1", Text: "你好"})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{Kind: agentproto.EventTurnStarted, ThreadID: "thread-1", TurnID: "turn-1", Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown}})

	detach := svc.ApplySurfaceAction(control.Action{Kind: control.ActionDetach, SurfaceSessionID: "surface-1"})
	if len(detach) < 2 {
		t.Fatalf("expected interrupt + detach_pending flow, got %#v", detach)
	}
	if !svc.root.Surfaces["surface-1"].Abandoning {
		t.Fatalf("expected surface to enter abandoning state")
	}

	events := svc.Tick(now.Add(21 * time.Second))
	surface := svc.root.Surfaces["surface-1"]
	if surface.AttachedInstanceID != "" || surface.Abandoning {
		t.Fatalf("expected watchdog to force detach, got %#v", surface)
	}
	if claim := svc.instanceClaims["inst-1"]; claim != nil {
		t.Fatalf("expected instance claim to be released, got %#v", claim)
	}
	var sawForced, sawKill bool
	for _, event := range events {
		if event.Notice != nil && event.Notice.Code == "detach_timeout_forced" {
			sawForced = true
		}
		if event.DaemonCommand != nil && event.DaemonCommand.Kind == control.DaemonCommandKillHeadless && event.DaemonCommand.InstanceID == "inst-1" {
			sawKill = true
		}
	}
	if !sawForced || !sawKill {
		t.Fatalf("expected forced detach notice and managed headless release, got %#v", events)
	}
}
