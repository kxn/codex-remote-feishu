package workstore

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestPersonalMigrationFenceSurvivesRestartAndAllowsSettlement(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "team.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err = s.Within(ctx, func(tx *Tx) error {
		if e := tx.PutRole(work.Role{ID: "role", Name: "Role", Description: "work", DescriptionVersion: 1, SettingsRevision: 1, Revision: 1}, 0); e != nil {
			return e
		}
		for _, id := range []string{"alice", "bob"} {
			if e := tx.PutPrincipal(work.Principal{ID: id, DisplayName: id, Revision: 1}, 0); e != nil {
				return e
			}
			if e := tx.PutScope(work.WorkScope{ID: id, Kind: work.ScopePersonal, PrincipalID: id, RoleID: "role", Revision: 1}, 0); e != nil {
				return e
			}
		}
		if e := tx.PutTeam(work.Team{ID: "team", Name: "Team", LeaderRoleID: "role", Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutScope(work.WorkScope{ID: "project", Kind: work.ScopeProject, ProjectID: "project-id", TeamID: "team", Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutProject(work.Project{ID: "project-id", ScopeID: "project", TeamID: "team", Name: "Project", LeaderRoleID: "role", State: work.ProjectActive, Revision: 1}, 0); e != nil {
			return e
		}
		if _, e := tx.BeginPersonalMigrationFence("project", "bad", now); !errors.Is(e, ErrScopeDenied) {
			t.Fatal("group scope accepted migration fence")
		}
		body := work.NewContent("body", "alice", "text/plain", []byte("payload"), now)
		if e := tx.PutContent(body); e != nil {
			return e
		}
		for _, id := range []string{"pending", "inflight"} {
			op := work.Operation{ID: id, ScopeID: "alice", Actor: work.Actor{Kind: work.ActorSystem, ID: "output"}, Kind: "feishu.publish", TargetID: "chat", TargetGeneration: 1, IdempotencyKey: id, RequestHash: body.SHA256, RequestRef: body.ID, State: work.OperationPrepared, Revision: 1}
			if _, _, e := tx.PutOperation(op, 0); e != nil {
				return e
			}
			state := work.OutboxPending
			if id == "inflight" {
				state = work.OutboxSending
			}
			if e := tx.PutOutbox(work.Outbox{ID: id, ScopeID: "alice", OperationID: id, Target: "chat", EventID: id, PayloadRef: body.ID, State: state, Revision: 1}, 0); e != nil {
				return e
			}
		}
		_, e := tx.BeginPersonalMigrationFence("alice", "migration", now)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.Within(ctx, func(tx *Tx) error {
		f, e := tx.GetPersonalMigrationFence("alice")
		if e != nil {
			return e
		}
		if f.State != "prepared" {
			t.Fatal("lost fence")
		}
		if _, e = tx.StartPersonalMigrationDrain("alice", "migration", f.Revision, now); e != nil {
			return e
		}
		status, e := tx.PersonalMigrationStatus("alice")
		if e != nil {
			return e
		}
		if status.UnsettledOutbox != 2 || status.NativeTransportProofAvailable {
			t.Fatal("invalid drain evidence")
		}
		if e = tx.PreservePersonalMigrationUnsent("alice", "operation", "pending", 1, now); e != nil {
			return e
		}
		if e = tx.PreservePersonalMigrationUnsent("alice", "outbox", "pending", 1, now); e != nil {
			return e
		}
		if e = tx.PreservePersonalMigrationUnsent("alice", "operation", "inflight", 1, now); !errors.Is(e, ErrConflict) {
			t.Fatal("sending outbox marked unsent")
		}
		status, e = tx.PersonalMigrationStatus("alice")
		if e != nil {
			return e
		}
		if status.UnsettledOutbox != 1 {
			t.Fatal("preserved pending outbox still unsettled")
		}
		if !errors.Is(tx.AssertPersonalMigrationOpen("alice"), ErrPersonalMigrationFenced) {
			t.Fatal("owner not fenced")
		}
		if e = tx.AssertPersonalMigrationOpen("bob"); e != nil {
			return e
		}
		if e = tx.AssertPersonalMigrationOpen("project"); e != nil {
			return e
		}
		if !errors.Is(tx.ClaimAttempt(ClaimRequest{Attempt: work.ExecutionAttempt{ScopeID: "alice"}}), ErrPersonalMigrationFenced) {
			t.Fatal("claim admitted")
		}
		launch := work.Operation{ID: "launch", ScopeID: "alice", Actor: work.Actor{Kind: work.ActorSystem, ID: "host"}, Kind: "worker.launch", TargetID: "worker", TargetGeneration: 1, IdempotencyKey: "launch", RequestHash: "hash", State: work.OperationPrepared, Revision: 1}
		if _, _, e = tx.PutOperation(launch, 0); !errors.Is(e, ErrPersonalMigrationFenced) {
			t.Fatal("launch admitted")
		}
		out, e := tx.GetOutbox("pending")
		if e != nil {
			return e
		}
		out.State = work.OutboxSending
		out.Revision++
		if !errors.Is(tx.PutOutbox(out, 1), ErrPersonalMigrationFenced) {
			t.Fatal("publication admitted")
		}
		out, e = tx.GetOutbox("inflight")
		if e != nil {
			return e
		}
		out.State = work.OutboxSent
		out.ExternalMessageID = "external"
		out.Revision++
		if e = tx.PutOutbox(out, 1); e != nil {
			return e
		}
		event := agentproto.Event{Seq: 1, Kind: agentproto.EventTurnStarted, ThreadID: "thread", TurnID: "turn"}
		hash, e := agentproto.RuntimeEventPayloadHash(event)
		if e != nil {
			return e
		}
		event.Journal = &agentproto.RuntimeEventIdentity{Version: 1, CarrierIncarnation: "carrier", Sequence: 1, PayloadHash: hash}
		_, _, e = tx.AcceptRuntimeEvent("instance", event, now, nil)
		return e
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPersonalMigrationFenceAdmissionConcurrent(t *testing.T) {
	ctx := t.Context()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "team.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().UTC()
	if err = s.Within(ctx, func(tx *Tx) error {
		if e := tx.PutRole(work.Role{ID: "r", Name: "R", Description: "work", DescriptionVersion: 1, SettingsRevision: 1, Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutPrincipal(work.Principal{ID: "p", DisplayName: "P", Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutScope(work.WorkScope{ID: "s", Kind: work.ScopePersonal, PrincipalID: "p", RoleID: "r", Revision: 1}, 0); e != nil {
			return e
		}
		return tx.PutContent(work.NewContent("body", "s", "text/plain", []byte("text"), now))
	}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(2)
	results := make(chan error, 2)
	go func() {
		defer wg.Done()
		results <- s.Within(ctx, func(tx *Tx) error { _, e := tx.BeginPersonalMigrationFence("s", "migration", now); return e })
	}()
	go func() {
		defer wg.Done()
		results <- s.Within(ctx, func(tx *Tx) error {
			_, _, e := tx.AcceptMessage(work.Message{ID: "m", ScopeID: "s", ConversationID: "chat", Source: work.Actor{Kind: work.ActorPrincipal, ID: "p"}, BodyRef: "body", Revision: 1, CreatedAt: now}, nil)
			return e
		})
	}()
	wg.Wait()
	close(results)
	for e := range results {
		if e != nil && !errors.Is(e, ErrPersonalMigrationFenced) {
			t.Fatal(e)
		}
	}
	if err = s.Within(ctx, func(tx *Tx) error {
		if !errors.Is(tx.AssertPersonalMigrationOpen("s"), ErrPersonalMigrationFenced) {
			t.Fatal("fence missing")
		}
		_, _, e := tx.AcceptMessage(work.Message{ID: "later", ScopeID: "s", ConversationID: "chat", Source: work.Actor{Kind: work.ActorPrincipal, ID: "p"}, BodyRef: "body", Revision: 1, CreatedAt: now}, nil)
		if !errors.Is(e, ErrPersonalMigrationFenced) {
			t.Fatalf("post-fence message accepted: %v", e)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
