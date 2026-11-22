package workingress

import (
	"errors"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"testing"
	"time"
)

func privateCommandSource(t *testing.T, i *Ingress, s *workstore.Store, id string) (workstore.PrivateActionSource, work.PrivateNavigation) {
	t.Helper()
	return privateCommandSourceFromIngress(t, i, s, rawMessage("leader", id, "dm", "p2p", "open-person", "person", "/new"))
}

func privateCommandSourceFromIngress(t *testing.T, i *Ingress, s *workstore.Store, raw work.RawIngressEnvelope) (workstore.PrivateActionSource, work.PrivateNavigation) {
	t.Helper()
	receipt := receive(t, i, raw)
	process(t, i)
	var source workstore.PrivateActionSource
	var nav work.PrivateNavigation
	txRun(t, s, func(tx *workstore.Tx) error {
		commands, e := tx.ListPendingPrivateCommands()
		if e != nil {
			return e
		}
		for _, command := range commands {
			if command.IngressID != receipt.IngressID {
				continue
			}
			previous := command.Revision
			command.Revision++
			command.State = "sending"
			if e = tx.PutPrivateCommand(command, previous); e != nil {
				return e
			}
			source = workstore.PrivateActionSource{PersonalScopeID: command.ScopeID, CommandID: command.ID}
			nav, e = tx.GetPrivateNavigation(command.ScopeID)
			return e
		}
		return workstore.ErrNotFound
	})
	return source, nav
}
func privateTestService(t *testing.T, s *workstore.Store) *workservice.Service {
	t.Helper()
	signer, e := workauth.New([]byte("0123456789abcdef0123456789abcdef"), func() time.Time { return ingressNow })
	if e != nil {
		t.Fatal(e)
	}
	service, e := workservice.New(s, signer, func() time.Time { return ingressNow })
	if e != nil {
		t.Fatal(e)
	}
	return service
}
func TestPrivateStopCancelsOnlyOwnPendingFrontAndKeepsNavigation(t *testing.T) {
	i, s, _ := ingressFixture(t, false)
	service := privateTestService(t, s)
	receive(t, i, rawMessage("leader", "work", "dm", "p2p", "open-person", "person", "private work"))
	process(t, i)
	source, nav := privateCommandSourceFromIngress(t, i, s, rawMessage("leader", "command", "dm", "p2p", "open-person", "person", "/stop"))
	txRun(t, s, func(tx *workstore.Tx) error {
		if e := tx.PutContext(work.WorkContext{ID: "other-front", ScopeID: nav.PersonalScopeID, RoleID: "leader", Workstream: "other", Revision: 1}, 0); e != nil {
			return e
		}
		for _, v := range []work.Inbox{{ID: "system", RecipientContextID: nav.FrontContextID}, {ID: "other-work", RecipientContextID: "other-front"}} {
			v.ScopeID = nav.PersonalScopeID
			v.RecipientRoleID = "leader"
			v.EventID = v.ID
			v.State = work.InboxPending
			v.Revision = 1
			if e := tx.PutInbox(v, 0); e != nil {
				return e
			}
		}
		return nil
	})
	req := workservice.TrustedPrivateStopRequest{Key: "stop", Source: source, ExpectedNavRevision: nav.Revision}
	if _, e := service.TrustedPrivateStop(t.Context(), req); e != nil {
		t.Fatal(e)
	}
	txRun(t, s, func(tx *workstore.Tx) error {
		inputs, e := tx.ListInbox(nav.PersonalScopeID)
		if e != nil {
			return e
		}
		for _, v := range inputs {
			if v.MessageID != "" && v.State != work.InboxCancelled {
				t.Fatalf("own pending input not cancelled: %+v", v)
			}
			if (v.ID == "system" || v.ID == "other-work") && v.State != work.InboxPending {
				t.Fatalf("unrelated input changed: %+v", v)
			}
		}
		return nil
	})
	txRun(t, s, func(tx *workstore.Tx) error {
		current, e := tx.GetPrivateNavigation(nav.PersonalScopeID)
		if e != nil {
			return e
		}
		if current != nav {
			t.Fatal("stop retargeted navigation")
		}
		gates, e := tx.ListPauseGates(nav.PersonalScopeID)
		if e != nil {
			return e
		}
		if len(gates) != 0 {
			t.Fatalf("stop installed a persistent pause: %+v", gates)
		}
		return nil
	})
}
func TestPrivateProjectNewCreatesRealProjectContextAndCASRollsBack(t *testing.T) {
	i, s, _ := ingressFixture(t, true)
	receive(t, i, rawMessage("leader", "membership", "group", "group", "open-person", "person", "hello"))
	process(t, i)
	source, nav := privateCommandSource(t, i, s, "command")
	service := privateTestService(t, s)
	req := workservice.TrustedPrivateNavigateRequest{Key: "enter", Source: source, Action: "enter_project", ProjectScopeID: "scope", ExpectedRevision: nav.Revision}
	view, e := service.TrustedPrivateNavigate(t.Context(), req)
	if e != nil {
		t.Fatal(e)
	}
	if !view.ProjectAccess || view.Navigation.ProjectContextID == "" {
		t.Fatalf("missing selected project: %+v", view)
	}
	txRun(t, s, func(tx *workstore.Tx) error {
		front, e := tx.GetContext(view.Navigation.FrontContextID)
		if e != nil {
			return e
		}
		project, e := tx.GetContext(view.Navigation.ProjectContextID)
		if e != nil {
			return e
		}
		if front.ScopeID != nav.PersonalScopeID || project.ScopeID != "scope" || front.ProjectContextID != project.ID {
			t.Fatal("new view lacks separate true project context")
		}
		return nil
	})
	req.Key = "new-stale"
	req.Action = "new"
	if _, e = service.TrustedPrivateNavigate(t.Context(), req); !errors.Is(e, workstore.ErrConflict) {
		t.Fatalf("stale navigation: %v", e)
	}
	req.Key = "new"
	req.ExpectedRevision = view.Navigation.Revision
	next, e := service.TrustedPrivateNavigate(t.Context(), req)
	if e != nil {
		t.Fatal(e)
	}
	if next.Navigation.ProjectContextID == view.Navigation.ProjectContextID || next.Navigation.FrontContextID == view.Navigation.FrontContextID {
		t.Fatal("new project work reused previous context")
	}
	req.Key = "select-old"
	req.Action = "select_context"
	req.ContextID = view.Navigation.FrontContextID
	req.ExpectedRevision = next.Navigation.Revision
	selected, e := service.TrustedPrivateNavigate(t.Context(), req)
	if e != nil {
		t.Fatal(e)
	}
	if selected.Navigation.ProjectContextID != view.Navigation.ProjectContextID {
		t.Fatal("select context guessed latest project context")
	}
}

