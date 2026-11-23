package daemon

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/worksettings"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// Directory data belongs to a verified profile/account, not an execution
// session. Age schedules a refresh; it never removes the last successful page.
func (p teamBotSettingsPort) ReadCatalog(ctx context.Context, binding work.RoleBotBinding, owner worksettings.CatalogOwner) (worksettings.ObservedCatalog, error) {
	if p.app == nil || p.app.teamRuntime == nil {
		return worksettings.ObservedCatalog{Unavailable: "模型查询服务尚未启动。"}, nil
	}
	if owner.Backend != agentproto.BackendCodex {
		return worksettings.ObservedCatalog{Unavailable: "当前后端未提供动态模型目录。"}, nil
	}
	if err := p.app.validateProfileCatalogOwner(ctx, binding, owner); err != nil {
		return worksettings.ObservedCatalog{}, err
	}
	target, err := p.app.teamProfileCatalog(ctx, binding, owner.ProfileID, owner.Cursor)
	if err != nil {
		reason := profileCatalogError(err)
		return worksettings.ObservedCatalog{Unavailable: reason, Evidence: workui.ID("catalog-identity-error", binding.RoleID, owner.ProfileID, reason)}, nil
	}
	record, err := p.app.readOrRefreshProfileCatalog(ctx, target, false)
	if err != nil {
		return worksettings.ObservedCatalog{}, err
	}
	return profileCatalogObservation(record), nil
}

func (a *App) validateProfileCatalogOwner(ctx context.Context, binding work.RoleBotBinding, owner worksettings.CatalogOwner) error {
	return a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
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
		return nil
	})
}

func profileCatalogObservation(record workstore.ProfileModelCatalog) worksettings.ObservedCatalog {
	result := worksettings.ObservedCatalog{Identity: record.Key, Evidence: workui.ID("profile-catalog-state", record.Key, fmt.Sprint(record.Revision))}
	if record.EvidenceID != "" {
		result.Snapshot = agentproto.CloneModelCatalogSnapshot(&record.Snapshot)
	}
	switch {
	case record.LastError != "":
		result.Unavailable = record.LastError
		if result.Snapshot != nil {
			result.Unavailable += " 已保留上次成功读取的模型列表，可以继续选择。"
		}
	case record.Refreshing:
		if result.Snapshot == nil {
			result.Unavailable = "正在读取模型列表，完成后会自动更新此卡片。"
		} else {
			result.Unavailable = "正在后台刷新；现有模型仍可选择。"
		}
	case result.Snapshot != nil && len(result.Snapshot.Entries) == 0:
		result.Unavailable = "当前配置没有返回可选模型；可以刷新或手动填写模型名。"
	}
	return result
}

func (a *App) readOrRefreshProfileCatalog(ctx context.Context, target teamProfileCatalogTarget, force bool) (workstore.ProfileModelCatalog, error) {
	var record workstore.ProfileModelCatalog
	err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error { var e error; record, e = tx.GetProfileModelCatalog(target.Key); return e })
	if err != nil && !errors.Is(err, workstore.ErrNotFound) {
		return record, err
	}
	now := time.Now().UTC()
	due := force || record.EvidenceID == "" || now.Sub(record.RefreshedAt) >= 24*time.Hour || record.Refreshing
	if !force && record.LastError != "" && now.Sub(record.AttemptedAt) < 5*time.Minute {
		due = false
	}
	if due {
		return a.startProfileCatalogRefresh(ctx, target)
	}
	return record, nil
}

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
	cursor := ""
	if len(frozen.CursorTrail) > 0 {
		cursor = frozen.CursorTrail[len(frozen.CursorTrail)-1]
	}
	owner := worksettings.CatalogOwner{ContextID: frozen.ContextID, ProfileID: teamSettingsProfile(snapshot.Settings), Backend: snapshot.Settings.Backend}
	if err = a.validateProfileCatalogOwner(ctx, binding, owner); err != nil {
		return err
	}
	target, err := a.teamProfileCatalog(ctx, binding, owner.ProfileID, cursor)
	if err != nil {
		return fmt.Errorf("%s", profileCatalogError(err))
	}
	_, err = a.readOrRefreshProfileCatalog(ctx, target, true)
	return err
}
