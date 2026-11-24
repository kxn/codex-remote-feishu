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
