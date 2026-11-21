package daemon

import (
	"context"
	"errors"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// Proactive completion is independent of model availability and the selected
// personal front. The original confirmed identity is never replaced with a
// newly configured bot's credentials or a guessed private chat.
func (a *App) runTeamCodexUpgradeViews(ctx context.Context) error {
	r := a.teamRuntime
	if r.codexUpgrader == nil {
		return nil
	}
	var candidates []workstore.UIView
	err := r.store.Within(ctx, func(tx *workstore.Tx) error {
		scopes, e := tx.ListScopes()
		if e != nil {
			return e
		}
		for _, scope := range scopes {
			if scope.Kind != work.ScopePersonal {
				continue
			}
			views, e := tx.ListUIViews(scope.ID)
			if e != nil {
				return e
			}
			for _, view := range views {
				if view.Kind == "host.codex-upgrade.confirm" || view.Kind == "host.codex-upgrade.status" {
					candidates = append(candidates, view)
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	var failures []error
	for _, source := range candidates {
		id := source.TargetID
		if source.Kind == "host.codex-upgrade.confirm" {
			id = teamID("codex-upgrade", source.TargetID)
		}
		record, e := r.codexUpgrader.Status(ctx, id)
		if errors.Is(e, workstore.ErrNotFound) {
			continue
		}
		if e != nil {
			failures = append(failures, e)
			continue
		}
		if record.Source.PersonalScopeID != source.ScopeID || (record.Stage != "completed" && record.Stage != "unknown") {
			continue
		}
		resultID := workui.ID("codex-upgrade-result", record.ID, record.Stage)
		if seen[resultID] {
			continue
		}
		queued := false
		e = r.store.Within(ctx, func(tx *workstore.Tx) error {
			if _, e := tx.GetUIView(resultID); e == nil {
				queued = true
				return nil
			} else if !errors.Is(e, workstore.ErrNotFound) {
				return e
			}
			binding, e := tx.GetRoleBotBinding(source.Binding.RoleID)
			if e != nil {
				return e
			}
			if binding != source.Binding {
				return nil
			}
			scope, e := tx.GetScope(source.ScopeID)
			if e != nil {
				return e
			}
			if scope.PrincipalID != source.PrincipalID || scope.RoleID != binding.RoleID {
				return workstore.ErrScopeDenied
			}
			card, e := workui.Card(resultID, teamCodexUpgradePanel(record))
			if e != nil {
				return e
			}
			view := workstore.UIView{ID: resultID, ScopeID: source.ScopeID, ContextID: source.ContextID, SurfaceKey: workui.ID("codex-upgrade-result", record.ID), Kind: "host.codex-upgrade.result", TargetID: record.ID, TargetRevision: record.Revision, Binding: binding, Tenant: source.Tenant, ChatID: source.ChatID, PrincipalID: source.PrincipalID}
			_, e = workui.Queue(tx, view, card)
			queued = e == nil
			return e
		})
		if queued {
			seen[resultID] = true
		}
		if e != nil {
			failures = append(failures, e)
		}
	}
	return errors.Join(failures...)
}
