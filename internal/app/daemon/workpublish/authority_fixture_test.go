package workpublish

import (
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func testPublicationAuthority(tx *workstore.Tx, scopeID, roleID, taskID, publicationID, tenant, chatID string, botGeneration int64) (string, *workservice.PublicationAuthority, error) {
	scope, err := tx.GetScope(scopeID)
	if err != nil {
		return "", nil, err
	}
	prefix := "publication-fixture:" + publicationID
	wc := work.WorkContext{ID: prefix + ":context", ScopeID: scope.ID, RoleID: roleID, Workstream: prefix, Revision: 1}
	if err = tx.PutContext(wc, 0); err != nil {
		return "", nil, err
	}
	worker := work.WorkerSession{
		ID: prefix + ":worker", ContextID: wc.ID, ScopeID: scope.ID, RoleID: roleID, Backend: work.BackendCodex,
		State: work.WorkerReady, Generation: 1, Controller: work.ControllerAutomatic, ControllerGeneration: 1,
		InstanceID: prefix + ":carrier", InstanceGeneration: 1, ProcessGeneration: 1, Revision: 1,
	}
	if err = tx.PutWorker(worker, 0); err != nil {
		return "", nil, err
	}
	input := work.Inbox{ID: prefix + ":input", ScopeID: scope.ID, RecipientContextID: wc.ID, RecipientRoleID: roleID, EventID: prefix, State: work.InboxPending, Revision: 1}
	authority := &workservice.PublicationAuthority{Content: "message", AttemptID: prefix + ":attempt"}
	switch scope.Kind {
	case work.ScopePersonal:
		authority.Kind = "private"
		binding, e := tx.GetRoleBotBinding(roleID)
		if e != nil {
			return "", nil, e
		}
		content := work.NewContent(prefix+":source", scope.ID, "text/plain", []byte("please send"), time.Now())
		if e = tx.PutContent(content); e != nil {
			return "", nil, e
		}
		message := work.Message{
			ID: prefix + ":message", ScopeID: scope.ID, ConversationID: "fixture-publication",
			Source:      work.Actor{Kind: work.ActorPrincipal, ID: scope.PrincipalID},
			PlatformKey: &work.MessageKey{Tenant: tenant, ChatID: chatID, MessageID: prefix + ":external"},
			BodyRef:     content.ID, Observations: []work.Observation{{AppID: binding.AppID, IdentityGeneration: botGeneration}}, Revision: 1,
		}
		if _, _, e = tx.AcceptMessage(message, []work.Inbox{input}); e != nil {
			return "", nil, e
		}
	case work.ScopeBootstrap:
		authority.Kind = "bootstrap"
		if e := tx.PutInbox(input, 0); e != nil {
			return "", nil, e
		}
	case work.ScopeProject:
		authority.Kind = "leader"
		project, e := tx.GetProject(scope.ProjectID)
		if e != nil {
			return "", nil, e
		}
		authority.LeadershipGeneration = project.LeadershipGeneration()
		member, e := tx.GetProjectMember(scope.ProjectID, roleID)
		if e != nil {
			return "", nil, e
		}
		team, e := tx.GetTeamMember(scope.TeamID, roleID)
		if e != nil {
			return "", nil, e
		}
		authority.Participant = work.PublicationMemberAuthority{RoleID: roleID, TeamRevision: team.Revision, ProjectRevision: member.Revision, BotGeneration: botGeneration}
		if e = tx.PutInbox(input, 0); e != nil {
			return "", nil, e
		}
	}
	if taskID != "" {
		task, e := tx.GetTask(taskID)
		if e != nil {
			return "", nil, e
		}
		authority.TargetTask = work.PublicationTaskAuthority{TaskID: task.ID, OwnerRoleID: task.OwnerRoleID, RequirementsRevision: task.RequirementsRevision, AssignmentGeneration: task.AssignmentGeneration}
	}
	attempt := work.ExecutionAttempt{
		ID: authority.AttemptID, ContextID: wc.ID, ScopeID: scope.ID,
		RunRef: work.RunRef{WorkerID: worker.ID, Generation: 1, RunID: prefix + ":run"}, State: work.AttemptDispatchPending,
		InputIDs: []string{input.ID}, CommandID: prefix + ":start", Revision: 1,
	}
	start := work.Operation{
		ID: attempt.CommandID, ScopeID: scope.ID, ContextID: wc.ID, Actor: work.Actor{Kind: work.ActorSystem, ID: "scheduler"},
		Kind: "native.start", TargetID: worker.ID, TargetGeneration: 1, IdempotencyKey: attempt.CommandID,
		RequestHash: attempt.CommandID, State: work.OperationPrepared, Revision: 1,
	}
	if err = tx.ClaimAttempt(workstore.ClaimRequest{
		Attempt: attempt, ExpectedWorkerRevision: 1, ControllerGeneration: 1,
		InstanceID: worker.InstanceID, InstanceGeneration: 1, ProcessGeneration: 1, StartOperation: start, Now: time.Now(),
	}); err != nil {
		return "", nil, err
	}
	attempt.State, attempt.Revision = work.AttemptRunning, 2
	if err = tx.PutAttempt(attempt, 1); err != nil {
		return "", nil, err
	}
	return wc.ID, authority, nil
}
