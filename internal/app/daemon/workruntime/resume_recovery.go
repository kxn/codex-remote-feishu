package workruntime

import (
	"errors"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// A fresh run cannot retry this recovery because only an explicit resume of
// an unavailable target can produce the qualifying rejection. The failed
// Attempt's original inbox rows are already pending again.
func (c *Coordinator) queueMissingResumeRecovery(tx *workstore.Tx, worker work.WorkerSession, request InspectionRequest, proof work.QuiescenceEvidence) (bool, error) {
	command, err := tx.GetNativeCommand(request.SubjectCommandID)
	if err != nil {
		return false, err
	}
	if !missingNativeResumeTarget(worker.Backend, request.SessionID, command) {
		return false, nil
	}
	wc, err := tx.GetContext(worker.ContextID)
	if err != nil {
		return false, err
	}
	checkpoint := strings.TrimSpace(wc.Checkpoint)
	if checkpoint == "" || checkpoint == missingReportNotice || checkpoint == resumeRejectedNotice || wc.Purpose == work.PurposeReview || wc.Purpose == work.PurposeControl {
		return false, nil
	}
	attempt, err := tx.GetAttempt(request.Binding.AttemptID)
	if err != nil {
		return false, err
	}
	if attempt.TaskID != "" {
		task, err := tx.GetTask(attempt.TaskID)
		if err != nil {
			return false, err
		}
		if task.AssignmentGeneration != attempt.FrozenAssignmentGeneration || task.RequirementsRevision != attempt.FrozenRequirementsRevision || task.State == work.TaskCancelled {
			return false, nil
		}
	}
	if err = tx.AssertAutomationEffectsSettled(attempt.ID); errors.Is(err, workstore.ErrAutomationNotApplicable) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	gate, err := tx.GetRuntimeGate(worker.ID)
	if err != nil {
		return false, err
	}
	sameRejection := gate.Reason == "native_resume_rejected" || (gate.Reason == "native_reconciliation" && gate.RecoveryReason == "native_resume_rejected")
	if gate.AttemptID != attempt.ID || !sameRejection || (gate.RecoveryReason != "" && gate.RecoveryReason != "native_resume_rejected") {
		return false, nil
	}
	id := "native-resume-recovery:" + attempt.ID
	text := "已核验原生会话无法恢复，本轮使用新的执行会话。请从持久检查点核对此前已完成的操作，继续处理本轮列出的未处理输入。"
	if err = c.recoveryInput(tx, id, worker, proof.Source, text); err != nil {
		return false, err
	}
	now := c.options.Clock()
	if err = tx.AcknowledgeRuntimeRecovery(worker.ID, gate.Reason, gate.Revision); err != nil {
		return false, err
	}
	current, err := tx.GetWorker(worker.ID)
	if err != nil {
		return false, err
	}
	current.State, current.Revision, current.UpdatedAt = work.WorkerRecovering, current.Revision+1, now
	if err = tx.PutWorker(current, current.Revision-1); err != nil {
		return false, err
	}
	return true, tx.PutRuntimeGate(workstore.RuntimeGate{WorkerID: worker.ID, ScopeID: worker.ScopeID, AttemptID: attempt.ID, Reason: "snapshot_rebuild_required", CreatedAt: now})
}

func missingNativeResumeTarget(backend work.Backend, sessionID string, command workstore.NativeCommand) bool {
	if sessionID == "" {
		return false
	}
	switch backend {
	case work.BackendClaude:
		return command.Ack != nil && command.Ack.Problem != nil && command.Ack.Problem.Code == "claude_resume_thread_not_found"
	case work.BackendCodex:
		event := command.TerminalRejection
		if event == nil || event.ThreadID != sessionID || event.TurnCompletionOrigin != agentproto.TurnCompletionOriginThreadResumeRejected {
			return false
		}
		message := strings.TrimSpace(event.ErrorMessage)
		return message == "thread not found" || message == "no rollout found for thread id "+sessionID
	default:
		return false
	}
}
