package workprovision

import (
	"context"
	"errors"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestProvisionRunsOnceAndAdoptsActualHumanSource(t *testing.T) {
	f := newFixture(t)
	f.leader.tabs = []feishu.ProjectChatTab{{ID: "human-tab", Name: "项目", Type: "url", URL: "https://human.example"}}
	report, err := f.service.Run(context.Background(), f.operationID)
	if err != nil || report.State != "confirmed" {
		t.Fatalf("run %#v, %v", report, err)
	}
	if f.leader.documentCalls != 1 || f.leader.appendCalls != 1 || f.leader.tabCalls != 1 || f.leader.publishCalls != 1 || f.leader.pinCalls != 1 || f.prepared != 1 {
		t.Fatalf("wrong effects doc=%d index=%d tab=%d nav=%d pin=%d prepare=%d", f.leader.documentCalls, f.leader.appendCalls, f.leader.tabCalls, f.leader.publishCalls, f.leader.pinCalls, f.prepared)
	}
	if _, err := f.service.Run(context.Background(), f.operationID); err != nil {
		t.Fatal(err)
	}
	if f.leader.documentCalls != 1 || f.leader.appendCalls != 1 || f.prepared != 1 {
		t.Fatal("confirmed saga replayed external establishment effects")
	}
	if len(f.leader.tabs) != 2 || f.leader.tabs[0].ID != "human-tab" || f.leader.tabs[0].URL != "https://human.example" {
		t.Fatal("human tab was claimed or replaced")
	}
	if err := f.store.Within(context.Background(), func(tx *workstore.Tx) error {
		home, err := tx.GetProjectHome(f.projectID)
		if err != nil {
			return err
		}
		if home.DocumentID != "document-created" || home.IndexBlockID != "managed-index" || home.PinMessageID != "navigation-message" || home.ResourceID == "" {
			t.Errorf("incomplete stable home %#v", home)
		}
		inputs, err := tx.ListInbox(f.scopeID)
		if err != nil {
			return err
		}
		if len(inputs) != 1 {
			t.Errorf("initial input count=%d", len(inputs))
			return nil
		}
		message, err := tx.GetMessage(inputs[0].MessageID)
		if err != nil {
			return err
		}
		body, err := tx.GetContent(f.scopeID, message.BodyRef)
		if err != nil {
			return err
		}
		if string(body.Data) != "Human original request: preserve these exact details." || message.Source != (work.Actor{Kind: work.ActorPrincipal, ID: "human"}) || message.PlatformKey != nil || message.DerivedFrom == nil || message.DerivedFrom.ID != f.sourceID {
			t.Errorf("initial requirement lost provenance: %+v body=%s", message, body.Data)
		}
		versions, err := tx.ListResourceVersions(f.scopeID, home.ResourceID)
		if err != nil {
			return err
		}
		if len(versions) == 0 || !versions[0].Saved {
			t.Error("home creation did not save actual source/version evidence")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	expertVerified := false
	for _, app := range f.world.selfChecks {
		if app == "app-expert" {
			expertVerified = true
		}
	}
	if !expertVerified {
		t.Error("expert bot was declared ready without its own credential query")
	}
}

func TestUnknownDocumentCreationSurvivesRestartWithoutRecreate(t *testing.T) {
	f := newFixture(t)
	f.leader.createErr = context.DeadlineExceeded
	report, err := f.service.Run(context.Background(), f.operationID)
	if err != nil || report.State != "unknown" {
		t.Fatalf("uncertain run %#v %v", report, err)
	}
	if f.prepared != 1 {
		t.Error("uncertain material creation blocked independent execution preparation")
	}
	if err := f.store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := workstore.Open(context.Background(), f.path)
	if err != nil {
		t.Fatal(err)
	}
	f.store = reopened
	f.service, err = New(reopened, f.options)
	if err != nil {
		t.Fatal(err)
	}
	report, err = f.service.Run(context.Background(), f.operationID)
	if err != nil || report.State != "unknown" {
		t.Fatalf("restart %#v %v", report, err)
	}
	if f.leader.documentCalls != 1 || f.leader.folderCalls != 1 || f.prepared != 1 {
		t.Fatalf("restart replayed unknown create doc=%d folder=%d preparation=%d", f.leader.documentCalls, f.leader.folderCalls, f.prepared)
	}
	if err := f.store.Within(context.Background(), func(tx *workstore.Tx) error {
		inputs, err := tx.ListInbox(f.scopeID)
		if len(inputs) != 1 {
			t.Errorf("restart duplicated initial input: %d", len(inputs))
		}
		step, e := tx.GetProvisionStep(f.operationID, "materials.document")
		if e != nil {
			return e
		}
		if step.State != work.OperationUnknown || step.ExternalID != "" {
			t.Errorf("lost unknown document evidence %#v", step)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestKnownDocumentIDSurvivesLaterBlockFailure(t *testing.T) {
	f := newFixture(t)
	f.leader.appendErr = context.DeadlineExceeded
	if _, err := f.service.Run(context.Background(), f.operationID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Run(context.Background(), f.operationID); err != nil {
		t.Fatal(err)
	}
	if f.leader.documentCalls != 1 || f.leader.appendCalls != 1 {
		t.Fatalf("lost stable object after later failure doc=%d append=%d", f.leader.documentCalls, f.leader.appendCalls)
	}
	if err := f.store.Within(context.Background(), func(tx *workstore.Tx) error {
		home, err := tx.GetProjectHome(f.projectID)
		if home.DocumentID != "document-created" {
			t.Error("returned document ID was discarded")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPartialMemberFailureKeepsReadyLeaderUsable(t *testing.T) {
	f := newFixture(t)
	f.leader.inviteErr = errors.New("expert application unavailable")
	if _, err := f.service.Run(context.Background(), f.operationID); err != nil {
		t.Fatal(err)
	}
	if err := f.store.Within(context.Background(), func(tx *workstore.Tx) error {
		leader, err := tx.GetProjectMember(f.projectID, "leader")
		if err != nil {
			return err
		}
		expert, err := tx.GetProjectMember(f.projectID, "expert")
		if err != nil {
			return err
		}
		project, err := tx.GetProject(f.projectID)
		if leader.State != work.ProjectMemberReady || expert.State == work.ProjectMemberReady || project.State != work.ProjectDegraded {
			t.Errorf("partial readiness leader=%s expert=%s project=%s", leader.State, expert.State, project.State)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if f.prepared != 1 {
		t.Error("member failure stopped independent preparation")
	}
	f.world.present["app-expert"] = true
	if err := f.service.RefreshMembers(context.Background(), f.projectID); err != nil {
		t.Fatal(err)
	}
	if err := f.store.Within(context.Background(), func(tx *workstore.Tx) error {
		member, err := tx.GetProjectMember(f.projectID, "expert")
		if member.State != work.ProjectMemberReady {
			t.Error("own credential reconciliation did not mark joined bot ready")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
