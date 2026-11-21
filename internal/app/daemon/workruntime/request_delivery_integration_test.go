package workruntime

import (
	"context"
	"errors"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workrequests"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type requestTestDriver struct {
	f     *fixture
	calls int
	fail  bool
}

func (d *requestTestDriver) SendCommand(ctx context.Context, instance string, command agentproto.Command) error {
	d.calls++
	d.f.tx(func(tx *workstore.Tx) error {
		op, err := tx.GetOperation(command.CommandID)
		if err != nil {
			return err
		}
		if op.RequestRef == "" || op.State != work.OperationSending || command.Request.RequestID != "approval-1" || command.Execution == nil || *command.Execution != *d.f.command.Execution || instance != d.f.carrier.OriginInstanceID {
			d.f.t.Fatal("response not durably bound before send")
		}
		return nil
	})
	if d.fail {
		return errors.New("connection lost after write")
	}
	return nil
}

func requestFixture(t *testing.T) (*fixture, workstore.NativeRequest, *requestTestDriver, *workrequests.Service) {
	f := newFixture(t, work.BackendClaude)
	f.coordinator.options.ApplyNativeRequest = workrequests.Apply
	f.start()
	if err := f.handle(agentproto.Event{Kind: agentproto.EventRequestStarted, ThreadID: "session", TurnID: "turn-1", RequestID: "approval-1", RequestPrompt: &agentproto.RequestPrompt{Type: agentproto.RequestTypeApproval, Title: "Run tests?"}}); err != nil {
		t.Fatal(err)
	}
	var request workstore.NativeRequest
	f.tx(func(tx *workstore.Tx) error {
		list, e := tx.ListNativeRequests("scope")
		if e != nil {
			return e
		}
		if len(list) != 1 {
			t.Fatalf("requests=%d", len(list))
		}
		request = list[0]
		return tx.PutContent(work.NewContent("human-action", "scope", "application/json", []byte(`{"verified_human":"principal"}`), f.now))
	})
	driver := &requestTestDriver{f: f}
	service, err := workrequests.New(f.store, driver, func(_ context.Context, r workstore.NativeRequest, d workrequests.Decision) error {
		if d.PrincipalID != "principal" || d.EvidenceRef != "human-action" {
			return errors.New("not verified human")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return f, request, driver, service
}
func requestDecision(r workstore.NativeRequest) workrequests.Decision {
	return workrequests.Decision{RequestID: r.ID, ExpectedRevision: r.Revision, PrincipalID: "principal", EvidenceRef: "human-action", Response: agentproto.Request{Response: map[string]any{"decision": "accept"}}}
}

func TestTeamNativeRequestUnknownResponseIsNotResentAndOnlyNativeResolutionCompletes(t *testing.T) {
	f, request, driver, service := requestFixture(t)
	driver.fail = true
	decision := requestDecision(request)
	opID, err := service.Respond(context.Background(), decision)
	if err == nil {
		t.Fatal("expected unknown send")
	}
	if _, err = service.Respond(context.Background(), decision); err != nil {
		t.Fatal(err)
	}
	if driver.calls != 1 {
		t.Fatal("unknown response was resent")
	}
	f.tx(func(tx *workstore.Tx) error {
		op, e := tx.GetOperation(opID)
		if e != nil {
			return e
		}
		if op.State != work.OperationUnknown {
			t.Fatal(op.State)
		}
		r, e := tx.GetNativeRequest(request.ID)
		if e != nil {
			return e
		}
		if r.State != "response_pending" {
			t.Fatal(r.State)
		}
		return nil
	})
	if err = f.handle(agentproto.Event{Kind: agentproto.EventRequestResolved, ThreadID: "session", TurnID: "turn-1", RequestID: "approval-1"}); err != nil {
		t.Fatal(err)
	}
	f.tx(func(tx *workstore.Tx) error {
		op, e := tx.GetOperation(opID)
		if e != nil {
			return e
		}
		if op.State != work.OperationConfirmed {
			t.Fatal(op.State)
		}
		r, e := tx.GetNativeRequest(request.ID)
		if e != nil {
			return e
		}
		if r.State != "resolved" || r.ResolvedEvidenceRef == "" {
			t.Fatal(r)
		}
		return nil
	})
}

func TestTeamNativeRequestChangedPromptOrIdentityCannotUseOldDecision(t *testing.T) {
	t.Run("changed prompt", func(t *testing.T) {
		f, request, driver, service := requestFixture(t)
		if err := f.handle(agentproto.Event{Kind: agentproto.EventRequestStarted, ThreadID: "session", TurnID: "turn-1", RequestID: "approval-1", RequestPrompt: &agentproto.RequestPrompt{Type: agentproto.RequestTypeApproval, Title: "Delete files?"}}); err != nil {
			t.Fatal(err)
		}
		if _, err := service.Respond(context.Background(), requestDecision(request)); err == nil {
			t.Fatal("old card accepted changed prompt")
		}
		if driver.calls != 0 {
			t.Fatal("stale response sent")
		}
	})
	t.Run("wrong human", func(t *testing.T) {
		_, request, driver, service := requestFixture(t)
		decision := requestDecision(request)
		decision.PrincipalID = "someone-else"
		if _, err := service.Respond(context.Background(), decision); err == nil {
			t.Fatal("unverified principal accepted")
		}
		if driver.calls != 0 {
			t.Fatal("unauthorized response sent")
		}
	})
}
