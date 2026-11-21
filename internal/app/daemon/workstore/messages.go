package workstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"reflect"
)

func (t *Tx) GetMessage(id string) (work.Message, error) {
	var v work.Message
	err := t.read("messages", id, &v)
	return v, err
}
func (t *Tx) ListMessages(scopeID string) ([]work.Message, error) {
	return listRecords[work.Message](t, "messages", " WHERE scope_id=?", scopeID)
}
func platformKey(v work.Message) string {
	if v.PlatformKey == nil {
		return ""
	}
	data, _ := json.Marshal(v.PlatformKey)
	return string(data)
}
func (t *Tx) putMessage(v work.Message, expected int64) error {
	scope, scopeErr := t.GetScope(v.ScopeID)
	if scopeErr != nil {
		return scopeErr
	}
	if scope.Kind == work.ScopeLibrary {
		return ErrScopeDenied
	}
	if err := v.Validate(); err != nil {
		return err
	}
	if err := t.validateMessageAttachments(v); err != nil {
		return err
	}
	if err := t.requireContent(v.ScopeID, v.BodyRef); err != nil {
		return err
	}
	if expected < 0 || v.Revision != expected+1 {
		return ErrConflict
	}
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if expected == 0 {
		var key any
		if v.PlatformKey != nil {
			key = platformKey(v)
		}
		_, err = t.tx.ExecContext(t.ctx, "INSERT INTO messages(id,revision,source_key,payload) VALUES(?,?,?,?)", v.ID, v.Revision, key, string(payload))
		return err
	}
	old, err := t.GetMessage(v.ID)
	if err != nil {
		return err
	}
	if old.ScopeID != v.ScopeID || old.Source != v.Source || platformKey(old) != platformKey(v) || old.BodyRef != v.BodyRef || old.ConversationID != v.ConversationID || !reflect.DeepEqual(old.Attachments, v.Attachments) || !reflect.DeepEqual(old.DerivedFrom, v.DerivedFrom) || (old.RecalledEvidenceRef != "" && old.RecalledEvidenceRef != v.RecalledEvidenceRef) {
		return fmt.Errorf("message ownership changed")
	}
	return t.record("messages", v.ID, v.Revision, expected, v)
}

