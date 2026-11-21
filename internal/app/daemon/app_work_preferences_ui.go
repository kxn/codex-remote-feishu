package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/worksettings"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type teamSettingsState struct {
	CursorTrail        []string
	NextCursor         string
	Kind               string
	Fingerprint        string
	ContextID          string
	ContextRevision    int64
	NavigationRevision int64
}
type teamSettingsIntent struct {
	Bot     *worksettings.UpdateRequest
	Context *worksettings.ContextUpdateRequest
}

func settingsPage(kind control.ActionKind) string {
	switch kind {
	case control.ActionModelCommand:
		return "model"
	case control.ActionReasoningCommand:
		return "reasoning"
	case control.ActionAccessCommand:
		return "access"
	case control.ActionPlanCommand:
		return "plan"
	case control.ActionVerboseCommand:
		return "verbose"
	case control.ActionCodexProfileCommand:
		return "codex-profile"
	case control.ActionClaudeProfileCommand:
		return "claude-profile"
	case control.ActionOpenCodeProfileCommand:
		return "opencode-profile"
	case control.ActionModeCommand:
		return "backend"
	default:
		return "overview"
	}
}
func settingsClear(value string) bool {
	return strings.EqualFold(value, "clear") || strings.EqualFold(value, "reset")
}
func (a *App) handleTeamSettingsCommand(ctx context.Context, source teamHumanSource, action control.Action) (string, error) {
	page := settingsPage(action.Kind)
	if page == "overview" {
		return "", fmt.Errorf("unknown settings command")
	}
	var frozen teamSettingsIntent
	found := false
	err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		body, e := tx.GetContent(source.ScopeID, source.Key+":settings-intent")
		if e == nil {
			found = true
			return json.Unmarshal(body.Data, &frozen)
		}
		if errors.Is(e, workstore.ErrNotFound) {
			return nil
		}
		return e
	})
	if err != nil {
		return "", err
	}
	if found {
		return a.applyTeamSettingsIntent(ctx, source, frozen)
	}
	parts := strings.Fields(strings.TrimSpace(action.Text))
	if len(parts) <= 1 {
		return a.queueTeamSettings(ctx, source, page, "")
	}
	service, err := a.teamPreferencesService()
	if err != nil {
		return "", err
	}
	snapshot, err := service.Discover(ctx, source.PrincipalID, source.RoleID, source.Authority)
	if err != nil {
		return "", err
	}
	var wc work.WorkContext
	var pref work.ContextPreferences
	err = a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		wc, _, e = worksettings.FrozenContext(tx, source.Authority)
		if e != nil {
			return e
		}
		pref, e = tx.GetContextPreferences(wc.ID)
		if errors.Is(e, workstore.ErrNotFound) {
			pref = work.ContextPreferences{ContextID: wc.ID, ScopeID: wc.ScopeID}
			return nil
		}
		return e
	})
	if err != nil {
		return "", err
	}
	intent, err := parseTeamSettingsIntent(source, action, parts, snapshot, wc, pref)
	if err != nil {
		return "", err
	}
	err = a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		if _, _, e := tx.ValidatePrivateAction(source.Authority); e != nil {
			return e
		}
		body, e := json.Marshal(intent)
		if e != nil {
			return e
		}
		return tx.PutContent(work.NewContent(source.Key+":settings-intent", source.ScopeID, "application/json", body, time.Now().UTC()))
	})
	if err != nil {
		return "", err
	}
	return a.applyTeamSettingsIntent(ctx, source, intent)
}
func parseTeamSettingsIntent(source teamHumanSource, action control.Action, parts []string, snapshot worksettings.Snapshot, wc work.WorkContext, pref work.ContextPreferences) (teamSettingsIntent, error) {
	var result teamSettingsIntent
	if len(parts) < 2 || len(parts) > 3 {
		return result, fmt.Errorf("invalid settings command arguments")
	}
	value := parts[1]
	bot := worksettings.UpdateRequest{Key: source.Key, RoleID: source.RoleID, ExpectedFingerprint: snapshot.Fingerprint}
	local := worksettings.ContextUpdateRequest{Key: source.Key, ContextID: wc.ID, ExpectedRevision: pref.Revision}
	switch action.Kind {
	case control.ActionModelCommand:
		if settingsClear(value) {
			if len(parts) != 2 {
				return result, fmt.Errorf("/model clear accepts no extra argument")
			}
			value = ""
		}
		bot.Model = &value
		if len(parts) == 3 {
			effort := parts[2]
			bot.Reasoning = &effort
		}
		result.Bot = &bot
	case control.ActionReasoningCommand:
		if len(parts) != 2 {
			return result, fmt.Errorf("/reasoning accepts exactly one value")
		}
		if settingsClear(value) {
			value = ""
		}
		bot.Reasoning = &value
		result.Bot = &bot
	case control.ActionCodexProfileCommand, control.ActionClaudeProfileCommand, control.ActionOpenCodeProfileCommand:
		if len(parts) != 2 {
			return result, fmt.Errorf("profile selection accepts exactly one configured ID")
		}
		backend := agentproto.BackendCodex
		if action.Kind == control.ActionClaudeProfileCommand {
			backend = agentproto.BackendClaude
		}
		if action.Kind == control.ActionOpenCodeProfileCommand {
			backend = agentproto.BackendOpenCode
		}
		if settingsClear(value) {
			switch backend {
			case agentproto.BackendClaude:
				value = state.DefaultClaudeProfileID
			case agentproto.BackendOpenCode:
				value = state.DefaultOpenCodeProfileID
			default:
				value = state.NativeCodexProfileID
			}
		}
		bot.Backend = &backend
		bot.ProfileID = &value
		result.Bot = &bot
	case control.ActionModeCommand:
		if len(parts) != 2 {
			return result, fmt.Errorf("/mode accepts exactly one backend")
		}
		if value == "normal" {
			value = "codex"
		}
		backend := agentproto.Backend(value)
		if backend != agentproto.BackendCodex && backend != agentproto.BackendClaude && backend != agentproto.BackendOpenCode {
			return result, fmt.Errorf("choose codex, claude or opencode")
		}
		bot.Backend = &backend
		result.Bot = &bot
	case control.ActionAccessCommand, control.ActionPlanCommand, control.ActionVerboseCommand:
		if len(parts) != 2 {
			return result, fmt.Errorf("context settings accept exactly one value")
		}
		field := settingsPage(action.Kind)
		if settingsClear(value) {
			local.Reset = []string{field}
		} else {
			switch field {
			case "access":
				if snapshot.Settings.Backend == agentproto.BackendOpenCode && state.NormalizeOpenCodeRuntimeAccessMode(value) == "" {
					return result, fmt.Errorf("OpenCode access supports full or confirm")
				}
				local.AccessMode = &value
			case "plan":
				local.PlanMode = &value
			case "verbose":
				local.Verbose = &value
			}
		}
		result.Context = &local
	default:
		return result, fmt.Errorf("unknown settings command")
	}
	return result, nil
}
func (a *App) applyTeamSettingsIntent(ctx context.Context, source teamHumanSource, intent teamSettingsIntent) (string, error) {
	service, err := a.teamPreferencesService()
	if err != nil {
		return "", err
	}
	note := "已保存，下次执行会采用新设置；当前运行保持原快照。"
	if intent.Bot != nil {
		result, e := service.UpdateBot(ctx, source.PrincipalID, source.Authority, *intent.Bot)
		if e != nil {
			return result.OperationID, e
		}
		if result.Status != "completed" {
			note = "设置操作尚待核查；没有重复执行或重启当前运行。"
		}
	}
	if intent.Context != nil {
		if _, err = service.UpdateContext(ctx, source.PrincipalID, source.Authority, *intent.Context); err != nil {
			return "", err
		}
		note = "已保存当前工作的设置；其他工作和机器人全局配置保持原值。"
	}
	if intent.Bot == nil && intent.Context == nil {
		return "", fmt.Errorf("settings intent has no action")
	}
	return a.queueTeamSettings(ctx, source, "overview", note)
}

