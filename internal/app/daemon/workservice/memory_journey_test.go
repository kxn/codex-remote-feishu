package workservice

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workhttp"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workingress"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"github.com/kxn/codex-remote-feishu/internal/core/workcmd"
)

type memoryJourneyDirectory struct{}

func (memoryJourneyDirectory) ResolveGateway(_ context.Context, gateway, app string) (work.IngressGateway, error) {
	return work.IngressGateway{GatewayRef: gateway, AppID: app, RoleID: "role", BotGeneration: 1}, nil
}
func (memoryJourneyDirectory) ResolveLeader(context.Context, string, string, string) (string, error) {
	return "role", nil
}
func (memoryJourneyDirectory) ResolveMention(context.Context, work.IngressGateway, work.RawMention) (string, error) {
	return "", fmt.Errorf("no mention")
}

// This journey crosses durable SDK ingress, authenticated HTTP commands and a
// closed/reopened SQLite database. Native carrier replacement is independently
// exercised by the workhost and wrapper pipeline journeys.
func TestMemoryJourneyPersistsScopeAndApplicabilityAcrossReopen(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "work.db")
	store, err := workstore.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { store.Close() }()
	signer, err := workauth.New([]byte("0123456789abcdef0123456789abcdef"), nil)
	if err != nil {
		t.Fatal(err)
	}
	err = store.Within(ctx, func(tx *workstore.Tx) error {
		if e := tx.PutRole(work.Role{ID: "role", Name: "Designer", Description: "Design", DescriptionVersion: 1, SettingsRevision: 1, Revision: 1}, 0); e != nil {
			return e
		}
		return tx.PutRoleBotBinding(work.RoleBotBinding{RoleID: "role", GatewayRef: "gateway", AppID: "app", IdentityGeneration: 1}, 0)
	})
	if err != nil {
		t.Fatal(err)
	}
	ingress, err := workingress.New(store, workingress.Config{Directory: memoryJourneyDirectory{}})
	if err != nil {
		t.Fatal(err)
	}
	raw := work.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", EventID: "human-event", Tenant: "tenant", ChatID: "private-chat", ChatType: "p2p", MessageID: "human-message", SenderType: "user", SenderIDs: work.SenderIDs{OpenID: "human"}, MessageType: "text", RawContent: `{"text":"私人偏好不要分享。将公开经验记到你的通用经验，但只用于移动端低网速场景。"}`, ReceivedAt: time.Now().UTC()}
	if _, err = ingress.Receive(ctx, raw); err != nil {
		t.Fatal(err)
	}
	processed, err := ingress.ProcessPending(ctx, 16)
	if err != nil || len(processed.Failures) != 0 {
		t.Fatalf("ingress: %+v %v", processed, err)
	}
	var scope work.WorkScope
	var input work.Inbox
	var source work.SourceRef
	var contextID string
	err = store.Within(ctx, func(tx *workstore.Tx) error {
		scopes, e := tx.ListScopes()
		if e != nil {
			return e
		}
		for _, s := range scopes {
			if s.Kind == work.ScopePersonal {
				scope = s
			}
		}
		inputs, e := tx.ListPendingInputs(scope.ID, "role")
		if e != nil {
			return e
		}
		if len(inputs) != 1 {
			return fmt.Errorf("inputs=%d", len(inputs))
		}
		input = inputs[0]
		contextID = input.RecipientContextID
		if contextID == "" {
			return fmt.Errorf("ingress did not freeze personal context")
		}
		msg, e := tx.GetMessage(input.MessageID)
		if e != nil {
			return e
		}
		source = work.SourceRef{Kind: "message", ID: msg.ID, Version: fmt.Sprint(msg.Revision), EvidenceRef: msg.BodyRef}
		worker := work.WorkerSession{ID: "worker", ContextID: contextID, ScopeID: scope.ID, RoleID: "role", Backend: work.BackendClaude, State: work.WorkerReady, Generation: 1, Controller: work.ControllerAutomatic, ControllerGeneration: 1, InstanceID: "carrier", InstanceGeneration: 1, ProcessGeneration: 1, Revision: 1}
		if e = tx.PutWorker(worker, 0); e != nil {
			return e
		}
		attempt := work.ExecutionAttempt{ID: "attempt", ContextID: contextID, ScopeID: scope.ID, RunRef: work.RunRef{WorkerID: "worker", Generation: 1, RunID: "run"}, State: work.AttemptDispatchPending, CommandID: "start", InputIDs: []string{input.ID}, Revision: 1}
		op := work.Operation{ID: "start", ScopeID: scope.ID, ContextID: contextID, Actor: work.Actor{Kind: work.ActorSystem, ID: "scheduler"}, Kind: "native.start", TargetID: "worker", TargetGeneration: 1, IdempotencyKey: "start", RequestHash: "start", State: work.OperationPrepared, Revision: 1}
		if e = tx.ClaimAttempt(workstore.ClaimRequest{Attempt: attempt, ExpectedWorkerRevision: 1, ControllerGeneration: 1, InstanceID: "carrier", InstanceGeneration: 1, ProcessGeneration: 1, ExpectedInputRevisions: map[string]int64{input.ID: input.Revision}, StartOperation: op, Now: time.Now()}); e != nil {
			return e
		}
		attempt.State = work.AttemptRunning
		attempt.Revision++
		return tx.PutAttempt(attempt, 1)
	})
	if err != nil {
		t.Fatal(err)
	}
	token, err := signer.Issue(workauth.Claims{Purpose: "worker", RoleID: "role", BotGeneration: 1, ScopeID: scope.ID, ContextID: contextID, WorkerID: "worker", SessionGeneration: 1, ControllerGeneration: 1}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(store, signer, nil)
	if err != nil {
		t.Fatal(err)
	}
	before, err := service.BuildInstructions(ctx, contextID)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(workhttp.NewHandler(service))
	defer func() { server.Close() }()
	call := func(command string, payload any, result any) {
		t.Helper()
		body, e := json.Marshal(payload)
		if e != nil {
			t.Fatal(e)
		}
		requestBody, e := json.Marshal(workcmd.DispatchRequest{Name: command, RunRef: "run", Payload: body})
		if e != nil {
			t.Fatal(e)
		}
		request, e := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+workcmd.ExecutePath, bytes.NewReader(requestBody))
		if e != nil {
			t.Fatal(e)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+token)
		response, e := server.Client().Do(request)
		if e != nil {
			t.Fatal(e)
		}
		defer response.Body.Close()
		var envelope struct {
			Result json.RawMessage `json:"result"`
			Error  any             `json:"error"`
		}
		if e = json.NewDecoder(response.Body).Decode(&envelope); e != nil {
			t.Fatal(e)
		}
		if response.StatusCode != http.StatusOK {
			t.Fatalf("%s: %d %+v", command, response.StatusCode, envelope.Error)
		}
		if result != nil {
			if e = json.Unmarshal(envelope.Result, result); e != nil {
				t.Fatal(e)
			}
		}
	}
	var personal, shared MemoryWriteResult
	call("memory.write", MemoryWriteRequest{Key: "private", Content: "private-design-preference", Applicability: "only this person", Sources: []work.SourceRef{source}}, &personal)
	call("memory.write", MemoryWriteRequest{Key: "shared", Kind: work.MemoryRole, Content: "public-design-experience", Applicability: "only mobile clients on slow networks", Sources: []work.SourceRef{source}, SharingSourceMessageID: input.MessageID}, &shared)
	call("outcome.report", ReportOutcomeRequest{Key: "report", ProcessedInputIDs: []string{input.ID}, Summary: "Saved exactly the selected memories", Checkpoint: "Resume the agreed design discussion", WorkDisposition: "complete"}, nil)
	server.Close()
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = workstore.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	service, err = New(store, signer, nil)
	if err != nil {
		t.Fatal(err)
	}
	server = httptest.NewServer(workhttp.NewHandler(service))
	bundle, err := service.BuildInstructions(ctx, contextID)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"private-design-preference", "public-design-experience", "only this person", "only mobile clients on slow networks"} {
		if !strings.Contains(bundle.Text, want) {
			t.Fatalf("restored instruction lost %q", want)
		}
	}
	if bundle.Revision == before.Revision {
		t.Fatal("saved memory did not change next native instruction revision")
	}
	err = store.Within(ctx, func(tx *workstore.Tx) error {
		report, e := tx.GetProcessingReport("attempt")
		if e != nil {
			return e
		}
		if report.Checkpoint != "Resume the agreed design discussion" {
			return fmt.Errorf("checkpoint lost")
		}
		if e = tx.PutPrincipal(work.Principal{ID: "second-human", DisplayName: "Second", Revision: 1}, 0); e != nil {
			return e
		}
		if e = tx.PutScope(work.WorkScope{ID: "other-scope", Kind: work.ScopePersonal, PrincipalID: "second-human", RoleID: "role", Revision: 1}, 0); e != nil {
			return e
		}
		return tx.PutContext(work.WorkContext{ID: "other-context", ScopeID: "other-scope", RoleID: "role", Workstream: "main", Revision: 1}, 0)
	})
	if err != nil {
		t.Fatal(err)
	}
	other, err := service.BuildInstructions(ctx, "other-context")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(other.Text, "private-design-preference") || !strings.Contains(other.Text, "public-design-experience") || !strings.Contains(other.Text, "only mobile clients on slow networks") {
		t.Fatal("restored role mixed private scope or lost shared applicability")
	}
	call("memory.invalidate", MemoryInvalidateRequest{Key: "retract", ID: shared.ObjectID, ExpectedRevision: shared.Revision, Source: source}, nil)
	revised, err := service.BuildInstructions(ctx, "other-context")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(revised.Text, "public-design-experience") || revised.Revision == other.Revision {
		t.Fatal("retracted memory still injected after correction")
	}
}
