package workstore

import (
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// atomic protects compound methods even when a caller deliberately handles an
// error and continues the surrounding transaction.
func (t *Tx) atomic(fn func() error) error {
	if _, err := t.tx.ExecContext(t.ctx, "SAVEPOINT work_atomic"); err != nil {
		return err
	}
	if err := fn(); err != nil {
		_, rollback := t.tx.ExecContext(t.ctx, "ROLLBACK TO work_atomic")
		_, release := t.tx.ExecContext(t.ctx, "RELEASE work_atomic")
		if rollback != nil {
			return fmt.Errorf("%w (rollback failed: %v)", err, rollback)
		}
		if release != nil {
			return fmt.Errorf("%w (release failed: %v)", err, release)
		}
		return err
	}
	_, err := t.tx.ExecContext(t.ctx, "RELEASE work_atomic")
	return err
}
func (t *Tx) Delegate(child work.Task, input work.Inbox, dependency work.Dependency) error {
	return t.atomic(func() error {
		parent, err := t.GetTask(dependency.TaskID)
		if err != nil {
			return err
		}
		if dependency.DependsOnTaskID != child.ID || parent.ScopeID != child.ScopeID || input.ScopeID != child.ScopeID || input.RecipientRoleID != child.OwnerRoleID {
			return fmt.Errorf("delegation scope/recipient mismatch")
		}
		if input.State != work.InboxPending {
			return fmt.Errorf("delegation input must be pending")
		}
		if err = t.PutTask(child, 0); err != nil {
			return err
		}
		if err = t.PutInbox(input, 0); err != nil {
			return err
		}
		return t.AddDependency(dependency)
	})
}
