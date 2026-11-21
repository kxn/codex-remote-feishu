package workstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (t *Tx) PutRoleBotBinding(v work.RoleBotBinding, expectedGeneration int64) error {
	if err := t.exists("roles", v.RoleID); err != nil {
		return err
	}
	return t.record("role_bot_bindings", v.RoleID, v.IdentityGeneration, expectedGeneration, v)
}
func (t *Tx) GetRoleBotBinding(roleID string) (work.RoleBotBinding, error) {
	var v work.RoleBotBinding
	err := t.read("role_bot_bindings", roleID, &v)
	return v, err
}
func (t *Tx) ListRoleBotBindings() ([]work.RoleBotBinding, error) {
	return listRecords[work.RoleBotBinding](t, "role_bot_bindings", "")
}

// PutProjectChannelBinding preserves all generations. Rebinding deactivates the
// previous row, and the unique partial index fences active project ownership.
func (t *Tx) PutProjectChannelBinding(v work.ProjectChannelBinding, expectedGeneration int64) error {
	return t.atomic(func() error {
		if err := v.Validate(); err != nil {
			return err
		}
		if v.BindingGeneration != expectedGeneration+1 {
			return ErrConflict
		}
		if err := t.exists("projects", v.ProjectID); err != nil {
			return err
		}
		old, err := t.GetProjectChannelBinding(v.Tenant, v.ChatID)
		if errors.Is(err, ErrNotFound) {
			if expectedGeneration != 0 {
				return ErrConflict
			}
		} else if err != nil {
			return err
		} else {
			if old.BindingGeneration != expectedGeneration {
				return ErrConflict
			}
			if old.Active {
				old.Active = false
				payload, e := json.Marshal(old)
				if e != nil {
					return e
				}
				if _, e = t.tx.ExecContext(t.ctx, "UPDATE channel_bindings SET payload=? WHERE tenant=? AND chat_id=? AND generation=?", string(payload), old.Tenant, old.ChatID, old.BindingGeneration); e != nil {
					return e
				}
			}
		}
		payload, err := json.Marshal(v)
		if err != nil {
			return err
		}
		_, err = t.tx.ExecContext(t.ctx, "INSERT INTO channel_bindings(tenant,chat_id,generation,payload) VALUES(?,?,?,?)", v.Tenant, v.ChatID, v.BindingGeneration, string(payload))
		return err
	})
}
func (t *Tx) GetProjectChannelBinding(tenant, chatID string) (work.ProjectChannelBinding, error) {
	var v work.ProjectChannelBinding
	var payload string
	err := t.tx.QueryRowContext(t.ctx, "SELECT payload FROM channel_bindings WHERE tenant=? AND chat_id=? ORDER BY generation DESC LIMIT 1", tenant, chatID).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, err
	}
	err = json.Unmarshal([]byte(payload), &v)
	return v, err
}
func (t *Tx) GetChannelProject(tenant, chatID string) (work.Project, error) {
	binding, err := t.GetProjectChannelBinding(tenant, chatID)
	if err != nil {
		return work.Project{}, err
	}
	if !binding.Active {
		return work.Project{}, ErrNotFound
	}
	return t.GetProject(binding.ProjectID)
}
func (t *Tx) ListProjectChannelBindings(projectID string) ([]work.ProjectChannelBinding, error) {
	rows, err := t.tx.QueryContext(t.ctx, "SELECT payload FROM channel_bindings WHERE project_id=? ORDER BY tenant,chat_id,generation", projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []work.ProjectChannelBinding{}
	for rows.Next() {
		var payload string
		if err = rows.Scan(&payload); err != nil {
			return nil, err
		}
		var value work.ProjectChannelBinding
		if err = json.Unmarshal([]byte(payload), &value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}
func (t *Tx) SatisfyDependency(dependency work.Dependency) error {
	if err := dependency.Validate(); err != nil {
		return err
	}
	if dependency.SatisfiedBy == nil {
		return fmt.Errorf("dependency satisfaction needs evidence")
	}
	result, err := t.tx.ExecContext(t.ctx, "UPDATE dependencies SET satisfied_by=? WHERE parent_id=? AND child_id=? AND condition=?", mustJSON(dependency.SatisfiedBy), dependency.TaskID, dependency.DependsOnTaskID, dependency.Condition)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrNotFound
	}
	return nil
}
func mustJSON(value any) string { data, _ := json.Marshal(value); return string(data) }
