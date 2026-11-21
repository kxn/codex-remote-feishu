package workservice

import (
	"context"
	"fmt"
	"strconv"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type ProjectLeaderRequest struct {
	Key              string `json:"key"`
	ProjectID        string `json:"project_id"`
	ExpectedRevision int64  `json:"expected_revision"`
	LeaderRoleID     string `json:"leader_role_id"`
	SourceMessageID  string `json:"source_message_id"`
}

func (s *Service) ProjectLeaderChange(ctx context.Context, token, run string, req ProjectLeaderRequest) (Result, error) {
	var result Result
	err := s.withWorker(ctx, token, run, func(tx *workstore.Tx, c Caller) error {
		message, _, err := currentHumanInstruction(tx, c, req.SourceMessageID)
		if err != nil {
			return err
		}
		result, err = s.mutate(tx, c, "project.leader.change", req.Key, req.ProjectID, req, func() (string, int64, error) {
			project, err := currentProject(tx, c, req.ProjectID, true)
			if err != nil {
				return "", 0, err
			}
			if project.Revision != req.ExpectedRevision {
				return "", 0, workstore.ErrConflict
			}
			if err = ensureMember(tx, project.ScopeID, req.LeaderRoleID); err != nil {
				return "", 0, err
			}
			if project.LeaderRoleID != req.LeaderRoleID {
				if err = s.migrateProjectAuthority(tx, c, project, project.LeaderRoleID, req.LeaderRoleID, message, req.Key); err != nil {
					return "", 0, err
				}
			}
			project.LeaderRoleID = req.LeaderRoleID
			project.Revision++
			if err = tx.PutProject(project, req.ExpectedRevision); err != nil {
				return "", 0, err
			}
			return project.ID, project.Revision, nil
		})
		return err
	})
	return result, err
}
func (s *Service) migrateProjectAuthority(tx *workstore.Tx, c Caller, project work.Project, oldRole, newRole string, source work.Message, key string) error {
	agreements, err := tx.ListAgreements(project.ScopeID)
	if err != nil {
		return err
	}
	requests, err := tx.ListDecisionRequests(project.ScopeID)
	if err != nil {
		return err
	}
	oldActor := work.Actor{Kind: work.ActorRole, ID: oldRole}
	newActor := work.Actor{Kind: work.ActorRole, ID: newRole}
	for _, agreement := range agreements {
		if agreement.DecisionOwner != oldActor {
			continue
		}
		if newRole == "" {
			return fmt.Errorf("participation agreement %s needs an explicit successor", agreement.ID)
		}
		next := agreement
		next.Revision++
		next.DecisionOwner = newActor
		next.Source = humanSource(source)
		next.Initiator = source.Source
		change := workstore.ApplyAgreementRequest{Agreement: next, ExpectedRevision: agreement.Revision}
		for _, request := range requests {
			if request.Kind != work.DecisionBusiness || request.State != work.DecisionOpen || request.AgreementID != agreement.ID {
				continue
			}
			replacement := s.replaceLeadershipRequest(request, newActor, next.Revision, key)
			change.Replacements = append(change.Replacements, workstore.DecisionReplacement{OldRequestID: request.ID, ExpectedRevision: request.Revision, Replacement: replacement})
			change.Inbox = append(change.Inbox, s.leadershipInbox(replacement))
		}
		if err = tx.ApplyAgreement(change); err != nil {
			return err
		}
	}
	for _, request := range requests {
		if request.Kind != work.DecisionBusiness || request.State != work.DecisionOpen || request.AgreementID != "" || request.Owner != oldActor {
			continue
		}
		if newRole == "" {
			return fmt.Errorf("open decision %s needs an explicit successor", request.ID)
		}
		replacement := s.replaceLeadershipRequest(request, newActor, request.AgreementRevision, key)
		if err = tx.PutDecisionRequest(replacement, 0); err != nil {
			return err
		}
		old := request.Revision
		request.Revision++
		request.State = work.DecisionSuperseded
		request.SupersededBy = replacement.ID
		if err = tx.PutDecisionRequest(request, old); err != nil {
			return err
		}
		if err = tx.PutInbox(s.leadershipInbox(replacement), 0); err != nil {
			return err
		}
	}
	_ = c // Caller authority is verified by the enclosing mutation transaction.
	return nil
}
func (s *Service) replaceLeadershipRequest(old work.DecisionRequest, owner work.Actor, agreementRevision int64, key string) work.DecisionRequest {
	next := old
	next.ID = stableID("leadership_decision", old.ID, key, strconv.FormatInt(old.RequestGeneration+1, 10))
	next.Owner = owner
	next.AllowedActors = []work.Actor{owner}
	next.AgreementRevision = agreementRevision
	next.RequestGeneration++
	next.Revision = 1
	next.SupersededBy = ""
	next.ResponseOperationID = ""
	next.CreatedAt = s.now()
	return next
}
func (s *Service) leadershipInbox(request work.DecisionRequest) work.Inbox {
	return work.Inbox{ID: stableID("leadership_inbox", request.ID), ScopeID: request.ScopeID, TaskID: request.TaskID, RecipientRoleID: request.Owner.ID, EventID: "decision:" + request.ID, State: work.InboxPending, Revision: 1, AvailableAt: s.now()}
}
