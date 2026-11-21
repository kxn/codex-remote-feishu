package workstore

import (
	"errors"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (t *Tx) RequireOpenBootstrapScope(scope work.WorkScope) error {
	if scope.Kind != work.ScopeBootstrap {
		return ErrScopeDenied
	}
	binding, err := t.GetProjectChannelBinding(scope.Tenant, scope.ChatID)
	if errors.Is(err, ErrNotFound) && scope.BindingGeneration == 0 {
		return nil
	}
	if err != nil {
		return err
	}
	if binding.Active || binding.BindingGeneration != scope.BindingGeneration {
		return ErrConflict
	}
	return nil
}
