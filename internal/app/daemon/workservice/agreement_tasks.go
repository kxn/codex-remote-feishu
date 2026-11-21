package workservice

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type AgreementTaskImpact struct {
	TaskID       string                `json:"task_id"`
	TaskRevision int64                 `json:"task_revision"`
	Point        string                `json:"participation_point"`
	Stage        string                `json:"stage"`
	Facts        string                `json:"observed_facts"`
	Choices      []work.DecisionChoice `json:"choices,omitempty"`
	References   []work.DeliveryRef    `json:"references,omitempty"`
}

type AgreementTaskOutcome struct {
	TaskID            string `json:"task_id"`
	Point             string `json:"participation_point"`
	Stage             string `json:"stage"`
	Facts             string `json:"observed_facts"`
	DecisionRequestID string `json:"decision_request_id,omitempty"`
}

const (
	agreementStageBefore   = "before_point"
	agreementStagePassed   = "point_passed"
	agreementNotApplicable = "not_applicable"
)

func (impact AgreementTaskImpact) outcome(agreementID string, revision int64) AgreementTaskOutcome {
	result := AgreementTaskOutcome{TaskID: impact.TaskID, Point: impact.Point, Stage: impact.Stage, Facts: impact.Facts}
	if impact.Stage == agreementStageBefore {
		result.DecisionRequestID = stableID("participation_decision", agreementID, strconv.FormatInt(revision, 10), impact.TaskID, impact.Point)
	}
	return result
}

func validateAgreementTaskImpacts(tx *workstore.Tx, c Caller, next work.ParticipationAgreement, impacts []AgreementTaskImpact) error {
	for _, impact := range impacts {
		task, err := tx.GetTask(impact.TaskID)
		if err != nil {
			return err
		}
		if err = mayManageTask(tx, c, task); err != nil {
			return err
		}
		if next.SubjectKind == work.AgreementItem && task.ID != next.SubjectID {
			return workauth.ErrUnauthorized
		}
	}
	return nil
}

func (s *Service) agreementChange(tx *workstore.Tx, next, old work.ParticipationAgreement, req AgreementApplyRequest, attemptID string) (workstore.ApplyAgreementRequest, []AgreementTaskOutcome, error) {
	change := workstore.ApplyAgreementRequest{Agreement: next, ExpectedRevision: req.ExpectedRevision, ReportingOnly: req.ReportingOnly}
	if req.ReportingOnly && (req.ExpectedRevision == 0 || len(req.TaskImpacts) != 0) {
		return change, nil, fmt.Errorf("reporting-only changes require an existing agreement and cannot create task decisions")
	}
	if err := validateAgreementImpactCoverage(tx, next, old, req.TaskImpacts); err != nil {
		return change, nil, err
	}
	if next.DecisionOwner.Kind == work.ActorPrincipal {
		if err := s.requireAgreementPrincipal(tx, next.ScopeID, next.DecisionOwner.ID); err != nil {
			return change, nil, err
		}
	}
	requests, err := tx.ListDecisionRequests(next.ScopeID)
	if err != nil {
		return change, nil, err
	}
	if req.ExpectedRevision > 0 && !req.ReportingOnly && (old.DecisionOwner != next.DecisionOwner || !reflect.DeepEqual(old.ParticipationPoints, next.ParticipationPoints)) {
		for _, request := range requests {
			if request.AgreementID != next.ID || request.State != work.DecisionOpen || request.Kind != work.DecisionBusiness {
				continue
			}
			if request.ParticipationGateID != "" && !containsAgreementPoint(next.ParticipationPoints, request.Presentation.Title) {
				change.Withdrawals = append(change.Withdrawals, request)
				continue
			}
			replacement := request
			replacement.ID = stableID("decision-request", request.ID, strconv.FormatInt(next.Revision, 10))
			replacement.Owner = next.DecisionOwner
			replacement.AllowedActors = []work.Actor{next.DecisionOwner}
			replacement.AgreementRevision = next.Revision
			replacement.RequestGeneration++
			replacement.Revision = 1
			replacement.CreatedAt = s.now()
			replacement.SupersededBy = ""
			replacement.ResponseOperationID = ""
			change.Replacements = append(change.Replacements, workstore.DecisionReplacement{OldRequestID: request.ID, ExpectedRevision: request.Revision, Replacement: replacement})
			if replacement.Owner.Kind == work.ActorRole {
				change.Inbox = append(change.Inbox, s.agreementDecisionInbox(replacement))
			}
		}
	}
	seen := map[[2]string]bool{}
	var outcomes []AgreementTaskOutcome
	for _, impact := range req.TaskImpacts {
		key := [2]string{impact.TaskID, impact.Point}
		if seen[key] {
			return change, nil, fmt.Errorf("duplicate participation point for task")
		}
		seen[key] = true
		outcome, err := s.addAgreementTaskImpact(tx, &change, old, attemptID, impact)
		if err != nil {
			return change, nil, err
		}
		outcomes = append(outcomes, outcome)
	}
	return change, outcomes, nil
}

