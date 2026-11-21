package workservice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type TaskEvidenceSubmitRequest struct {
	Key                  string         `json:"key"`
	TaskID               string         `json:"task_id"`
	ExpectedRevision     int64          `json:"expected_revision"`
	RequirementsRevision int64          `json:"requirements_revision"`
	AssignmentGeneration int64          `json:"assignment_generation"`
	RequirementID        string         `json:"requirement_id"`
	RequirementVersion   string         `json:"requirement_version"`
	Kind                 string         `json:"kind"`
	Source               work.SourceRef `json:"source"`
}
type TaskEvidenceAdoptRequest struct {
	Key              string `json:"key"`
	TaskID           string `json:"task_id"`
	ExpectedRevision int64  `json:"expected_revision"`
	SubmissionID     string `json:"submission_id"`
	// Empty means this role's own review. A human conclusion requires the exact
	// actual message admitted to this attempt, never a user-supplied actor.
	SourceMessageID string `json:"source_message_id,omitempty"`
}
type TaskSubmissionListRequest struct {
	TaskID string `json:"task_id"`
	Limit  int    `json:"limit,omitempty"`
	After  string `json:"after,omitempty"`
}
type TaskSubmissionView struct {
	ID          string                    `json:"id"`
	SubmittedBy work.Actor                `json:"submitted_by"`
	Submission  TaskEvidenceSubmitRequest `json:"submission"`
}

func validateCurrentSubmission(task work.Task, req TaskEvidenceSubmitRequest) error {
	if task.State == work.TaskDone || task.State == work.TaskCancelled {
		return fmt.Errorf("terminal task cannot adopt new evidence")
	}
	if task.ID != req.TaskID || task.Revision != req.ExpectedRevision || task.RequirementsRevision != req.RequirementsRevision || task.AssignmentGeneration != req.AssignmentGeneration {
		return workstore.ErrConflict
	}
	for _, r := range task.Requirements {
		if r.ID == req.RequirementID && r.Source.Version == req.RequirementVersion {
			if req.Kind == "" {
				return fmt.Errorf("evidence kind required")
			}
			return req.Source.Validate()
		}
	}
	return fmt.Errorf("evidence must identify an exact adopted requirement version")
}

