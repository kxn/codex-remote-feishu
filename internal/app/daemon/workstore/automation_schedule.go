package workstore

import (
	"encoding/json"
	"errors"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"strings"
	"time"
)

// QueueAutomationFollowup is called only after native reconciliation committed
// the terminal outcome and its three independent quiescence proofs.
func (t *Tx) QueueAutomationFollowup(attemptID string, kind work.AutomationKind, terminal work.SourceRef, now time.Time) (episode work.AutomationEpisode, created bool, err error) {
	err = t.atomic(func() error {
		attempt, e := t.GetAttempt(attemptID)
		if e != nil {
			return e
		}
		stopped, e := t.PrivateAttemptStopped(attempt)
		if e != nil {
			return e
		}
		if stopped {
			return ErrAutomationNotApplicable
		}
		wc, e := t.GetContext(attempt.ContextID)
		if e != nil {
			return e
		}
		if wc.Purpose == work.PurposeControl || wc.Purpose == work.PurposeReview {
			return ErrAutomationNotApplicable
		}
		policy, e := t.GetAutomationPolicy(wc.ID, kind)
		if errors.Is(e, ErrNotFound) {
			return ErrAutomationNotApplicable
		}
		if e != nil {
			return e
		}
		if !policy.Enabled {
			return ErrAutomationNotApplicable
		}
		var n int
		if e = t.tx.QueryRowContext(t.ctx, "SELECT count(*) FROM quiescence_receipts WHERE attempt_id=?", attemptID).Scan(&n); e != nil {
			return e
		}
		if n != 1 {
			return ErrConflict
		}
		occupied, e := t.WorkerHasOccupancy(attempt.RunRef.WorkerID)
		if e != nil {
			return e
		}
		if occupied {
			return ErrConflict
		}
		id := "automation:" + string(kind) + ":" + attemptID
		if old, e := t.GetAutomationEpisode(id); e == nil {
			episode = old
			return nil
		} else if !errors.Is(e, ErrNotFound) {
			return e
		}
		event, e := t.reviewEvent(attempt, terminal)
		if e != nil {
			return e
		}
		if event.Kind != agentproto.EventTurnCompleted || event.TurnCompletionOrigin != agentproto.TurnCompletionOriginRuntime {
			return ErrScopeDenied
		}
		switch kind {
		case work.AutomationWhip:
			if attempt.State != work.AttemptCompleted || event.Status != "completed" {
				return ErrAutomationNotApplicable
			}
			report, e := t.GetProcessingReport(attemptID)
			if e != nil {
				return e
			}
			if report.WorkDisposition != "unfinished" {
				return ErrAutomationNotApplicable
			}
		case work.AutomationContinue:
			if attempt.State != work.AttemptFailed || event.Status != "failed" || !retryableAutomationProblem(event.Problem) {
				return ErrAutomationNotApplicable
			}
		default:
			return ErrScopeDenied
		}
		if attempt.TaskID != "" {
			task, e := t.GetTask(attempt.TaskID)
			if e != nil {
				return e
			}
			if task.State == work.TaskDone || task.State == work.TaskCancelled || task.RequirementsRevision != attempt.FrozenRequirementsRevision || task.AssignmentGeneration != attempt.FrozenAssignmentGeneration {
				return ErrAutomationNotApplicable
			}
		}
		if e = t.AssertAutomationEffectsSettled(attemptID); e != nil {
			if errors.Is(e, ErrAutomationNotApplicable) {
				return e
			}
			return e
		}

		root, count, e := t.automationLineage(attempt, kind)
		if e != nil {
			return e
		}

		delay, e := automationDelay(kind, count)
		if e != nil {
			return e
		}
		due := now.Add(delay)
		episode = work.AutomationEpisode{ID: id, ScopeID: wc.ScopeID, ContextID: wc.ID, Kind: kind, PolicyRevision: policy.Revision, TriggerAttemptID: attempt.ID, RootAttemptID: root, Count: count, InputID: id + ":input", DueAt: due, Source: terminal}
		if _, e = t.tx.ExecContext(t.ctx, "INSERT INTO automation_episodes(id,context_id,trigger_attempt_id,kind,payload) VALUES(?,?,?,?,?)", id, wc.ID, attempt.ID, kind, mustJSON(episode)); e != nil {
			return e
		}
		text := "检查原有已授权工作是否还有待办，只继续已有工作，不创建新目标；已完成请如实报告 complete，否则报告 unfinished。"
		if kind == work.AutomationContinue {
			text = "上轮因明确的可恢复连接或服务错误停止。先核对上轮结果和已发生的写入，只继续未完成的原授权工作；不要重复已经完成的外部副作用。"
		}
		body := work.NewContent(id+":body", wc.ScopeID, "text/plain", []byte(text), now)
		if e = t.PutContent(body); e != nil {
			return e
		}
		message := work.Message{ID: id + ":message", ScopeID: wc.ScopeID, ConversationID: wc.ID, Source: work.Actor{Kind: work.ActorSystem, ID: string(kind)}, DerivedFrom: &terminal, BodyRef: body.ID, Revision: 1, CreatedAt: now}
		input := work.Inbox{ID: episode.InputID, ScopeID: wc.ScopeID, RecipientRoleID: wc.RoleID, RecipientContextID: wc.ID, TaskID: attempt.TaskID, AutomationEpisodeID: id, MessageID: message.ID, EventID: id, State: work.InboxPending, Revision: 1, AvailableAt: due}
		if _, _, e = t.AcceptMessage(message, []work.Inbox{input}); e != nil {
			return e
		}
		if kind == work.AutomationContinue {
			for _, id := range attempt.InputIDs {
				old, e := t.GetInbox(id)
				if e != nil {
					return e
				}
				if old.State == work.InboxPending {
					before := old.Revision
					old.Revision++
					old.AvailableAt = due
					if e = t.PutInbox(old, before); e != nil {
						return e
					}
				}
			}
		}
		created = true
		return nil
	})
	return
}
func retryableAutomationProblem(problem *agentproto.ErrorInfo) bool {
	if problem == nil {
		return false
	}
	if problem.Layer != "" && problem.Layer != "codex" && problem.Layer != "gateway" {
		return false
	}
	switch strings.TrimSpace(problem.Code) {
	case "other", "responseStreamDisconnected", "responseTooManyFailedAttempts", "serverOverloaded":
		return true
	}
	return false
}