func (s *Service) addAgreementTaskImpact(tx *workstore.Tx, change *workstore.ApplyAgreementRequest, old work.ParticipationAgreement, attemptID string, impact AgreementTaskImpact) (AgreementTaskOutcome, error) {
	next := change.Agreement
	outcome := impact.outcome(next.ID, next.Revision)
	task, err := tx.GetTask(impact.TaskID)
	if err != nil {
		return outcome, err
	}
	if task.ScopeID != next.ScopeID || (next.SubjectKind == work.AgreementItem && task.ID != next.SubjectID) {
		return outcome, workstore.ErrScopeDenied
	}
	if task.Revision != impact.TaskRevision {
		return outcome, workstore.ErrConflict
	}
	if !containsAgreementPoint(next.ParticipationPoints, impact.Point) || containsAgreementPoint(old.ParticipationPoints, impact.Point) || strings.TrimSpace(impact.Facts) == "" || len(impact.Facts) > 12*1024 {
		return outcome, fmt.Errorf("task impact needs an added point and bounded current facts")
	}
	for _, reference := range impact.References {
		if err = reference.Validate(); err != nil {
			return outcome, err
		}
		version, e := tx.GetResourceVersion(next.ScopeID, reference.VersionID)
		if e != nil {
			return outcome, e
		}
		if reference.Current || version.ResourceID != reference.ResourceID || !version.Saved || !version.Complete {
			return outcome, fmt.Errorf("stage evidence must name a saved, complete project resource version")
		}
	}
	switch impact.Stage {
	case agreementStagePassed, agreementNotApplicable:
		if len(impact.Choices) != 0 {
			return outcome, fmt.Errorf("passed and unrelated participation points cannot create a decision")
		}
		return outcome, nil
	case agreementStageBefore:
		if task.State == work.TaskDone || task.State == work.TaskCancelled {
			return outcome, fmt.Errorf("finished task cannot be paused for a new participation point")
		}
	default:
		return outcome, fmt.Errorf("task impact stage must be before_point, point_passed, or not_applicable")
	}
	requests, e := tx.ListDecisionRequests(next.ScopeID)
	if e != nil {
		return outcome, e
	}
	for _, request := range requests {
		if request.TaskID == task.ID && request.AgreementID != next.ID && request.ParticipationGateID != "" && request.State == work.DecisionOpen && request.Presentation != nil && request.Presentation.Title == impact.Point && request.Owner != next.DecisionOwner {
			return outcome, fmt.Errorf("participation point overlaps existing agreement %s and decision %s; coordinate that agreement before changing its owner", request.AgreementID, request.ID)
		}
	}
	if next.DecisionOwner.Kind != work.ActorPrincipal {
		return outcome, fmt.Errorf("a new human participation point requires a principal decision owner")
	}
	if err = s.requireAgreementPrincipal(tx, next.ScopeID, next.DecisionOwner.ID); err != nil {
		return outcome, err
	}
	if err = s.requireAgreementPrincipal(tx, next.ScopeID, next.Initiator.ID); err != nil {
		return outcome, err
	}
	text := fmt.Sprintf("任务：%s（%s，状态 %s）\n参与点：%s\n%s", task.Title, task.ID, task.State, impact.Point, impact.Facts)
	body := work.NewContent(stableID("participation_prompt", outcome.DecisionRequestID), next.ScopeID, "text/plain; charset=utf-8", []byte(text), s.now())
	if err = tx.PutContent(body); err != nil {
		return outcome, err
	}
	push := work.DecisionRequest{
		ID: outcome.DecisionRequestID, ScopeID: next.ScopeID, TaskID: task.ID, Kind: work.DecisionBusiness, State: work.DecisionOpen,
		Owner: next.DecisionOwner, AllowedActors: []work.Actor{next.DecisionOwner}, AgreementID: next.ID, AgreementRevision: next.Revision,
		AttemptID: attemptID, RequestGeneration: 1, Revision: 1, CreatedAt: s.now(),
		ParticipationGateID: stableID("participation_gate", outcome.DecisionRequestID),
		Presentation:        &work.DecisionPresentation{Title: impact.Point, PromptRef: body.ID, Choices: impact.Choices, References: impact.References, TaskRequirementsRevision: task.RequirementsRevision, TaskAssignmentGeneration: task.AssignmentGeneration},
	}
	change.NewRequests = append(change.NewRequests, push)
	change.Gates = append(change.Gates, work.PauseGate{ID: push.ParticipationGateID, ScopeID: task.ScopeID, TaskID: task.ID, Source: work.GateParticipation, SourceID: push.ParticipationGateID, State: work.GateClosed, Revision: 1})
	return outcome, nil
}

