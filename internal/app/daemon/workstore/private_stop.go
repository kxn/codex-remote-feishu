package workstore

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// A private stop follows the occupied attempt through native reconciliation.
// It does not close a context gate for later human input.
type privateAttemptStopRecord struct {
	Run    work.RunRef
	Source work.SourceRef
	Cutoff time.Time
}

func (t *Tx) RecordPrivateAttemptStop(attempt work.ExecutionAttempt, scope work.WorkScope, source work.SourceRef, cutoff, now time.Time) error {
	if scope.Kind != work.ScopePersonal || scope.ID != attempt.ScopeID {
		return ErrScopeDenied
	}
	stopped, err := t.PrivateAttemptStopped(attempt)
	if err != nil || stopped {
		return err
	}
	body, err := json.Marshal(privateAttemptStopRecord{Run: attempt.RunRef, Source: source, Cutoff: cutoff})
	if err != nil {
		return err
	}
	return t.PutContent(work.NewContent("private-stop:"+attempt.ID, scope.ID, "application/json", body, now))
}

func (t *Tx) PrivateAttemptStopped(attempt work.ExecutionAttempt) (bool, error) {
	stop, err := t.privateAttemptStop(attempt)
	return !stop.Cutoff.IsZero(), err
}

func (t *Tx) privateAttemptStop(attempt work.ExecutionAttempt) (privateAttemptStopRecord, error) {
	var stop privateAttemptStopRecord
	body, err := t.GetContent(attempt.ScopeID, "private-stop:"+attempt.ID)
	if errors.Is(err, ErrNotFound) {
		return stop, nil
	}
	if err != nil {
		return stop, err
	}
	if err = json.Unmarshal(body.Data, &stop); err != nil {
		return stop, err
	}
	if stop.Run != attempt.RunRef {
		return stop, ErrConflict
	}
	return stop, nil
}

// An old input can be claimed while its Stop event is waiting to be processed.
// The input's receipt resolves that race independently of claim time.
func (t *Tx) PrivateAttemptPredatesStop(attempt work.ExecutionAttempt, scope work.WorkScope, cutoff time.Time) (bool, error) {
	if !attempt.CreatedAt.After(cutoff) {
		return true, nil
	}
	ids, err := t.EffectiveAttemptInputIDs(attempt.ID)
	if err != nil {
		return false, err
	}
	for _, id := range ids {
		input, e := t.GetInbox(id)
		if e != nil {
			return false, e
		}
		if input.MessageID == "" {
			continue
		}
		message, e := t.GetMessage(input.MessageID)
		if e != nil {
			return false, e
		}
		if !privateStopInput(input, message, scope.PrincipalID) {
			continue
		}
		older, e := t.privateInputPredatesStop(input, message.CreatedAt, cutoff)
		if e != nil || older {
			return older, e
		}
	}
	return false, nil
}

func privateStopInput(input work.Inbox, message work.Message, principalID string) bool {
	return message.Source == (work.Actor{Kind: work.ActorPrincipal, ID: principalID}) ||
		input.AutomationEpisodeID != "" || input.SessionGoalIntentID != ""
}

func (t *Tx) privateInputPredatesStop(input work.Inbox, receivedAt, cutoff time.Time) (bool, error) {
	if !receivedAt.After(cutoff) {
		return true, nil
	}
	if input.AutomationEpisodeID == "" {
		return false, nil
	}
	episode, err := t.GetAutomationEpisode(input.AutomationEpisodeID)
	if err != nil {
		return false, err
	}
	root, err := t.GetAttempt(episode.RootAttemptID)
	if err != nil {
		return false, err
	}
	return !root.CreatedAt.After(cutoff), nil
}

// CancelOlderPrivateInputs preserves unrelated system or project work and
// private messages received after the human stop cutoff.
func (t *Tx) CancelOlderPrivateInputs(scope work.WorkScope, contextID string, cutoff time.Time) error {
	if scope.Kind != work.ScopePersonal {
		return ErrScopeDenied
	}
	inputs, err := t.ListInbox(scope.ID)
	if err != nil {
		return err
	}
	cancelled := map[string]bool{}
	for _, input := range inputs {
		if input.State != work.InboxPending || input.RecipientContextID != contextID || input.MessageID == "" {
			continue
		}
		message, e := t.GetMessage(input.MessageID)
		if e != nil {
			return e
		}
		if !privateStopInput(input, message, scope.PrincipalID) {
			continue
		}
		older, e := t.privateInputPredatesStop(input, message.CreatedAt, cutoff)
		if e != nil {
			return e
		}
		if !older {
			continue
		}
		if e = t.cancelPrivateInbox(input); e != nil {
			return e
		}
		cancelled[input.ID] = true
	}
	for _, input := range inputs {
		if input.State == work.InboxPending && cancelled[input.ControlOfInputID] {
			if err = t.cancelPrivateInbox(input); err != nil {
				return err
			}
		}
	}
	return nil
}

func (t *Tx) cancelPrivateInbox(input work.Inbox) error {
	before := input.Revision
	input.Revision++
	input.State = work.InboxCancelled
	return t.PutInbox(input, before)
}
