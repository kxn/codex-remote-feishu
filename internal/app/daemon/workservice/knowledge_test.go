package workservice

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func knowledgeService(t *testing.T) (*Service, string, string) {
	t.Helper()
	s, _, _ := runningService(t)
	err := s.store.Within(context.Background(), func(tx *workstore.Tx) error {
		if err := tx.PutContext(work.WorkContext{ID: "sharing-context", ScopeID: "scope", RoleID: "leader", Workstream: "sharing", Revision: 1}, 0); err != nil {
			return err
		}
		worker := work.WorkerSession{ID: "sharing-worker", ContextID: "sharing-context", ScopeID: "scope", RoleID: "leader", Backend: work.BackendCodex, State: work.WorkerReady, Generation: 1, Controller: work.ControllerAutomatic, ControllerGeneration: 1, InstanceID: "sharing-carrier", InstanceGeneration: 1, ProcessGeneration: 1, Revision: 1}
		if err := tx.PutWorker(worker, 0); err != nil {
			return err
		}
		if err := tx.PutContent(work.NewContent("human-content", "scope", "text/plain", []byte("Share this experience and delegate this decision to the leader."), s.now())); err != nil {
			return err
		}
		message := work.Message{ID: "human-input", ScopeID: "scope", ConversationID: "conversation", Source: work.Actor{Kind: work.ActorPrincipal, ID: "human"}, PlatformKey: &work.MessageKey{Tenant: "tenant", ChatID: "personal-chat", MessageID: "platform-message"}, BodyRef: "human-content", Revision: 1}
		inbox := work.Inbox{ID: "human-inbox", ScopeID: "scope", RecipientRoleID: "leader", RecipientContextID: "sharing-context", EventID: "human", State: work.InboxPending, Revision: 1}
		if _, _, err := tx.AcceptMessage(message, []work.Inbox{inbox}); err != nil {
			return err
		}
		attempt := work.ExecutionAttempt{ID: "sharing-attempt", ContextID: "sharing-context", ScopeID: "scope", RunRef: work.RunRef{WorkerID: worker.ID, Generation: 1, RunID: "sharing-run"}, State: work.AttemptDispatchPending, CommandID: "sharing-start", InputIDs: []string{"human-inbox"}, Revision: 1}
		op := work.Operation{ID: attempt.CommandID, ScopeID: "scope", ContextID: worker.ContextID, Actor: work.Actor{Kind: work.ActorSystem, ID: "scheduler"}, Kind: "native.start", TargetID: worker.ID, TargetGeneration: 1, IdempotencyKey: "sharing-start", RequestHash: "sharing-start", State: work.OperationPrepared, Revision: 1}
		if err := tx.ClaimAttempt(workstore.ClaimRequest{Attempt: attempt, ExpectedWorkerRevision: 1, ControllerGeneration: 1, InstanceID: worker.InstanceID, InstanceGeneration: 1, ProcessGeneration: 1, StartOperation: op, Now: s.now()}); err != nil {
			return err
		}
		attempt.State = work.AttemptRunning
		attempt.Revision++
		return tx.PutAttempt(attempt, 1)
	})
	if err != nil {
		t.Fatal(err)
	}
	token, err := s.signer.Issue(workauth.Claims{Purpose: "worker", RoleID: "leader", BotGeneration: 1, ScopeID: "scope", ContextID: "sharing-context", WorkerID: "sharing-worker", SessionGeneration: 1, ControllerGeneration: 1}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return s, token, "sharing-run"
}
func knowledgeSource() work.SourceRef {
	return work.SourceRef{Kind: "message", ID: "human-input", Version: "1", EvidenceRef: "human-content"}
}

func TestMemoryDefaultsPrivateAndSharingNeedsCurrentHumanInput(t *testing.T) {
	s, token, run := knowledgeService(t)
	ctx := context.Background()
	private, err := s.MemoryWrite(ctx, token, run, MemoryWriteRequest{Key: "private", Content: "a中文z", Applicability: "personal", Sources: []work.SourceRef{knowledgeSource()}})
	if err != nil || private.Owner.Kind != work.MemoryPersonal {
		t.Fatalf("private write %#v, %v", private, err)
	}
	view, err := s.MemoryRead(ctx, token, run, KnowledgeObjectRequest{ID: private.ObjectID, Offset: 1, Limit: 2})
	if err != nil || view.Content.Text != "中文" || view.Content.NextOffset == nil {
		t.Fatalf("paged memory %#v, %v", view, err)
	}
	if err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		if err := tx.PutPrincipal(work.Principal{ID: "other-human", DisplayName: "Other", Revision: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutScope(work.WorkScope{ID: "other-private", Kind: work.ScopePersonal, RoleID: "leader", PrincipalID: "other-human", Revision: 1}, 0); err != nil {
			return err
		}
		if _, _, err := tx.ReadMemory(work.ReadScope{ScopeID: "other-private", RoleID: "leader"}, private.ObjectID); !errors.Is(err, workstore.ErrScopeDenied) {
			t.Errorf("private memory leaked: %v", err)
		}
		_, _, err := tx.AcceptMessage(work.Message{ID: "old-human", ScopeID: "scope", ConversationID: "conversation", Source: work.Actor{Kind: work.ActorPrincipal, ID: "human"}, BodyRef: "human-content", Revision: 1}, nil)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	sharedRequest := MemoryWriteRequest{Key: "shared", Kind: work.MemoryRole, Content: "explicit shared experience", Applicability: "role-wide", Sources: []work.SourceRef{knowledgeSource()}, SharingSourceMessageID: "old-human"}
	if _, err := s.MemoryWrite(ctx, token, run, sharedRequest); !errors.Is(err, workauth.ErrUnauthorized) {
		t.Fatalf("old unadmitted message authorized sharing: %v", err)
	}
	sharedRequest.SharingSourceMessageID = "human-input"
	shared, err := s.MemoryWrite(ctx, token, run, sharedRequest)
	if err != nil || shared.Owner.Kind != work.MemoryRole || shared.SharingGrantID == "" {
		t.Fatalf("shared write %#v, %v", shared, err)
	}
	if err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		_, body, err := tx.ReadMemory(work.ReadScope{ScopeID: "other-private", RoleID: "leader"}, shared.ObjectID)
		if err == nil && string(body.Data) != "explicit shared experience" {
			t.Error("wrong shared body")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	replay, err := s.MemoryWrite(ctx, token, run, sharedRequest)
	if err != nil || !replay.Replay || replay.ObjectID != shared.ObjectID || replay.SharingGrantID != shared.SharingGrantID {
		t.Fatalf("sharing replay %#v, %v", replay, err)
	}
	page, err := s.MemoryList(ctx, token, run, KnowledgeListRequest{Limit: 1})
	if err != nil || len(page.Items) != 1 || page.NextCursor == "" {
		t.Fatalf("memory discovery %#v, %v", page, err)
	}
	if _, err := s.MemoryInvalidate(ctx, token, run, MemoryInvalidateRequest{Key: "invalidate-shared", ID: shared.ObjectID, ExpectedRevision: 1, Source: knowledgeSource()}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MemoryRead(ctx, token, run, KnowledgeObjectRequest{ID: shared.ObjectID}); !errors.Is(err, workstore.ErrNotFound) {
		t.Fatalf("invalidated shared memory visible: %v", err)
	}
}

func TestPracticePublicationAndAdoptionFreezeVersionWithoutForcingWorkflow(t *testing.T) {
	s, token, run := knowledgeService(t)
	ctx := context.Background()
	req := PracticePublishRequest{Key: "method", Kind: work.KnowledgeGlobal, ID: "review", Version: "1", Purpose: "Review", Applicability: "changes", Exclusions: "no changes", Escalation: "ask owner", Content: "# Review\nInspect evidence.", SharingSourceMessageID: "human-input"}
	published, err := s.PracticePublish(ctx, token, run, req)
	if err != nil || published.SharingGrantID == "" {
		t.Fatalf("publish %#v, %v", published, err)
	}
	discovered, err := s.PracticeDiscover(ctx, token, run, KnowledgeListRequest{})
	if err != nil || len(discovered.Items) != 1 {
		t.Fatalf("discover %#v, %v", discovered, err)
	}
	if err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		adoptions, err := tx.ListPracticeAdoptions("scope")
		if len(adoptions) != 0 {
			t.Error("discovery automatically imposed a workflow")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	view, err := s.PracticeRead(ctx, token, run, PracticeReadRequest{Owner: published.Owner, ID: "review", Version: "1", Limit: 8})
	if err != nil || view.Content.NextOffset == nil {
		t.Fatalf("read %#v, %v", view, err)
	}
	adoption, err := s.PracticeAdopt(ctx, token, run, PracticeAdoptRequest{Key: "adopt", Owner: published.Owner, ID: "review", Version: "1", Exceptions: []string{"small changes use focused checks"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		stored, err := tx.GetPracticeAdoption("scope", adoption.ObjectID)
		if stored.AdoptedBy != (work.Actor{Kind: work.ActorRole, ID: "leader"}) || stored.Practice.Version != "1" {
			t.Error("adoption actor/version not frozen")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
