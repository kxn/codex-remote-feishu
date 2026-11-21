package workprovision

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestLateCreationReceiptDoesNotChangeNewChannelSelection(t *testing.T) {
	f := newFixture(t)
	f.leader.afterCreate = func() {
		if err := f.store.Within(context.Background(), func(tx *workstore.Tx) error {
			if err := tx.PutScope(work.WorkScope{ID: "new-scope", Kind: work.ScopeProject, TeamID: "team", ProjectID: "new-project", Revision: 1}, 0); err != nil {
				return err
			}
			if err := tx.PutProject(work.Project{ID: "new-project", ScopeID: "new-scope", TeamID: "team", LeaderRoleID: "leader", Name: "New selection", State: work.ProjectActive, Revision: 1}, 0); err != nil {
				return err
			}
			return tx.PutProjectChannelBinding(work.ProjectChannelBinding{Tenant: "tenant", ChatID: "project-chat", ProjectID: "new-project", Purpose: "main", BindingGeneration: 2, Active: true}, 1)
		}); err != nil {
			t.Fatal(err)
		}
	}
	_, err := f.service.Run(context.Background(), f.operationID)
	if !errors.Is(err, ErrStale) {
		t.Fatalf("late run did not fence new binding: %v", err)
	}
	if err := f.store.Within(context.Background(), func(tx *workstore.Tx) error {
		current, err := tx.GetProjectChannelBinding("tenant", "project-chat")
		if err != nil {
			return err
		}
		if current.ProjectID != "new-project" || current.BindingGeneration != 2 {
			t.Error("late effect changed the new project binding")
		}
		step, err := tx.GetProvisionStep(f.operationID, "materials.document")
		if err != nil {
			return err
		}
		if step.ExternalID != "document-created" {
			t.Error("late object receipt was lost instead of remaining on the old operation")
		}
		if _, err := tx.GetProjectHome("new-project"); !errors.Is(err, workstore.ErrNotFound) {
			t.Errorf("old operation wrote new project's home: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if f.leader.publishCalls != 0 {
		t.Error("late old provisioning published into the new channel generation")
	}
}

func TestHomeRefreshKeepsStableDocumentAndDoesNotOverwriteHumanEdits(t *testing.T) {
	f := newFixture(t)
	if _, err := f.service.Run(context.Background(), f.operationID); err != nil {
		t.Fatal(err)
	}
	if err := f.store.Within(context.Background(), func(tx *workstore.Tx) error {
		return tx.PutTask(work.Task{ID: "new-task", ScopeID: f.scopeID, Title: "Task truth", OwnerRoleID: "leader", RequirementsRevision: 1, AssignmentGeneration: 1, State: work.TaskTodo, Revision: 1}, 0)
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.service.RefreshHome(context.Background(), f.projectID); err != nil {
		t.Fatal(err)
	}
	before := f.leader.updateCalls
	f.leader.blocks.Index.Text = "Human edit: everything looks completed"
	f.leader.blocks.Index.Fingerprint = fingerprint(f.leader.blocks.Index.ID, f.leader.blocks.Index.Text)
	if err := f.service.RefreshHome(context.Background(), f.projectID); err == nil {
		t.Error("manual edit was not surfaced as a conflict")
	}
	if f.leader.updateCalls != before || f.leader.blocks.Index.Text != "Human edit: everything looks completed" || f.leader.documentCalls != 1 || f.leader.appendCalls != 1 {
		t.Fatal("manual content overwritten or home recreated")
	}
	if err := f.store.Within(context.Background(), func(tx *workstore.Tx) error {
		home, err := tx.GetProjectHome(f.projectID)
		if err != nil {
			return err
		}
		if home.SyncState != "conflict" || home.DocumentID != "document-created" {
			t.Errorf("home conflict/stable ID lost %#v", home)
		}
		task, err := tx.GetTask("new-task")
		if task.State != work.TaskTodo {
			t.Error("editing the projection changed task truth")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestInputAdoptionCrashWindowIsIdempotent(t *testing.T) {
	f := newFixture(t)
	if err := f.store.Within(context.Background(), func(tx *workstore.Tx) error {
		intent, _ := json.Marshal(map[string]string{"source_message_id": f.sourceID})
		step := workstore.ProvisionStep{ID: workstore.ProvisionStepID(f.operationID, "initial_input.adopt"), ScopeID: f.scopeID, OperationID: f.operationID, StepID: "initial_input.adopt", Identity: f.leader.binding, State: work.OperationPrepared, Intent: intent, Revision: 1, UpdatedAt: f.now}
		if err := tx.PutProvisionStep(step, 0); err != nil {
			return err
		}
		step.Revision++
		step.State = work.OperationSending
		if err := tx.PutProvisionStep(step, 1); err != nil {
			return err
		}
		_, _, err := tx.AdoptBootstrapInput(workstore.AdoptBootstrapInputRequest{OperationID: f.operationID, SourceMessageID: f.sourceID})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	report, err := f.service.Run(context.Background(), f.operationID)
	if err != nil || report.State != "confirmed" {
		t.Fatalf("adoption resume %#v %v", report, err)
	}
	if err := f.store.Within(context.Background(), func(tx *workstore.Tx) error {
		inputs, err := tx.ListInbox(f.scopeID)
		if len(inputs) != 1 {
			t.Errorf("exactly one adopted first input expected, got %d", len(inputs))
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestUnknownTabRequiresExplicitHumanIDRecoveryAndDoesNotRecreate(t *testing.T) {
	f := newFixture(t)
	f.leader.tabErr = context.DeadlineExceeded
	first, err := f.service.Run(context.Background(), f.operationID)
	if err != nil || first.State != "unknown" {
		t.Fatalf("unknown tab %#v %v", first, err)
	}
	if _, err := f.service.Run(context.Background(), f.operationID); err != nil {
		t.Fatal(err)
	}
	if f.leader.tabCalls != 1 || f.leader.documentCalls != 1 {
		t.Fatal("unknown Tab caused blind retry or a whole-project rebuild")
	}
	var revision int64
	if err := f.store.Within(context.Background(), func(tx *workstore.Tx) error {
		step, err := tx.GetProvisionStep(f.operationID, "navigation.tab")
		if err != nil {
			return err
		}
		revision = step.Revision
		content := work.NewContent("recovery-human-body", f.scopeID, "text/plain", []byte("I verified this exact tab ID. Adopt it for this project."), f.now.Add(time.Minute))
		if err := tx.PutContent(content); err != nil {
			return err
		}
		_, _, err = tx.AcceptMessage(work.Message{ID: "new-human-recovery", ScopeID: f.scopeID, ConversationID: "project-chat", Source: work.Actor{Kind: work.ActorPrincipal, ID: "human"}, PlatformKey: &work.MessageKey{Tenant: "tenant", ChatID: "project-chat", MessageID: "recovery-platform-event"}, BodyRef: content.ID, Revision: 1, CreatedAt: f.now.Add(time.Minute)}, nil)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	candidate := Recovery{OperationID: f.operationID, StepID: "navigation.tab", ExternalID: "system-project-tab", ExpectedRevision: revision}
	if _, err := f.service.ResolveUnknown(context.Background(), candidate); err == nil {
		t.Error("candidate ID alone authorized claiming an unknown Tab")
	}
	candidate.SourceMessageID = "new-human-recovery"
	result, err := f.service.ResolveUnknown(context.Background(), candidate)
	if err != nil || result.State != "confirmed" {
		t.Fatalf("recovery %#v %v", result, err)
	}
	if f.leader.tabCalls != 1 || f.leader.documentCalls != 1 || f.leader.publishCalls != 1 {
		t.Fatal("recovery repeated an already accepted object creation")
	}
}