func TestPrivateInputKeepsReceiveTimeFrontAfterNewNavigation(t *testing.T) {
	i, s, _ := ingressFixture(t, false)
	source, nav := privateCommandSource(t, i, s, "command")
	service := privateTestService(t, s)
	receipt := receive(t, i, rawMessage("leader", "queued-before-new", "dm", "p2p", "open-person", "person", "keep this in the existing work"))
	view, e := service.TrustedPrivateNavigate(t.Context(), workservice.TrustedPrivateNavigateRequest{Key: "new", Source: source, Action: "new", ExpectedRevision: nav.Revision})
	if e != nil {
		t.Fatal(e)
	}
	if view.Navigation.FrontContextID == nav.FrontContextID {
		t.Fatal("new did not create separate front")
	}
	process(t, i)
	txRun(t, s, func(tx *workstore.Tx) error {
		raw, e := tx.GetIngress(receipt.IngressID)
		if e != nil {
			return e
		}
		inputs, e := tx.ListInbox(nav.PersonalScopeID)
		if e != nil {
			return e
		}
		for _, input := range inputs {
			if input.MessageID == raw.MessageID && input.RecipientContextID != nav.FrontContextID {
				t.Fatal("queued input followed latest navigation")
			}
		}
		return nil
	})
}

func TestPrivateCommandsStayFIFOAndCannotRetargetFrozenWork(t *testing.T) {
	i, s, _ := ingressFixture(t, false)
	first := receive(t, i, rawMessage("leader", "command-first", "dm", "p2p", "open-person", "person", "/new"))
	second := receive(t, i, rawMessage("leader", "command-second", "dm", "p2p", "open-person", "person", "/stop"))
	process(t, i)
	txRun(t, s, func(tx *workstore.Tx) error {
		commands, e := tx.ListPendingPrivateCommands()
		if e != nil {
			return e
		}
		if len(commands) != 2 || commands[0].IngressID != first.IngressID || commands[1].IngressID != second.IngressID {
			t.Fatalf("command FIFO lost: %+v", commands)
		}
		command := commands[0]
		command.Revision++
		command.ContextID = "different-front"
		if e = tx.PutPrivateCommand(command, commands[0].Revision); e == nil {
			t.Fatal("command source context is mutable")
		}
		return nil
	})
}
