package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/worksettings"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (a *App) registerTeamCatalogView(tx *workstore.Tx, view workstore.UIView, state teamSettingsState, snapshot worksettings.Snapshot) error {
	if state.Kind != "model" && state.Kind != "reasoning" || teamFixedModel(snapshot) != "" {
		return nil
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if err = tx.PutContent(work.NewContent(view.ID+":catalog-wait", view.ScopeID, "application/json", raw, time.Now().UTC())); err != nil {
		return err
	}
	baseline, _ := json.Marshal(snapshot.CatalogEvidence)
	return tx.PutContent(work.NewContent(view.ID+":catalog-baseline", view.ScopeID, "application/json", baseline, time.Now().UTC()))
}

// Data arrival and initial Feishu publication are independent. The durable
// waiting view keeps their order without requiring another user refresh.
func (a *App) advanceTeamModelCatalogViews(ctx context.Context) error {
	var views []workstore.UIView
	if err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		var err error
		views, err = tx.ListWaitingModelCatalogViews(a.teamRuntime.catalogViewCursor, 32)
		return err
	}); err != nil {
		return err
	}
	if len(views) == 32 {
		a.teamRuntime.catalogViewCursor = views[len(views)-1].ID
	} else {
		a.teamRuntime.catalogViewCursor = ""
	}
	var failures []error
	for _, view := range views {
		if err := a.advanceTeamModelCatalogView(ctx, view); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
func (a *App) advanceTeamModelCatalogView(ctx context.Context, view workstore.UIView) error {
	var state teamSettingsState
	var owner worksettings.CatalogOwner
	valid := false
	if err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		current, err := tx.GetUIView(view.ID)
		if err != nil {
			return err
		}
		if current.Invalidated {
			return nil
		}
		body, err := tx.GetContent(view.ScopeID, view.ID+":catalog-wait")
		if err != nil {
			return err
		}
		if err = json.Unmarshal(body.Data, &state); err != nil {
			return err
		}
		nav, err := tx.GetPrivateNavigation(view.ScopeID)
		if err != nil {
			return err
		}
		if nav.Revision != state.NavigationRevision || view.ContextID != state.ContextID {
			return nil
		}
		publication, err := tx.GetOperation(view.OperationID)
		if err != nil {
			return err
		}
		if publication.State != work.OperationConfirmed || view.ExternalMessageID == "" {
			return nil
		}
		role, err := tx.GetRole(view.Binding.RoleID)
		if err != nil {
			return err
		}
		if err = tx.CheckRoleSettingsReference(role.ID, state.Fingerprint, role.SettingsRevision); err != nil {
			return nil
		}
		owner = worksettings.CatalogOwner{ContextID: view.ContextID, SettingsRevision: role.SettingsRevision}
		if len(state.CursorTrail) > 0 {
			owner.Cursor = state.CursorTrail[len(state.CursorTrail)-1]
		}
		worker, err := tx.CurrentContextWorker(view.ContextID)
		if err == nil {
			owner.WorkerID, owner.SessionGeneration = worker.ID, worker.Generation
		} else if !errors.Is(err, workstore.ErrNotFound) {
			return err
		}
		valid = true
		return nil
	}); err != nil || !valid {
		return err
	}
	port := teamBotSettingsPort{app: a}
	snapshot, err := port.Snapshot(ctx, view.Binding)
	if err != nil {
		return err
	}
	if snapshot.Fingerprint != state.Fingerprint {
		return nil
	}
	owner.Backend, owner.ProfileID = snapshot.Settings.Backend, teamSettingsProfile(snapshot.Settings)
	observed, err := port.ReadCatalog(ctx, view.Binding, owner)
	if err != nil {
		return err
	}
	if observed.Snapshot == nil {
		return nil
	}
	sameEvidence := false
	if err = a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		body, e := tx.GetContent(view.ScopeID, view.ID+":catalog-baseline")
		if e != nil {
			return e
		}
		var baseline string
		if e = json.Unmarshal(body.Data, &baseline); e != nil {
			return e
		}
		sameEvidence = baseline == observed.Evidence
		return nil
	}); err != nil {
		return err
	}
	if sameEvidence {
		return nil
	}
	snapshot.Catalog, snapshot.CatalogBackend, snapshot.CatalogProfileID, snapshot.CatalogEvidence = observed.Snapshot, owner.Backend, owner.ProfileID, observed.Evidence
	snapshot.CatalogUnavailable = ""
	return a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		current, err := tx.GetUIView(view.ID)
		if err != nil {
			return err
		}
		if current.Invalidated || current.ExternalMessageID == "" {
			return nil
		}
		nav, err := tx.GetPrivateNavigation(view.ScopeID)
		if err != nil {
			return err
		}
		if nav.Revision != state.NavigationRevision {
			return nil
		}
		binding, err := tx.GetRoleBotBinding(view.Binding.RoleID)
		if err != nil {
			return err
		}
		if binding != view.Binding {
			return nil
		}
		if err = tx.CheckRoleSettingsReference(binding.RoleID, state.Fingerprint, owner.SettingsRevision); err != nil {
			return nil
		}
		pref, err := tx.GetContextPreferences(view.ContextID)
		if errors.Is(err, workstore.ErrNotFound) {
			pref = work.ContextPreferences{ContextID: view.ContextID, ScopeID: view.ScopeID}
		} else if err != nil {
			return err
		}
		if pref.Revision != state.ContextRevision {
			return nil
		}
		id := workui.ID(view.ID, "catalog-ready", observed.Evidence)
		state.NextCursor = observed.Snapshot.NextCursor
		raw, err := json.Marshal(state)
		if err != nil {
			return err
		}
		if err = tx.PutContent(work.NewContent(id+":settings-state", view.ScopeID, "application/json", raw, time.Now().UTC())); err != nil {
			return err
		}
		card, err := teamSettingsCard(id, snapshot, pref, state.Kind, "", state.CursorTrail...)
		if err != nil {
			return err
		}
		next := current
		next.ID, next.ReplacesViewID = id, current.ID
		next.BodyRef, next.OperationID, next.ExternalMessageID = "", "", ""
		if _, err = workui.Queue(tx, next, card); err != nil {
			return err
		}
		return tx.PutContent(work.NewContent(view.ID+":catalog-result", view.ScopeID, "text/plain", []byte(id), time.Now().UTC()))
	})
}
