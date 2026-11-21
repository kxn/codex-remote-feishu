package workservice

import (
	"context"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func lifecycleProjectService(t *testing.T, bootstrap bool, admittedHumans ...string) (*Service, string, string) {
	t.Helper()
	s, _, _ := runningService(t)
	scope := work.WorkScope{ID: "project-scope", Kind: work.ScopeProject, ProjectID: "project", TeamID: "team", Revision: 1}
	if bootstrap {
		scope = work.WorkScope{ID: "bootstrap-scope", Kind: work.ScopeBootstrap, RoleID: "leader", PrincipalID: "human", Tenant: "tenant", ChatID: "group", BootstrapOperationID: "bootstrap-create", Revision: 1}
	}
	err := s.store.Within(context.Background(), func(tx *workstore.Tx) error {
		if err := tx.PutRole(work.Role{ID: "expert", Name: "Expert", Description: "Review", DescriptionVersion: 1, SettingsRevision: 1, Revision: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutRoleBotBinding(work.RoleBotBinding{RoleID: "expert", GatewayRef: "expert-gateway", AppID: "expert-app", IdentityGeneration: 1}, 0); err != nil {
			return err
		}
		if !bootstrap {
			if err := tx.PutTeam(work.Team{ID: "team", Name: "Team", LeaderRoleID: "leader", Revision: 1}, 0); err != nil {
				return err
			}
			for _, role := range []string{"leader", "expert"} {
				if err := tx.PutTeamMember(work.TeamMember{TeamID: "team", RoleID: role, Active: true, Revision: 1}, 0); err != nil {
					return err
				}
			}
		}
		if err := tx.PutScope(scope, 0); err != nil {
			return err
		}
		if bootstrap {
			if _, _, err := tx.PutOperation(work.Operation{ID: scope.BootstrapOperationID, ScopeID: scope.ID, Actor: work.Actor{Kind: work.ActorPrincipal, ID: scope.PrincipalID}, Kind: "project.bootstrap", TargetID: scope.ChatID, TargetGeneration: 1, IdempotencyKey: scope.BootstrapOperationID, RequestHash: "human-source", State: work.OperationPrepared, Revision: 1}, 0); err != nil {
				return err
			}
		}
		if !bootstrap {
			if err := tx.PutProject(work.Project{ID: "project", ScopeID: scope.ID, TeamID: scope.TeamID, Name: "Product", LeaderRoleID: "leader", State: work.ProjectActive, Revision: 1}, 0); err != nil {
				return err
			}
			if err := tx.PutProjectChannelBinding(work.ProjectChannelBinding{ProjectID: "project", Tenant: "tenant", ChatID: "group", Purpose: "main", BindingGeneration: 1, Active: true}, 0); err != nil {
				return err
			}
			for _, role := range []string{"leader", "expert"} {
				if err := tx.PutProjectMember(work.ProjectMember{ProjectID: "project", RoleID: role, State: work.ProjectMemberReady, BotGeneration: 1, Revision: 1}, 0); err != nil {
					return err
				}
			}
		}
		if err := tx.PutContext(work.WorkContext{ID: "lifecycle-context", ScopeID: scope.ID, RoleID: "leader", Workstream: "lifecycle", Revision: 1}, 0); err != nil {
			return err
		}
		worker := work.WorkerSession{ID: "lifecycle-worker", ContextID: "lifecycle-context", ScopeID: scope.ID, RoleID: "leader", Backend: work.BackendCodex, State: work.WorkerReady, Generation: 1, Controller: work.ControllerAutomatic, ControllerGeneration: 1, InstanceID: "lifecycle-carrier", InstanceGeneration: 1, ProcessGeneration: 1, Revision: 1}
		if err := tx.PutWorker(worker, 0); err != nil {
			return err
		}
		body := work.NewContent("lifecycle-human-body", scope.ID, "text/plain", []byte("Change the responsibilities and manage this project/team as requested."), s.now())
		if err := tx.PutContent(body); err != nil {
			return err
		}
		message := work.Message{ID: "lifecycle-human", ScopeID: scope.ID, ConversationID: "group", Source: work.Actor{Kind: work.ActorPrincipal, ID: "human"}, PlatformKey: &work.MessageKey{Tenant: "tenant", ChatID: "group", MessageID: "original-human"}, BodyRef: body.ID, Revision: 1}
		inbox := work.Inbox{ID: "lifecycle-input", ScopeID: scope.ID, RecipientRoleID: "leader", RecipientContextID: worker.ContextID, EventID: "original-human", State: work.InboxPending, Revision: 1}
		if _, _, err := tx.AcceptMessage(message, []work.Inbox{inbox}); err != nil {
			return err
		}
		inputIDs := []string{inbox.ID}
		for _, name := range admittedHumans {
			binding, e := tx.GetRoleBotBinding("leader")
			if e != nil {
				return e
			}
			gateway := work.IngressGateway{RoleID: binding.RoleID, GatewayRef: binding.GatewayRef, AppID: binding.AppID, BotGeneration: binding.IdentityGeneration}
			raw := work.RawIngressEnvelope{GatewayRef: binding.GatewayRef, AppID: binding.AppID, EventID: name, Tenant: "tenant", ChatID: "group", ChatType: "group", MessageID: name, SenderType: "user", SenderIDs: work.SenderIDs{OpenID: name}, MessageType: "text", RawContent: `{"text":"Participation instruction"}`, MessageCreatedAt: s.now(), ReceivedAt: s.now()}
			record, _, e := tx.AcceptRawIngress(raw, gateway)
			if e != nil {
				return e
			}
			person, _, e := tx.ResolvePrincipal(work.IdentityEvidence{Tenant: raw.Tenant, AppID: raw.AppID, Generation: gateway.BotGeneration, IDs: raw.SenderIDs, ObservationRef: record.ID})
			if e != nil {
				return e
			}
			if e = tx.GrantProjectAccessFromIngress(scope.ID, person.ID, record.ID, s.now().Add(time.Hour)); e != nil {
				return e
			}
			personMessage := message
			personMessage.ID = name
			personMessage.Source.ID = person.ID
			personMessage.ConversationID = "conversation:" + name
			personMessage.PlatformKey = &work.MessageKey{Tenant: raw.Tenant, ChatID: raw.ChatID, MessageID: raw.MessageID}
			personInbox := inbox
			personInbox.ID, personInbox.EventID = "input-"+name, name
			if _, _, e = tx.AcceptMessage(personMessage, []work.Inbox{personInbox}); e != nil {
				return e
			}
			inputIDs = append(inputIDs, personInbox.ID)
		}
		attempt := work.ExecutionAttempt{ID: "lifecycle-attempt", ScopeID: scope.ID, ContextID: worker.ContextID, RunRef: work.RunRef{WorkerID: worker.ID, Generation: 1, RunID: "lifecycle-run"}, CommandID: "lifecycle-start", State: work.AttemptDispatchPending, InputIDs: inputIDs, Revision: 1}
		op := work.Operation{ID: attempt.CommandID, ScopeID: scope.ID, ContextID: worker.ContextID, Actor: work.Actor{Kind: work.ActorSystem, ID: "scheduler"}, Kind: "native.start", TargetID: worker.ID, TargetGeneration: 1, IdempotencyKey: attempt.CommandID, RequestHash: "start", State: work.OperationPrepared, Revision: 1}
		if err := tx.ClaimAttempt(workstore.ClaimRequest{Attempt: attempt, ExpectedWorkerRevision: 1, ControllerGeneration: 1, InstanceID: worker.InstanceID, InstanceGeneration: 1, ProcessGeneration: 1, StartOperation: op, Now: s.now()}); err != nil {
			return err
		}
		attempt.State = work.AttemptRunning
		attempt.Revision++
		return tx.PutAttempt(attempt, 1)
	})
	if err != nil {
		t.Fatal(err)
	}
	token, err := s.signer.Issue(workauth.Claims{Purpose: "worker", RoleID: "leader", BotGeneration: 1, ScopeID: scope.ID, ContextID: "lifecycle-context", WorkerID: "lifecycle-worker", SessionGeneration: 1, ControllerGeneration: 1}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return s, token, "lifecycle-run"
}
