package workruntime

import (
	"encoding/json"
	"errors"
	"github.com/kxn/codex-remote-feishu/internal/adapter/codex"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"testing"
)

func nativeStartRejectedFixture(t *testing.T, origin agentproto.TurnCompletionOrigin, control bool) *fixture {
	t.Helper()
	f := newFixture(t, work.BackendCodex, false, true, control, origin == agentproto.TurnCompletionOriginThreadStartRejected)
	f.coordinator.options.EnqueueReconcile = func(string) {}
	translator := codex.NewTranslator(f.carrier.OriginInstanceID)
	frames, err := translator.TranslateCommand(f.command)
	if err != nil || len(frames) != 1 {
		t.Fatalf("start command=%d %v", len(frames), err)
	}
	requestID := func(frame []byte) string {
		var rpc struct {
			ID string `json:"id"`
		}
		if e := json.Unmarshal(frame, &rpc); e != nil {
			t.Fatal(e)
		}
		return rpc.ID
	}
	id := requestID(frames[0])
	if origin == agentproto.TurnCompletionOriginTurnStartRejected {
		response, _ := json.Marshal(map[string]any{"id": id, "result": map[string]any{}})
		result, e := translator.ObserveServer(response)
		if e != nil || len(result.OutboundToCodex) != 1 {
			t.Fatalf("resume before turn=%+v %v", result, e)
		}
		id = requestID(result.OutboundToCodex[0])
	}
	message := "explicit native start rejection"
	if origin == agentproto.TurnCompletionOriginThreadResumeRejected {
		message = "no rollout found for thread id session"
	}
	response, _ := json.Marshal(map[string]any{"id": id, "error": map[string]any{"code": -32000, "message": message}})
	result, err := translator.ObserveServer(response)
	if err != nil || len(result.Events) != 1 || result.Events[0].TurnCompletionOrigin != origin {
		t.Fatalf("actual rejection=%+v %v", result, err)
	}
	if err = f.handle(result.Events[0]); err != nil {
		t.Fatal(err)
	}
	f.driver.inspect = func(request InspectionRequest) (NativeObservation, error) {
		var terminal *agentproto.Event
		f.tx(func(tx *workstore.Tx) error {
			command, e := tx.GetNativeCommand(request.SubjectCommandID)
			terminal = command.TerminalRejection
			return e
		})
		result := agentproto.RuntimeInspectionResult{Version: agentproto.RuntimeInspectionProtocolVersion, CarrierIncarnation: request.Carrier.Incarnation, InstanceGeneration: request.Carrier.InstanceGeneration, ProcessGeneration: request.Carrier.ProcessGeneration, LocationGeneration: request.LocationGeneration, ObservedAt: f.now, NativeRejection: terminal, Bootstrap: RPCProbe{Request: json.RawMessage(`{"id":"ready","method":"initialize"}`), Response: json.RawMessage(`{"id":"ready","result":{}}`)}, Background: codexInspection(request).Background}
		return ObserveRuntimeInspection(request, result)
	}
	return f
}

func TestPrivateExactNativeStartRejectionSettlesWithoutModelReport(t *testing.T) {
	for _, origin := range []agentproto.TurnCompletionOrigin{agentproto.TurnCompletionOriginThreadStartRejected, agentproto.TurnCompletionOriginTurnStartRejected} {
		for _, control := range []bool{false, true} {
			t.Run(string(origin)+map[bool]string{false: "/normal", true: "/control"}[control], func(t *testing.T) {
				f := nativeStartRejectedFixture(t, origin, control)
				for range 2 {
					if err := f.coordinator.Reconcile(t.Context(), "attempt"); err != nil {
						t.Fatal(err)
					}
				}
				f.tx(func(tx *workstore.Tx) error {
					attempt, e := tx.GetAttempt("attempt")
					if e != nil {
						return e
					}
					if attempt.State != work.AttemptFailed {
						t.Fatalf("attempt=%s", attempt.State)
					}
					input, e := tx.GetInbox("input")
					if e != nil {
						return e
					}
					if input.State != work.InboxProcessed {
						t.Fatal("known rejected request can be replayed")
					}
					report, e := tx.GetProcessingReport("attempt")
					if e != nil {
						return e
					}
					if len(report.ProcessedInputIDs) != 1 || report.ProcessedInputIDs[0] != "input" {
						t.Fatal("wrong input consumed")
					}
					if control {
						original, e := tx.GetInbox("original-control-input")
						if e != nil {
							return e
						}
						if original.State != work.InboxPending {
							t.Fatal("management rejection consumed original business")
						}
					}
					if _, e = tx.GetRuntimeGate("worker"); !errors.Is(e, workstore.ErrNotFound) {
						t.Fatalf("known rejection kept gate: %v", e)
					}
					_, e = tx.GetQuiescenceReceipt("attempt")
					return e
				})
				if !control {
					claimNextPrivateInputAfterRejection(t, f)
				}
			})
		}
	}
}

