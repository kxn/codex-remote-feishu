package control

import (
	"reflect"
	"testing"
)

func TestParseFeishuTextActionRecognizesDebugCommand(t *testing.T) {
	action, ok := ParseFeishuTextAction("/debug upgrade")
	if !ok {
		t.Fatal("expected /debug upgrade to be parsed")
	}
	if action.Kind != ActionDebugCommand {
		t.Fatalf("action kind = %q, want %q", action.Kind, ActionDebugCommand)
	}
	if action.Text != "/debug upgrade" {
		t.Fatalf("action text = %q, want %q", action.Text, "/debug upgrade")
	}
}

func TestParseFeishuTextActionRecognizesDebugAdminCommand(t *testing.T) {
	action, ok := ParseFeishuTextAction("/debug admin")
	if !ok {
		t.Fatal("expected /debug admin to be parsed")
	}
	if action.Kind != ActionDebugCommand {
		t.Fatalf("action kind = %q, want %q", action.Kind, ActionDebugCommand)
	}
	if action.Text != "/debug admin" {
		t.Fatalf("action text = %q, want %q", action.Text, "/debug admin")
	}
}

func TestParseFeishuTextActionRecognizesUpgradeCommand(t *testing.T) {
	tests := []string{
		"/upgrade",
		"/upgrade latest",
		"/upgrade local",
	}
	for _, input := range tests {
		action, ok := ParseFeishuTextAction(input)
		if !ok {
			t.Fatalf("expected %q to be parsed", input)
		}
		if action.Kind != ActionUpgradeCommand {
			t.Fatalf("input %q => kind %q, want %q", input, action.Kind, ActionUpgradeCommand)
		}
		if action.Text != input {
			t.Fatalf("input %q => text %q, want raw command", input, action.Text)
		}
	}
}

func TestParseFeishuTextActionRecognizesAutoContinueCommand(t *testing.T) {
	tests := []string{
		"/autowhip",
		"/autowhip on",
		"/autowhip off",
		"/autocontinue",
		"/autocontinue on",
		"/autocontinue off",
	}
	for _, input := range tests {
		action, ok := ParseFeishuTextAction(input)
		if !ok {
			t.Fatalf("expected %q to be parsed", input)
		}
		if action.Kind != ActionAutoContinueCommand {
			t.Fatalf("input %q => kind %q, want %q", input, action.Kind, ActionAutoContinueCommand)
		}
		if action.Text != input {
			t.Fatalf("input %q => text %q, want raw command", input, action.Text)
		}
	}
}

func TestParseFeishuTextActionRecognizesModeCommand(t *testing.T) {
	tests := []string{
		"/mode",
		"/mode normal",
		"/mode vscode",
	}
	for _, input := range tests {
		action, ok := ParseFeishuTextAction(input)
		if !ok {
			t.Fatalf("expected %q to be parsed", input)
		}
		if action.Kind != ActionModeCommand {
			t.Fatalf("input %q => kind %q, want %q", input, action.Kind, ActionModeCommand)
		}
		if action.Text != input {
			t.Fatalf("input %q => text %q, want raw command", input, action.Text)
		}
	}
}

func TestParseFeishuTextActionRecognizesVSCodeMigrateCommand(t *testing.T) {
	action, ok := ParseFeishuTextAction("/vscode-migrate")
	if !ok {
		t.Fatal("expected /vscode-migrate to be parsed")
	}
	if action.Kind != ActionVSCodeMigrate {
		t.Fatalf("action kind = %q, want %q", action.Kind, ActionVSCodeMigrate)
	}
}

func TestFeishuCommandCatalogsHideKillInstanceFromVisibleEntries(t *testing.T) {
	cases := []struct {
		name    string
		catalog CommandCatalog
	}{
		{name: "help", catalog: FeishuCommandHelpCatalog()},
		{name: "menu", catalog: FeishuCommandMenuCatalog()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, section := range tc.catalog.Sections {
				for _, entry := range section.Entries {
					for _, command := range entry.Commands {
						if command == "/killinstance" {
							t.Fatalf("catalog still exposes /killinstance in commands: %#v", entry)
						}
					}
					for _, button := range entry.Buttons {
						if button.CommandText == "/killinstance" {
							t.Fatalf("catalog still exposes /killinstance in buttons: %#v", entry)
						}
					}
				}
			}
		})
	}
}

