package workstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

var ErrPrivateReactionSourceGone = errors.New("private reaction source no longer exists")

const SchemaPrivateReactionsV1 = `CREATE TABLE IF NOT EXISTS team_private_reactions (
 id TEXT PRIMARY KEY, input_id TEXT NOT NULL UNIQUE, finished INTEGER NOT NULL,
 next_at INTEGER NOT NULL, updated_at INTEGER NOT NULL, payload TEXT NOT NULL CHECK(json_valid(payload))
); CREATE INDEX IF NOT EXISTS team_private_reactions_pending ON team_private_reactions(finished,next_at,updated_at);`

// PrivateReaction is only a presentation receipt. It never admits or consumes input.
// InFlight survives uncertain HTTP writes so restart reconciles the exact bot's
// platform reaction before changing the presentation again.
type PrivateReaction struct {
	ID             string              `json:"id"`
	InputID        string              `json:"input_id"`
	ScopeID        string              `json:"scope_id"`
	Binding        work.RoleBotBinding `json:"binding"`
	Message        work.MessageKey     `json:"message"`
	ReactionIDs    map[string]string   `json:"reaction_ids"`
	InFlightEmoji  string              `json:"in_flight_emoji,omitempty"`
	InFlightAction string              `json:"in_flight_action,omitempty"`
	Finished       bool                `json:"finished"`
	NextAt         time.Time           `json:"next_at"`
	UpdatedAt      time.Time           `json:"updated_at"`
	LastError      string              `json:"last_error,omitempty"`
	Revision       int64               `json:"revision"`
}

func (t *Tx) GetPrivateReaction(id string) (v PrivateReaction, err error) {
	var raw string
	err = t.tx.QueryRowContext(t.ctx, "SELECT payload FROM team_private_reactions WHERE id=?", id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, err
	}
	err = json.Unmarshal([]byte(raw), &v)
	return
}

func (t *Tx) PutPrivateReaction(v PrivateReaction, expected int64) error {
	if v.ID == "" || v.InputID == "" || v.ScopeID == "" || v.Revision != expected+1 || v.UpdatedAt.IsZero() {
		return fmt.Errorf("invalid private reaction receipt")
	}
	if err := v.Message.Validate(); err != nil {
		return err
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if expected == 0 {
		if err = t.AssertPersonalMigrationOpen(v.ScopeID); err != nil {
			return err
		}
		_, err = t.tx.ExecContext(t.ctx, "INSERT INTO team_private_reactions(id,input_id,finished,next_at,updated_at,payload) VALUES(?,?,?,?,?,?)", v.ID, v.InputID, v.Finished, privateReactionTime(v.NextAt), privateReactionTime(v.UpdatedAt), string(raw))
		return err
	}
	old, err := t.GetPrivateReaction(v.ID)
	if err != nil {
		return err
	}
	if old.Revision != expected {
		return ErrConflict
	}
	if old.InputID != v.InputID || old.ScopeID != v.ScopeID || old.Binding != v.Binding || old.Message != v.Message {
		return ErrIdempotencyConflict
	}
	if old.InFlightAction == "" && v.InFlightAction != "" {
		if err = t.AssertPersonalMigrationOpen(v.ScopeID); err != nil {
			return err
		}
	}
	result, err := t.tx.ExecContext(t.ctx, "UPDATE team_private_reactions SET finished=?,next_at=?,updated_at=?,payload=? WHERE id=? AND json_extract(payload,'$.revision')=?", v.Finished, privateReactionTime(v.NextAt), privateReactionTime(v.UpdatedAt), string(raw), v.ID, expected)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return ErrConflict
	}
	return err
}

