package workstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (t *Tx) GetContextPreferences(contextID string) (work.ContextPreferences, error) {
	var value work.ContextPreferences
	var raw string
	err := t.tx.QueryRowContext(t.ctx, `SELECT payload FROM context_preferences WHERE context_id=?`, contextID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return value, ErrNotFound
	}
	if err != nil {
		return value, err
	}
	err = json.Unmarshal([]byte(raw), &value)
	return value, err
}

// PutContextPreferences validates its actual private command/interaction/input
// rather than accepting a caller-supplied actor or arbitrary content reference.
func (t *Tx) PutContextPreferences(value work.ContextPreferences, expected int64, source PrivateActionSource) error {
	if err := value.Validate(); err != nil {
		return err
	}
	if value.Revision != expected+1 {
		return ErrConflict
	}
	scope, ref, err := t.ValidatePrivateAction(source)
	if err != nil {
		return err
	}
	context, err := t.GetContext(value.ContextID)
	if err != nil {
		return err
	}
	if context.ScopeID != scope.ID || value.ScopeID != scope.ID || context.RoleID != scope.RoleID || value.UpdatedBy != (work.Actor{Kind: work.ActorPrincipal, ID: scope.PrincipalID}) || value.Source != ref {
		return ErrScopeDenied
	}
	if source.MessageID != "" {
		attempt, err := t.GetAttemptByRunRef(source.RunRef.RunID)
		if err != nil {
			return err
		}
		if attempt.ContextID != context.ID {
			return ErrScopeDenied
		}
	} else {
		nav, err := t.GetPrivateNavigation(scope.ID)
		if err != nil {
			return err
		}
		if nav.FrontContextID != context.ID {
			return fmt.Errorf("settings target is not the selected private context")
		}
		if source.CommandID != "" {
			command, err := t.GetPrivateCommand(source.CommandID)
			if err != nil {
				return err
			}
			if command.ContextID != context.ID || command.NavigationRevision != nav.Revision {
				return ErrScopeDenied
			}
		}
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	var result sql.Result
	if expected == 0 {
		result, err = t.tx.ExecContext(t.ctx, `INSERT INTO context_preferences(context_id,scope_id,revision,payload) VALUES(?,?,?,?) ON CONFLICT DO NOTHING`, value.ContextID, value.ScopeID, value.Revision, string(raw))
	} else {
		result, err = t.tx.ExecContext(t.ctx, `UPDATE context_preferences SET revision=?,payload=? WHERE context_id=? AND scope_id=? AND revision=?`, value.Revision, string(raw), value.ContextID, value.ScopeID, expected)
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
	return nil
}
