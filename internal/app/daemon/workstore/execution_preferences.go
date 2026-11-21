package workstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

const SchemaExecutionPreferencesV1 = `
CREATE TABLE IF NOT EXISTS execution_preferences (
 scope_id TEXT PRIMARY KEY REFERENCES scopes(id),
 revision INTEGER NOT NULL CHECK(revision>0),
 payload TEXT NOT NULL
);
`

// ExecutionPreference is a human selection of existing resources. It is not a
// worktree policy. Agents manage their Git branches inside these selected
// locations; the host never guesses a repository from a previous chat.
type ExecutionPreference struct {
	ScopeID             string     `json:"scope_id"`
	SelectedLocationIDs []string   `json:"selected_location_ids"`
	DefaultLocationID   string     `json:"default_location_id,omitempty"`
	SelectedBy          work.Actor `json:"selected_by"`
	SourceRef           string     `json:"source_ref"`
	Revision            int64      `json:"revision"`
}

// PutExecutionPreference is a trusted UI/migration entry point, not an agent
// tool. A caller must authenticate the selecting principal before invoking it.
func (t *Tx) PutExecutionPreference(v ExecutionPreference, expected int64) error {
	scope, err := t.GetScope(v.ScopeID)
	if err != nil {
		return err
	}
	if v.Revision != expected+1 || v.SelectedBy.Kind != work.ActorPrincipal || v.SelectedBy.Validate() != nil {
		return fmt.Errorf("human resource selection with exact revision required")
	}
	if _, err = t.GetPrincipal(v.SelectedBy.ID); err != nil {
		return err
	}
	if (scope.Kind == work.ScopePersonal || scope.Kind == work.ScopeBootstrap) && scope.PrincipalID != v.SelectedBy.ID {
		return fmt.Errorf("personal location selection belongs to another principal")
	}
	if err = t.requireContent(v.ScopeID, v.SourceRef); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, id := range v.SelectedLocationIDs {
		if seen[id] {
			return fmt.Errorf("duplicate selected location")
		}
		seen[id] = true
		location, lookupErr := t.GetLocation(id)
		if lookupErr != nil {
			err = lookupErr
			return err
		}
		if location.IsolatedContextID != "" {
			return fmt.Errorf("isolated scratch is not a selectable business directory")
		}
	}
	if v.DefaultLocationID != "" && !seen[v.DefaultLocationID] {
		return fmt.Errorf("default location must be explicitly selected")
	}
	if len(v.SelectedLocationIDs) > 0 && v.DefaultLocationID == "" {
		return fmt.Errorf("select an explicit default execution location")
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if expected == 0 {
		result, e := t.tx.ExecContext(t.ctx, `INSERT INTO execution_preferences(scope_id,revision,payload) VALUES(?,?,?) ON CONFLICT(scope_id) DO NOTHING`, v.ScopeID, v.Revision, string(raw))
		if e != nil {
			return e
		}
		n, e := result.RowsAffected()
		if e != nil {
			return e
		}
		if n != 1 {
			return ErrConflict
		}
		return nil
	}
	result, err := t.tx.ExecContext(t.ctx, `UPDATE execution_preferences SET revision=?,payload=? WHERE scope_id=? AND revision=?`, v.Revision, string(raw), v.ScopeID, expected)
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

func (t *Tx) GetExecutionPreference(scopeID string) (ExecutionPreference, error) {
	var v ExecutionPreference
	var raw string
	err := t.tx.QueryRowContext(t.ctx, `SELECT payload FROM execution_preferences WHERE scope_id=?`, scopeID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, err
	}
	err = json.Unmarshal([]byte(raw), &v)
	return v, err
}
