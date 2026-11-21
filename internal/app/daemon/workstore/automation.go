package workstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"strings"
	"time"
)

var ErrAutomationNotApplicable = errors.New("automation does not apply to this settled attempt")

const SchemaAutomationV1 = `CREATE TABLE automation_policies(context_id TEXT NOT NULL REFERENCES contexts(id),kind TEXT NOT NULL CHECK(kind IN ('autowhip','autocontinue')),revision INTEGER NOT NULL CHECK(revision>0),payload TEXT NOT NULL CHECK(json_valid(payload)),PRIMARY KEY(context_id,kind));CREATE TABLE automation_episodes(id TEXT PRIMARY KEY,context_id TEXT NOT NULL REFERENCES contexts(id),trigger_attempt_id TEXT NOT NULL REFERENCES attempts(id),kind TEXT NOT NULL,payload TEXT NOT NULL CHECK(json_valid(payload)),UNIQUE(trigger_attempt_id,kind));`

func (t *Tx) GetAutomationPolicy(contextID string, kind work.AutomationKind) (work.AutomationPolicy, error) {
	var v work.AutomationPolicy
	var raw string
	err := t.tx.QueryRowContext(t.ctx, "SELECT payload FROM automation_policies WHERE context_id=? AND kind=?", contextID, kind).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, err
	}
	err = json.Unmarshal([]byte(raw), &v)
	return v, err
}

// Policies are human UI/command preferences. Role credentials cannot enable
// automation by synthesizing a message source or selecting a different context.
func (t *Tx) PutAutomationPolicy(v work.AutomationPolicy, expected int64, source PrivateActionSource) error {
	return t.atomic(func() error {
		if source.MessageID != "" {
			return ErrScopeDenied
		}
		scope, proof, err := t.ValidatePrivateAction(source)
		if err != nil {
			return err
		}
		wc, err := t.GetContext(v.ContextID)
		if err != nil {
			return err
		}
		if wc.ScopeID != scope.ID || v.ScopeID != scope.ID || wc.RoleID != scope.RoleID || wc.Purpose == work.PurposeControl || wc.Purpose == work.PurposeReview {
			return ErrScopeDenied
		}
		contextID := ""
		switch {
		case source.CommandID != "":
			command, e := t.GetPrivateCommand(source.CommandID)
			if e != nil {
				return e
			}
			contextID = command.ContextID
			expectedVerb := "off"
			if v.Enabled {
				expectedVerb = "on"
			}
			fields := strings.Fields(command.Text)
			if len(fields) != 2 || fields[0] != "/"+string(v.Kind) || fields[1] != expectedVerb {
				return ErrScopeDenied
			}
		case source.MenuInteractionID != "":
			menu, e := t.GetMenuInteraction(source.MenuInteractionID)
			if e != nil {
				return e
			}
			contextID = menu.ContextID
			suffix := "_off"
			if v.Enabled {
				suffix = "_on"
			}
			if menu.Envelope.EventKey != string(v.Kind)+suffix {
				return ErrScopeDenied
			}
		case source.InteractionID != "":
			interaction, e := t.GetHumanInteraction(source.InteractionID)
			if e != nil {
				return e
			}
			viewID, _ := interaction.Envelope.Values["team_view"].(string)
			view, e := t.GetUIView(viewID)
			if e != nil {
				return e
			}
			contextID = view.ContextID
			expectedAction := "automation." + string(v.Kind) + ".off"
			if v.Enabled {
				expectedAction = "automation." + string(v.Kind) + ".on"
			}
			if interaction.Envelope.Values["action"] != expectedAction {
				return ErrScopeDenied
			}
		}
		if contextID != wc.ID || v.Source != proof || v.Revision != expected+1 {
			return ErrScopeDenied
		}
		if old, e := t.GetAutomationPolicy(v.ContextID, v.Kind); e == nil && old.Source.ID == proof.ID && old.Source.Kind == proof.Kind && old.Enabled == v.Enabled {
			return nil
		} else if e != nil && !errors.Is(e, ErrNotFound) {
			return e
		}
		if err = v.Validate(); err != nil {
			return err
		}
		var result sql.Result
		if expected == 0 {
			result, err = t.tx.ExecContext(t.ctx, "INSERT INTO automation_policies(context_id,kind,revision,payload) VALUES(?,?,?,?) ON CONFLICT DO NOTHING", v.ContextID, v.Kind, v.Revision, mustJSON(v))
		} else {
			result, err = t.tx.ExecContext(t.ctx, "UPDATE automation_policies SET revision=?,payload=? WHERE context_id=? AND kind=? AND revision=?", v.Revision, mustJSON(v), v.ContextID, v.Kind, expected)
		}
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrConflict
		}
		if !v.Enabled {
			rows, err := t.tx.QueryContext(t.ctx, "SELECT payload FROM automation_episodes WHERE context_id=? AND kind=?", wc.ID, v.Kind)
			if err != nil {
				return err
			}
			var inputs []string
			for rows.Next() {
				var raw string
				var episode work.AutomationEpisode
				if err = rows.Scan(&raw); err != nil {
					rows.Close()
					return err
				}
				if err = json.Unmarshal([]byte(raw), &episode); err != nil {
					rows.Close()
					return err
				}
				inputs = append(inputs, episode.InputID)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return err
			}
			for _, id := range inputs {
				input, e := t.GetInbox(id)
				if errors.Is(e, ErrNotFound) {
					continue
				}
				if e != nil {
					return e
				}
				if input.State == work.InboxPending {
					before := input.Revision
					input.Revision++
					input.State = work.InboxCancelled
					if e = t.PutInbox(input, before); e != nil {
						return e
					}
				}
			}
		}
		return nil
	})
}
func (t *Tx) GetAutomationEpisode(id string) (work.AutomationEpisode, error) {
	var v work.AutomationEpisode
	var raw string
	err := t.tx.QueryRowContext(t.ctx, "SELECT payload FROM automation_episodes WHERE id=?", id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, err
	}
	err = json.Unmarshal([]byte(raw), &v)
	return v, err
}
func automationDelay(kind work.AutomationKind, count int) (time.Duration, error) {
	delays := []time.Duration{3 * time.Second, 10 * time.Second, 30 * time.Second}
	if kind == work.AutomationContinue {
		delays = []time.Duration{0, 0, 2 * time.Second, 5 * time.Second, 10 * time.Second}
	}
	if count < 1 || count > len(delays) {
		return 0, ErrAutomationNotApplicable
	}
	return delays[count-1], nil
}
