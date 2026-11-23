package daemon

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/worksettings"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// Summaries describe persisted choices and observed defaults. A missing native
// catalog is never presented as a verified effective model selection.
func teamSettingsSummary(snapshot worksettings.Snapshot, pref work.ContextPreferences) []string {
	model, effort := teamSettingsModelSelection(snapshot)
	profileID := teamSettingsProfile(snapshot.Settings)
	profileName := profileID
	for _, profile := range snapshot.Profiles {
		if profile.Backend == snapshot.Settings.Backend && profile.ID == profileID && profile.Name != "" {
			profileName = profile.Name + "（" + profileID + "）"
		}
	}
	local := func(value *string) string {
		if value == nil {
			return "跟随后端默认"
		}
		return *value
	}
	return []string{
		fmt.Sprintf("执行后端：%s · /mode", snapshot.Settings.Backend),
		fmt.Sprintf("当前配置：%s · /%sprofile", profileName, snapshot.Settings.Backend),
		"模型：" + model + " · /model",
		"推理强度：" + effort + " · /reasoning",
		"执行权限：" + teamPrivateAccessSummary(snapshot, pref) + " · /access",
		"计划模式：" + teamPrivatePlanSummary(snapshot, pref) + " · /plan",
		"消息详细程度：" + local(pref.Verbose) + " · /verbose",
	}
}

func teamSettingsModelSelection(snapshot worksettings.Snapshot) (string, string) {
	model := snapshot.Settings.PromptOverride.Model
	effort := snapshot.Settings.PromptOverride.ReasoningEffort
	for _, profile := range snapshot.Profiles {
		if profile.Backend == snapshot.Settings.Backend && profile.ID == teamSettingsProfile(snapshot.Settings) && profile.FixedModel != "" {
			model = profile.FixedModel
			effort = profile.FixedReasoning
			break
		}
	}
	if model == "" && snapshot.Catalog != nil {
		for _, entry := range snapshot.Catalog.Entries {
			if entry.IsDefault && !entry.Hidden {
				model = entry.Model
				if model == "" {
					model = entry.ID
				}
				break
			}
		}
	}
	if model == "" {
		model = "跟随当前配置（尚未观察到默认模型）"
	}
	if effort == "" {
		effort = "自动"
	}
	return model, effort
}

func teamSettingsSlash(kind string) string {
	return map[string]string{"overview": "/menu settings", "backend": "/mode", "model": "/model", "reasoning": "/reasoning", "codex-profile": "/codexprofile", "claude-profile": "/claudeprofile", "opencode-profile": "/opencodeprofile", "access": "/access", "plan": "/plan", "verbose": "/verbose"}[kind]
}
func teamSettingsPageLabel(snapshot worksettings.Snapshot, pref work.ContextPreferences, page string) string {
	index := map[string]int{"backend": 0, "codex-profile": 1, "claude-profile": 1, "opencode-profile": 1, "model": 2, "reasoning": 3, "access": 4, "plan": 5, "verbose": 6}[page]
	return teamSettingsSummary(snapshot, pref)[index]
}

func teamSettingsAutomationSummary(tx *workstore.Tx, contextID string, raw json.RawMessage) (json.RawMessage, error) {
	policy, err := tx.GetAutomationPolicy(contextID, work.AutomationContinue)
	if err != nil && !errors.Is(err, workstore.ErrNotFound) {
		return nil, err
	}
	status := "关闭"
	if policy.Enabled {
		status = "开启"
	}
	var card map[string]any
	if err = json.Unmarshal(raw, &card); err != nil {
		return nil, err
	}
	body, ok := card["body"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("settings card body missing")
	}
	elements, ok := body["elements"].([]any)
	if !ok {
		return nil, fmt.Errorf("settings card elements missing")
	}
	for _, element := range elements {
		button, ok := element.(map[string]any)
		if !ok {
			continue
		}
		behaviors, _ := button["behaviors"].([]any)
		for _, behavior := range behaviors {
			b, _ := behavior.(map[string]any)
			v, _ := b["value"].(map[string]any)
			if v["action"] == "automation.autocontinue.status" {
				button["text"] = map[string]any{"tag": "plain_text", "content": "断线后自动继续：" + status + " · /autocontinue"}
			}
		}
	}
	return json.Marshal(card)
}

func teamPrivateAccessSummary(snapshot worksettings.Snapshot, pref work.ContextPreferences) string {
	if pref.AccessMode != nil {
		return *pref.AccessMode
	}
	return agentproto.EffectiveAccessMode(snapshot.Settings.PromptOverride.AccessMode) + "（个人默认）"
}

func teamPrivatePlanSummary(snapshot worksettings.Snapshot, pref work.ContextPreferences) string {
	if pref.PlanMode != nil {
		return *pref.PlanMode
	}
	if snapshot.Settings.PlanModeOverrideSet {
		return string(snapshot.Settings.PlanMode) + "（个人默认）"
	}
	return "跟随后端默认"
}
