package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestPrivateControlProjectionRestoresExactHolds(t *testing.T) {
	for _, kind := range []agentproto.CommandKind{agentproto.CommandReviewStart, agentproto.CommandThreadCompactStart} {
		t.Run(string(kind), func(t *testing.T) {
			svc, surface, _ := newReviewSessionService(t)
			svc.config.PrivateApplication = true
			if kind == agentproto.CommandReviewStart {
				surface.ReviewSession = &state.ReviewSessionRecord{Phase: state.ReviewSessionPhaseActive, ParentThreadID: "thread-main", ReviewThreadID: "thread-review", InitialTurnID: "turn"}
				svc.turns.bindPendingReview("command", surface.SurfaceSessionID)
			} else {
				svc.turns.compactTurns["inst-1"] = &compactTurnBinding{InstanceID: "inst-1", SurfaceSessionID: surface.SurfaceSessionID, ThreadID: "thread-main", CommandID: "command", Status: compactTurnStatusDispatching}
			}
			cp, err := svc.ExportPrivateCheckpoint(surface.SurfaceSessionID)
			if err != nil {
				t.Fatal(err)
			}
			cp.GatewayID, cp.ChatID, cp.ActorUserID = "gateway", "chat", "user"
			now := time.Now()
			restored := NewService(func() time.Time { return now }, Config{PrivateApplication: true}, nil)
			if err = restored.RestorePrivateCheckpoint(cp); err != nil {
				t.Fatal(err)
			}
			thread := "thread-main"
			if kind == agentproto.CommandReviewStart {
				thread = "thread-review"
			}
			if err = restored.RestorePrivateControlProjection(surface.SurfaceSessionID, "inst-1", "wrong", kind, "thread-main", thread, "turn"); err != nil {
				t.Fatal(err)
			}
			if kind == agentproto.CommandThreadCompactStart && restored.turns.compactTurns["inst-1"] != nil {
				t.Fatal("wrong compact command restored")
			}
			if kind == agentproto.CommandReviewStart && restored.root.Surfaces[surface.SurfaceSessionID].ReviewSession != nil {
				t.Fatal("wrong review command restored")
			}
			if err = restored.RestorePrivateControlProjection(surface.SurfaceSessionID, "inst-1", "command", kind, "thread-main", thread, "turn"); err != nil {
				t.Fatal(err)
			}
			if kind == agentproto.CommandReviewStart {
				if restored.root.Surfaces[surface.SurfaceSessionID].ReviewSession == nil || restored.turns.pendingReviewSurface("command") != surface.SurfaceSessionID {
					t.Fatal("review was not restored")
				}
			} else if restored.turns.compactTurns["inst-1"] == nil {
				t.Fatal("compact was not restored")
			}
		})
	}
}

func TestProjectVerifiedPrivateReviewThreadChecksOwnerAndPreservesNativeFacts(t *testing.T) {
	svc, surface, _ := newReviewSessionService(t)
	svc.config.PrivateApplication = true
	surface.ReviewSession = &state.ReviewSessionRecord{Phase: state.ReviewSessionPhasePending, ParentThreadID: "thread-main"}
	inst := svc.root.Instances["inst-1"]
	thread := svc.ensureThread(inst, "review-child")
	thread.Source = &agentproto.ThreadSourceRecord{Kind: "vscode"}
	if err := svc.ProjectVerifiedPrivateReviewThread(surface.SurfaceSessionID, "inst-1", "wrong-parent", "review-child"); err == nil {
		t.Fatal("accepted wrong parent")
	}
	if thread.Source.Kind != "vscode" {
		t.Fatal("invalid proof changed source")
	}
	if err := svc.ProjectVerifiedPrivateReviewThread(surface.SurfaceSessionID, "inst-1", "thread-main", "review-child"); err != nil {
		t.Fatal(err)
	}
	if thread.Source.Kind != "review" || thread.Source.ParentThreadID != "thread-main" || thread.ForkedFromID != "" {
		t.Fatalf("invalid source projection: %#v", thread)
	}
}

func TestPrivateReviewChildTurnTracksProvenRootForCompletion(t *testing.T) {
	svc, surface, _ := newReviewSessionService(t)
	svc.config.PrivateApplication = true
	surface.ReviewSession = &state.ReviewSessionRecord{Phase: state.ReviewSessionPhasePending, ParentThreadID: "thread-main"}
	inst := svc.root.Instances["inst-1"]
	thread := svc.ensureThread(inst, "review-child")
	thread.Source = &agentproto.ThreadSourceRecord{Kind: agentproto.ThreadSourceKindReview, ParentThreadID: "thread-main"}
	event := agentproto.Event{ThreadID: "review-child", TurnID: "child-turn", RootTurnID: "root-turn", NativeBinding: &agentproto.NativeTurnBinding{}}
	session := svc.activateReviewSessionRecord(surface, thread, event)
	if session.InitialTurnID != "root-turn" || session.ActiveTurnID != "root-turn" || event.TurnID != "child-turn" {
		t.Fatalf("wrong review identity: session=%+v native=%+v", session, event)
	}
}

func TestPrivateReviewRootOwnsInstanceBusyUntilRootTerminal(t *testing.T) {
	svc, surface, _ := newReviewSessionService(t)
	svc.config.PrivateApplication = true
	surface.ReviewSession = &state.ReviewSessionRecord{Phase: state.ReviewSessionPhasePending, ParentThreadID: "thread-main"}
	binding := &agentproto.NativeTurnBinding{}
	started := agentproto.Event{Kind: agentproto.EventTurnStarted, ThreadID: "thread-review", TurnID: "child", RootTurnID: "root", NativeBinding: binding, Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surface.SurfaceSessionID}}
	svc.ApplyAgentEvent("inst-1", started)
	inst := svc.root.Instances["inst-1"]
	if inst.ActiveTurnID != "root" || started.TurnID != "child" {
		t.Fatalf("lost native/root identity: active=%q native=%q", inst.ActiveTurnID, started.TurnID)
	}
	svc.ApplyAgentEvent("inst-1", agentproto.Event{Kind: agentproto.EventTurnCompleted, ThreadID: "other-thread", TurnID: "root", Status: "completed"})
	if inst.ActiveTurnID != "root" {
		t.Fatal("foreign thread terminal cleared occupancy")
	}
	svc.ApplyAgentEvent("inst-1", agentproto.Event{Kind: agentproto.EventTurnCompleted, ThreadID: "thread-review", TurnID: "root", Status: "completed"})
	if inst.ActiveTurnID != "" {
		t.Fatalf("root terminal left instance busy: %q", inst.ActiveTurnID)
	}
}
