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
		if kind == "overview" {
			card, e = teamSettingsAutomationSummary(tx, wc.ID, card)
			if e != nil {
				return e
			}
		}
		view := workstore.UIView{ID: id, ScopeID: source.ScopeID, ContextID: wc.ID, Kind: "bot.settings", TargetID: source.RoleID, TargetRevision: navRevision, Binding: binding, Tenant: source.Tenant, ChatID: source.ChatID, PrincipalID: source.PrincipalID, ReplyToMessageID: source.MessageID}
		if e = workui.PrivateNavigationFromInteraction(tx, &view, source.Authority.InteractionID); e != nil {
			return e
		}
		saved, e := workui.Queue(tx, view, card)
		if e != nil {
			return e
		}
		return a.registerTeamCatalogView(tx, saved, frozen, snapshot)
	})
	return id, err
}
func teamFixedModel(snapshot worksettings.Snapshot) string {
	for _, profile := range snapshot.Profiles {
		if profile.Backend == snapshot.Settings.Backend && profile.ID == teamSettingsProfile(snapshot.Settings) {
			return profile.FixedModel
		}
	}
	return ""
}

func teamSettingsButton(id, label, action string, extra map[string]any) map[string]any {
	value := cardkit.CloneMap(extra)
	if value == nil {
		value = map[string]any{}
	}
	value["team_view"] = id
	value["action"] = action
	return map[string]any{"tag": "button", "width": "fill", "text": cardkit.PlainText(label), "behaviors": []map[string]any{{"type": "callback", "value": value}}}
}
func teamSettingsCard(id string, snapshot worksettings.Snapshot, pref work.ContextPreferences, kind, note string, trail ...string) (json.RawMessage, error) {
	elements := []map[string]any{}
	text := func(value string) {
		if block := cardkit.PlainTextBlockElement(value); block != nil {
			elements = append(elements, block)
		}
	}
	text(note)

	currentModel := snapshot.Settings.PromptOverride.Model
	currentEffort := snapshot.Settings.PromptOverride.ReasoningEffort
	effectiveModel, effectiveEffort := teamSettingsModelSelection(snapshot)
	local := func(value *string) string {
		if value == nil {
			return "跟随后端默认"
		}
		return *value
	}
	titles := map[string]string{"overview": "参数设置", "model": "模型", "reasoning": "推理强度", "backend": "执行后端", "codex-profile": "Codex 配置", "claude-profile": "Claude 配置", "opencode-profile": "OpenCode 配置", "access": "执行权限", "plan": "计划模式", "verbose": "消息详细程度"}
	initial := ""
	switch kind {
	case "overview":
		text("选择设置项查看选项或修改当前值。权限、计划模式和详细程度只作用于当前会话。")
		if snapshot.Settings.Backend == agentproto.BackendOpenCode {
			text(teamSettingsSummary(snapshot, pref)[2])
		}
	case "backend", "codex-profile", "claude-profile", "opencode-profile":
		text(fmt.Sprintf("当前执行后端：%s\n当前配置：%s", snapshot.Settings.Backend, teamSettingsProfile(snapshot.Settings)))
		if kind == "backend" {
			initial = string(snapshot.Settings.Backend)
		} else {
			initial = teamSettingsProfile(snapshot.Settings)
		}
	case "model":
		text("当前模型：" + effectiveModel)
		initial = currentModel
	case "reasoning":
		text("当前模型：" + effectiveModel + "\n当前推理强度：" + effectiveEffort)
		initial = currentEffort
	case "access":
		text("当前执行权限：" + local(pref.AccessMode))
		if pref.AccessMode != nil {
			initial = *pref.AccessMode
		}
	case "plan":
		text("当前计划模式：" + local(pref.PlanMode))
		if pref.PlanMode != nil {
			initial = *pref.PlanMode
		}
	case "verbose":
		text("当前详细程度：" + local(pref.Verbose))
		if pref.Verbose != nil {
			initial = *pref.Verbose
		}
	}
	if initial == "" {
		initial = "clear"
	}
	options := []map[string]any{}
	add := func(label, value string) {
		options = append(options, map[string]any{"text": cardkit.PlainText(label), "value": value})
	}
	if kind == "overview" {
		pages := []string{"backend"}
		profilePage := string(snapshot.Settings.Backend) + "-profile"
		if titles[profilePage] != "" {
			pages = append(pages, profilePage)
		}
		if snapshot.Settings.Backend != agentproto.BackendOpenCode {
			pages = append(pages, "model")
		}
		pages = append(pages, "reasoning", "access", "plan", "verbose")
		for _, page := range pages {
			elements = append(elements, teamSettingsButton(id, teamSettingsPageLabel(snapshot, pref, page), "settings.page", map[string]any{"setting": page}))
		}
		elements = append(elements, teamSettingsButton(id, "断线后自动继续", "automation.autocontinue.status", nil))
		elements = append(elements, teamSettingsButton(id, "返回菜单首页", "private.menu", nil))
	} else {
		switch kind {
		case "backend":
			for _, backend := range []agentproto.Backend{agentproto.BackendCodex, agentproto.BackendClaude, agentproto.BackendOpenCode} {
				for _, profile := range snapshot.Profiles {
					if profile.Available && profile.Backend == backend {
						add(string(backend), string(backend))
						break
					}
				}
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
				text(snapshot.CatalogUnavailable)
				if snapshot.Catalog != nil {
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
				text(snapshot.CatalogUnavailable)
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
			add("简洁 · 主要显示最终回复", "quiet")
			add("普通 · 显示正文、计划和文件变化", "normal")
			add("详细 · 增加工具和差异摘要", "verbose")
			add("逐阶段 · 保留可公开的阶段和工具摘要", "chatty")
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
			for _, option := range options {
				if option["value"] == initial {
					selectField["initial_option"] = initial
					break
				}
			}
			submit := teamSettingsButton(id, "保存选择", "settings.apply", nil)
			submit["name"] = "__settings_submit"
			submit["form_action_type"] = "submit"
			elements = append(elements, map[string]any{"tag": "form", "name": "__settings_form", "elements": []map[string]any{selectField, submit}})
		} else {
			text("当前没有可选配置，请先在管理页面维护已有 Profile。")
		}
		if kind == "model" && teamFixedModel(snapshot) == "" && snapshot.Settings.Backend != agentproto.BackendOpenCode {
			elements = append(elements, teamManualModelForm(id, currentModel, currentEffort))
		}
		elements = append(elements, teamSettingsButton(id, "刷新列表", "settings.page", map[string]any{"setting": kind}), teamSettingsButton(id, "返回参数设置", "settings.page", map[string]any{"setting": "overview"}))
	}
	// Preserve the old hierarchy: breadcrumb/back first, refresh and common
	// actions last. All menu buttons fill the same available width.
	navigation := []map[string]any{}
	footer := []map[string]any{}
	content := []map[string]any{}
	for _, element := range elements {
		label := ""
		if value, ok := element["text"].(map[string]any); ok {
			label, _ = value["content"].(string)
		}
		switch {
		case strings.HasPrefix(label, "返回"):
			navigation = append(navigation, element)
		case label == "刷新列表" || strings.HasPrefix(label, "上一页") || strings.HasPrefix(label, "下一页"):
			footer = append(footer, element)
		default:
			content = append(content, element)
		}
	}
	slash := teamSettingsSlash(kind)
	breadcrumb := "菜单首页 › 参数设置"
	if kind != "overview" {
		breadcrumb += " › " + titles[kind]
	}
	elements = []map[string]any{cardkit.PlainTextBlockElement(breadcrumb)}
	if kind != "overview" {
		navigation = append(navigation, teamSettingsButton(id, "返回菜单首页", "private.menu", nil))
	}
	elements = append(elements, navigation...)
	elements = append(elements, content...)
	if len(footer) > 0 {
		elements = append(elements, map[string]any{"tag": "hr"})
		elements = append(elements, footer...)
	}
	raw, err := json.Marshal(map[string]any{"schema": "2.0", "config": map[string]any{"update_multi": true}, "header": map[string]any{"title": cardkit.PlainText(titles[kind] + " · " + slash)}, "body": map[string]any{"elements": elements}})
	if err != nil {
		return nil, err
	}
	if len(raw) > 28*1024 {
		return nil, fmt.Errorf("settings card exceeds the message budget; paginate the actual catalog instead of truncating it")
	}
	return raw, nil
}
