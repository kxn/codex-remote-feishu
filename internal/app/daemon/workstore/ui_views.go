package workstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

const SchemaUIViewsV1 = `
CREATE TABLE IF NOT EXISTS team_ui_views (
 id TEXT PRIMARY KEY, scope_id TEXT NOT NULL REFERENCES scopes(id),
 surface_key TEXT NOT NULL, revision INTEGER NOT NULL CHECK(revision>0),
 payload TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS team_ui_views_surface ON team_ui_views(surface_key,id);
`

// UIView contains presentation ownership only. Its target revision is checked
// again by the target business use case; a card never owns an execution.
type UIView struct {
	ReplacesViewID    string              `json:"replaces_view_id,omitempty"`
	ID                string              `json:"id"`
	ScopeID           string              `json:"scope_id"`
	ContextID         string              `json:"context_id,omitempty"`
	SurfaceKey        string              `json:"surface_key"`
	Kind              string              `json:"kind"`
	TargetID          string              `json:"target_id"`
	TargetRevision    int64               `json:"target_revision"`
	Binding           work.RoleBotBinding `json:"binding"`
	Tenant            string              `json:"tenant"`
	ChatID            string              `json:"chat_id"`
	PrincipalID       string              `json:"principal_id,omitempty"`
	ChannelGeneration int64               `json:"channel_generation"`
	BodyRef           string              `json:"body_ref"`
	OperationID       string              `json:"operation_id"`
	ExternalMessageID string              `json:"external_message_id,omitempty"`
	ReplyToMessageID  string              `json:"reply_to_message_id,omitempty"`
	Invalidated       bool                `json:"invalidated"`
	Revision          int64               `json:"revision"`
	CreatedAt         time.Time           `json:"created_at"`
}

func (t *Tx) PutUIView(v UIView, expected int64) error {
	if v.ID == "" || v.SurfaceKey == "" || v.Kind == "" || v.ChatID == "" || v.Tenant == "" || v.TargetID == "" || v.TargetRevision < 1 || v.Revision != expected+1 {
		return fmt.Errorf("view requires precise presentation and target identity")
	}
	if err := t.requireContent(v.ScopeID, v.BodyRef); err != nil {
		return err
	}
	op, err := t.GetOperation(v.OperationID)
	if err != nil {
		return err
	}
	if op.ScopeID != v.ScopeID || op.Kind != "feishu.publish" || op.TargetID != v.ChatID {
		return fmt.Errorf("view operation mismatch")
	}
	if expected > 0 {
		old, e := t.GetUIView(v.ID)
		if e != nil {
			return e
		}
		left, right := old, v
		left.Revision = 0
		right.Revision = 0
		left.ExternalMessageID = ""
		right.ExternalMessageID = ""
		left.Invalidated = false
		right.Invalidated = false
		a, _ := json.Marshal(left)
		b, _ := json.Marshal(right)
		if string(a) != string(b) || old.Invalidated && !v.Invalidated || old.ExternalMessageID != "" && old.ExternalMessageID != v.ExternalMessageID {
			return ErrIdempotencyConflict
		}
	}
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var result sql.Result
	if expected == 0 {
		result, err = t.tx.ExecContext(t.ctx, "INSERT INTO team_ui_views(id,scope_id,surface_key,revision,payload) VALUES(?,?,?,?,?) ON CONFLICT(id) DO NOTHING", v.ID, v.ScopeID, v.SurfaceKey, v.Revision, string(body))
	} else {
		result, err = t.tx.ExecContext(t.ctx, "UPDATE team_ui_views SET revision=?,payload=? WHERE id=? AND revision=?", v.Revision, string(body), v.ID, expected)
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

func (t *Tx) GetUIView(id string) (UIView, error) {
	var v UIView
	var raw string
	err := t.tx.QueryRowContext(t.ctx, "SELECT payload FROM team_ui_views WHERE id=?", id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, err
	}
	err = json.Unmarshal([]byte(raw), &v)
	return v, err
}
func (t *Tx) ListUIViews(scopeID string) ([]UIView, error) {
	rows, err := t.tx.QueryContext(t.ctx, "SELECT payload FROM team_ui_views WHERE scope_id=? ORDER BY id", scopeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UIView
	for rows.Next() {
		var raw string
		var v UIView
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ListActiveRuntimeViews limits recovery retirement to the worker's current
// actionable cards instead of decoding the scope's entire navigation history.
func (t *Tx) ListActiveRuntimeViews(scopeID, surfaceKey string) ([]UIView, error) {
	return listRecords[UIView](t, "team_ui_views", " WHERE scope_id=? AND surface_key=? AND COALESCE(json_extract(payload,'$.invalidated'),0)=0 AND json_extract(payload,'$.kind') IN ('runtime.worker','runtime.rebuilding')", scopeID, surfaceKey)
}

func (t *Tx) ValidateViewInteraction(view UIView, interaction HumanInteraction) error {
	raw := interaction.Envelope
	if view.Invalidated || view.Binding.GatewayRef != interaction.Gateway.GatewayRef || view.Binding.AppID != interaction.Gateway.AppID || view.Binding.IdentityGeneration != interaction.Gateway.BotGeneration || view.ChatID != raw.ChatID || view.Tenant != raw.Tenant || view.ExternalMessageID == "" || view.ExternalMessageID != raw.MessageID || view.PrincipalID != "" && view.PrincipalID != interaction.PrincipalID {
		return fmt.Errorf("this view is obsolete or belongs to another conversation")
	}
	current, err := t.GetRoleBotBinding(view.Binding.RoleID)
	if err != nil {
		return err
	}
	if current != view.Binding {
		return ErrConflict
	}
	scope, err := t.GetScope(view.ScopeID)
	if err != nil {
		return err
	}
	if scope.Kind == work.ScopeProject {
		channel, e := t.GetProjectChannelBinding(view.Tenant, view.ChatID)
		if e != nil {
			return e
		}
		if !channel.Active || channel.ProjectID != scope.ProjectID || channel.BindingGeneration != view.ChannelGeneration {
			return fmt.Errorf("project conversation binding changed")
		}
	} else if scope.PrincipalID != interaction.PrincipalID {
		return fmt.Errorf("personal view belongs to another person")
	}
	return nil
}
