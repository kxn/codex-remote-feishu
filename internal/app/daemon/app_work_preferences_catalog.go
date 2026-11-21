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
		if key.WorkerID != owner.WorkerID || key.SessionGeneration != owner.SessionGeneration || key.SettingsRevision != owner.SettingsRevision || key.PreferencesRevision != prefRevision || key.ProfileID != owner.ProfileID || string(key.Backend) != string(owner.Backend) || key.BotGeneration != binding.IdentityGeneration {
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
	if observed.CommandID != "" {
		return worksettings.ObservedCatalog{Snapshot: &observed.Snapshot, Evidence: observed.CommandID}, nil
	}
	if !ready {
		return unavailable("当前上下文的实际模型目录尚未就绪；等待原生连接后刷新，不采用其他会话的模型列表。"), nil
	}
	if r.runContext == nil || r.runContext.Err() != nil {
		return unavailable("运行时尚未启动，尚未请求模型目录。"), nil
	}
	raw, _ := json.Marshal(key)
	jobID := workui.ID("model-catalog", string(raw), owner.Cursor)
	r.catalogMu.Lock()
	if r.catalogPending == nil {
		r.catalogPending = map[string]bool{}
	}
	if r.catalogPending[jobID] {
		r.catalogMu.Unlock()
		return unavailable("正在读取该原生会话的模型目录，请稍后刷新。"), nil
	}
	r.catalogPending[jobID] = true
	r.workers.Add(1)
	r.catalogMu.Unlock()
	go func() {
		defer r.workers.Done()
		defer func() { r.catalogMu.Lock(); delete(r.catalogPending, jobID); r.catalogMu.Unlock() }()
		discoverCtx, cancel := context.WithTimeout(r.runContext, 35*time.Second)
		defer cancel()
		// The driver freezes the actual carrier/launch identity again before issuing
		// owner-only model.list. It never sends a business prompt or invents a run.
		_, _ = r.driver.DiscoverModels(discoverCtx, owner.WorkerID, agentproto.ModelListCommand{Cursor: owner.Cursor, Limit: 50})
	}()
	return unavailable(fmt.Sprintf("正在读取当前上下文 %s 的真实模型目录，请稍后刷新。", owner.ContextID)), nil
}
