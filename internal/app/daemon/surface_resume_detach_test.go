package daemon

import (
	"context"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/surfaceresume"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestDetachActionsClearPersistedResumeTarget(t *testing.T) {
	for _, actionKind := range []control.ActionKind{control.ActionDetach, control.ActionWorkspaceDetach} {
		t.Run(string(actionKind), func(t *testing.T) {
			stateDir := t.TempDir()
			putSurfaceResumeStateForTest(t, stateDir, surfaceresume.Entry{
				SurfaceSessionID:   "surface-1",
				GatewayID:          "app-1",
				ChatID:             "chat-1",
				ActorUserID:        "user-1",
				ProductMode:        "normal",
				Backend:            "codex",
				ResumeInstanceID:   "inst-headless-1",
				ResumeThreadID:     "thread-1",
				ResumeThreadCWD:    "/data/dl/droid",
				ResumeWorkspaceKey: "/data/dl/droid",
				ResumeRouteMode:    "pinned",
				ResumeHeadless:     true,
			})
			app := newRestoreHintTestApp(stateDir)

			app.HandleAction(context.Background(), control.Action{
				Kind:             actionKind,
				GatewayID:        "app-1",
				SurfaceSessionID: "surface-1",
				ChatID:           "chat-1",
				ActorUserID:      "user-1",
			})

			entry := app.SurfaceResumeState("surface-1")
			if entry == nil {
				t.Fatal("expected detached surface metadata to remain persisted")
			}
			if entry.ResumeInstanceID != "" || entry.ResumeThreadID != "" || entry.ResumeThreadCWD != "" || entry.ResumeWorkspaceKey != "" || entry.ResumeRouteMode != "" || entry.ResumeHeadless {
				t.Fatalf("expected %s to clear persisted resume target, got %#v", actionKind, entry)
			}
			if _, ok := app.surfaceResumeRuntime.recovery["surface-1"]; ok {
				t.Fatalf("expected %s to remove surface from recovery state", actionKind)
			}

			restarted := newRestoreHintTestApp(stateDir)
			if _, ok := restarted.surfaceResumeRuntime.recovery["surface-1"]; ok {
				t.Fatalf("expected %s to stay out of recovery after state reload", actionKind)
			}
		})
	}
}
