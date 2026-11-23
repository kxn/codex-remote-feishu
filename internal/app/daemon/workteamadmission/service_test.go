package workteamadmission

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workingress"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workruntime"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type fixtureDriver struct {
	coordinator *workruntime.Coordinator
	sent        []agentproto.Command
	sendErr     error
	crash       bool
}

func (d *fixtureDriver) SendCommand(ctx context.Context, _ string, cmd agentproto.Command) error {
	if d.crash {
		panic("simulated crash before transport")
	}
	if d.sendErr != nil {
		return d.sendErr
	}
	if err := d.coordinator.RegisterCommand(ctx, "inc", cmd); err != nil {
		return err
	}
	d.sent = append(d.sent, cmd)
	return nil
}

func (d *fixtureDriver) Inspect(_ context.Context, request workruntime.InspectionRequest) (workruntime.NativeObservation, error) {
	return workruntime.NativeObservation{CarrierIncarnation: request.Carrier.Incarnation, InstanceID: request.Carrier.OriginInstanceID, InstanceGeneration: request.Carrier.InstanceGeneration, ProcessGeneration: request.Carrier.ProcessGeneration, LocationGeneration: request.LocationGeneration, SessionID: request.SessionID, TurnID: request.TurnID, NativeStopped: true, ContinuationStopped: true, BackgroundWorkStopped: true, NativeEvidence: json.RawMessage(`{"type":"result","subtype":"success"}`), ContinuationEvidence: json.RawMessage(`{"cleared":true}`), BackgroundEvidence: json.RawMessage(`{"processes":[],"tasks":[]}`), ObservedAt: time.Now().UTC()}, nil
}

type teamFixture struct {
	store  *workstore.Store
	coord  *workruntime.Coordinator
	lane   *Service
	driver *fixtureDriver
	seq    uint64
	now    time.Time
}

