package workstore

import "github.com/kxn/codex-remote-feishu/internal/core/work"

// Both failure admission and followup scheduling use this same authorization
// lineage. A transport failure cannot acquire a broader continuation right.
func (t *Tx) automationLineage(attempt work.ExecutionAttempt, kind work.AutomationKind) (string, int, error) {
	root := attempt.ID
	count := 1
	authorized := false
	if attempt.TaskID != "" {
		task, err := t.GetTask(attempt.TaskID)
		if err != nil {
			return "", 0, err
		}
		if task.State == work.TaskDone || task.State == work.TaskCancelled || task.RequirementsRevision != attempt.FrozenRequirementsRevision || task.AssignmentGeneration != attempt.FrozenAssignmentGeneration {
			return "", 0, ErrAutomationNotApplicable
		}
		authorized = true
	}
	for _, id := range attempt.InputIDs {
		input, err := t.GetInbox(id)
		if err != nil {
			return "", 0, err
		}
		if input.SessionGoalIntentID != "" {
			if input.State != work.InboxProcessed {
				return "", 0, ErrAutomationNotApplicable
			}
			authorized = true
		}
		if input.AutomationEpisodeID != "" {
			previous, err := t.GetAutomationEpisode(input.AutomationEpisodeID)
			if err != nil {
				return "", 0, err
			}
			if previous.Kind == kind && previous.ScopeID == attempt.ScopeID && previous.ContextID == attempt.ContextID {
				if previous.Count+1 > count {
					root = previous.RootAttemptID
					count = previous.Count + 1
				}
				authorized = true
			}
		}
		if input.MessageID != "" {
			message, err := t.GetMessage(input.MessageID)
			if err != nil {
				return "", 0, err
			}
			if message.ScopeID == attempt.ScopeID && message.Source.Kind == work.ActorPrincipal && message.RecalledEvidenceRef == "" {
				authorized = true
			}
		}
	}
	if !authorized {
		return "", 0, ErrAutomationNotApplicable
	}
	return root, count, nil
}
