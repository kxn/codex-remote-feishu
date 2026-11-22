package workpublish

import (
	"context"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// Gateway resolution can yield. Check current authority again immediately
// before issuing the write; revocation cannot retract a request already sent.
func (p *Publisher) revalidateClaim(ctx context.Context, job publication) error {
	return p.store.Within(ctx, func(tx *workstore.Tx) error {
		binding, err := tx.GetRoleBotBinding(job.Binding.RoleID)
		if err != nil {
			return err
		}
		if binding != job.Binding {
			return workservice.ErrPublicationDenied
		}
		scope, err := tx.GetScope(job.Operation.ScopeID)
		if err != nil {
			return err
		}
		if scope.Kind == work.ScopeProject {
			channel, err := tx.GetProjectChannelBinding(job.Projection.Tenant, job.Projection.ChatID)
			if err != nil {
				return err
			}
			if !channel.Active || channel.ProjectID != scope.ProjectID || channel.BindingGeneration != job.Projection.ChannelGeneration {
				return workservice.ErrPublicationDenied
			}
		}
		if job.Projection.UIViewID != "" {
			view, err := tx.GetUIView(job.Projection.UIViewID)
			if err != nil {
				return err
			}
			if view.Invalidated {
				return workservice.ErrPublicationDenied
			}
		}
		if obsolete, e := p.progressObsolete(tx, scope, job.Operation, job.Projection); e != nil {
			return e
		} else if obsolete {
			return workservice.ErrPublicationDenied
		}
		return workservice.ValidatePublication(tx, job.Operation, job.Projection)
	})
}
