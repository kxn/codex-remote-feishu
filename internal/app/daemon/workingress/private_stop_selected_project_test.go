package workingress

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestPrivateStopWithProjectSelectedLeavesProjectAndNewPrivateInputRunnable(t *testing.T) {
	ingress, store, _ := ingressFixture(t, true)
	receive(t, ingress, rawMessage("leader", "membership", "group", "group", "open-person", "person", "project work"))
	process(t, ingress)
	menu, nav := privateCommandSource(t, ingress, store, "private-entry")
	service := privateTestService(t, store)
	selected, err := service.TrustedPrivateNavigate(t.Context(), workservice.TrustedPrivateNavigateRequest{Key: "enter-project", Source: menu, Action: "enter_project", ProjectScopeID: "scope", ExpectedRevision: nav.Revision})
	if err != nil {
		t.Fatal(err)
	}

	receive(t, ingress, rawMessage("leader", "old-private", "dm", "p2p", "open-person", "person", "old private queue"))
	process(t, ingress)
	stop := rawMessage("leader", "stop-private", "dm", "p2p", "open-person", "person", "/stop")
	stop.ReceivedAt = stop.ReceivedAt.Add(time.Millisecond)
	source, nav := privateCommandSourceFromIngress(t, ingress, store, stop)
	newInput := rawMessage("leader", "new-private", "dm", "p2p", "open-person", "person", "new private request")
	newInput.ReceivedAt = stop.ReceivedAt.Add(time.Millisecond)
	receive(t, ingress, newInput)
	process(t, ingress)
	var original work.Project
	txRun(t, store, func(tx *workstore.Tx) error {
		scope, e := tx.GetScope("scope")
		if e != nil {
			return e
		}
		original, e = tx.GetProject(scope.ProjectID)
		return e
	})
	if _, err = service.TrustedPrivateStop(t.Context(), workservice.TrustedPrivateStopRequest{Key: "stop-private", Source: source, ExpectedNavRevision: nav.Revision}); err != nil {
		t.Fatal(err)
	}
	txRun(t, store, func(tx *workstore.Tx) error {
		project, e := tx.GetProject(original.ID)
		if e != nil {
			return e
		}
		if project.State != work.ProjectActive || project.Revision != original.Revision {
			t.Fatalf("private stop mutated selected project: before=%+v after=%+v", original, project)
		}
		projectInputs, e := tx.ListInbox(project.ScopeID)
		if e != nil {
			return e
		}
		if len(projectInputs) != 1 || projectInputs[0].State != work.InboxPending {
			t.Fatalf("private stop changed project work: %+v", projectInputs)
		}
		inputs, e := tx.ListInbox(nav.PersonalScopeID)
		if e != nil {
			return e
		}
		states := map[string]work.InboxState{}
		for _, input := range inputs {
			msg, e := tx.GetMessage(input.MessageID)
			if e != nil {
				return e
			}
			if input.RecipientContextID != selected.Navigation.FrontContextID || msg.PlatformKey == nil {
				t.Fatalf("private input retargeted or copied to management: %+v", input)
			}
			states[msg.PlatformKey.MessageID] = input.State
		}
		if len(states) != 2 || states["old-private"] != work.InboxCancelled || states["new-private"] != work.InboxPending {
			t.Fatalf("stop must cancel only earlier private inputs: %+v", states)
		}
		gates, e := tx.ListPauseGates(nav.PersonalScopeID)
		if e != nil {
			return e
		}
		allowed, e := work.GatesAllowRunForContext(nav.PersonalScopeID, "", selected.Navigation.FrontContextID, gates)
		if e != nil || !allowed || len(gates) != 0 {
			t.Fatalf("new private work has an unexpected pause: %+v %v", gates, e)
		}
		return nil
	})
}