// Submission intent is durable independently of adoption. It never contributes
// to completion until a responsible role (or proven human) explicitly adopts it.
func (s *Service) TaskEvidenceSubmit(ctx context.Context, token, run string, req TaskEvidenceSubmitRequest) (Result, error) {
	var result Result
	err := s.withLifecycleWorker(ctx, token, run, "task.evidence.submit", req.Key, false, func(tx *workstore.Tx, c Caller) error {
		if strings.TrimSpace(req.Key) == "" || len(req.Key) > 256 {
			return fmt.Errorf("idempotency key required (maximum 256 bytes)")
		}
		raw, err := json.Marshal(req)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(raw)
		opID := stableID("op", c.Worker.ScopeID, string(work.ActorRole), c.Worker.RoleID, "task.evidence.submit", req.Key)
		body := work.NewContent(stableID("submission", opID), c.Worker.ScopeID, "application/json", raw, s.now())
		if err = tx.PutContent(body); err != nil {
			return err
		}
		op := work.Operation{ID: opID, ScopeID: c.Worker.ScopeID, ContextID: c.Worker.ContextID, Actor: work.Actor{Kind: work.ActorRole, ID: c.Worker.RoleID}, Kind: "task.evidence.submit", TargetID: req.TaskID, TargetGeneration: req.AssignmentGeneration, IdempotencyKey: req.Key, RequestHash: hex.EncodeToString(sum[:]), RequestRef: body.ID, State: work.OperationPrepared, Revision: 1, CreatedAt: s.now(), UpdatedAt: s.now()}
		original, created, err := tx.PutOperation(op, 0)
		if err != nil {
			return err
		}
		if !created {
			if err = json.Unmarshal([]byte(original.ExternalReceipt), &result); err != nil {
				return err
			}
			result.Replay = true
			return nil
		}
		task, err := tx.GetTask(req.TaskID)
		if err != nil {
			return err
		}
		if err = mayManageTask(tx, c, task); err != nil {
			return err
		}
		if err = validateCurrentSubmission(task, req); err != nil {
			return err
		}
		op.State = work.OperationSending
		op.Revision++
		if _, _, err = tx.PutOperation(op, 1); err != nil {
			return err
		}
		result = Result{Status: "completed", OperationID: op.ID, ObjectID: op.ID, Revision: 1}
		receipt, err := json.Marshal(result)
		if err != nil {
			return err
		}
		op.State = work.OperationConfirmed
		op.Revision++
		op.ExternalReceipt = string(receipt)
		_, _, err = tx.PutOperation(op, 2)
		return err
	})
	return result, err
}
func (s *Service) TaskEvidenceList(ctx context.Context, token, run string, req TaskSubmissionListRequest) (KnowledgePage[TaskSubmissionView], error) {
	var page KnowledgePage[TaskSubmissionView]
	err := s.withWorker(ctx, token, run, func(tx *workstore.Tx, c Caller) error {
		task, err := tx.GetTask(req.TaskID)
		if err != nil {
			return err
		}
		if task.ScopeID != c.Worker.ScopeID {
			return workauth.ErrUnauthorized
		}
		ops, err := tx.ListOperations(task.ScopeID)
		if err != nil {
			return err
		}
		items := []TaskSubmissionView{}
		for _, op := range ops {
			if op.Kind != "task.evidence.submit" || op.TargetID != task.ID || op.State != work.OperationConfirmed {
				continue
			}
			body, err := tx.GetContent(task.ScopeID, op.RequestRef)
			if err != nil {
				return err
			}
			var submitted TaskEvidenceSubmitRequest
			if err = json.Unmarshal(body.Data, &submitted); err != nil {
				return err
			}
			items = append(items, TaskSubmissionView{ID: op.ID, SubmittedBy: op.Actor, Submission: submitted})
		}
		page, err = knowledgePage(items, KnowledgeListRequest{Limit: req.Limit, After: req.After}, func(v TaskSubmissionView) string { return v.ID })
		return err
	})
	return page, err
}
func (s *Service) taskEvidenceAdopt(ctx context.Context, token, run, kind string, req TaskEvidenceAdoptRequest) (Result, error) {
	var result Result
	err := s.withLifecycleWorker(ctx, token, run, kind, req.Key, false, func(tx *workstore.Tx, c Caller) error {
		var err error
		result, err = s.mutate(tx, c, kind, req.Key, req.TaskID, req, func() (string, int64, error) {
			task, err := tx.GetTask(req.TaskID)
			if err != nil {
				return "", 0, err
			}
			if err = mayManageTask(tx, c, task); err != nil {
				return "", 0, err
			}
			if task.Revision != req.ExpectedRevision {
				return "", 0, workstore.ErrConflict
			}
			submission, err := tx.GetOperation(req.SubmissionID)
			if err != nil {
				return "", 0, err
			}
			if submission.ScopeID != task.ScopeID || submission.TargetID != task.ID || submission.Kind != "task.evidence.submit" || submission.State != work.OperationConfirmed {
				return "", 0, workauth.ErrUnauthorized
			}
			body, err := tx.GetContent(task.ScopeID, submission.RequestRef)
			if err != nil {
				return "", 0, err
			}
			var original TaskEvidenceSubmitRequest
			if err = json.Unmarshal(body.Data, &original); err != nil {
				return "", 0, err
			}
			// The task revision may advance for state changes; requirement and assignment
			// versions must still exactly match the original submitted evidence.
			original.ExpectedRevision = req.ExpectedRevision
			if err = validateCurrentSubmission(task, original); err != nil {
				return "", 0, err
			}
			actor := work.Actor{Kind: work.ActorRole, ID: c.Worker.RoleID}
			if req.SourceMessageID != "" {
				message, _, err := currentHumanInstruction(tx, c, req.SourceMessageID)
				if err != nil {
					return "", 0, err
				}
				var review work.DecisionRequest
				if err = requireHumanSubmissionReview(tx, message, task, submission.ID, &review); err != nil {
					return "", 0, err
				}
				actor = message.Source
				original.Source = work.SourceRef{Kind: "message", ID: message.ID, Version: strconv.FormatInt(message.Revision, 10), EvidenceRef: message.BodyRef}
				decision := work.Decision{ID: stableID("natural-human-review", review.ID, message.ID), RequestID: review.ID, RequestRevision: review.Revision, RequestGeneration: review.RequestGeneration, AgreementRevision: review.AgreementRevision, Actor: actor, Choice: "approve", Source: original.Source, CreatedAt: s.now()}
				response := work.Operation{ID: stableID("natural-human-review-effect", review.ID, message.ID), ScopeID: task.ScopeID, ContextID: c.Worker.ContextID, Actor: actor, Kind: "decision.apply", TargetID: review.ID, TargetGeneration: review.RequestGeneration, IdempotencyKey: decision.ID, RequestHash: stableID("natural-human-review-hash", review.ID, message.ID, submission.ID), State: work.OperationPrepared, Revision: 1, CreatedAt: s.now(), UpdatedAt: s.now()}
				if err = tx.AcceptDecision(workstore.AcceptDecisionRequest{ScopeID: task.ScopeID, Decision: decision, ResponseOperation: response}); err != nil {
					return "", 0, err
				}
				if err = s.applyBusinessDecision(tx, review, decision, response, message.ID); err != nil {
					return "", 0, err
				}
				return stableID("decision-evidence", decision.ID), 1, nil

			}
			id := stableID("evidence", task.ScopeID, c.Worker.RoleID, kind, req.Key)
			evidence := work.RequirementEvidence{ID: id, ScopeID: task.ScopeID, TaskID: task.ID, RequirementsRevision: task.RequirementsRevision, AssignmentGeneration: task.AssignmentGeneration, RequirementID: original.RequirementID, RequirementVersion: original.RequirementVersion, Kind: original.Kind, Source: original.Source, AcceptedBy: actor}
			if err = tx.PutEvidence(evidence); err != nil {
				return "", 0, err
			}
			return id, 1, nil
		})
		return err
	})
	return result, err
}
func (s *Service) TaskEvidenceAdopt(ctx context.Context, t, r string, q TaskEvidenceAdoptRequest) (Result, error) {
	return s.taskEvidenceAdopt(ctx, t, r, "task.evidence.adopt", q)
}
func (s *Service) TaskReview(ctx context.Context, t, r string, q TaskEvidenceAdoptRequest) (Result, error) {
	return s.taskEvidenceAdopt(ctx, t, r, "task.review", q)
}
