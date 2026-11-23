package orchestrator

import (
	"errors"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestPrivateQueryNegativeCompletionKeepsExactOwnerAndClearsPending(t *testing.T) {
	for _, kind := range []control.ActionKind{control.ActionShowHistory, control.ActionMCPOAuthCommand} {
		t.Run(string(kind), func(t *testing.T) {
			now := time.Now()
			s := newServiceForTest(&now)
			s.config.PrivateApplication = true
			s.UpsertInstance(&state.InstanceRecord{InstanceID: "instance", Online: true, WorkspaceRoot: "/repo", WorkspaceKey: "/repo", Threads: map[string]*state.ThreadRecord{"thread": {ThreadID: "thread", CWD: "/repo", Loaded: true}}})
			s.MaterializeSurface("surface", "gateway", "chat", "user")
			surface := s.root.Surfaces["surface"]
			surface.AttachedInstanceID = "instance"
			surface.SelectedThreadID = "thread"
			surface.RouteMode = state.RouteModePinned
			text := "/history"
			if kind == control.ActionMCPOAuthCommand {
				text = "/mcpoauth github"
			}
			effects := s.ApplySurfaceAction(control.Action{Kind: kind, SurfaceSessionID: "surface", ActorUserID: "user", Text: text})
			command := ""
			for _, e := range effects {
				if e.Command != nil {
					command = e.Command.CommandID
				}
			}
			if command == "" {
				t.Fatal("missing query")
			}
			cp, err := s.ExportPrivateCheckpoint("surface")
			if err != nil {
				t.Fatal(err)
			}
			restored := newServiceForTest(&now)
			restored.config.PrivateApplication = true
			if err = restored.RestorePrivateCheckpoint(cp); err != nil {
				t.Fatal(err)
			}
			cause := errors.New("backend rejected before writing request")
			if _, handled := restored.HandlePrivateQueryFailure("surface", "other", command, cause); handled {
				t.Fatal("foreign instance settled query")
			}
			if _, handled := restored.HandlePrivateQueryFailure("other", "instance", command, cause); handled {
				t.Fatal("foreign surface settled query")
			}
			result, handled := restored.HandlePrivateQueryFailure("surface", "instance", command, cause)
			if !handled || len(result) != 1 {
				t.Fatalf("query not settled: %+v %v", result, handled)
			}
			if kind == control.ActionShowHistory && (result[0].ThreadHistoryView == nil || result[0].ThreadHistoryView.Loading) {
				t.Fatalf("history failure projection: event=%+v view=%+v flow=%+v", result[0], result[0].ThreadHistoryView, restored.activeOwnerCardFlow(restored.root.Surfaces["surface"]))
			}
			if kind == control.ActionMCPOAuthCommand && result[0].Notice == nil {
				t.Fatal("OAuth failure lost")
			}
			if _, handled = restored.HandlePrivateQueryFailure("surface", "instance", command, cause); handled {
				t.Fatal("duplicate negative completion republished")
			}
		})
	}
}
