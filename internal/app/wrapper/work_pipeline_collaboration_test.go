package wrapper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workingress"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

const (
	pipelineRevisedRequest = "CSV only. I want to review the scope before you continue."
	pipelineDelegateReview = "You can decide this scope review. Only tell me the result."
	pipelineTopicSummary   = "Export work assigned to the engineer. The reviewer will independently check the result."
	pipelineDelivery       = "CSV export is complete. The reviewer checked CSV only. Excel was deferred."
)

func TestSDKGroupCollaborationRequirementAndParticipationJourney(t *testing.T) {
	t.Setenv("FSC_NATIVE_PIPELINE_COLLABORATION", "1")
	runSDKNativePipeline(t, buildPipelineTeamCLI(t), work.BackendCodex, 3)
}

type pipelineCollaborationJourney struct {
	store     *workstore.Store
	scopeID   string
	ingress   *workingress.Ingress
	original  work.RawIngressEnvelope
	taskID    string
	topic     string
	phase     int
	holdTicks int
}

func (j *pipelineCollaborationJourney) advance(ctx context.Context) (bool, error) {
	var nextMessage string
	complete := false
	err := j.store.Within(ctx, func(tx *workstore.Tx) error {
		tasks, err := tx.ListTasks(j.scopeID)
		if err != nil || len(tasks) == 0 {
			return err
		}
		if len(tasks) != 1 {
			return fmt.Errorf("journey has %d tasks, expected one current export task", len(tasks))
		}
		task := tasks[0]
		j.taskID = task.ID
		topic, err := tx.GetTaskConversation(j.scopeID, task.ID)
		if errors.Is(err, workstore.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		j.topic = topic.RootMessageID
		switch j.phase {
		case 0:
			evidence, e := tx.ListEvidence(task.ID)
			if e != nil {
				return e
			}
			if len(evidence) == 1 && evidence[0].RequirementsRevision == 1 && !evidence[0].Obsolete {
				nextMessage = pipelineRevisedRequest
			}
		case 1:
			if task.RequirementsRevision != 2 {
				return nil
			}
			attempts, e := tx.ListAttempts(j.scopeID)
			if e != nil {
				return e
			}
			for _, attempt := range attempts {
				if attempt.TaskID == task.ID && attempt.FrozenRequirementsRevision == 2 {
					return fmt.Errorf("expert admitted revised requirements before the human transferred the decision")
				}
			}
			gates, e := tx.ListPauseGates(j.scopeID)
			if e != nil {
				return e
			}
			allowed, e := work.GatesAllowRun(j.scopeID, task.ID, gates)
			if e != nil || allowed {
				return fmt.Errorf("revised work must retain the participation hold: allowed=%v err=%v", allowed, e)
			}
			j.holdTicks++
			if j.holdTicks >= 8 {
				nextMessage = pipelineDelegateReview
			}
		case 2:
			if task.State != work.TaskDone {
				return nil
			}
			inputs, e := tx.ListInbox(j.scopeID)
			if e != nil {
				return e
			}
			for _, input := range inputs {
				if input.State != work.InboxProcessed && input.State != work.InboxCancelled {
					return nil
				}
			}
			mapping, e := tx.GetPlatformMessageMapping(j.original.Tenant, j.original.ChatID, "pipeline-source-receipt-2")
			if errors.Is(e, workstore.ErrNotFound) {
				return nil
			}
			if e != nil {
				return e
			}
			complete = mapping.TaskID == task.ID && mapping.RootMessageID == j.topic
		}
		return nil
	})
	if err != nil || nextMessage == "" {
		return complete, err
	}
	raw := j.original
	raw.EventID = fmt.Sprintf("sdk-event-followup-%d", j.phase)
	raw.MessageID = fmt.Sprintf("sdk-human-followup-%d", j.phase)
	raw.RootID, raw.ParentID = j.topic, j.topic
	raw.ReceivedAt = time.Now().UTC()
	raw.Mentions = []work.RawMention{{Key: "@_user_1", Type: "bot", IDs: work.SenderIDs{OpenID: "pipeline-leader-open"}}}
	body, err := json.Marshal(map[string]string{"text": "@_user_1 " + nextMessage})
	if err != nil {
		return false, err
	}
	raw.RawContent = string(body)
	if _, err = j.ingress.Receive(ctx, raw); err != nil {
		return false, err
	}
	processed, err := j.ingress.ProcessPending(ctx, 16)
	if err != nil || len(processed.Failures) != 0 {
		return false, fmt.Errorf("SDK follow-up %d: %+v %v", j.phase, processed, err)
	}
	j.phase++
	return false, nil
}

func (j *pipelineCollaborationJourney) check(t *testing.T, sender *pipelinePublisher) {
	t.Helper()
	sender.mu.Lock()
	defer sender.mu.Unlock()
	if j.phase != 2 || len(sender.requests) != 2 ||
		sender.requests[0].Text != pipelineTopicSummary || sender.requests[0].ReplyToMessageID != "" ||
		sender.requests[1].Text != pipelineDelivery || sender.requests[1].ReplyToMessageID != j.topic ||
		sender.requests[0].ChatID != j.original.ChatID || sender.requests[1].ChatID != j.original.ChatID {
		t.Fatalf("expected root assignment and final threaded delivery, phase=%d topic=%s requests=%+v", j.phase, j.topic, sender.requests)
	}
	if err := j.store.Within(t.Context(), func(tx *workstore.Tx) error {
		task, err := tx.GetTask(j.taskID)
		if err != nil {
			return err
		}
		evidence, err := tx.ListEvidence(task.ID)
		if err != nil {
			return err
		}
		counts := map[int64]int{}
		for _, record := range evidence {
			if record.Obsolete != (record.RequirementsRevision == 1) {
				return fmt.Errorf("unexpected evidence validity %+v", record)
			}
			counts[record.RequirementsRevision]++
		}
		if task.State != work.TaskDone || task.RequirementsRevision != 2 || counts[1] != 1 || counts[2] != 1 {
			return fmt.Errorf("task completion or evidence lost: task=%+v evidence=%+v", task, evidence)
		}
		agreements, err := tx.ListAgreements(j.scopeID)
		if err != nil {
			return err
		}
		if len(agreements) != 1 || agreements[0].DecisionOwner != (work.Actor{Kind: work.ActorRole, ID: "pipeline-role"}) ||
			agreements[0].Reporting != work.ReportingActionRequired {
			return fmt.Errorf("delegation was not retained independently from reporting: %+v", agreements)
		}
		requests, err := tx.ListDecisionRequests(j.scopeID)
		if err != nil {
			return err
		}
		var old, resolved int
		for _, req := range requests {
			switch req.State {
			case work.DecisionSuperseded:
				old++
			case work.DecisionResolved:
				if req.Owner == agreements[0].DecisionOwner && req.Presentation.TaskRequirementsRevision == 2 {
					resolved++
				}
			}
		}
		if old != 2 || resolved != 1 {
			return fmt.Errorf("expected version supersession, transfer, and resolved current decision, got %+v", requests)
		}
		attempts, err := tx.ListAttempts(j.scopeID)
		if err != nil {
			return err
		}
		var reviewerRuns, revisedTaskRuns int
		for _, attempt := range attempts {
			if attempt.State != work.AttemptCompleted {
				return fmt.Errorf("nonterminal journey attempt %+v", attempt)
			}
			if _, e := tx.GetQuiescenceReceipt(attempt.ID); e != nil {
				return e
			}
			worker, e := tx.GetWorker(attempt.RunRef.WorkerID)
			if e != nil {
				return e
			}
			if worker.RoleID == "pipeline-reviewer" {
				reviewerRuns++
				if attempt.TaskID != "" {
					return fmt.Errorf("reviewer wrongly assumed ownership of engineer's task")
				}
			}
			if attempt.TaskID == task.ID && attempt.FrozenRequirementsRevision == 2 {
				revisedTaskRuns++
			}
		}
		if reviewerRuns != 2 || revisedTaskRuns != 2 || !strings.Contains(task.Title, "CSV only") {
			return fmt.Errorf("wrong native recipients or requirements: reviewer=%d revised=%d task=%+v", reviewerRuns, revisedTaskRuns, task)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
