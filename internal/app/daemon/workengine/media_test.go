package workengine

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestPrivateImagesWaitWithoutProvisioningThenJoinLaterTextAndPreserveTail(t *testing.T) {
	store, _ := fixture(t)
	transaction(t, store, func(tx *workstore.Tx) error {
		if e := tx.PutScope(work.WorkScope{ID: "personal", Kind: work.ScopePersonal, PrincipalID: "human", RoleID: "leader", Revision: 1}, 0); e != nil {
			return e
		}
		return putWorker(tx, "personal", "leader", "", "")
	})
	add := func(id string, await bool, at time.Time) {
		t.Helper()
		transaction(t, store, func(tx *workstore.Tx) error {
			body := work.NewContent(id+":body", "personal", "text/plain", []byte("actual human source"), at)
			if e := tx.PutContent(body); e != nil {
				return e
			}
			_, _, e := tx.AcceptMessage(work.Message{ID: id + ":message", ScopeID: "personal", ConversationID: "private", Source: work.Actor{Kind: work.ActorPrincipal, ID: "human"}, BodyRef: body.ID, Revision: 1}, []work.Inbox{{ID: id, ScopeID: "personal", RecipientRoleID: "leader", RecipientContextID: "context:personal:leader", MessageID: id + ":message", EventID: id, State: work.InboxPending, ContentState: work.InputContentReady, AwaitingText: await, AvailableAt: at, Revision: 1}})
			return e
		})
	}
	add("image-one", true, testNow.Add(-3*time.Second))
	add("image-two", true, testNow.Add(-2*time.Second))
	planner := &testPlanner{}
	dispatcher := &testDispatcher{}
	engine := engine(t, store, 1, planner, dispatcher)
	engine.config.MaxInputsPerAttempt = 1 // Staged attachments belong to the following request.
	candidates, _, needs, e := engine.snapshot(t.Context())
	if e != nil || len(candidates) != 0 || len(needs) != 0 {
		t.Fatalf("standalone images triggered model preparation: candidates=%d needs=%d %v", len(candidates), len(needs), e)
	}
	add("human-instruction", false, testNow.Add(-time.Second))
	add("later-image", true, testNow)
	candidates, _, _, e = engine.snapshot(t.Context())
	if e != nil || len(candidates) != 1 {
		t.Fatalf("image plus text did not become runnable: %d %v", len(candidates), e)
	}
	candidate := candidates[0]
	if len(candidate.Inputs) != 3 || candidate.Inputs[0].ID != "image-one" || candidate.Inputs[1].ID != "image-two" || candidate.Inputs[2].ID != "human-instruction" {
		t.Fatal("image/text order lost or later image borrowed earlier instruction")
	}
	// Even a forged planner candidate cannot claim the image before its text.
	forged := candidate
	forged.Inputs = forged.Inputs[:2]
	plan, e := planner.Plan(t.Context(), forged)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = engine.admit(t.Context(), forged, plan); e == nil {
		t.Fatal("durable claim admitted standalone images")
	}
	transaction(t, store, func(tx *workstore.Tx) error {
		for _, id := range []string{"image-one", "image-two", "human-instruction", "later-image"} {
			input, e := tx.GetInbox(id)
			if e != nil {
				return e
			}
			if input.State != work.InboxPending {
				t.Fatal("failed media claim consumed queue")
			}
		}
		return nil
	})
}