func claimNextPrivateInputAfterRejection(t *testing.T, f *fixture) {
	t.Helper()
	f.tx(func(tx *workstore.Tx) error {
		scope, e := tx.GetScope("scope")
		if e != nil {
			return e
		}
		worker, e := tx.GetWorker("worker")
		if e != nil {
			return e
		}
		content := work.NewContent("next-user-body", "scope", "text/plain", []byte("next independently authorized request"), f.now)
		if e = tx.PutContent(content); e != nil {
			return e
		}
		message := work.Message{ID: "next-user-message", ScopeID: "scope", ConversationID: "private", Source: work.Actor{Kind: work.ActorPrincipal, ID: scope.PrincipalID}, PlatformKey: &work.MessageKey{Tenant: "tenant", ChatID: "private-chat", MessageID: "om-next"}, BodyRef: content.ID, Observations: []work.Observation{{AppID: "app", IdentityGeneration: 1, ReceivedAt: f.now}}, Revision: 1, CreatedAt: f.now}
		input := work.Inbox{ID: "next-input", ScopeID: "scope", RecipientRoleID: "role", RecipientContextID: "context", EventID: "next-human", MessageID: message.ID, State: work.InboxPending, Revision: 1, AvailableAt: f.now}
		if _, _, e = tx.AcceptMessage(message, []work.Inbox{input}); e != nil {
			return e
		}
		binding := f.carrier.Owner
		binding.AttemptID = "next-attempt"
		binding.RunRef = "next-run"
		command := agentproto.Command{CommandID: "next-start", Kind: agentproto.CommandPromptSend, Execution: &binding, Target: agentproto.Target{ExecutionMode: agentproto.PromptExecutionModeStartNew}, Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "next independently authorized request"}}}}
		op, e := f.operation(tx, command)
		if e != nil {
			return e
		}
		return tx.ClaimAttempt(workstore.ClaimRequest{Attempt: work.ExecutionAttempt{ID: binding.AttemptID, ScopeID: "scope", ContextID: "context", RunRef: work.RunRef{WorkerID: "worker", Generation: worker.Generation, RunID: binding.RunRef}, State: work.AttemptDispatchPending, InputIDs: []string{input.ID}, CommandID: command.CommandID, Revision: 1}, ExpectedWorkerRevision: worker.Revision, ControllerGeneration: worker.ControllerGeneration, InstanceID: worker.InstanceID, InstanceGeneration: worker.InstanceGeneration, ProcessGeneration: worker.ProcessGeneration, LocationGeneration: worker.LocationGeneration, StartOperation: op, Now: f.now})
	})
}

func TestPrivateStartRejectionKeepsResumeRecoveryAndUnknownEvidence(t *testing.T) {
	for _, kind := range []string{"resume-missing", "background-unknown", "proof-mismatch", "effect-unknown", "actual-turn"} {
		t.Run(kind, func(t *testing.T) {
			origin := agentproto.TurnCompletionOriginTurnStartRejected
			if kind == "resume-missing" {
				origin = agentproto.TurnCompletionOriginThreadResumeRejected
			}
			f := nativeStartRejectedFixture(t, origin, false)
			inspect := f.driver.inspect
			if kind == "background-unknown" || kind == "proof-mismatch" {
				f.driver.inspect = func(request InspectionRequest) (NativeObservation, error) {
					observation, e := inspect(request)
					if kind == "background-unknown" {
						observation.BackgroundWorkStopped = false
					} else {
						observation.NativeEvidence = json.RawMessage(`{"not":"the registered native rejection"}`)
					}
					return observation, e
				}
			}
			if kind == "effect-unknown" {
				f.tx(func(tx *workstore.Tx) error {
					body := work.NewContent("unknown-effect", "scope", "text/plain", []byte("uncertain write"), f.now)
					if e := tx.PutContent(body); e != nil {
						return e
					}
					_, _, e := tx.PutOperation(work.Operation{ID: "unresolved-write", ScopeID: "scope", ContextID: "context", Actor: work.Actor{Kind: work.ActorRole, ID: "role"}, Kind: "resource.write", TargetID: "document", TargetGeneration: 1, IdempotencyKey: "unknown-effect", RequestHash: body.SHA256, RequestRef: body.ID, State: work.OperationUnknown, Revision: 1}, 0)
					return e
				})
			}
			if kind == "actual-turn" {
				if err := f.handle(agentproto.Event{Kind: agentproto.EventTurnStarted, CommandID: "command", ThreadID: "session", TurnID: "later-accepted-turn"}); err != nil {
					t.Fatal(err)
				}
			}
			_ = f.coordinator.Reconcile(t.Context(), "attempt")
			f.tx(func(tx *workstore.Tx) error {
				input, e := tx.GetInbox("input")
				if e != nil {
					return e
				}
				if input.State == work.InboxProcessed {
					t.Fatal("uncertain/executed/resume request was consumed")
				}
				if report, e := tx.GetProcessingReport("attempt"); e == nil && len(report.ProcessedInputIDs) > 0 {
					t.Fatal("unsafe failure report recorded")
				}
				return nil
			})
		})
	}
}
