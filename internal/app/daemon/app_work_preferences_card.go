package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu/cardkit"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/worksettings"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (a *App) queueTeamSettings(ctx context.Context, source teamHumanSource, kind, note string) (string, error) {
	return a.queueTeamSettingsCursor(ctx, source, kind, note, nil)
}
func (a *App) queueTeamSettingsCursor(ctx context.Context, source teamHumanSource, kind, note string, trail []string) (string, error) {
	cursor := ""
	if len(trail) > 0 {
		cursor = trail[len(trail)-1]
	}
	allowed := map[string]bool{"overview": true, "model": true, "reasoning": true, "backend": true, "codex-profile": true, "claude-profile": true, "opencode-profile": true, "access": true, "plan": true, "verbose": true}
	if !allowed[kind] {
		return "", fmt.Errorf("unknown settings page")
	}
	id := workui.ID(source.Key, "settings", kind, cursor)
	exists := false
	if err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		_, e := tx.GetUIView(id)
		if e == nil {
			exists = true
			return nil
		}
		if errors.Is(e, workstore.ErrNotFound) {
			return nil
		}
		return e
	}); err != nil {
		return "", err
	}
	if exists {
		return id, nil
	}
	if kind == "model" || kind == "reasoning" {
		if e := a.prepareTeamModelCatalog(ctx, source); e != nil {
			note = e.Error()
		}
	}
	service, err := a.teamPreferencesService()
	if err != nil {
		return "", err
	}
	snapshot, err := service.DiscoverPage(ctx, source.PrincipalID, source.RoleID, source.Authority, cursor)
	if err != nil {
		return "", err
	}
	err = a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		wc, navRevision, e := worksettings.FrozenContext(tx, source.Authority)
		if e != nil {
			return e
		}
		if source.NavigationRevision > 0 && source.NavigationRevision != navRevision {
			return workstore.ErrConflict
		}
		pref, e := tx.GetContextPreferences(wc.ID)
		if errors.Is(e, workstore.ErrNotFound) {
			pref = work.ContextPreferences{ContextID: wc.ID, ScopeID: wc.ScopeID}
		} else if e != nil {
			return e
		}
		binding, e := tx.GetRoleBotBinding(source.RoleID)
		if e != nil {
			return e
		}
		if binding.AppID != source.Gateway.AppID || binding.IdentityGeneration != source.Gateway.BotGeneration {
			return workstore.ErrConflict
		}
		nextCursor := ""
		if snapshot.Catalog != nil {
			nextCursor = snapshot.Catalog.NextCursor
		}
		frozen := teamSettingsState{CursorTrail: trail, NextCursor: nextCursor, Kind: kind, Fingerprint: snapshot.Fingerprint, ContextID: wc.ID, ContextRevision: pref.Revision, NavigationRevision: navRevision}
		raw, e := json.Marshal(frozen)
		if e != nil {
			return e
		}
		if e = tx.PutContent(work.NewContent(id+":settings-state", source.ScopeID, "application/json", raw, time.Now().UTC())); e != nil {
			return e
		}
		card, e := teamSettingsCard(id, snapshot, pref, kind, note, trail...)
		if e != nil {
			return e
		}
		view := workstore.UIView{ID: id, ScopeID: source.ScopeID, ContextID: wc.ID, SurfaceKey: "settings:" + source.ScopeID, Kind: "bot.settings", TargetID: source.RoleID, TargetRevision: navRevision, Binding: binding, Tenant: source.Tenant, ChatID: source.ChatID, PrincipalID: source.PrincipalID, ReplyToMessageID: source.MessageID}
		if e = workui.ReplaceFromInteraction(tx, &view, source.Authority.InteractionID); e != nil {
			return e
		}
		_, e = workui.Queue(tx, view, card)
		return e
	})
	return id, err
}
func teamSettingsButton(id, label, action string, extra map[string]any) map[string]any {
	value := cardkit.CloneMap(extra)
	if value == nil {
		value = map[string]any{}
	}
	value["team_view"] = id
	value["action"] = action
	return map[string]any{"tag": "button", "text": cardkit.PlainText(label), "behaviors": []map[string]any{{"type": "callback", "value": value}}}
}
func teamSettingsCard(id string, snapshot worksettings.Snapshot, pref work.ContextPreferences, kind, note string, trail ...string) (json.RawMessage, error) {
	elements := []map[string]any{}
	text := func(value string) {
		if block := cardkit.PlainTextBlockElement(value); block != nil {
			elements = append(elements, block)
		}
	}
	text(note)
	text("机器人配置适用于这个助手。执行权限、计划模式和详细程度只影响当前工作；运行中的执行继续使用其原快照。")
	model := snapshot.Settings.PromptOverride.Model
	if model == "" {
		model = "跟随所选配置"
	}
	effort := snapshot.Settings.PromptOverride.ReasoningEffort
	if effort == "" {
		effort = "自动"
	}
	text(fmt.Sprintf("机器人：%s · %s\n模型：%s\n推理强度：%s", snapshot.Settings.Backend, teamSettingsProfile(snapshot.Settings), model, effort))
	if pref.AccessMode != nil {
		text("当前工作权限：" + *pref.AccessMode)
	}
	if pref.PlanMode != nil {
		text("当前工作计划模式：" + *pref.PlanMode)
	}
	if pref.Verbose != nil {
		text("当前工作详细程度：" + *pref.Verbose)
	}
	options := []map[string]any{}
	add := func(label, value string) {
		options = append(options, map[string]any{"text": cardkit.PlainText(label), "value": value})
	}
	if kind == "overview" {
		pages := []struct{ key, label string }{{"backend", "执行后端"}, {"codex-profile", "Codex 配置"}, {"claude-profile", "Claude 配置"}, {"opencode-profile", "OpenCode 配置"}, {"model", "模型"}, {"reasoning", "推理强度"}, {"access", "工作权限"}, {"plan", "计划模式"}, {"verbose", "详细程度"}}
		for _, page := range pages {
			elements = append(elements, teamSettingsButton(id, page.label, "settings.page", map[string]any{"setting": page.key}))
		}
	} else {
		switch kind {
		case "backend":
			for _, backend := range []string{"codex", "claude", "opencode"} {
				add(backend, backend)
			}
		case "codex-profile", "claude-profile", "opencode-profile":
			backend := agentproto.Backend(strings.TrimSuffix(kind, "-profile"))
			for _, profile := range snapshot.Profiles {
				if profile.Backend == backend && profile.Available {
					label := profile.Name
					if label == "" {
						label = profile.ID
					}
					if profile.FixedModel != "" {
						label += " · " + profile.FixedModel
					}
					add(label, profile.ID)
				}
			}
		case "model":
			add("跟随配置默认模型（清除覆盖）", "clear")
			fixed := false
			for _, profile := range snapshot.Profiles {
				if profile.Backend == snapshot.Settings.Backend && profile.ID == teamSettingsProfile(snapshot.Settings) && profile.FixedModel != "" {
					text("当前 Profile 固定模型：" + profile.FixedModel)
					add(profile.FixedModel, profile.FixedModel)
					fixed = true
				}
			}
			if !fixed {
				if snapshot.Catalog == nil {
					text(snapshot.CatalogUnavailable)
				} else {
					for _, entry := range snapshot.Catalog.Entries {
						if entry.Hidden {
							continue
						}
						value := entry.Model
						if value == "" {
							value = entry.ID
						}
						if value == "" {
							continue
						}
						label := entry.DisplayName
						if label == "" {
							label = value
						}
						if entry.Description != "" {
							text(value + "：" + entry.Description)
						}
						add(label, value)
					}
					if snapshot.Catalog.NextCursor != "" {
						text("模型列表尚有未加载部分；当前只显示已观察到的选项。")
					}
				}
			}
		case "reasoning":
			add("自动（清除覆盖）", "clear")
			fixed := false
			for _, profile := range snapshot.Profiles {
				if profile.Backend == snapshot.Settings.Backend && profile.ID == teamSettingsProfile(snapshot.Settings) && profile.FixedModel != "" {
					fixed = true
					if profile.FixedReasoning != "" {
						add(profile.FixedReasoning, profile.FixedReasoning)
					}
				}
			}
			if !fixed {
				model := snapshot.Settings.PromptOverride.Model
				if snapshot.Catalog != nil {
					for _, entry := range snapshot.Catalog.Entries {
						if entry.Hidden || (model != "" && entry.Model != model && entry.ID != model) || (model == "" && !entry.IsDefault) {
							continue
						}
						for _, effort := range entry.SupportedReasoningEfforts {
							add(effort.ReasoningEffort, effort.ReasoningEffort)
						}
					}
				}
				if len(options) == 1 {
					text("当前模型尚未声明可选推理强度，先保持自动并刷新真实模型列表。")
				}
			}
		case "access":
			add("跟随当前工作的默认权限", "clear")
			add("需要确认", "confirm")
			add("完全访问", "full_access")
			if snapshot.Settings.Backend != agentproto.BackendOpenCode {
				add("允许文件编辑", "accept_edits")
			}
		case "plan":
			add("跟随当前工作的默认计划模式", "clear")
			add("开启计划模式", "on")
			add("关闭计划模式", "off")
		case "verbose":
			add("跟随默认详细程度", "clear")
			for _, value := range []string{"quiet", "normal", "verbose", "chatty"} {
				add(value, value)
			}
		}
		if len(options) > 100 {
			return nil, fmt.Errorf("settings picker has more than 100 choices; narrow or paginate the actual catalog")
		}
		if kind == "model" && snapshot.Catalog != nil && snapshot.Catalog.NextCursor != "" {
			elements = append(elements, teamSettingsButton(id, "下一页模型", "settings.next", nil))
		}
		if kind == "model" && len(trail) > 0 {
			elements = append(elements, teamSettingsButton(id, "上一页模型", "settings.previous", nil))
		}
		if len(options) > 0 {
			selectField := map[string]any{"tag": "select_static", "name": "selection", "placeholder": cardkit.PlainText("请选择"), "options": options}
			submit := teamSettingsButton(id, "保存选择", "settings.apply", nil)
			submit["name"] = "__settings_submit"
			submit["form_action_type"] = "submit"
			elements = append(elements, map[string]any{"tag": "form", "name": "__settings_form", "elements": []map[string]any{selectField, submit}})
		} else {
			text("当前没有可选配置，请先在管理页面维护已有 Profile。")
		}
		elements = append(elements, teamSettingsButton(id, "刷新列表", "settings.page", map[string]any{"setting": kind}), teamSettingsButton(id, "返回设置", "settings.page", map[string]any{"setting": "overview"}))
	}
	raw, err := json.Marshal(map[string]any{"schema": "2.0", "config": map[string]any{"update_multi": true}, "header": map[string]any{"title": cardkit.PlainText("模型和当前工作设置")}, "body": map[string]any{"elements": elements}})
	if err != nil {
		return nil, err
	}
	if len(raw) > 28*1024 {
		return nil, fmt.Errorf("settings card exceeds the message budget; paginate the actual catalog instead of truncating it")
	}
	return raw, nil
}
