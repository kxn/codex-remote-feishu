package workservice

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func attachmentAttempt(t *testing.T) (*Service, work.ExecutionAttempt) {
	t.Helper()
	return attachmentAttemptState(t, true)
}
func attachmentAttemptState(t *testing.T, claim bool) (*Service, work.ExecutionAttempt) {
	t.Helper()
	s, _, _ := runningService(t)
	var attempt work.ExecutionAttempt
	err := s.store.Within(t.Context(), func(tx *workstore.Tx) error {
		wc := work.WorkContext{ID: "attachments", ScopeID: "scope", RoleID: "leader", Workstream: "attachments", Revision: 1}
		if err := tx.PutContext(wc, 0); err != nil {
			return err
		}
		worker := work.WorkerSession{ID: "attachment-worker", ContextID: wc.ID, ScopeID: wc.ScopeID, RoleID: wc.RoleID, Backend: work.BackendCodex, State: work.WorkerReady, Generation: 1, Controller: work.ControllerAutomatic, ControllerGeneration: 1, InstanceID: "attachment-carrier", InstanceGeneration: 1, ProcessGeneration: 1, Revision: 1}
		if err := tx.PutWorker(worker, 0); err != nil {
			return err
		}
		image := work.NewContent("image-bytes", wc.ScopeID, "image/png", []byte("durable image fixture"), s.now())
		file := work.NewContent("file-bytes", wc.ScopeID, "text/plain", []byte("actual file content"), s.now())
		body := work.NewContent("attachment-body", wc.ScopeID, "text/plain", []byte("inspect these attachments"), s.now())
		for _, content := range []work.Content{image, file, body} {
			if err := tx.PutContent(content); err != nil {
				return err
			}
		}
		message := work.Message{ID: "attachment-message", ScopeID: wc.ScopeID, ConversationID: "actual-conversation", Source: work.Actor{Kind: work.ActorPrincipal, ID: "human"}, BodyRef: body.ID, Revision: 1, CreatedAt: s.now(), Attachments: []work.MessageAttachment{{ContentRef: image.ID, SHA256: image.SHA256, Size: int64(len(image.Data)), MediaType: image.MediaType, Kind: "image", SourceMessageID: "sdk-message"}, {ContentRef: file.ID, SHA256: file.SHA256, Size: int64(len(file.Data)), MediaType: file.MediaType, Kind: "file", FileName: "../../ignored-name.txt", SourceMessageID: "sdk-message"}}}
		inbox := work.Inbox{ID: "attachment-inbox", ScopeID: wc.ScopeID, RecipientRoleID: wc.RoleID, MessageID: message.ID, EventID: "attachment-event", State: work.InboxPending, ContentState: work.InputContentReady, AvailableAt: s.now(), Revision: 1}
		if _, _, err := tx.AcceptMessage(message, []work.Inbox{inbox}); err != nil {
			return err
		}
		attempt = work.ExecutionAttempt{ID: "attachment-attempt", ContextID: wc.ID, ScopeID: wc.ScopeID, RunRef: work.RunRef{WorkerID: worker.ID, Generation: 1, RunID: "attachment-run"}, State: work.AttemptDispatchPending, InputIDs: []string{inbox.ID}, CommandID: "attachment-start", Revision: 1}
		if !claim {
			return nil
		}
		operation := work.Operation{ID: attempt.CommandID, ScopeID: wc.ScopeID, ContextID: wc.ID, Actor: work.Actor{Kind: work.ActorSystem, ID: "scheduler"}, Kind: "native.start", TargetID: worker.ID, TargetGeneration: 1, IdempotencyKey: attempt.CommandID, RequestHash: "fixture", State: work.OperationPrepared, Revision: 1}
		if err := tx.ClaimAttempt(workstore.ClaimRequest{Attempt: attempt, ExpectedWorkerRevision: 1, ControllerGeneration: 1, InstanceID: worker.InstanceID, InstanceGeneration: 1, ProcessGeneration: 1, StartOperation: operation, Now: s.now()}); err != nil {
			return err
		}
		var err error
		attempt, err = tx.GetAttempt(attempt.ID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return s, attempt
}
func TestWorkPromptMaterializesOnlyAdmittedDurableBytes(t *testing.T) {
	s, attempt := attachmentAttempt(t)
	root := t.TempDir()
	inputs, err := s.BuildWorkPrompt(t.Context(), attempt, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 3 || inputs[1].Type != agentproto.InputLocalImage || !strings.HasPrefix(inputs[1].Path, root) || !strings.Contains(inputs[2].Text, "actual file") && !strings.Contains(inputs[2].Text, "local_path") {
		t.Fatalf("wrong typed prompt %+v", inputs)
	}
	data, err := os.ReadFile(inputs[1].Path)
	if err != nil || !bytes.Equal(data, []byte("durable image fixture")) {
		t.Fatalf("actual bytes %q %v", data, err)
	}
	if err = os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if _, err = s.BuildWorkPrompt(t.Context(), attempt, root); err != nil {
		t.Fatalf("lost restart materialization: %v", err)
	}
	forged := attempt
	forged.InputIDs = nil
	if _, err = s.BuildWorkPrompt(t.Context(), forged, root); err == nil {
		t.Fatal("forged input list accepted")
	}
	forged = attempt
	forged.ScopeID = "foreign"
	if _, err = s.BuildWorkPrompt(t.Context(), forged, root); err == nil {
		t.Fatal("foreign scope accepted")
	}
	if err = os.WriteFile(inputs[1].Path, []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.BuildWorkPrompt(t.Context(), attempt, root); err == nil {
		t.Fatal("tampered materialization silently used")
	}
}

func TestWorkPromptPlannedCandidateRequiresPendingExactInputs(t *testing.T) {
	s, candidate := attachmentAttemptState(t, false)
	inputs, err := s.BuildPlannedWorkPrompt(t.Context(), candidate, t.TempDir())
	if err != nil || len(inputs) != 3 || inputs[1].Type != agentproto.InputLocalImage {
		t.Fatalf("candidate prompt: %v, inputs=%d", err, len(inputs))
	}
	if _, err = s.BuildWorkPrompt(t.Context(), candidate, t.TempDir()); err == nil {
		t.Fatal("unclaimed attempt accepted by persisted builder")
	}
	err = s.store.Within(t.Context(), func(tx *workstore.Tx) error {
		_, e := tx.GetAttempt(candidate.ID)
		if e != workstore.ErrNotFound {
			t.Fatalf("planning persisted attempt: %v", e)
		}
		in, e := tx.GetInbox(candidate.InputIDs[0])
		if e != nil {
			return e
		}
		if in.State != work.InboxPending || in.ClaimedAttemptID != "" {
			t.Fatal("planning consumed input")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	forged := candidate
	forged.TaskID = "foreign-task"
	if _, err = s.BuildPlannedWorkPrompt(t.Context(), forged, t.TempDir()); err == nil {
		t.Fatal("cross-task planned input accepted")
	}
	forged = candidate
	forged.Revision = 2
	if _, err = s.BuildPlannedWorkPrompt(t.Context(), forged, t.TempDir()); err == nil {
		t.Fatal("existing revision accepted as planned candidate")
	}
	admitted, attempt := attachmentAttempt(t)
	if _, err = admitted.BuildPlannedWorkPrompt(t.Context(), attempt, t.TempDir()); err == nil {
		t.Fatal("already claimed attempt accepted as new candidate")
	}
}
