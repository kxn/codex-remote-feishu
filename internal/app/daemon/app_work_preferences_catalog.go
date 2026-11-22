package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/worksettings"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// ReadCatalog reads only this frozen context/carrier/profile's persisted page.
// Native discovery runs asynchronously so the ingress pump can persist its reply.
func (p teamBotSettingsPort) ReadCatalog(ctx context.Context, binding work.RoleBotBinding, owner worksettings.CatalogOwner) (worksettings.ObservedCatalog, error) {
	unavailable := func(reason string) worksettings.ObservedCatalog {
		return worksettings.ObservedCatalog{Unavailable: reason}
	}
	if p.app == nil || p.app.teamRuntime == nil {
		return unavailable("模型目录尚未启动，请稍后刷新。"), nil
	}
	r := p.app.teamRuntime
	var key workstore.RuntimeModelCatalogKey
	var observed workstore.RuntimeModelCatalog
	connection := ""
	if owner.Backend == agentproto.BackendCodex {
		var identityErr error
		connection, identityErr = p.app.teamCatalogConnection(ctx, binding, owner.ProfileID)
		if identityErr != nil {
			return unavailable("当前登录身份尚未确认；连接就绪后会自动更新模型目录。"), nil
		}
	}
	ready := false
	err := r.store.Within(ctx, func(tx *workstore.Tx) error {
		actual, err := tx.GetRoleBotBinding(binding.RoleID)
		if err != nil {
			return err
		}
		if actual != binding {
			return worksettings.ErrConflict
		}
		wc, err := tx.GetContext(owner.ContextID)
		if err != nil {
			return err
		}
		scope, err := tx.GetScope(wc.ScopeID)
		if err != nil {
			return err
		}
		if scope.Kind != work.ScopePersonal || wc.RoleID != binding.RoleID || scope.RoleID != binding.RoleID {
			return workstore.ErrScopeDenied
		}
		if connection != "" {
			cached, e := tx.RuntimeCatalogForConnection(binding, owner.ProfileID, connection, owner.Cursor, time.Now().UTC())
			if e == nil {
				observed = cached
				return nil
			}
			if !errors.Is(e, workstore.ErrNotFound) {
				return e
			}
		}
		if owner.WorkerID == "" {
			return nil
		}
		worker, err := tx.GetWorker(owner.WorkerID)
		if err != nil {
			return err
		}
		if worker.ContextID != wc.ID || worker.ScopeID != scope.ID || worker.RoleID != binding.RoleID || worker.Generation != owner.SessionGeneration {
			return workstore.ErrScopeDenied
		}
		if worker.State == work.WorkerRetired {
			return nil
		}
		role, err := tx.GetRole(binding.RoleID)
		if err != nil {
			return err
		}
		if role.SettingsRevision != owner.SettingsRevision {
			return worksettings.ErrConflict
		}
		var prefRevision int64
		pref, err := tx.GetContextPreferences(wc.ID)
		if err == nil {
			prefRevision = pref.Revision
		} else if !errors.Is(err, workstore.ErrNotFound) {
			return err
		}
		key, err = tx.RuntimeCatalogKey(worker.ID)
		if errors.Is(err, workstore.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		sameConnection := false
		if connection != "" && key.Backend == work.BackendCodex {
			actual, e := tx.RuntimeCatalogConnection(key)
			if e != nil && !errors.Is(e, workstore.ErrNotFound) {
				return e
			}
			sameConnection = actual != "" && actual == connection
		}
		if key.WorkerID != owner.WorkerID || key.SessionGeneration != owner.SessionGeneration || (!sameConnection && (key.SettingsRevision != owner.SettingsRevision || key.PreferencesRevision != prefRevision)) || key.ProfileID != owner.ProfileID || string(key.Backend) != string(owner.Backend) || key.BotGeneration != binding.IdentityGeneration || (connection != "" && !sameConnection) {
			key = workstore.RuntimeModelCatalogKey{}
			return nil
		}
		ready = true
		observed, err = tx.GetRuntimeModelCatalogPage(key, owner.Cursor)
		if errors.Is(err, workstore.ErrNotFound) {
			return nil
		}
		return err
	})
	if err != nil {
		return worksettings.ObservedCatalog{}, err
	}
	if observed.CommandID != "" && !observed.Snapshot.RefreshedAt.IsZero() && !observed.Snapshot.RefreshedAt.Before(time.Now().Add(-5*time.Minute)) {
		return worksettings.ObservedCatalog{Snapshot: &observed.Snapshot, Evidence: observed.CommandID}, nil
	}
	if !ready {
		return unavailable("正在连接模型服务；连接就绪后会自动更新此卡片。"), nil
	}
	started := r.startModelCatalogDiscovery(key, owner.Cursor)
	if observed.CommandID != "" {
		return worksettings.ObservedCatalog{Snapshot: &observed.Snapshot, Evidence: observed.CommandID}, nil
	}
	if !started {
		if r.runContext == nil || r.runContext.Err() != nil || r.driver == nil {
			return unavailable("模型服务当前未运行；恢复连接后会自动更新此卡片。"), nil
		}
		return unavailable("正在读取模型列表，完成后会自动更新此卡片。"), nil
	}
	return unavailable("正在读取模型列表，完成后会自动更新此卡片。"), nil
}

func (r *teamRuntime) startModelCatalogDiscovery(key workstore.RuntimeModelCatalogKey, cursor string) bool {
	if r.runContext == nil || r.runContext.Err() != nil || r.driver == nil {
		return false
	}
	raw, _ := json.Marshal(key)
	jobID := workui.ID("model-catalog", string(raw), cursor)
	r.catalogMu.Lock()
	if r.catalogPending == nil {
		r.catalogPending = map[string]bool{}
	}
	if r.catalogPending[jobID] {
		r.catalogMu.Unlock()
		return false
	}
	r.catalogPending[jobID] = true
	r.workers.Add(1)
	r.catalogMu.Unlock()
	go func() {
		defer r.workers.Done()
		defer func() { r.catalogMu.Lock(); delete(r.catalogPending, jobID); r.catalogMu.Unlock() }()
		discoverCtx, cancel := context.WithTimeout(r.runContext, 35*time.Second)
		defer cancel()
		_, _ = r.driver.DiscoverModels(discoverCtx, key.WorkerID, agentproto.ModelListCommand{Cursor: cursor, Limit: 50})
	}()
	return true
}

// Bootstrap completion is the earliest valid model-list boundary. This never
// starts a native session/turn and never waits on the ingress response pump.
func (a *App) prefetchTeamModelCatalog(ctx context.Context, hello agentproto.Hello) {
	r := a.teamRuntime
	if r == nil || hello.CarrierIncarnation == "" {
		return
	}
	var key workstore.RuntimeModelCatalogKey
	err := r.store.Within(ctx, func(tx *workstore.Tx) error {
		carrier, err := tx.GetCarrier(hello.CarrierIncarnation)
		if err != nil {
			return err
		}
		if carrier.Backend != work.BackendCodex {
			return workstore.ErrNotFound
		}
		key, err = tx.RuntimeCatalogKey(carrier.Owner.WorkerSessionID)
		if err != nil {
			return err
		}
		if key.CarrierIncarnation != hello.CarrierIncarnation {
			return workstore.ErrConflict
		}
		if page, e := tx.GetRuntimeModelCatalogPage(key, ""); e == nil && page.CommandID != "" {
			return workstore.ErrNotFound
		}
		return nil
	})
	if err == nil {
		r.startModelCatalogDiscovery(key, "")
	}
}

// Refresh keeps the last good choices visible while requesting one new page
// from the current authenticated carrier. Discovery is coalesced by key/page.
func (a *App) refreshTeamModelCatalog(ctx context.Context, source teamHumanSource, binding work.RoleBotBinding, frozen teamSettingsState) error {
	snapshot, err := (teamBotSettingsPort{app: a}).Snapshot(ctx, binding)
	if err != nil {
		return err
	}
	if snapshot.Fingerprint != frozen.Fingerprint {
		return worksettings.ErrConflict
	}
	if snapshot.Settings.Backend != agentproto.BackendCodex || teamFixedModel(snapshot) != "" {
		return nil
	}
	connection, err := a.teamCatalogConnection(ctx, binding, teamSettingsProfile(snapshot.Settings))
	if err != nil {
		return err
	}
	var key workstore.RuntimeModelCatalogKey
	load := func() error {
		return a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
			worker, e := tx.CurrentContextWorker(frozen.ContextID)
			if e != nil {
				return e
			}
			actual, e := tx.GetRoleBotBinding(binding.RoleID)
			if e != nil {
				return e
			}
			if actual != binding || worker.RoleID != binding.RoleID {
				return worksettings.ErrConflict
			}
			key, e = tx.RuntimeCatalogKey(worker.ID)
			if e != nil {
				return e
			}
			actualConnection, e := tx.RuntimeCatalogConnection(key)
			if e != nil {
				return e
			}
			if actualConnection != connection || key.ProfileID != teamSettingsProfile(snapshot.Settings) {
				key = workstore.RuntimeModelCatalogKey{}
				return workstore.ErrNotFound
			}
			return nil
		})
	}
	err = load()
	if errors.Is(err, workstore.ErrNotFound) {
		if a.teamRuntime.host == nil {
			return fmt.Errorf("当前模型连接未运行，暂时无法刷新列表。")
		}
		if err = a.prepareTeamModelCatalog(ctx, source); err != nil {
			return err
		}
		connection, err = a.teamCatalogConnection(ctx, binding, teamSettingsProfile(snapshot.Settings))
		if err != nil {
			return err
		}
		err = load()
	}
	if err != nil {
		return err
	}
	cursor := ""
	if len(frozen.CursorTrail) > 0 {
		cursor = frozen.CursorTrail[len(frozen.CursorTrail)-1]
	}
	if key.WorkerID != "" {
		a.teamRuntime.startModelCatalogDiscovery(key, cursor)
	}
	return nil
}