// AcceptMessage records each App's observation, while platform identity chooses
// a single canonical message. Existing recipients are not recreated on replay.
func (t *Tx) AcceptMessage(v work.Message, recipients []work.Inbox) (canonical work.Message, created bool, err error) {
	err = t.atomic(func() error {
		var inner error
		canonical, created, inner = t.acceptMessage(v, recipients)
		return inner
	})
	return
}
func (t *Tx) acceptMessage(v work.Message, recipients []work.Inbox) (work.Message, bool, error) {
	if err := v.Validate(); err != nil {
		return work.Message{}, false, err
	}
	canonical := v
	created := true
	if v.PlatformKey != nil {
		var id string
		err := t.tx.QueryRowContext(t.ctx, "SELECT id FROM messages WHERE source_key=?", platformKey(v)).Scan(&id)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return canonical, false, err
		}
		if err == nil {
			canonical, err = t.GetMessage(id)
			if err != nil {
				return canonical, false, err
			}
			if canonical.ScopeID != v.ScopeID || canonical.Source != v.Source || canonical.BodyRef != v.BodyRef || !reflect.DeepEqual(canonical.Attachments, v.Attachments) {
				return canonical, false, ErrIdempotencyConflict
			}
			created = false
		}
	}
	if !created {
		changed := false
		for _, observation := range v.Observations {
			found := false
			for _, existing := range canonical.Observations {
				if existing.AppID == observation.AppID && existing.IdentityGeneration == observation.IdentityGeneration {
					found = true
					break
				}
			}
			if !found {
				canonical.Observations = append(canonical.Observations, observation)
				changed = true
			}
		}
		if changed {
			expected := canonical.Revision
			canonical.Revision++
			if err := t.putMessage(canonical, expected); err != nil {
				return canonical, false, err
			}
		}
	} else {
		if err := t.putMessage(canonical, 0); err != nil {
			return canonical, false, err
		}
	}
	for _, input := range recipients {
		if input.ScopeID != canonical.ScopeID {
			return canonical, false, fmt.Errorf("recipient scope mismatch")
		}
		input.MessageID = canonical.ID
		// Normalize message events to the canonical identity rather than the App's
		// delivery event so multiple bots cannot create duplicate inbox entries.
		input.EventID = "message:" + canonical.ID
		existing, err := t.findInbox(input.ScopeID, input.RecipientRoleID, input.RecipientContextID, input.EventID)
		if err == nil {
			if existing.MessageID != canonical.ID {
				return canonical, false, ErrIdempotencyConflict
			}
			continue
		}
		if !errors.Is(err, ErrNotFound) {
			return canonical, false, err
		}
		if err = t.PutInbox(input, 0); err != nil {
			return canonical, false, err
		}
	}
	return canonical, created, nil
}
func (t *Tx) findInbox(scope, role, context, event string) (work.Inbox, error) {
	var id string
	var err error
	if context == "" {
		err = t.tx.QueryRowContext(t.ctx, "SELECT id FROM inbox WHERE scope_id=? AND role_id=? AND event_id=? ORDER BY id LIMIT 1", scope, role, event).Scan(&id)
	} else {
		err = t.tx.QueryRowContext(t.ctx, "SELECT id FROM inbox WHERE scope_id=? AND role_id=? AND coalesce(context_id,'')=? AND event_id=?", scope, role, context, event).Scan(&id)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return work.Inbox{}, ErrNotFound
	}
	if err != nil {
		return work.Inbox{}, err
	}
	return t.GetInbox(id)
}
func (t *Tx) PutInbox(v work.Inbox, expected int64) error {
	if v.State == work.InboxProcessed {
		if err := t.ValidateSessionGoalInputConsumption(v); err != nil {
			return err
		}
	}
	if v.AutomationEpisodeID != "" {
		episode, err := t.GetAutomationEpisode(v.AutomationEpisodeID)
		if err != nil {
			return err
		}
		if episode.InputID != v.ID || episode.ScopeID != v.ScopeID || episode.ContextID != v.RecipientContextID {
			return ErrScopeDenied
		}
	}
	if v.SessionGoalIntentID != "" {
		intent, err := t.GetSessionGoalIntent(v.SessionGoalIntentID)
		if err != nil {
			return err
		}
		if intent.InputID != v.ID || intent.ScopeID != v.ScopeID || intent.ContextID != v.RecipientContextID || v.TaskID != "" {
			return ErrScopeDenied
		}
	}
	if err := t.validateControlInput(v, expected); err != nil {
		return err
	}
	if expected == 0 || v.State == work.InboxClaimed {
		if err := t.requireRoleInScope(v.ScopeID, v.RecipientRoleID); err != nil {
			return err
		}
	}
	if err := v.Validate(); err != nil {
		return err
	}
	if v.RelatedTaskID != "" {
		related, err := t.GetTask(v.RelatedTaskID)
		if err != nil {
			return err
		}
		if related.ScopeID != v.ScopeID {
			return ErrScopeDenied
		}
	}
	if v.TaskID != "" {
		task, err := t.GetTask(v.TaskID)
		if err != nil {
			return err
		}
		if task.ScopeID != v.ScopeID {
			return ErrScopeDenied
		}
		if (expected == 0 || v.State == work.InboxClaimed) && task.OwnerRoleID != v.RecipientRoleID {
			return fmt.Errorf("input task owner differs from recipient")
		}
	}
	if v.RecipientContextID != "" {
		context, err := t.GetContext(v.RecipientContextID)
		if err != nil {
			return err
		}
		if context.ScopeID != v.ScopeID || context.RoleID != v.RecipientRoleID {
			return fmt.Errorf("inbox recipient binding mismatch")
		}
	}
	if v.MessageID != "" {
		message, err := t.GetMessage(v.MessageID)
		if err != nil {
			return err
		}
		if message.ScopeID != v.ScopeID {
			return fmt.Errorf("inbox message outside scope")
		}
	}
	if v.State == work.InboxProcessed {
		if err := t.requireContent(v.ScopeID, v.ProcessingOutcomeRef); err != nil {
			return err
		}
	}
	if v.State == work.InboxClaimed || v.State == work.InboxProcessed {
		attempt, err := t.GetAttempt(v.ClaimedAttemptID)
		if err != nil {
			return err
		}
		if attempt.ScopeID != v.ScopeID || attempt.ContextID != v.RecipientContextID {
			return fmt.Errorf("inbox claim belongs to another attempt scope/context")
		}
		admitted := false
		for _, id := range attempt.InputIDs {
			if id == v.ID {
				admitted = true
			}
		}
		if !admitted {
			admissions, e := t.ListAttemptInputAdmissions(attempt.ID)
			if e != nil {
				return e
			}
			for _, admission := range admissions {
				allowed := admission.State == work.InputAdmissionConfirmed || v.State == work.InboxClaimed && (admission.State == work.InputAdmissionSending || admission.State == work.InputAdmissionUnknown)
				if !allowed || admission.RunRef != attempt.RunRef || admission.ScopeID != v.ScopeID || admission.ContextID != v.RecipientContextID {
					continue
				}
				for _, id := range admission.InputIDs {
					if id == v.ID {
						admitted = true
						break
					}
				}
			}
		}
		if !admitted {
			return fmt.Errorf("inbox was not admitted to attempt")
		}
	}
	if expected > 0 {
		old, err := t.GetInbox(v.ID)
		if err != nil {
			return err
		}
		if err = immutable(old.AutomationEpisodeID != v.AutomationEpisodeID || old.SessionGoalIntentID != v.SessionGoalIntentID || old.ControlOfInputID != v.ControlOfInputID || old.ScopeID != v.ScopeID || old.RecipientRoleID != v.RecipientRoleID || old.EventID != v.EventID || old.MessageID != v.MessageID || old.TaskID != v.TaskID || old.RelatedTaskID != v.RelatedTaskID); err != nil {
			return err
		}
		if old.RecipientContextID != v.RecipientContextID && (old.RecipientContextID != "" || old.State != work.InboxPending) {
			return fmt.Errorf("cannot change assigned inbox context")
		}
		if err = work.ValidateInboxTransition(old.State, v.State); err != nil {
			return err
		}
		if old.State == work.InboxProcessed && old.ProcessingOutcomeRef != v.ProcessingOutcomeRef {
			return fmt.Errorf("processed outcome immutable")
		}
	}
	return t.record("inbox", v.ID, v.Revision, expected, v)
}
func (t *Tx) GetInbox(id string) (work.Inbox, error) {
	var v work.Inbox
	err := t.read("inbox", id, &v)
	return v, err
}
func (t *Tx) ListInbox(scopeID string) ([]work.Inbox, error) {
	return listRecords[work.Inbox](t, "inbox", " WHERE scope_id=?", scopeID)
}

func (t *Tx) validateMessageAttachments(message work.Message) error {
	seen := map[string]bool{}
	for _, attachment := range message.Attachments {
		if err := attachment.Validate(); err != nil {
			return err
		}
		if seen[attachment.ContentRef] {
			return fmt.Errorf("duplicate message attachment")
		}
		seen[attachment.ContentRef] = true
		body, err := t.GetContent(message.ScopeID, attachment.ContentRef)
		if err != nil {
			return err
		}
		if attachment.SHA256 != body.SHA256 || attachment.Size != int64(len(body.Data)) || attachment.MediaType != body.MediaType {
			return fmt.Errorf("attachment content metadata mismatch")
		}
	}
	return nil
}