func newTeamFixture(t *testing.T, backend work.Backend, withTask ...bool) *teamFixture {
	taskID, workstream := "", "main"
	if len(withTask) > 0 && withTask[0] {
		taskID, workstream = "task", "task:task"
	}
	t.Helper()
	root := t.TempDir()
	store, err := workstore.Open(t.Context(), filepath.Join(root, "work.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	signer, err := workauth.New([]byte("0123456789abcdef0123456789abcdef"), nil)
	if err != nil {
		t.Fatal(err)
	}
	service, err := workservice.New(store, signer, nil)
	if err != nil {
		t.Fatal(err)
	}
	driver := &fixtureDriver{}
	coord := workruntime.New(store, workruntime.Options{Driver: driver})
	driver.coordinator = coord
	lane, err := New(store, service, driver, root)
	if err != nil {
		t.Fatal(err)
	}
	f := &teamFixture{store: store, coord: coord, lane: lane, driver: driver, now: time.Now().UTC()}
	owner := agentproto.ExecutionBinding{Version: 1, ScopeID: "scope", ContextID: "context", RoleID: "role", WorkerSessionID: "worker", SessionGeneration: 1, ControllerGeneration: 1, BotGeneration: 1}
	carrier := workstore.CarrierRegistration{Incarnation: "inc", OriginInstanceID: "instance", Owner: owner, Backend: backend, InstanceGeneration: 1, ProcessGeneration: 1, LocationGeneration: 1}
	f.tx(t, func(tx *workstore.Tx) error {
		if e := tx.PutRole(work.Role{ID: "role", Name: "Role", Description: "work", DescriptionVersion: 1, SettingsRevision: 1, Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutRoleBotBinding(work.RoleBotBinding{RoleID: "role", GatewayRef: "gateway", AppID: "app", IdentityGeneration: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutTeam(work.Team{ID: "team", Name: "Team", LeaderRoleID: "role", Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutTeamMember(work.TeamMember{TeamID: "team", RoleID: "role", Active: true, Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutScope(work.WorkScope{ID: "scope", Kind: work.ScopeProject, TeamID: "team", ProjectID: "project", Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutProject(work.Project{ID: "project", ScopeID: "scope", TeamID: "team", Name: "Project", LeaderRoleID: "role", State: work.ProjectActive, Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutProjectChannelBinding(work.ProjectChannelBinding{ProjectID: "project", Tenant: "tenant", ChatID: "group", Purpose: "main", BindingGeneration: 1, Active: true}, 0); e != nil {
			return e
		}
		if e := tx.PutProjectMember(work.ProjectMember{ProjectID: "project", RoleID: "role", State: work.ProjectMemberReady, BotGeneration: 1, Revision: 1}, 0); e != nil {
			return e
		}
		if taskID != "" {
			if e := tx.PutTask(work.Task{ID: taskID, ScopeID: "scope", Title: "task", OwnerRoleID: "role", State: work.TaskTodo, RequirementsRevision: 1, AssignmentGeneration: 1, Revision: 1}, 0); e != nil {
				return e
			}
		}
		if e := tx.PutContext(work.WorkContext{ID: "context", ScopeID: "scope", RoleID: "role", TaskID: taskID, Workstream: workstream, Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutLocation(work.ExecutionLocation{ID: "location", HostID: "host", Cwd: root, Generation: 1, Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutWorker(work.WorkerSession{ID: "worker", ContextID: "context", ScopeID: "scope", RoleID: "role", Backend: backend, State: work.WorkerPreparing, Generation: 1, Controller: work.ControllerAutomatic, ControllerGeneration: 1, InstanceID: "instance", InstanceGeneration: 1, ProcessGeneration: 1, CarrierIncarnation: "inc", LocationID: "location", LocationGeneration: 1, Revision: 1}, 0); e != nil {
			return e
		}
		return tx.PutInbox(work.Inbox{ID: "initial", ScopeID: "scope", TaskID: taskID, RecipientRoleID: "role", RecipientContextID: "context", EventID: "initial", State: work.InboxPending, Revision: 1}, 0)
	})
	if err = coord.RegisterCarrier(t.Context(), carrier); err != nil {
		t.Fatal(err)
	}
	if err = coord.HandleHello(t.Context(), agentproto.Hello{Protocol: agentproto.WireProtocol, Instance: agentproto.InstanceHello{InstanceID: "instance", Backend: agentproto.Backend(backend), Managed: true, Source: "headless"}, WorkerProtocolVersion: 1, JournalProtocolVersion: 1, CarrierIncarnation: "inc"}); err != nil {
		t.Fatal(err)
	}
	binding := owner
	binding.AttemptID = "attempt"
	binding.RunRef = "run"
	cmd := agentproto.Command{CommandID: "start", Kind: agentproto.CommandPromptSend, Execution: &binding, InstructionRevision: "v1", Target: agentproto.Target{ThreadID: "session", CWD: root}, Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "initial"}}}}
	raw, _ := json.Marshal(cmd)
	sum := sha256.Sum256(raw)
	f.tx(t, func(tx *workstore.Tx) error {
		if e := tx.PutContent(work.NewContent("request", "scope", "application/json", raw, f.now)); e != nil {
			return e
		}
		worker, e := tx.GetWorker("worker")
		if e != nil {
			return e
		}
		op := work.Operation{ID: "start", ScopeID: "scope", ContextID: "context", Actor: work.Actor{Kind: work.ActorRole, ID: "role"}, Kind: string(cmd.Kind), TargetID: "worker", TargetGeneration: 1, IdempotencyKey: "start", RequestHash: hex.EncodeToString(sum[:]), RequestRef: "request", State: work.OperationPrepared, Revision: 1}
		return tx.ClaimAttempt(workstore.ClaimRequest{Attempt: work.ExecutionAttempt{ID: "attempt", ContextID: "context", ScopeID: "scope", RunRef: work.RunRef{WorkerID: "worker", Generation: 1, RunID: "run"}, State: work.AttemptDispatchPending, TaskID: taskID, FrozenRequirementsRevision: boolRevision(taskID), FrozenAssignmentGeneration: boolRevision(taskID), InputIDs: []string{"initial"}, CommandID: "start", Revision: 1}, ExpectedWorkerRevision: worker.Revision, ControllerGeneration: 1, InstanceID: "instance", InstanceGeneration: 1, ProcessGeneration: 1, LocationGeneration: 1, StartOperation: op, Now: f.now})
	})
	if err = coord.RegisterCommand(t.Context(), "inc", cmd); err != nil {
		t.Fatal(err)
	}
	f.event(t, agentproto.Event{Kind: agentproto.EventTurnStarted, CommandID: "start", ThreadID: "session", TurnID: "turn"})
	return f
}
func (f *teamFixture) tx(t *testing.T, fn func(*workstore.Tx) error) {
	t.Helper()
	if e := f.store.Within(t.Context(), fn); e != nil {
		t.Fatal(e)
	}
}
func (f *teamFixture) event(t *testing.T, event agentproto.Event) {
	t.Helper()
	f.seq++
	event.Seq = f.seq
	hash, e := agentproto.RuntimeEventPayloadHash(event)
	if e != nil {
		t.Fatal(e)
	}
	event.Journal = &agentproto.RuntimeEventIdentity{Version: 1, CarrierIncarnation: "inc", Sequence: f.seq, PayloadHash: hash}
	if e = f.coord.HandleEvent(t.Context(), "instance", event); e != nil {
		t.Fatal(e)
	}
}
func (f *teamFixture) message(t *testing.T, id, contextID string, tasks ...string) {
	taskID := ""
	if len(tasks) > 0 {
		taskID = tasks[0]
	}
	t.Helper()
	f.tx(t, func(tx *workstore.Tx) error {
		body := work.NewContent(id+":body", "scope", "text/plain", []byte("new message"), f.now)
		if e := tx.PutContent(body); e != nil {
			return e
		}
		_, _, e := tx.AcceptMessage(work.Message{ID: id, ScopeID: "scope", ConversationID: "chat", Source: work.Actor{Kind: work.ActorRole, ID: "role"}, BodyRef: body.ID, Revision: 1}, []work.Inbox{{ID: id + ":input", ScopeID: "scope", TaskID: taskID, RecipientRoleID: "role", RecipientContextID: contextID, EventID: "message:" + id, MessageID: id, State: work.InboxPending, Revision: 1}})
		if e != nil {
			return e
		}
		request, _ := json.Marshal(map[string]string{"recipient_role_id": "role", "recipient_context_id": contextID, "task_id": taskID, "body": "new message"})
		requestBody := work.NewContent(id+":request", "scope", "application/json", request, f.now)
		if e = tx.PutContent(requestBody); e != nil {
			return e
		}
		_, _, e = tx.PutOperation(work.Operation{ID: id + ":operation", ScopeID: "scope", ContextID: "context", Actor: work.Actor{Kind: work.ActorRole, ID: "role"}, Kind: "message.send", TargetID: id, TargetGeneration: 1, IdempotencyKey: id, RequestHash: "hash", RequestRef: requestBody.ID, State: work.OperationConfirmed, ExternalReceipt: "ok", Revision: 1}, 0)
		return e
	})
}
func TestProjectSteerRequiresNativeReceipt(t *testing.T) {
	for _, backend := range []work.Backend{work.BackendCodex, work.BackendClaude} {
		t.Run(string(backend), func(t *testing.T) {
			f := newTeamFixture(t, backend)
			f.message(t, "late", "")
			if e := f.lane.Tick(t.Context(), 10); e != nil {
				t.Fatal(e)
			}
			if len(f.driver.sent) != 1 {
				t.Fatalf("sent=%d", len(f.driver.sent))
			}
			cmd := f.driver.sent[0]
			f.tx(t, func(tx *workstore.Tx) error {
				ids, e := tx.EffectiveAttemptInputIDs("attempt")
				if e != nil {
					return e
				}
				if len(ids) != 1 {
					return fmt.Errorf("unconfirmed input delivered: %v", ids)
				}
				return nil
			})
			f.event(t, agentproto.Event{Kind: agentproto.EventNativeTurnInputAccepted, CommandID: cmd.CommandID, RequestID: "native-rpc", ThreadID: "session", TurnID: "turn", Status: "accepted", Execution: cmd.Execution})
			f.tx(t, func(tx *workstore.Tx) error {
				ids, e := tx.EffectiveAttemptInputIDs("attempt")
				if e != nil {
					return e
				}
				if len(ids) != 2 || ids[1] != "late:input" {
					return fmt.Errorf("confirmed input missing: %v", ids)
				}
				return nil
			})
		})
	}
}

type provedUnsentError struct{}

func (provedUnsentError) Error() string             { return "transport was not called" }
func (provedUnsentError) NativeCommandUnsent() bool { return true }

func TestProjectAdmissionCrashAndPreSendFailure(t *testing.T) {
	t.Run("crash", func(t *testing.T) {
		f := newTeamFixture(t, work.BackendCodex)
		f.message(t, "late", "")
		f.driver.crash = true
		func() {
			defer func() {
				if recover() == nil {
					t.Fatal("expected simulated crash")
				}
			}()
			_ = f.lane.Tick(t.Context(), 10)
		}()
		f.driver.crash = false
		restarted, e := New(f.store, f.lane.service, f.driver, f.lane.stateDir)
		if e != nil {
			t.Fatal(e)
		}
		f.tx(t, func(tx *workstore.Tx) error { return tx.RecoverProjectAdmissionsAfterRestart() })
		if e = restarted.Tick(t.Context(), 10); e != nil {
			t.Fatal(e)
		}
		if len(f.driver.sent) != 0 {
			t.Fatal("uncertain send was replayed")
		}
		f.tx(t, func(tx *workstore.Tx) error {
			admissions, e := tx.ListAttemptInputAdmissions("attempt")
			if e != nil {
				return e
			}
			if len(admissions) != 1 || admissions[0].State != work.InputAdmissionUnknown {
				return fmt.Errorf("not recovered as unknown: %+v", admissions)
			}
			input, e := tx.GetInbox("late:input")
			if e != nil {
				return e
			}
			if input.State != work.InboxClaimed {
				return fmt.Errorf("uncertain input not held: %s", input.State)
			}
			return nil
		})
	})
	t.Run("proved_unsent", func(t *testing.T) {
		f := newTeamFixture(t, work.BackendCodex)
		f.message(t, "late", "")
		f.driver.sendErr = provedUnsentError{}
		if e := f.lane.Tick(t.Context(), 10); e != nil {
			t.Fatal(e)
		}
		f.tx(t, func(tx *workstore.Tx) error {
			admissions, e := tx.ListAttemptInputAdmissions("attempt")
			if e != nil {
				return e
			}
			if len(admissions) != 1 || admissions[0].State != work.InputAdmissionNotSent {
				return fmt.Errorf("bad unsent state: %+v", admissions)
			}
			input, e := tx.GetInbox("late:input")
			if e != nil {
				return e
			}
			if input.State != work.InboxPending {
				return fmt.Errorf("unsent input lost: %s", input.State)
			}
			return nil
		})
		if e := f.lane.Tick(t.Context(), 10); e != nil {
			t.Fatal(e)
		}
		if len(f.driver.sent) != 0 {
			t.Fatal("unexpected retry")
		}
	})
}

func TestProjectAdmissionRejectedDoesNotRetrySameAttempt(t *testing.T) {
	f := newTeamFixture(t, work.BackendCodex)
	f.message(t, "late", "")
	if e := f.lane.Tick(t.Context(), 10); e != nil {
		t.Fatal(e)
	}
	cmd := f.driver.sent[0]
	f.event(t, agentproto.Event{Kind: agentproto.EventNativeTurnInputRejected, CommandID: cmd.CommandID, RequestID: "native-rpc", ThreadID: "session", TurnID: "turn", Status: "rejected", Execution: cmd.Execution})
	if e := f.lane.Tick(t.Context(), 10); e != nil {
		t.Fatal(e)
	}
	if len(f.driver.sent) != 1 {
		t.Fatal("rejected input repeatedly injected")
	}
	f.tx(t, func(tx *workstore.Tx) error {
		input, e := tx.GetInbox("late:input")
		if e != nil {
			return e
		}
		if input.State != work.InboxPending {
			return fmt.Errorf("rejected input not pending: %s", input.State)
		}
		return nil
	})
}

type sdkDirectory struct{}

func (sdkDirectory) ResolveGateway(context.Context, string, string) (work.IngressGateway, error) {
	return work.IngressGateway{GatewayRef: "gateway", AppID: "app", RoleID: "role", BotGeneration: 1}, nil
}
func (sdkDirectory) ResolveLeader(context.Context, string, string, string) (string, error) {
	return "role", nil
}
func (sdkDirectory) ResolveMention(context.Context, work.IngressGateway, work.RawMention) (string, error) {
	return "", fmt.Errorf("no mention")
}
func TestGroupHumanMessageSteersLeaderOnlyWithSDKProof(t *testing.T) {
	for _, backend := range []work.Backend{work.BackendCodex, work.BackendClaude} {
		t.Run(string(backend), func(t *testing.T) {
			f := newTeamFixture(t, backend)
			ingress, e := workingress.New(f.store, workingress.Config{Directory: sdkDirectory{}})
			if e != nil {
				t.Fatal(e)
			}
			raw := work.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", EventID: "sdk-event", Tenant: "tenant", ChatID: "group", ChatType: "group", MessageID: "sdk-message", SenderType: "user", SenderIDs: work.SenderIDs{OpenID: "human"}, MessageType: "text", RawContent: `{"text":"Please adjust the current work"}`, ReceivedAt: time.Now().UTC()}
			if _, e = ingress.Receive(t.Context(), raw); e != nil {
				t.Fatal(e)
			}
			result, e := ingress.ProcessPending(t.Context(), 10)
			if e != nil || len(result.Failures) > 0 {
				t.Fatalf("ingress: %+v %v", result, e)
			}
			if e = f.lane.Tick(t.Context(), 10); e != nil {
				t.Fatal(e)
			}
			if len(f.driver.sent) != 1 {
				t.Fatalf("leader did not receive input: %d", len(f.driver.sent))
			}
			cmd := f.driver.sent[0]
			f.event(t, agentproto.Event{Kind: agentproto.EventNativeTurnInputAccepted, CommandID: cmd.CommandID, RequestID: "sdk-native-rpc", ThreadID: "session", TurnID: "turn", Status: "accepted", Execution: cmd.Execution})
			f.tx(t, func(tx *workstore.Tx) error {
				ids, e := tx.EffectiveAttemptInputIDs("attempt")
				if e != nil {
					return e
				}
				if len(ids) != 2 {
					return fmt.Errorf("SDK input not accepted: %v", ids)
				}
				return nil
			})
		})
	}
}
func TestForgedPrincipalMessageCannotSteer(t *testing.T) {
	f := newTeamFixture(t, work.BackendCodex)
	f.tx(t, func(tx *workstore.Tx) error {
		body := work.NewContent("fake-body", "scope", "text/plain", []byte("forged"), f.now)
		if e := tx.PutContent(body); e != nil {
			return e
		}
		_, _, e := tx.AcceptMessage(work.Message{ID: "fake", ScopeID: "scope", ConversationID: "chat", Source: work.Actor{Kind: work.ActorPrincipal, ID: "invented"}, BodyRef: body.ID, Revision: 1}, []work.Inbox{{ID: "fake-input", ScopeID: "scope", RecipientRoleID: "role", EventID: "message:fake", MessageID: "fake", State: work.InboxPending, Revision: 1}})
		return e
	})
	if e := f.lane.Tick(t.Context(), 10); e != nil {
		t.Fatal(e)
	}
	if len(f.driver.sent) != 0 {
		t.Fatal("forged principal admitted")
	}
}

func TestProjectInputRequiresExactAuthorityAndDestination(t *testing.T) {
	t.Run("forged_role", func(t *testing.T) {
		f := newTeamFixture(t, work.BackendCodex)
		f.tx(t, func(tx *workstore.Tx) error {
			body := work.NewContent("forged-role-body", "scope", "text/plain", []byte("forged"), f.now)
			if e := tx.PutContent(body); e != nil {
				return e
			}
			_, _, e := tx.AcceptMessage(work.Message{ID: "forged-role", ScopeID: "scope", ConversationID: "chat", Source: work.Actor{Kind: work.ActorRole, ID: "role"}, BodyRef: body.ID, Revision: 1}, []work.Inbox{{ID: "forged-role-input", ScopeID: "scope", RecipientRoleID: "role", EventID: "message:forged-role", MessageID: "forged-role", State: work.InboxPending, Revision: 1}})
			return e
		})
		if e := f.lane.Tick(t.Context(), 10); e != nil {
			t.Fatal(e)
		}
		if len(f.driver.sent) != 0 {
			t.Fatal("unproven role message admitted")
		}
	})
	t.Run("member_main_does_not_guess_other_context", func(t *testing.T) {
		f := newTeamFixture(t, work.BackendCodex)
		f.message(t, "late", "")
		f.tx(t, func(tx *workstore.Tx) error {
			return tx.PutContext(work.WorkContext{ID: "other-context", ScopeID: "scope", RoleID: "role", Workstream: "other", Revision: 1}, 0)
		})
		if e := f.lane.Tick(t.Context(), 10); e != nil {
			t.Fatal(e)
		}
		if len(f.driver.sent) != 1 || f.driver.sent[0].Execution.ContextID != "context" {
			t.Fatal("member main was not selected exactly")
		}
	})
	t.Run("explicit_context", func(t *testing.T) {
		f := newTeamFixture(t, work.BackendCodex)
		f.message(t, "late", "context")
		f.tx(t, func(tx *workstore.Tx) error {
			return tx.PutContext(work.WorkContext{ID: "another-main", ScopeID: "scope", RoleID: "role", Workstream: "other", Revision: 1}, 0)
		})
		if e := f.lane.Tick(t.Context(), 10); e != nil {
			t.Fatal(e)
		}
		if len(f.driver.sent) != 1 {
			t.Fatal("explicit context was not admitted")
		}
	})
}

func TestOldAdmissionPayloadStillValid(t *testing.T) {
	raw := `{"id":"a","command_id":"cmd","scope_id":"s","context_id":"c","attempt_id":"attempt","run_ref":{"worker_id":"w","generation":1,"run_id":"run"},"native_session_id":"session","native_turn_id":"turn","input_ids":["input"],"source":{"kind":"message","id":"m","version":"1","evidence_ref":"body"},"state":"sending","revision":1,"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}`
	var admission work.AttemptInputAdmission
	if err := json.Unmarshal([]byte(raw), &admission); err != nil {
		t.Fatal(err)
	}
	if err := admission.Validate(); err != nil {
		t.Fatalf("old payload rejected: %v", err)
	}
}

func boolRevision(id string) int64 {
	if id != "" {
		return 1
	}
	return 0
}

func TestGroupTaskThreadReachesBusyTaskOwner(t *testing.T) {
	f := newTeamFixture(t, work.BackendCodex, true)
	ingress, e := workingress.New(f.store, workingress.Config{Directory: sdkDirectory{}})
	if e != nil {
		t.Fatal(e)
	}
	raw := work.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", EventID: "root-event", Tenant: "tenant", ChatID: "group", ChatType: "group", MessageID: "root", SenderType: "user", SenderIDs: work.SenderIDs{OpenID: "human"}, MessageType: "text", RawContent: `{"text":"Project"}`, ReceivedAt: time.Now().UTC()}
	if _, e = ingress.Receive(t.Context(), raw); e != nil {
		t.Fatal(e)
	}
	if result, e := ingress.ProcessPending(t.Context(), 10); e != nil || len(result.Failures) > 0 {
		t.Fatalf("root: %+v %v", result, e)
	}
	f.tx(t, func(tx *workstore.Tx) error {
		return tx.PutTaskConversation(work.TaskConversation{ScopeID: "scope", TaskID: "task", Tenant: "tenant", ChatID: "group", RootMessageID: "root", ChannelGeneration: 1, Revision: 1}, 0)
	})
	raw.EventID = "reply-event"
	raw.MessageID = "reply"
	raw.RootID = "root"
	raw.ParentID = "root"
	raw.ReceivedAt = time.Now().UTC()
	raw.RawContent = `{"text":"Apply this change to the task"}`
	if _, e = ingress.Receive(t.Context(), raw); e != nil {
		t.Fatal(e)
	}
	if result, e := ingress.ProcessPending(t.Context(), 10); e != nil || len(result.Failures) > 0 {
		t.Fatalf("reply: %+v %v", result, e)
	}
	if e = f.lane.Tick(t.Context(), 10); e != nil {
		t.Fatal(e)
	}
	if len(f.driver.sent) != 1 {
		t.Fatalf("task message not sent: %d", len(f.driver.sent))
	}
	cmd := f.driver.sent[0]
	f.event(t, agentproto.Event{Kind: agentproto.EventNativeTurnInputAccepted, CommandID: cmd.CommandID, RequestID: "task-native-rpc", ThreadID: "session", TurnID: "turn", Status: "accepted", Execution: cmd.Execution})
	f.tx(t, func(tx *workstore.Tx) error {
		ids, e := tx.EffectiveAttemptInputIDs("attempt")
		if e != nil {
			return e
		}
		if len(ids) != 2 {
			return fmt.Errorf("task input not accepted: %v", ids)
		}
		return nil
	})
}

func TestProjectInputsPreserveArrivalOrderAndWaitForReceipt(t *testing.T) {
	f := newTeamFixture(t, work.BackendCodex)
	f.message(t, "z-first", "")
	f.message(t, "a-second", "")
	if e := f.lane.Tick(t.Context(), 10); e != nil {
		t.Fatal(e)
	}
	if len(f.driver.sent) != 1 {
		t.Fatalf("expected one pending send, got %d", len(f.driver.sent))
	}
	first := f.driver.sent[0]
	if first.NativeInputAdmission.InputIDs[0] != "z-first:input" {
		t.Fatalf("opaque IDs reordered: %+v", first.NativeInputAdmission.InputIDs)
	}
	if e := f.lane.Tick(t.Context(), 10); e != nil {
		t.Fatal(e)
	}
	if len(f.driver.sent) != 1 {
		t.Fatal("sent next input before native confirmation")
	}
	f.event(t, agentproto.Event{Kind: agentproto.EventNativeTurnInputAccepted, CommandID: first.CommandID, RequestID: "first-rpc", ThreadID: "session", TurnID: "turn", Status: "accepted", Execution: first.Execution})
	if e := f.lane.Tick(t.Context(), 10); e != nil {
		t.Fatal(e)
	}
	if len(f.driver.sent) != 2 || f.driver.sent[1].NativeInputAdmission.InputIDs[0] != "a-second:input" {
		t.Fatal("second input did not follow confirmed first")
	}
}

func TestTaskRevisionChangeBetweenSelectionAndAdmissionStaysPending(t *testing.T) {
	f := newTeamFixture(t, work.BackendCodex, true)
	f.message(t, "late", "", "task")
	var candidate workstore.ProjectSteerCandidate
	f.tx(t, func(tx *workstore.Tx) error {
		var e error
		candidate, e = tx.ProjectSteerCandidateFor("late:input", time.Now().UTC())
		return e
	})
	f.tx(t, func(tx *workstore.Tx) error {
		task, e := tx.GetTask("task")
		if e != nil {
			return e
		}
		task.Title = "revised"
		task.RequirementsRevision++
		task.Revision++
		return tx.PutTask(task, task.Revision-1)
	})
	if e := f.lane.dispatch(t.Context(), candidate); e == nil {
		t.Fatal("stale task accepted")
	}
	f.tx(t, func(tx *workstore.Tx) error {
		input, e := tx.GetInbox("late:input")
		if e != nil {
			return e
		}
		if input.State != work.InboxPending {
			return fmt.Errorf("stale task input lost: %+v", input)
		}
		return nil
	})
}

type expertMentionDirectory struct{ sdkDirectory }

func (expertMentionDirectory) ResolveMention(context.Context, work.IngressGateway, work.RawMention) (string, error) {
	return "role", nil
}
func TestGroupMentionExpertReachesBusyMember(t *testing.T) {
	f := newTeamFixture(t, work.BackendCodex)
	f.tx(t, func(tx *workstore.Tx) error {
		if e := tx.PutRole(work.Role{ID: "leader", Name: "Leader", Description: "lead", DescriptionVersion: 1, SettingsRevision: 1, Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutTeamMember(work.TeamMember{TeamID: "team", RoleID: "leader", Active: true, Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutProjectMember(work.ProjectMember{ProjectID: "project", RoleID: "leader", State: work.ProjectMemberReady, BotGeneration: 1, Revision: 1}, 0); e != nil {
			return e
		}
		project, e := tx.GetProject("project")
		if e != nil {
			return e
		}
		project.LeaderRoleID = "leader"
		project.Revision++
		return tx.PutProject(project, project.Revision-1)
	})
	ingress, e := workingress.New(f.store, workingress.Config{Directory: expertMentionDirectory{}})
	if e != nil {
		t.Fatal(e)
	}
	raw := work.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", EventID: "mention-event", Tenant: "tenant", ChatID: "group", ChatType: "group", MessageID: "mention", SenderType: "user", SenderIDs: work.SenderIDs{OpenID: "human"}, MessageType: "text", RawContent: `{"text":"@expert check this"}`, Mentions: []work.RawMention{{Key: "expert", Type: "bot", IDs: work.SenderIDs{OpenID: "bot"}}}, ReceivedAt: time.Now().UTC()}
	if _, e = ingress.Receive(t.Context(), raw); e != nil {
		t.Fatal(e)
	}
	if result, e := ingress.ProcessPending(t.Context(), 10); e != nil || len(result.Failures) > 0 {
		t.Fatalf("mention: %+v %v", result, e)
	}
	if e = f.lane.Tick(t.Context(), 10); e != nil {
		t.Fatal(e)
	}
	if len(f.driver.sent) != 1 || f.driver.sent[0].Execution.RoleID != "role" {
		t.Fatalf("expert not targeted: %+v", f.driver.sent)
	}
	cmd := f.driver.sent[0]
	f.event(t, agentproto.Event{Kind: agentproto.EventNativeTurnInputAccepted, CommandID: cmd.CommandID, RequestID: "mention-rpc", ThreadID: "session", TurnID: "turn", Status: "accepted", Execution: cmd.Execution})
}

func TestRejectedInputIsAvailableToNextAttempt(t *testing.T) {
	f := newTeamFixture(t, work.BackendCodex)
	f.message(t, "late", "")
	f.message(t, "later", "")
	if e := f.lane.Tick(t.Context(), 10); e != nil {
		t.Fatal(e)
	}
	cmd := f.driver.sent[0]
	f.event(t, agentproto.Event{Kind: agentproto.EventNativeTurnInputRejected, CommandID: cmd.CommandID, RequestID: "rejected-rpc", ThreadID: "session", TurnID: "turn", Status: "rejected", Execution: cmd.Execution})
	if e := f.lane.Tick(t.Context(), 10); e != nil {
		t.Fatal(e)
	}
	if len(f.driver.sent) != 1 {
		t.Fatal("later input overtook rejected input")
	}
	f.tx(t, func(tx *workstore.Tx) error {
		body := work.NewContent("finished-body", "scope", "text/plain", []byte("finished"), f.now)
		if e := tx.PutContent(body); e != nil {
			return e
		}
		return tx.PutProcessingReport(workstore.ProcessingReport{AttemptID: "attempt", RunRef: work.RunRef{WorkerID: "worker", Generation: 1, RunID: "run"}, ProcessedInputIDs: []string{"initial"}, OutcomeRef: body.ID, Revision: 1}, 0)
	})
	f.event(t, agentproto.Event{Kind: agentproto.EventTurnCompleted, CommandID: "start", ThreadID: "session", TurnID: "turn", Status: "completed", TurnCompletionOrigin: agentproto.TurnCompletionOriginRuntime})
	f.tx(t, func(tx *workstore.Tx) error {
		worker, e := tx.GetWorker("worker")
		if e != nil {
			return e
		}
		input, e := tx.GetInbox("late:input")
		if e != nil {
			return e
		}
		if input.State != work.InboxPending {
			return fmt.Errorf("rejected input missing: %s", input.State)
		}
		pending, e := tx.ListProjectPendingInArrivalOrder("scope", "role")
		if e != nil {
			return e
		}
		if len(pending) != 2 || pending[0].ID != "late:input" || pending[1].ID != "later:input" {
			return fmt.Errorf("pending order lost: %+v", pending)
		}
		binding := agentproto.ExecutionBinding{Version: 1, ScopeID: "scope", ContextID: "context", RoleID: "role", WorkerSessionID: "worker", SessionGeneration: 1, ControllerGeneration: 1, BotGeneration: 1, AttemptID: "next", RunRef: "next-run"}
		next := agentproto.Command{CommandID: "next-start", Kind: agentproto.CommandPromptSend, Execution: &binding, InstructionRevision: "v1", Target: agentproto.Target{ThreadID: "session", CWD: "/workspace"}, Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "next"}}}}
		raw, _ := json.Marshal(next)
		sum := sha256.Sum256(raw)
		body := work.NewContent("next-request", "scope", "application/json", raw, time.Now().UTC())
		if e = tx.PutContent(body); e != nil {
			return e
		}
		op := work.Operation{ID: "next-start", ScopeID: "scope", ContextID: "context", Actor: work.Actor{Kind: work.ActorRole, ID: "role"}, Kind: string(next.Kind), TargetID: "worker", TargetGeneration: 1, IdempotencyKey: "next-start", RequestHash: hex.EncodeToString(sum[:]), RequestRef: body.ID, State: work.OperationPrepared, Revision: 1}
		return tx.ClaimAttempt(workstore.ClaimRequest{Attempt: work.ExecutionAttempt{ID: "next", ContextID: "context", ScopeID: "scope", RunRef: work.RunRef{WorkerID: "worker", Generation: 1, RunID: "next-run"}, State: work.AttemptDispatchPending, InputIDs: []string{input.ID, pending[1].ID}, CommandID: "next-start", Revision: 1}, ExpectedWorkerRevision: worker.Revision, ControllerGeneration: 1, InstanceID: "instance", InstanceGeneration: 1, ProcessGeneration: 1, LocationGeneration: 1, StartOperation: op, Now: time.Now().UTC()})
	})
}
