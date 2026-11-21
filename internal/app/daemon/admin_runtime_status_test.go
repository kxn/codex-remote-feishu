package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestPersistentTeamRuntimeStatusUsesWorkStore(t *testing.T) {
	fixture := teamProjectViewFixture(t)
	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	app.service.MaterializeSurface("legacy-private-surface", "app", "chat", "user")
	store := fixture.teamRuntime.store
	app.teamRuntime = fixture.teamRuntime

	if err := store.Within(t.Context(), func(tx *workstore.Tx) error {
		context := work.WorkContext{ID: "context", ScopeID: "scope", RoleID: "leader", Workstream: "secret-workstream", Revision: 1}
		if err := tx.PutContext(context, 0); err != nil {
			return err
		}
		worker := work.WorkerSession{ID: "worker", ContextID: context.ID, ScopeID: "scope", RoleID: "leader", Backend: work.BackendCodex, State: work.WorkerReady, Generation: 1, Controller: work.ControllerAutomatic, ControllerGeneration: 1, InstanceID: "worker-carrier", InstanceGeneration: 1, ProcessGeneration: 1, Revision: 1}
		if err := tx.PutWorker(worker, 0); err != nil {
			return err
		}
		now := time.Now().UTC()
		input := work.Inbox{ID: "input", ScopeID: "scope", RecipientRoleID: "leader", RecipientContextID: context.ID, EventID: "event", State: work.InboxPending, ContentState: work.InputContentReady, AvailableAt: now, Revision: 1}
		if err := tx.PutInbox(input, 0); err != nil {
			return err
		}
		attempt := work.ExecutionAttempt{ID: "attempt", ContextID: context.ID, ScopeID: "scope", RunRef: work.RunRef{WorkerID: worker.ID, Generation: 1, RunID: "run"}, State: work.AttemptDispatchPending, InputIDs: []string{input.ID}, CommandID: "start", Revision: 1}
		start := work.Operation{ID: attempt.CommandID, ScopeID: "scope", ContextID: context.ID, Actor: work.Actor{Kind: work.ActorSystem, ID: "scheduler"}, Kind: "native.start", TargetID: worker.ID, TargetGeneration: 1, IdempotencyKey: attempt.CommandID, RequestHash: "fixture", State: work.OperationPrepared, Revision: 1}
		err := tx.ClaimAttempt(workstore.ClaimRequest{Attempt: attempt, ExpectedWorkerRevision: 1, ControllerGeneration: 1, InstanceID: worker.InstanceID, InstanceGeneration: 1, ProcessGeneration: 1, StartOperation: start, Now: now})
		if err != nil {
			return err
		}
		attempt.State = work.AttemptRunning
		attempt.Revision++
		if err := tx.PutAttempt(attempt, 1); err != nil {
			return err
		}
		input.ID, input.EventID = "next-input", "next-event"
		return tx.PutInbox(input, 0)
	}); err != nil {
		t.Fatal(err)
	}

	for path, handler := range map[string]http.HandlerFunc{"/v1/status": app.handleStatus, "/api/admin/runtime-status": app.handleRuntimeStatus} {
		t.Run(path, func(t *testing.T) {
			payload := readTeamStatus(t, path, handler)
			workStatus := payload.Work
			if workStatus == nil || !workStatus.Available || workStatus.ReadError != "" || workStatus.RuntimeStatus == nil {
				t.Fatalf("expected available team status, got %+v", workStatus)
			}
			if workStatus.ProjectTotal != 1 || workStatus.ContextTotal != 1 || workStatus.WorkerTotal != 1 || workStatus.PendingInput != 1 || workStatus.RunningAttempts != 1 || workStatus.OccupiedAttempts != 1 {
				t.Fatalf("unexpected totals: %+v", workStatus.RuntimeStatus)
			}
			if workStatus.DetailLimit < 1 || len(workStatus.Projects) != 1 || workStatus.Projects[0].ID != "project" || len(workStatus.Workers) != 1 || workStatus.Workers[0].ID != "worker" {
				t.Fatalf("unexpected status details: %+v", workStatus.RuntimeStatus)
			}
		})
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	payload := readTeamStatus(t, "/v1/status", app.handleStatus)
	if payload.Work == nil || payload.Work.Available || payload.Work.ReadError == "" || payload.Work.RuntimeStatus != nil {
		t.Fatalf("closed store should report an unavailable projection: %+v", payload.Work)
	}
}

func TestPersistentTeamRuntimeStatusBeforeStoreStarts(t *testing.T) {
	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	app.service.MaterializeSurface("legacy-private-surface", "app", "chat", "user")
	app.headlessRuntime.LegacyHistoryOnly = true
	payload := readTeamStatus(t, "/v1/status", app.handleStatus)
	if payload.Work == nil || payload.Work.Available || payload.Work.ReadError == "" || payload.Work.RuntimeStatus != nil {
		t.Fatalf("missing store should report an unavailable projection: %+v", payload.Work)
	}
}

func readTeamStatus(t *testing.T, path string, handler http.HandlerFunc) runtimeStatusPayload {
	t.Helper()
	response := httptest.NewRecorder()
	handler(response, httptest.NewRequest(http.MethodGet, path, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, response = %s", response.Code, response.Body.String())
	}
	var payload runtimeStatusPayload
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Authority != "persistent_teams" || payload.Instances == nil || payload.Surfaces == nil || payload.PendingRemoteTurns == nil || payload.ActiveRemoteTurns == nil {
		t.Fatalf("unexpected status contract: %s", response.Body.String())
	}
	if len(payload.Instances)+len(payload.Surfaces)+len(payload.PendingRemoteTurns)+len(payload.ActiveRemoteTurns)+len(payload.SurfaceStatuses) != 0 {
		t.Fatalf("retired surfaces leaked into team status: %s", response.Body.String())
	}
	if strings.Contains(response.Body.String(), "legacy-private-surface") || strings.Contains(response.Body.String(), "secret-workstream") {
		t.Fatalf("private details leaked into team status: %s", response.Body.String())
	}
	return payload
}