func TestParseFeishuLegacyKillInstanceCommandsAsRemoved(t *testing.T) {
	action, ok := ParseFeishuTextAction("/killinstance")
	if !ok {
		t.Fatal("expected /killinstance to be parsed")
	}
	if action.Kind != ActionRemovedCommand || action.Text != "/killinstance" {
		t.Fatalf("unexpected text action for /killinstance: %#v", action)
	}

	menu, ok := ParseFeishuMenuAction("kill_instance")
	if !ok {
		t.Fatal("expected kill_instance menu action to be parsed")
	}
	if menu.Kind != ActionRemovedCommand || menu.Text != "kill_instance" {
		t.Fatalf("unexpected menu action for kill_instance: %#v", menu)
	}
}

func TestFeishuRecommendedMenusStayInSuggestedOrder(t *testing.T) {
	got := FeishuRecommendedMenus()
	want := []FeishuRecommendedMenu{
		{Key: "menu", Name: "命令菜单", Description: "打开阶段感知的命令菜单首页。"},
		{Key: "stop", Name: "停止当前执行", Description: "中断当前执行，并丢弃飞书侧尚未发送的排队输入。"},
		{Key: "new", Name: "新建会话", Description: "仅 normal 模式可用：准备一个新会话，下一条消息会作为首条输入。"},
		{Key: "reasoning", Name: "推理强度", Description: "打开推理强度参数卡；如果知道完整 key，也可直接使用 `reasoning_high` 这类直达入口。"},
		{Key: "model", Name: "模型", Description: "打开模型卡片；如果知道完整 key，也可直接使用 `model_gpt-5.4` 这类直达入口。"},
		{Key: "access", Name: "执行权限", Description: "打开执行权限参数卡；如果知道完整 key，也可直接使用 `access_confirm` 这类直达入口。"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("recommended menus mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestFeishuCommandCatalogsIncludeAutoContinue(t *testing.T) {
	for _, catalog := range []CommandCatalog{FeishuCommandHelpCatalog(), FeishuCommandMenuCatalog()} {
		found := false
		for _, section := range catalog.Sections {
			for _, entry := range section.Entries {
				for _, command := range entry.Commands {
					if command == "/autowhip" {
						found = true
					}
				}
			}
		}
		if !found {
			t.Fatalf("catalog %#v does not include /autowhip", catalog.Title)
		}
	}
}

func TestFeishuCommandCatalogsIncludeMode(t *testing.T) {
	for _, catalog := range []CommandCatalog{FeishuCommandHelpCatalog(), FeishuCommandMenuCatalog()} {
		found := false
		for _, section := range catalog.Sections {
			for _, entry := range section.Entries {
				for _, command := range entry.Commands {
					if command == "/mode" {
						found = true
					}
				}
			}
		}
		if !found {
			t.Fatalf("catalog %#v does not include /mode", catalog.Title)
		}
	}
}

func TestFeishuCommandCatalogsIncludeUpgrade(t *testing.T) {
	for _, catalog := range []CommandCatalog{FeishuCommandHelpCatalog(), FeishuCommandMenuCatalog()} {
		found := false
		for _, section := range catalog.Sections {
			for _, entry := range section.Entries {
				for _, command := range entry.Commands {
					if command == "/upgrade latest" || command == "/upgrade" {
						found = true
					}
				}
			}
		}
		if !found {
			t.Fatalf("catalog %#v does not include /upgrade", catalog.Title)
		}
	}
}

func TestFeishuCommandHelpCatalogUsesCanonicalCommandsOnly(t *testing.T) {
	catalog := FeishuCommandHelpCatalog()
	var commands []string
	for _, section := range catalog.Sections {
		for _, entry := range section.Entries {
			commands = append(commands, entry.Commands...)
		}
	}
	for _, legacy := range []string{"/threads", "/sessions", "/approval", "/effort"} {
		for _, command := range commands {
			if command == legacy {
				t.Fatalf("help catalog should not expose legacy alias %q: %#v", legacy, commands)
			}
		}
	}
	for _, canonical := range []string{"/use", "/access", "/reasoning", "/menu"} {
		found := false
		for _, command := range commands {
			if command == canonical {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("help catalog missing canonical command %q: %#v", canonical, commands)
		}
	}
}

func TestParseFeishuTextActionRecognizesMenuSubcommands(t *testing.T) {
	action, ok := ParseFeishuTextAction("/menu send_settings")
	if !ok {
		t.Fatal("expected /menu send_settings to be parsed")
	}
	if action.Kind != ActionShowCommandMenu {
		t.Fatalf("action kind = %q, want %q", action.Kind, ActionShowCommandMenu)
	}
	if action.Text != "/menu send_settings" {
		t.Fatalf("unexpected action text: %#v", action)
	}
}

func TestParseFeishuTextActionRejectsBareMenuAlias(t *testing.T) {
	action, ok := ParseFeishuTextAction("menu")
	if ok {
		t.Fatalf("expected bare menu text to be ignored, got %#v", action)
	}
}