func (t *Tx) registerPrivateInputReaction(input work.Inbox) error {
	if input.MessageID == "" || input.ControlOfInputID != "" {
		return nil
	}
	scope, err := t.GetScope(input.ScopeID)
	if err != nil {
		return err
	}
	if scope.Kind != work.ScopePersonal {
		return nil
	}
	if err = t.AssertPersonalMigrationOpen(scope.ID); errors.Is(err, ErrPersonalMigrationFenced) {
		return nil
	} else if err != nil {
		return err
	}
	message, err := t.GetMessage(input.MessageID)
	if err != nil {
		return err
	}
	if message.Source.Kind != work.ActorPrincipal || message.PlatformKey == nil {
		return nil
	}
	binding, err := t.GetRoleBotBinding(input.RecipientRoleID)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	observed := false
	for _, observation := range message.Observations {
		if observation.AppID == binding.AppID && observation.IdentityGeneration == binding.IdentityGeneration {
			observed = true
		}
	}
	if !observed {
		return nil
	}
	id := "input-reaction:" + input.ID
	old, err := t.GetPrivateReaction(id)
	if errors.Is(err, ErrNotFound) {
		return t.PutPrivateReaction(PrivateReaction{ID: id, InputID: input.ID, ScopeID: input.ScopeID, Binding: binding, Message: *message.PlatformKey, ReactionIDs: map[string]string{}, UpdatedAt: time.Now().UTC(), Revision: 1}, 0)
	}
	if err != nil {
		return err
	}
	// A replacement bot is never authorized to mutate the previous bot's receipt.
	if old.Binding != binding {
		return nil
	}
	previous := old.Revision
	old.Revision++
	old.Finished = false
	old.NextAt = time.Time{}
	old.UpdatedAt = time.Now().UTC()
	return t.PutPrivateReaction(old, previous)
}

// Backfill only live input on deployment. Historical completed messages must
// not acquire fresh acknowledgements merely because this feature was enabled.
func (t *Tx) BackfillPrivateInputReactions(limit int) error {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT i.payload FROM inbox i JOIN scopes s ON s.id=json_extract(i.payload,'$.scope_id') JOIN messages m ON m.id=json_extract(i.payload,'$.message_id') JOIN role_bot_bindings b ON b.id=json_extract(i.payload,'$.recipient_role_id') WHERE json_extract(s.payload,'$.kind')='personal' AND NOT EXISTS(SELECT 1 FROM personal_migration_fences f WHERE f.scope_id=s.id) AND json_extract(m.payload,'$.source.kind')='principal' AND json_extract(m.payload,'$.platform_key.message_id') IS NOT NULL AND coalesce(json_extract(i.payload,'$.control_of_input_id'),'')='' AND json_extract(i.payload,'$.state') IN ('pending','claimed') AND EXISTS(SELECT 1 FROM json_each(json_extract(m.payload,'$.observations')) o WHERE json_extract(o.value,'$.app_id')=json_extract(b.payload,'$.app_id') AND json_extract(o.value,'$.identity_generation')=json_extract(b.payload,'$.identity_generation')) AND NOT EXISTS(SELECT 1 FROM team_private_reactions r WHERE r.input_id=i.id) ORDER BY i.id LIMIT ?`, limit)
	if err != nil {
		return err
	}
	var inputs []work.Inbox
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return err
		}
		var input work.Inbox
		if err = json.Unmarshal([]byte(raw), &input); err != nil {
			rows.Close()
			return err
		}
		inputs = append(inputs, input)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, input := range inputs {
		if err = t.registerPrivateInputReaction(input); err != nil {
			return err
		}
	}
	return nil
}

func (t *Tx) ListPendingPrivateReactions(now time.Time, limit int) ([]PrivateReaction, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT r.payload FROM team_private_reactions r WHERE r.finished=0 AND r.next_at<=? AND NOT EXISTS(SELECT 1 FROM personal_migration_fences f WHERE f.scope_id=json_extract(r.payload,'$.scope_id')) ORDER BY r.updated_at,r.id LIMIT ?`, privateReactionTime(now), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []PrivateReaction
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var v PrivateReaction
		if err = json.Unmarshal([]byte(raw), &v); err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, rows.Err()
}

