package daemon

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workpublish"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestProjectLifecycleTickSettlesAcceptedHumanActions(t *testing.T) {
	for _, scenario := range []struct {
		action           string
		waiting, settled work.ProjectState
	}{
		{"pause", work.ProjectPausing, work.ProjectPaused},
		{"archive", work.ProjectArchiving, work.ProjectArchived},
	} {
		t.Run(scenario.action, func(t *testing.T) {
			ctx := t.Context()
			app := teamProjectViewFixture(t)
			store := app.teamRuntime.store
			signer, err := workauth.New([]byte("0123456789abcdef0123456789abcdef"), time.Now)
			if err != nil {
				t.Fatal(err)
			}
			service, err := workservice.New(store, signer, time.Now)
			if err != nil {
				t.Fatal(err)
			}
			app.teamRuntime.service = service
			if err := store.Within(ctx, func(tx *workstore.Tx) error {
				if err := tx.PutPrincipal(work.Principal{ID: "auditor", DisplayName: "Auditor", Revision: 1, CreatedAt: time.Now()}, 0); err != nil {
					return err
				}
				body := work.NewContent("actual:audit:body", "scope", "text/plain", []byte(scenario.action), time.Now())
				if err := tx.PutContent(body); err != nil {
					return err
				}
				_, _, err := tx.AcceptMessage(work.Message{ID: "actual:audit", ScopeID: "scope", ConversationID: "chat", Source: work.Actor{Kind: work.ActorPrincipal, ID: "auditor"}, BodyRef: body.ID, Revision: 1, CreatedAt: time.Now()}, nil)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			result, err := service.ApplyHumanProjectLifecycle(ctx, "auditor", workservice.HumanProjectLifecycleRequest{Action: scenario.action, ProjectLifecycleRequest: workservice.ProjectLifecycleRequest{Key: "audit", ProjectID: "project", ExpectedRevision: 1, SourceMessageID: "actual:audit"}})
			if err != nil || result.Status != "accepted" {
				t.Fatalf("apply %+v %v", result, err)
			}
			state := func(want work.ProjectState) {
				t.Helper()
				err := store.Within(ctx, func(tx *workstore.Tx) error {
					project, err := tx.GetProject("project")
					if err == nil && project.State != want {
						t.Errorf("state %s, want %s", project.State, want)
					}
					return err
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			for range 3 {
				if err := app.runTeamProjectViews(ctx); err != nil {
					t.Fatal(err)
				}
				state(scenario.waiting)
			}
			publisher, err := workpublish.New(store, recoveryUISender{}, time.Now)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = publisher.RunOnce(ctx, 32); err != nil {
				t.Fatal(err)
			}
			if err := app.runTeamProjectViews(ctx); err != nil {
				t.Fatal(err)
			}
			state(scenario.waiting)
			if err := app.runTeamProjectLifecycle(ctx); err != nil {
				t.Fatal(err)
			}
			state(scenario.settled)
		})
	}
}