// Settings callbacks read the selection and server-owned page state from the
// independently saved interaction/view. Payload cannot choose a higher actor.
func (a *App) handleTeamSettingsView(ctx context.Context, source teamHumanSource, view workstore.UIView, action string) (string, error) {
	if action == "private.settings" {
		return a.queueTeamSettings(ctx, source, "overview", "")
	}
	if view.Kind != "bot.settings" {
		return "", fmt.Errorf("this view is not a settings picker")
	}
	var interaction workstore.HumanInteraction
	var frozen teamSettingsState
	err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		interaction, e = tx.GetHumanInteraction(source.Authority.InteractionID)
		if e != nil {
			return e
		}
		if e = tx.ValidateViewInteraction(view, interaction); e != nil {
			return e
		}
		body, e := tx.GetContent(view.ScopeID, view.ID+":settings-state")
		if e != nil {
			return e
		}
		return json.Unmarshal(body.Data, &frozen)
	})
	if err != nil {
		return "", err
	}
	if action == "settings.next" || action == "settings.previous" {
		trail := append([]string(nil), frozen.CursorTrail...)
		if action == "settings.next" {
			if frozen.NextCursor == "" {
				return "", fmt.Errorf("no next model page")
			}
			trail = append(trail, frozen.NextCursor)
		} else {
			if len(trail) == 0 {
				return "", fmt.Errorf("already first model page")
			}
			trail = trail[:len(trail)-1]
		}
		return a.queueTeamSettingsCursor(ctx, source, frozen.Kind, "", trail)
	}
	if action == "settings.page" {
		page, _ := interaction.Envelope.Values["setting"].(string)
		return a.queueTeamSettings(ctx, source, page, "")
	}
	if action != "settings.apply" {
		return "", fmt.Errorf("unknown settings action")
	}
	value, ok := interaction.Envelope.Form["selection"].(string)
	if !ok || value == "" {
		return "", fmt.Errorf("choose one of the displayed settings")
	}
	actionKind := map[string]control.ActionKind{"model": control.ActionModelCommand, "reasoning": control.ActionReasoningCommand, "backend": control.ActionModeCommand, "access": control.ActionAccessCommand, "plan": control.ActionPlanCommand, "verbose": control.ActionVerboseCommand, "codex-profile": control.ActionCodexProfileCommand, "claude-profile": control.ActionClaudeProfileCommand, "opencode-profile": control.ActionOpenCodeProfileCommand}[frozen.Kind]
	if actionKind == "" {
		return "", fmt.Errorf("picker is not an editable settings page")
	}
	service, err := a.teamPreferencesService()
	if err != nil {
		return "", err
	}
	cursor := ""
	if len(frozen.CursorTrail) > 0 {
		cursor = frozen.CursorTrail[len(frozen.CursorTrail)-1]
	}
	snapshot, err := service.DiscoverPage(ctx, source.PrincipalID, source.RoleID, source.Authority, cursor)
	if err != nil {
		return "", err
	}
	snapshot.Fingerprint = frozen.Fingerprint
	wc := work.WorkContext{ID: frozen.ContextID, ScopeID: source.ScopeID}
	pref := work.ContextPreferences{Revision: frozen.ContextRevision}
	intent, err := parseTeamSettingsIntent(source, control.Action{Kind: actionKind}, []string{"settings", value}, snapshot, wc, pref)
	if err != nil {
		return "", err
	}
	if intent.Bot != nil {
		intent.Bot.CatalogCursor = cursor
	}
	return a.applyTeamSettingsIntent(ctx, source, intent)
}
