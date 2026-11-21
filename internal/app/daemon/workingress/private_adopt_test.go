package workingress

import (
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"testing"
	"time"
)

func TestPrivateInstructionAdoptionDoesNotExposePrivateBodyOrHistory(t *testing.T) {
	i, s, _ := ingressFixture(t, true)
	receive(t, i, rawMessage("leader", "membership", "group", "group", "open-person", "person", "hello"))
	process(t, i)
	source, nav := privateCommandSource(t, i, s, "command")
	service := privateTestService(t, s)
	view, e := service.TrustedPrivateNavigate(t.Context(), workservice.TrustedPrivateNavigateRequest{Key: "enter", Source: source, Action: "enter_project", ProjectScopeID: "scope", ExpectedRevision: nav.Revision})
	if e != nil {
		t.Fatal(e)
	}
	receive(t, i, rawMessage("leader", "private-old", "dm", "p2p", "open-person", "person", "private earlier history never shared"))
	process(t, i)
	current := receive(t, i, rawMessage("leader", "private-new", "dm", "p2p", "open-person", "person", "请在项目实施搜索。我的私人密码不属于需求"))
	process(t, i)
	var original work.Message
	var input work.Inbox
	txRun(t, s, func(tx *workstore.Tx) error {
		raw, e := tx.GetIngress(current.IngressID)
		if e != nil {
			return e
		}
		original, e = tx.GetMessage(raw.MessageID)
		if e != nil {
			return e
		}
		inputs, e := tx.ListInbox(nav.PersonalScopeID)
		if e != nil {
			return e
		}
		for _, v := range inputs {
			if v.MessageID == original.ID {
				input = v
			}
		}
		worker := work.WorkerSession{ID: "private-worker", ContextID: view.Navigation.FrontContextID, ScopeID: nav.PersonalScopeID, RoleID: "leader", Backend: work.BackendCodex, State: work.WorkerReady, Generation: 1, Controller: work.ControllerAutomatic, ControllerGeneration: 1, InstanceID: "private-carrier", InstanceGeneration: 1, ProcessGeneration: 1, Revision: 1}
		if e = tx.PutWorker(worker, 0); e != nil {
			return e
		}
		attempt := work.ExecutionAttempt{ID: "private-attempt", ScopeID: worker.ScopeID, ContextID: worker.ContextID, RunRef: work.RunRef{WorkerID: worker.ID, Generation: 1, RunID: "private-run"}, InputIDs: []string{input.ID}, State: work.AttemptDispatchPending, CommandID: "private-command", Revision: 1}
		operation := work.Operation{ID: attempt.CommandID, ScopeID: worker.ScopeID, ContextID: worker.ContextID, Actor: work.Actor{Kind: work.ActorSystem, ID: "scheduler"}, Kind: "native.start", TargetID: worker.ID, TargetGeneration: 1, IdempotencyKey: attempt.CommandID, RequestHash: "hash", State: work.OperationPrepared, Revision: 1}
		if e = tx.ClaimAttempt(workstore.ClaimRequest{Attempt: attempt, StartOperation: operation, ExpectedWorkerRevision: 1, ControllerGeneration: 1, InstanceID: worker.InstanceID, InstanceGeneration: 1, ProcessGeneration: 1, Now: ingressNow}); e != nil {
			return e
		}
		attempt.State = work.AttemptRunning
		attempt.Revision++
		return tx.PutAttempt(attempt, 1)
	})
	signer, _ := workauth.New([]byte("0123456789abcdef0123456789abcdef"), func() time.Time { return ingressNow })
	token, e := signer.Issue(workauth.Claims{Purpose: "worker", RoleID: "leader", BotGeneration: 1, ScopeID: nav.PersonalScopeID, ContextID: view.Navigation.FrontContextID, WorkerID: "private-worker", SessionGeneration: 1, ControllerGeneration: 1}, time.Hour)
	if e != nil {
		t.Fatal(e)
	}
	req := workservice.PrivateAdoptInstructionRequest{Key: "adopt", SourceMessageID: original.ID, Title: "实现搜索", Summary: "在项目中实现搜索"}
	result, e := service.PrivateAdoptInstruction(t.Context(), token, "private-run", req)
	if e != nil {
		t.Fatal(e)
	}
	replay, e := service.PrivateAdoptInstruction(t.Context(), token, "private-run", req)
	if e != nil || !replay.Replay || result.ObjectID != replay.ObjectID {
		t.Fatalf("adoption replay %+v %v", replay, e)
	}
	txRun(t, s, func(tx *workstore.Tx) error {
		task, e := tx.GetTask(result.ObjectID)
		if e != nil {
			return e
		}
		if task.ScopeID != "scope" {
			t.Fatal("project task remained personal")
		}
		message, e := tx.GetMessage(task.BusinessSource.ID)
		if e != nil {
			return e
		}
		body, e := tx.GetContent("scope", message.BodyRef)
		if e != nil {
			return e
		}
		if string(body.Data) != req.Summary {
			t.Fatalf("unexpected public body %s", body.Data)
		}
		if message.PlatformKey != nil || message.DerivedFrom == nil || message.DerivedFrom.ID == original.ID || message.DerivedFrom.EvidenceRef == original.BodyRef {
			t.Fatal("public provenance exposes private source")
		}
		if _, e = tx.GetContent("scope", original.BodyRef); e == nil {
			t.Fatal("project can retrieve private original")
		}
		return nil
	})
	req.Key = "unadmitted"
	req.SourceMessageID = "non-admitted"
	if _, e = service.PrivateAdoptInstruction(t.Context(), token, "private-run", req); e == nil {
		t.Fatal("unadmitted source adopted")
	}
}