func validateAgreementImpactCoverage(tx *workstore.Tx, next, old work.ParticipationAgreement, impacts []AgreementTaskImpact) error {
	added := make([]string, 0, len(next.ParticipationPoints))
	for _, point := range next.ParticipationPoints {
		if !containsAgreementPoint(old.ParticipationPoints, point) {
			added = append(added, point)
		}
	}
	if len(added) == 0 {
		return nil
	}
	tasks, err := tx.ListTasks(next.ScopeID)
	if err != nil {
		return err
	}
	coverage := make(map[[2]string]int64, len(impacts))
	for _, impact := range impacts {
		coverage[[2]string{impact.TaskID, impact.Point}] = impact.TaskRevision
	}
	for _, task := range tasks {
		if task.State == work.TaskDone || task.State == work.TaskCancelled || (next.SubjectKind == work.AgreementItem && task.ID != next.SubjectID) {
			continue
		}
		for _, point := range added {
			revision, present := coverage[[2]string{task.ID, point}]
			if !present {
				return fmt.Errorf("new participation point %q requires an impact or explicit exclusion for unfinished task %s at revision %d", point, task.ID, task.Revision)
			}
			if revision != task.Revision {
				return workstore.ErrConflict
			}
		}
	}
	return nil
}

func containsAgreementPoint(points []string, point string) bool {
	for _, current := range points {
		if current == point {
			return true
		}
	}
	return false
}

func (s *Service) agreementDecisionInbox(request work.DecisionRequest) work.Inbox {
	return work.Inbox{ID: stableID("decision-inbox", request.ID), ScopeID: request.ScopeID, RecipientRoleID: request.Owner.ID, EventID: "decision:" + request.ID, State: work.InboxPending, AvailableAt: s.now(), Revision: 1}
}

func (s *Service) requireAgreementPrincipal(tx *workstore.Tx, scopeID, principalID string) error {
	scope, err := tx.GetScope(scopeID)
	if err != nil {
		return err
	}
	if scope.Kind == work.ScopePersonal {
		if scope.PrincipalID != principalID {
			return workstore.ErrScopeDenied
		}
		return nil
	}
	return tx.RequireProjectPrincipalAccess(scopeID, principalID, s.now())
}
