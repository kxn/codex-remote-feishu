package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"github.com/kxn/codex-remote-feishu/internal/core/workcmd"
)

func TestCompletedTaskPublishesItsSavedDeliveryAndReceivesOutboxReceipt(t *testing.T) {
	f := newResourceRecoveryFixture(t)
	r := f.a.teamRuntime
	ctx := t.Context()
	task := work.Task{ID: "finishing-task", ScopeID: "scope", Title: "Report", OwnerRoleID: "leader", State: work.TaskActive, RequirementsRevision: 1, AssignmentGeneration: 1, Revision: 1}
	pickerTx(t, f.a, func(tx *workstore.Tx) error { return tx.PutTask(task, 0) })
	dir := t.TempDir()
	attempt := startDeliveryJourneyRun(t, r.store, dir, "task-delivery", "", task)
	run := attempt.RunRef.RunID
	token, err := f.signer.Issue(workauth.Claims{Purpose: "worker", RoleID: "leader", BotGeneration: 1, ScopeID: task.ScopeID,
		ContextID: attempt.ContextID, WorkerID: attempt.RunRef.WorkerID, SessionGeneration: 1, ControllerGeneration: 1}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	r.service.WithArtifactHost("local-delivery-host")
	if err = os.WriteFile(filepath.Join(dir, "report.txt"), []byte("Complete, verified report."), 0600); err != nil {
		t.Fatal(err)
	}
	save := workservice.SaveResourceFileRequest{Key: "save", Path: "report.txt", CollectionID: "deliverables", Purpose: "Report"}
	artifact, err := r.service.ResourceSaveFile(ctx, token, run, save)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.service.TaskDone(ctx, token, run, workservice.TaskTransitionRequest{Key: "done", TaskID: task.ID, ExpectedRevision: task.Revision}); err != nil {
		t.Fatal(err)
	}
	save.Key = "late-write"
	if _, err = r.service.ResourceSaveFile(ctx, token, run, save); !errors.Is(err, workauth.ErrUnauthorized) {
		t.Fatalf("completed run could write a new resource: %v", err)
	}
	request := workservice.PublishDeliveryRequest{Key: "delivery", Purpose: "Report", Summary: "Verified",
		References: []work.DeliveryRef{{ResourceID: artifact.ResourceID, VersionID: artifact.VersionID}},
		Audience:   []work.Actor{{Kind: work.ActorRole, ID: "leader"}}, Retention: work.RetentionPermanent}
	publish := func() (workservice.Result, error) {
		body, _ := json.Marshal(request)
		result, err := r.service.Execute(ctx, token, workcmd.DispatchRequest{Name: "delivery.publish", RunRef: run, Payload: body})
		if err != nil {
			return workservice.Result{}, err
		}
		return result.(workservice.Result), nil
	}
	if _, err = publish(); !errors.Is(err, workauth.ErrUnauthorized) {
		t.Fatalf("missing post-completion task identity: %v", err)
	}
	request.TaskID = "another-task"
	if _, err = publish(); !errors.Is(err, workauth.ErrUnauthorized) {
		t.Fatalf("cross-task post-completion publication: %v", err)
	}
	request.TaskID = task.ID
	published, err := publish()
	if err != nil || published.Status != "accepted" {
		t.Fatalf("publish exact already-saved result: %+v %v", published, err)
	}
	if count, err := r.publisher.RunOnce(ctx, 32); err != nil || count != 1 {
		t.Fatalf("publication count=%d err=%v", count, err)
	}
	pickerTx(t, f.a, func(tx *workstore.Tx) error {
		delivery, err := tx.GetDelivery(task.ScopeID, published.ObjectID)
		if err != nil {
			return err
		}
		outbox, err := tx.GetOutbox(delivery.NotificationOutboxID)
		if err != nil {
			return err
		}
		mapping, err := tx.GetPlatformMessageMapping("tenant", "chat", outbox.ExternalMessageID)
		if err != nil {
			return err
		}
		if outbox.State != work.OutboxSent || mapping.TaskID != task.ID || delivery.References[0].VersionID != artifact.VersionID {
			return fmt.Errorf("final delivery did not preserve receipt, task or version: %+v %+v", outbox, mapping)
		}
		return nil
	})
}