func TestProjectInputsKeepOriginalBatchLimitAndCanClaimWithoutPrivateMediaGate(t *testing.T) {
	store, _ := fixture(t, "project")
	transaction(t, store, func(tx *workstore.Tx) error {
		for _, id := range []string{"a", "b", "c"} {
			if err := tx.PutInbox(work.Inbox{ID: id, ScopeID: "project", RecipientRoleID: "leader", EventID: id, State: work.InboxPending, AwaitingText: true, AvailableAt: testNow, Revision: 1}, 0); err != nil {
				return err
			}
		}
		return nil
	})
	planner := &testPlanner{}
	e := engine(t, store, 1, planner, &testDispatcher{})
	e.config.MaxInputsPerAttempt = 2
	candidates, _, _, err := e.snapshot(t.Context())
	if err != nil || len(candidates) != 1 || len(candidates[0].Inputs) != 2 || candidates[0].Inputs[0].ID != "a" || candidates[0].Inputs[1].ID != "b" {
		t.Fatalf("project batch changed: %+v, %v", candidates, err)
	}
	plan, err := planner.Plan(t.Context(), candidates[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.admit(t.Context(), candidates[0], plan); err != nil {
		t.Fatalf("project claim hit private media gate: %v", err)
	}
}

func TestPrivateQueuedMediaRequestsRunAsSeparateGroups(t *testing.T) {
	store, _ := fixture(t)
	transaction(t, store, func(tx *workstore.Tx) error {
		if err := tx.PutScope(work.WorkScope{ID: "personal", Kind: work.ScopePersonal, PrincipalID: "human", RoleID: "leader", Revision: 1}, 0); err != nil {
			return err
		}
		return putWorker(tx, "personal", "leader", "", "")
	})
	for i, id := range []string{"image-one", "text-one", "image-two", "text-two"} {
		id, i := id, i
		transaction(t, store, func(tx *workstore.Tx) error {
			at := testNow.Add(time.Duration(i-4) * time.Second)
			body := work.NewContent(id+":body", "personal", "text/plain", []byte(id), at)
			if err := tx.PutContent(body); err != nil {
				return err
			}
			_, _, err := tx.AcceptMessage(work.Message{ID: id + ":message", ScopeID: "personal", ConversationID: "private", Source: work.Actor{Kind: work.ActorPrincipal, ID: "human"}, BodyRef: body.ID, Revision: 1}, []work.Inbox{{ID: id, ScopeID: "personal", RecipientRoleID: "leader", RecipientContextID: "context:personal:leader", MessageID: id + ":message", EventID: id, State: work.InboxPending, ContentState: work.InputContentReady, AwaitingText: i%2 == 0, AvailableAt: at, Revision: 1}})
			return err
		})
	}
	planner := &testPlanner{}
	e := engine(t, store, 1, planner, &testDispatcher{})
	candidates, _, _, err := e.snapshot(t.Context())
	if err != nil || len(candidates) != 1 || len(candidates[0].Inputs) != 2 || candidates[0].Inputs[0].ID != "image-one" || candidates[0].Inputs[1].ID != "text-one" {
		t.Fatalf("first group: %+v, %v", candidates, err)
	}
	plan, err := planner.Plan(t.Context(), candidates[0])
	if err != nil {
		t.Fatal(err)
	}
	pending, err := e.admit(t.Context(), candidates[0], plan)
	if err != nil {
		t.Fatal(err)
	}
	finish(t, store, pending.Attempt.ID)
	candidates, _, _, err = e.snapshot(t.Context())
	if err != nil || len(candidates) != 1 || len(candidates[0].Inputs) != 2 || candidates[0].Inputs[0].ID != "image-two" || candidates[0].Inputs[1].ID != "text-two" {
		t.Fatalf("second group: %+v, %v", candidates, err)
	}
}

func TestPrivateStagedMediaSkipsIndependentNoticeUntilHumanInstruction(t *testing.T) {
	store, _ := fixture(t)
	transaction(t, store, func(tx *workstore.Tx) error {
		if err := tx.PutScope(work.WorkScope{ID: "personal", Kind: work.ScopePersonal, PrincipalID: "human", RoleID: "leader", Revision: 1}, 0); err != nil {
			return err
		}
		return putWorker(tx, "personal", "leader", "", "")
	})
	for i, id := range []string{"image", "notice", "instruction"} {
		i, id := i, id
		transaction(t, store, func(tx *workstore.Tx) error {
			at := testNow.Add(time.Duration(i-4) * time.Second)
			body := work.NewContent(id+":body", "personal", "text/plain", []byte(id), at)
			if err := tx.PutContent(body); err != nil {
				return err
			}
			actor := work.Actor{Kind: work.ActorPrincipal, ID: "human"}
			if id == "notice" {
				actor = work.Actor{Kind: work.ActorRole, ID: "leader"}
			}
			input := work.Inbox{ID: id, ScopeID: "personal", RecipientRoleID: "leader", RecipientContextID: "context:personal:leader", MessageID: id + ":message", EventID: id, State: work.InboxPending, ContentState: work.InputContentReady, AwaitingText: id == "image", AvailableAt: at, Revision: 1}
			_, _, err := tx.AcceptMessage(work.Message{ID: input.MessageID, ScopeID: "personal", ConversationID: "private", Source: actor, BodyRef: body.ID, Revision: 1}, []work.Inbox{input})
			return err
		})
	}
	planner := &testPlanner{}
	e := engine(t, store, 1, planner, &testDispatcher{})
	candidates, _, _, err := e.snapshot(t.Context())
	if err != nil || len(candidates) != 1 || len(candidates[0].Inputs) != 2 || candidates[0].Inputs[0].ID != "image" || candidates[0].Inputs[1].ID != "instruction" {
		t.Fatalf("staged image did not reach its human instruction: %+v, %v", candidates, err)
	}
	plan, err := planner.Plan(t.Context(), candidates[0])
	if err != nil {
		t.Fatal(err)
	}
	pending, err := e.admit(t.Context(), candidates[0], plan)
	if err != nil {
		t.Fatal(err)
	}
	finish(t, store, pending.Attempt.ID)
	candidates, _, _, err = e.snapshot(t.Context())
	if err != nil || len(candidates) != 1 || len(candidates[0].Inputs) != 1 || candidates[0].Inputs[0].ID != "notice" {
		t.Fatalf("independent notice did not remain runnable: %+v, %v", candidates, err)
	}
}
