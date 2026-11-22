package daemon

import (
	"encoding/json"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/worksettings"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"strings"
	"testing"
)

func TestSettingsOverviewProjectsActualChoicesIntoEqualWidthEntries(t *testing.T) {
	access, plan, verbose := "confirm", "on", "chatty"
	pref := work.ContextPreferences{AccessMode: &access, PlanMode: &plan, Verbose: &verbose}
	snapshot := worksettings.Snapshot{Settings: state.BotCapabilitySettingsRecord{Backend: agentproto.BackendCodex, CodexProfileID: state.NativeCodexProfileID, PromptOverride: state.ModelConfigRecord{Model: "chosen-model", ReasoningEffort: "high"}}, Profiles: []worksettings.Profile{{ID: state.NativeCodexProfileID, Name: "实际配置", Backend: agentproto.BackendCodex}}}
	for _, fixed := range []bool{false, true} {
		if fixed {
			snapshot.Profiles[0].FixedModel = "profile-fixed"
			snapshot.Profiles[0].FixedReasoning = "low"
		}
		raw, err := teamSettingsCard("settings-view", snapshot, pref, "overview", "")
		if err != nil {
			t.Fatal(err)
		}
		var card struct {
			Header struct {
				Title struct {
					Content string `json:"content"`
				} `json:"title"`
			} `json:"header"`
			Body struct {
				Elements []map[string]any `json:"elements"`
			} `json:"body"`
		}
		if err = json.Unmarshal(raw, &card); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(card.Header.Title.Content, "/menu settings") || card.Body.Elements[1]["tag"] != "button" {
			t.Fatal("missing slash title or top back navigation")
		}
		choices := map[string]string{}
		for _, element := range card.Body.Elements {
			if element["tag"] != "button" {
				continue
			}
			if element["width"] != "fill" {
				t.Fatal("nonuniform settings button")
			}
			value := element["behaviors"].([]any)[0].(map[string]any)["value"].(map[string]any)
			if value["team_view"] != "settings-view" {
				t.Fatal("wrong callback view")
			}
			setting, _ := value["setting"].(string)
			label := element["text"].(map[string]any)["content"].(string)
			choices[setting] = label
		}
		model, effort := "chosen-model", "high"
		if fixed {
			model, effort = "profile-fixed", "low"
		}
		for page, want := range map[string]string{"backend": "codex", "codex-profile": "实际配置", "model": model, "reasoning": effort, "access": "confirm", "plan": "on", "verbose": "chatty"} {
			if !strings.Contains(choices[page], want) || !strings.Contains(choices[page], teamSettingsSlash(page)) {
				t.Fatalf("%s entry=%q want actual %q", page, choices[page], want)
			}
		}
	}
	summary := strings.Join(teamSettingsSummary(worksettings.Snapshot{}, work.ContextPreferences{}), "\n")
	if !strings.Contains(summary, "尚未观察到默认模型") || !strings.Contains(summary, "跟随后端默认") {
		t.Fatal("unobserved defaults claimed as actual choice")
	}
}

func TestSettingsDetailsKeepCurrentSelectionAndTopNavigation(t *testing.T) {
	access, plan, verbose := "full_access", "off", "verbose"
	pref := work.ContextPreferences{AccessMode: &access, PlanMode: &plan, Verbose: &verbose}
	snapshot := worksettings.Snapshot{Settings: state.BotCapabilitySettingsRecord{Backend: agentproto.BackendCodex, CodexProfileID: state.NativeCodexProfileID, PromptOverride: state.ModelConfigRecord{Model: "actual-model", ReasoningEffort: "high"}}, Profiles: []worksettings.Profile{{ID: state.NativeCodexProfileID, Name: "当前配置", Backend: agentproto.BackendCodex, Available: true}}, Catalog: &agentproto.ModelCatalogSnapshot{Entries: []agentproto.ModelCatalogEntry{{ID: "actual-model", Model: "actual-model", SupportedReasoningEfforts: []agentproto.ReasoningEffortOption{{ReasoningEffort: "high"}}}}}}
	for page, want := range map[string]string{"backend": "codex", "codex-profile": state.NativeCodexProfileID, "model": "actual-model", "reasoning": "high", "access": "full_access", "plan": "off", "verbose": "verbose"} {
		t.Run(page, func(t *testing.T) {
			raw, err := teamSettingsCard("details", snapshot, pref, page, "")
			if err != nil {
				t.Fatal(err)
			}
			var card map[string]any
			if err = json.Unmarshal(raw, &card); err != nil {
				t.Fatal(err)
			}
			title := card["header"].(map[string]any)["title"].(map[string]any)["content"].(string)
			if !strings.Contains(title, teamSettingsSlash(page)) {
				t.Fatal("wrong slash heading")
			}
			elements := card["body"].(map[string]any)["elements"].([]any)
			if elements[1].(map[string]any)["tag"] != "button" {
				t.Fatal("back navigation is not at top")
			}
			found := false
			for _, rawElement := range elements {
				element := rawElement.(map[string]any)
				if element["tag"] != "form" || element["name"] != "__settings_form" {
					continue
				}
				fields := element["elements"].([]any)
				field := fields[0].(map[string]any)
				if field["initial_option"] != want {
					t.Fatalf("current choice=%v want %s", field["initial_option"], want)
				}
				submit := fields[1].(map[string]any)
				if submit["form_action_type"] != "submit" || submit["width"] != "fill" {
					t.Fatal("save lost form submit or uniform width")
				}
				found = true
			}
			if !found {
				t.Fatal("actual choice form missing")
			}
		})
	}
}