// CanAutoContinueFailure is a read-only runtime decision before creating a
// narrow failed outcome report. It never asserts that the carrier stopped.
func (t *Tx) CanAutoContinueFailure(attemptID string, event agentproto.Event) (bool, error) {
	attempt, err := t.GetAttempt(attemptID)
	if err != nil {
		return false, err
	}
	stopped, err := t.PrivateAttemptStopped(attempt)
	if err != nil || stopped {
		return false, err
	}
	policy, err := t.GetAutomationPolicy(attempt.ContextID, work.AutomationContinue)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !policy.Enabled || event.Kind != agentproto.EventTurnCompleted || event.Status != "failed" || event.TurnCompletionOrigin != agentproto.TurnCompletionOriginRuntime || !retryableAutomationProblem(event.Problem) {
		return false, nil
	}
	if event.Journal == nil {
		return false, nil
	}
	if err = t.AssertAutomationEffectsSettled(attemptID); err != nil {
		return false, nil
	}
	_, count, e := t.automationLineage(attempt, work.AutomationContinue)
	if errors.Is(e, ErrAutomationNotApplicable) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	if _, e = automationDelay(work.AutomationContinue, count); e != nil {
		return false, nil
	}

	persisted, err := t.GetRuntimeEvent(event.Journal.CarrierIncarnation, event.Journal.Sequence)
	if err != nil {
		return false, err
	}
	left, _ := json.Marshal(event)
	right, _ := json.Marshal(persisted)
	if string(left) != string(right) {
		return false, ErrScopeDenied
	}
	turn, err := t.GetNativeTurn(event.Journal.CarrierIncarnation, event.ThreadID, event.TurnID)
	if err != nil {
		return false, err
	}
	return turn.Binding.AttemptID == attempt.ID && turn.Binding.RunRef == attempt.RunRef.RunID, nil
}

// AssertAutomationEffectsSettled does not treat the proven-stopped prompt as
// an uncertain external write, but blocks every unresolved business effect.
func (t *Tx) AssertAutomationEffectsSettled(attemptID string) error {
	attempt, err := t.GetAttempt(attemptID)
	if err != nil {
		return err
	}
	operations, err := t.ListOperations(attempt.ScopeID)
	if err != nil {
		return err
	}
	for _, op := range operations {
		if op.Kind == string(agentproto.CommandRuntimeInspect) || op.Kind == string(agentproto.CommandRuntimeMCPRead) || op.Kind == string(agentproto.CommandModelList) {
			continue
		}
		if op.ContextID == attempt.ContextID && op.ID != attempt.CommandID && (op.State == work.OperationSending || op.State == work.OperationUnknown || op.State == work.OperationPrepared) {
			return ErrAutomationNotApplicable
		}
	}
	return nil
}
