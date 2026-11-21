package workservice

import (
	"context"
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type SendMessageRequest struct {
	Key                string `json:"key"`
	RecipientRoleID    string `json:"recipient_role_id"`
	RecipientContextID string `json:"recipient_context_id,omitempty"`
	TaskID             string `json:"task_id,omitempty"`
	RelatedTaskID      string `json:"related_task_id,omitempty"`
	ConversationID     string `json:"conversation_id"`
	Body               string `json:"body"`
}

func (s *Service) SendMessage(ctx context.Context, token, runRef string, req SendMessageRequest) (Result, error) {
	var result Result
	err := s.withWorker(ctx, token, runRef, func(tx *workstore.Tx, c Caller) error {
		id := stableID("message", c.Worker.ScopeID, c.Worker.RoleID, req.Key)
		var err error
		result, err = s.mutate(tx, c, "message.send", req.Key, id, req, func() (string, int64, error) {
			if strings.TrimSpace(req.Body) == "" {
				return "", 0, fmt.Errorf("message body is required")
			}
			if err := ensureEnrolledMember(tx, c.Worker.ScopeID, req.RecipientRoleID); err != nil {
				return "", 0, err
			}
			delivery, e := messageDelivery(tx, c, req)
			if e != nil {
				return "", 0, e
			}
			if blocked, e := s.guardRepeatedMessage(tx, c, delivery); e != nil {
				return "", 0, e
			} else if blocked != "" {
				return blocked, 1, nil
			}
			body := work.NewContent(stableID("body", id), c.Worker.ScopeID, "text/plain; charset=utf-8", []byte(req.Body), s.now())
			if err := tx.PutContent(body); err != nil {
				return "", 0, err
			}
			message := work.Message{ID: id, ScopeID: c.Worker.ScopeID, ConversationID: req.ConversationID, Source: work.Actor{Kind: work.ActorRole, ID: c.Worker.RoleID}, BodyRef: body.ID, Revision: 1, CreatedAt: s.now()}
			input := work.Inbox{ID: stableID("inbox", id, delivery.RecipientRoleID, delivery.RecipientContextID), ScopeID: c.Worker.ScopeID, TaskID: delivery.TaskID, RelatedTaskID: delivery.RelatedTaskID, RecipientRoleID: delivery.RecipientRoleID, RecipientContextID: delivery.RecipientContextID, EventID: "message:" + id, MessageID: id, State: work.InboxPending, AvailableAt: s.now(), Revision: 1}
			if _, _, err := tx.AcceptMessage(message, []work.Inbox{input}); err != nil {
				return "", 0, err
			}
			return id, 1, nil
		})
		if err == nil && strings.HasPrefix(result.ObjectID, "coordination_block_") {
			result.Status = "needs_coordination"
		}
		return err
	})
	return result, err
}

func messageDelivery(tx *workstore.Tx, c Caller, req SendMessageRequest) (SendMessageRequest, error) {
	if req.RecipientContextID != "" {
		context, err := tx.GetContext(req.RecipientContextID)
		if err != nil {
			return req, err
		}
		if context.ScopeID != c.Worker.ScopeID || context.RoleID != req.RecipientRoleID || context.Purpose == work.PurposeControl || context.Purpose == work.PurposeReview {
			return req, workstore.ErrScopeDenied
		}
		if req.TaskID != "" && context.TaskID != req.TaskID {
			return req, fmt.Errorf("recipient context does not execute the selected task")
		}
		req.TaskID = context.TaskID
	}
	if req.TaskID != "" {
		task, err := tx.GetTask(req.TaskID)
		if err != nil {
			return req, err
		}
		if task.ScopeID != c.Worker.ScopeID {
			return req, workstore.ErrScopeDenied
		}
		if task.OwnerRoleID != req.RecipientRoleID || task.State == work.TaskDone || task.State == work.TaskCancelled {
			return req, fmt.Errorf("task_id must name an executable task assigned to the recipient; use related_task_id to discuss another task")
		}
	}
	if req.RelatedTaskID == "" {
		req.RelatedTaskID = c.Attempt.TaskID
	}
	if req.RelatedTaskID == req.TaskID {
		req.RelatedTaskID = ""
	}
	if req.RelatedTaskID != "" {
		task, err := tx.GetTask(req.RelatedTaskID)
		if err != nil {
			return req, err
		}
		if task.ScopeID != c.Worker.ScopeID {
			return req, workstore.ErrScopeDenied
		}
	}
	return req, nil
}

type ReportOutcomeRequest struct {
	WorkDisposition   string   `json:"work_disposition"`
	Key               string   `json:"key"`
	ExpectedRevision  int64    `json:"expected_revision"`
	ProcessedInputIDs []string `json:"processed_input_ids"`
	Summary           string   `json:"summary"`
	Checkpoint        string   `json:"checkpoint"`
}

func (s *Service) ReportOutcome(ctx context.Context, token, runRef string, req ReportOutcomeRequest) (Result, error) {
	var result Result
	err := s.withWorkerReceipt(ctx, token, runRef, func(tx *workstore.Tx, c Caller) error {
		var err error
		result, err = s.mutate(tx, c, "outcome.report", req.Key, c.Attempt.ID, req, func() (string, int64, error) {
			if strings.TrimSpace(req.Summary) == "" {
				return "", 0, fmt.Errorf("outcome summary is required")
			}
			body := work.NewContent(stableID("outcome", c.Attempt.ID, req.Key), c.Worker.ScopeID, "text/plain; charset=utf-8", []byte(req.Summary), s.now())
			if err := tx.PutContent(body); err != nil {
				return "", 0, err
			}
			report := workstore.ProcessingReport{WorkDisposition: req.WorkDisposition, AttemptID: c.Attempt.ID, RunRef: c.Attempt.RunRef, ProcessedInputIDs: req.ProcessedInputIDs, OutcomeRef: body.ID, Checkpoint: req.Checkpoint, Revision: req.ExpectedRevision + 1}
			if err := tx.PutProcessingReport(report, req.ExpectedRevision); err != nil {
				return "", 0, err
			}
			return c.Attempt.ID, report.Revision, nil
		})
		return err
	})
	return result, err
}