// PrivateInputReactionState derives only from exact durable ownership facts.
// It never changes an input, attempt, admission or runtime gate.
func (t *Tx) PrivateInputReactionState(v PrivateReaction) (desired []string, terminal bool, err error) {
	binding, err := t.GetRoleBotBinding(v.Binding.RoleID)
	if err != nil {
		return nil, false, err
	}
	if binding != v.Binding {
		return nil, true, nil
	}
	input, err := t.GetInbox(v.InputID)
	if err != nil {
		return nil, false, err
	}
	message, err := t.GetMessage(input.MessageID)
	if err != nil {
		return nil, false, err
	}
	if message.PlatformKey == nil || *message.PlatformKey != v.Message || input.ScopeID != v.ScopeID || input.RecipientRoleID != v.Binding.RoleID {
		return nil, false, ErrScopeDenied
	}
	recalled, err := t.IsPlatformMessageRecalled(v.Message.Tenant, v.Message.ChatID, v.Message.MessageID)
	if err != nil {
		return nil, false, err
	}
	if recalled || message.RecalledEvidenceRef != "" {
		return nil, true, ErrPrivateReactionSourceGone
	}
	if input.ShellOperationID != "" {
		shell, e := t.GetPrivateShellOperation(input.ShellOperationID)
		if e != nil {
			return nil, false, e
		}
		if !containsPrivateShellInput(shell, input.ID) || shell.ScopeID != v.ScopeID || shell.Binding != v.Binding {
			return nil, false, ErrScopeDenied
		}
		if shell.Acknowledged || shell.State == "running" || shell.State == "completed" {
			return []string{"APPLAUSE"}, shell.State == "completed", nil
		}
		if input.State == work.InboxCancelled {
			return []string{"ThumbsDown"}, true, nil
		}
		return []string{"OneSecond"}, false, nil
	}
	switch input.State {
	case work.InboxPending:
		return []string{"OneSecond"}, false, nil
	case work.InboxCancelled:
		return []string{"ThumbsDown"}, true, nil
	}
	attempt, err := t.GetAttempt(input.ClaimedAttemptID)
	if err != nil {
		return nil, false, err
	}
	terminal = attempt.State == work.AttemptCompleted || attempt.State == work.AttemptInterrupted || attempt.State == work.AttemptFailed || input.State == work.InboxProcessed
	original := false
	for _, id := range attempt.InputIDs {
		if id == input.ID {
			original = true
			break
		}
	}
	if !original {
		admissions, e := t.ListAttemptInputAdmissions(attempt.ID)
		if e != nil {
			return nil, false, e
		}
		for _, admission := range admissions {
			if admission.RunRef != attempt.RunRef {
				continue
			}
			for _, id := range admission.InputIDs {
				if id != input.ID {
					continue
				}
				if admission.State == work.InputAdmissionConfirmed {
					native, e := t.GetNativeCommand(admission.CommandID)
					if e != nil {
						return nil, false, e
					}
					if native.Kind != agentproto.CommandTurnSteer {
						return nil, false, ErrScopeDenied
					}
					emoji := "THUMBSUP"
					return []string{emoji}, terminal, nil
				}
				if !terminal {
					return []string{"OneSecond"}, false, nil
				}
			}
		}
		return nil, terminal, nil
	}
	if terminal {
		return nil, true, nil
	}
	if attempt.State == work.AttemptPrepared {
		return []string{"OneSecond"}, false, nil
	}
	// A staged image/file remains a distinct source, but the accompanying text
	// owns THINKING. Never imply that every attachment started a separate turn.
	for _, id := range attempt.InputIDs {
		candidate, e := t.GetInbox(id)
		if e != nil {
			return nil, false, e
		}
		if candidate.AwaitingText {
			continue
		}
		if id == input.ID {
			return []string{"THINKING"}, false, nil
		}
		return nil, false, nil
	}
	return nil, false, nil
}

func privateReactionTime(value time.Time) int64 {
	if value.IsZero() {
		return 0
	}
	return value.UnixNano()
}

func containsPrivateShellInput(op PrivateShellOperation, id string) bool {
	for _, item := range op.InputIDs {
		if item == id {
			return true
		}
	}
	return false
}
