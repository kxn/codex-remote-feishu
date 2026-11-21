package workprovision

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type provisionFixture struct {
	store                                     *workstore.Store
	path                                      string
	service                                   *Service
	world                                     *fakeWorld
	leader                                    *fakePlatform
	operationID, projectID, scopeID, sourceID string
	prepared                                  int
	now                                       time.Time
	options                                   Options
}

func newFixture(t *testing.T) *provisionFixture { return newFixtureForSDK(t, nil) }
func newFixtureForSDK(t *testing.T, identity *work.IdentityEvidence) *provisionFixture {
	principalID := "human"
	t.Helper()
	fixture := &provisionFixture{path: filepath.Join(t.TempDir(), "work.db"), now: time.Date(2026, 11, 21, 18, 0, 0, 0, time.UTC), sourceID: "original-human"}
	store, err := workstore.Open(context.Background(), fixture.path)
	if err != nil {
		t.Fatal(err)
	}
	fixture.store = store
	t.Cleanup(func() { fixture.store.Close() })
	fixture.world = &fakeWorld{present: map[string]bool{"app-leader": true}, platforms: map[string]*fakePlatform{}}
	for _, role := range []string{"leader", "expert"} {
		binding := work.RoleBotBinding{RoleID: role, GatewayRef: "gateway-" + role, AppID: "app-" + role, IdentityGeneration: 1}
		platform := &fakePlatform{world: fixture.world, binding: binding, messages: map[string]string{}, pins: map[string]bool{}}
		fixture.world.platforms[binding.AppID] = platform
		if role == "leader" {
			fixture.leader = platform
		}
	}
	err = store.Within(context.Background(), func(tx *workstore.Tx) error {
		if identity == nil {
			if err := tx.PutPrincipal(work.Principal{ID: principalID, DisplayName: "Human", Revision: 1}, 0); err != nil {
				return err
			}
		}

		for _, role := range []string{"leader", "expert"} {
			if err := tx.PutRole(work.Role{ID: role, Name: role, Description: "Responsibility", DescriptionVersion: 1, SettingsRevision: 1, Revision: 1}, 0); err != nil {
				return err
			}
			if err := tx.PutRoleBotBinding(fixture.world.platforms["app-"+role].binding, 0); err != nil {
				return err
			}
		}
		if err := tx.PutTeam(work.Team{ID: "team", Name: "Team", LeaderRoleID: "leader", Revision: 1}, 0); err != nil {
			return err
		}
		for _, role := range []string{"leader", "expert"} {
			if err := tx.PutTeamMember(work.TeamMember{TeamID: "team", RoleID: role, Active: true, Revision: 1}, 0); err != nil {
				return err
			}
		}
		if identity != nil {
			gateway := work.IngressGateway{GatewayRef: "gateway-leader", AppID: "app-leader", RoleID: "leader", BotGeneration: 1}
			envelope := work.RawIngressEnvelope{GatewayRef: gateway.GatewayRef, AppID: gateway.AppID, EventID: "actual-initial-sdk-event", Tenant: identity.Tenant, SenderTenant: identity.Tenant, SenderIDs: identity.IDs, ChatID: "project-chat", ChatType: "group", MessageID: "platform-original", MessageType: "text", SenderType: "user", RawContent: `{"text":"Build the actual project"}`, ReceivedAt: fixture.now, MessageCreatedAt: fixture.now}
			record, _, err := tx.AcceptRawIngress(envelope, gateway)
			if err != nil {
				return err
			}
			observed := *identity
			observed.ObservationRef = record.ID
			principal, _, err := tx.ResolvePrincipal(observed)
			if err != nil {
				return err
			}
			principalID = principal.ID
		}
		scope := work.WorkScope{ID: "bootstrap", Kind: work.ScopeBootstrap, RoleID: "leader", PrincipalID: principalID, Tenant: "tenant", ChatID: "project-chat", BootstrapOperationID: "create-request", Revision: 1}
		if err := tx.PutScope(scope, 0); err != nil {
			return err
		}
		if _, _, err := tx.PutOperation(work.Operation{ID: scope.BootstrapOperationID, ScopeID: scope.ID, Actor: work.Actor{Kind: work.ActorPrincipal, ID: principalID}, Kind: "project.bootstrap", TargetID: scope.ChatID, TargetGeneration: 1, IdempotencyKey: scope.BootstrapOperationID, RequestHash: "human-bootstrap", State: work.OperationPrepared, Revision: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutContext(work.WorkContext{ID: "bootstrap-context", ScopeID: scope.ID, RoleID: "leader", Workstream: "establish", Revision: 1}, 0); err != nil {
			return err
		}
		worker := work.WorkerSession{ID: "bootstrap-worker", ContextID: "bootstrap-context", ScopeID: scope.ID, RoleID: "leader", Backend: work.BackendCodex, State: work.WorkerReady, Generation: 1, Controller: work.ControllerAutomatic, ControllerGeneration: 1, InstanceID: "bootstrap-carrier", InstanceGeneration: 1, ProcessGeneration: 1, Revision: 1}
		if err := tx.PutWorker(worker, 0); err != nil {
			return err
		}
		body := work.NewContent("human-original-body", scope.ID, "text/plain", []byte("Human original request: preserve these exact details."), fixture.now)
		if err := tx.PutContent(body); err != nil {
			return err
		}
		message := work.Message{ID: fixture.sourceID, ScopeID: scope.ID, ConversationID: scope.ChatID, Source: work.Actor{Kind: work.ActorPrincipal, ID: principalID}, PlatformKey: &work.MessageKey{Tenant: "tenant", ChatID: scope.ChatID, MessageID: "platform-original"}, BodyRef: body.ID, Revision: 1, CreatedAt: fixture.now}
		inbox := work.Inbox{ID: "bootstrap-input", ScopeID: scope.ID, RecipientRoleID: "leader", RecipientContextID: worker.ContextID, EventID: "original", State: work.InboxPending, Revision: 1}
		if _, _, err := tx.AcceptMessage(message, []work.Inbox{inbox}); err != nil {
			return err
		}
		attempt := work.ExecutionAttempt{ID: "bootstrap-attempt", ScopeID: scope.ID, ContextID: worker.ContextID, RunRef: work.RunRef{WorkerID: worker.ID, Generation: 1, RunID: "bootstrap-run"}, State: work.AttemptDispatchPending, CommandID: "bootstrap-start", InputIDs: []string{inbox.ID}, Revision: 1}
		start := work.Operation{ID: attempt.CommandID, ScopeID: scope.ID, ContextID: worker.ContextID, Actor: work.Actor{Kind: work.ActorSystem, ID: "scheduler"}, Kind: "native.start", TargetID: worker.ID, TargetGeneration: 1, IdempotencyKey: attempt.CommandID, RequestHash: "start", State: work.OperationPrepared, Revision: 1}
		if err := tx.ClaimAttempt(workstore.ClaimRequest{Attempt: attempt, ExpectedWorkerRevision: 1, ControllerGeneration: 1, InstanceID: worker.InstanceID, InstanceGeneration: 1, ProcessGeneration: 1, StartOperation: start, Now: fixture.now}); err != nil {
			return err
		}
		attempt.State = work.AttemptRunning
		attempt.Revision++
		return tx.PutAttempt(attempt, 1)
	})
	if err != nil {
		t.Fatal(err)
	}
	signer, err := workauth.New([]byte("0123456789abcdef0123456789abcdef"), func() time.Time { return fixture.now })
	if err != nil {
		t.Fatal(err)
	}
	service, err := workservice.New(store, signer, func() time.Time { return fixture.now })
	if err != nil {
		t.Fatal(err)
	}
	token, err := signer.Issue(workauth.Claims{Purpose: "worker", RoleID: "leader", BotGeneration: 1, ScopeID: "bootstrap", ContextID: "bootstrap-context", WorkerID: "bootstrap-worker", SessionGeneration: 1, ControllerGeneration: 1}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := service.CreateProjectInRun(context.Background(), token, "bootstrap-run", workservice.CreateProjectRequest{Key: "create-request", Tenant: "tenant", ChatID: "project-chat", PrincipalID: principalID, Name: "Project", TeamID: "team", LeaderRoleID: "leader", MemberRoleIDs: []string{"expert"}, SourceMessageID: fixture.sourceID, InitialRequest: "MODEL PARAPHRASE MUST NOT REPLACE ORIGINAL HUMAN TEXT"})
	if err != nil {
		t.Fatal(err)
	}
	fixture.operationID = accepted.OperationID
	fixture.projectID = accepted.ObjectID
	err = store.Within(context.Background(), func(tx *workstore.Tx) error {
		project, err := tx.GetProject(fixture.projectID)
		fixture.scopeID = project.ScopeID
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	fixture.options = Options{Resolver: ResolverFunc(func(_ context.Context, binding work.RoleBotBinding) (Platform, error) {
		return fixture.world.platforms[binding.AppID], nil
	}), Execution: ExecutionPreparerFunc(func(_ context.Context, _ work.Project, ids []string) error {
		if len(ids) != 0 {
			t.Error("unexpected selected user directory")
		}
		fixture.prepared++
		return nil
	}), Now: func() time.Time { return fixture.now }}
	fixture.service, err = New(store, fixture.options)
	if err != nil {
		t.Fatal(err)
	}
	return fixture
}
