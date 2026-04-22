package control

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseFeishuTextActionRecognizesDebugCommand(t *testing.T) {
	action, ok := ParseFeishuTextAction("/debug")
	if !ok {
		t.Fatal("expected /debug to be parsed")
	}
	if action.Kind != ActionDebugCommand {
		t.Fatalf("action kind = %q, want %q", action.Kind, ActionDebugCommand)
	}
	if action.Text != "/debug" {
		t.Fatalf("action text = %q, want %q", action.Text, "/debug")
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
		"/upgrade track",
		"/upgrade track beta",
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

func TestParseFeishuTextActionRecognizesSteerAllCommand(t *testing.T) {
	action, ok := ParseFeishuTextAction("/steerall")
	if !ok {
		t.Fatal("expected /steerall to be parsed")
	}
	if action.Kind != ActionSteerAll {
		t.Fatalf("action kind = %q, want %q", action.Kind, ActionSteerAll)
	}
}

func TestParseFeishuMenuActionRecognizesSteerAllCommand(t *testing.T) {
	tests := []string{"steerall", "steer_all"}
	for _, key := range tests {
		action, ok := ParseFeishuMenuAction(key)
		if !ok {
			t.Fatalf("expected %q to be parsed", key)
		}
		if action.Kind != ActionSteerAll {
			t.Fatalf("event key %q => kind %q, want %q", key, action.Kind, ActionSteerAll)
		}
	}
}

func TestParseFeishuTextActionRecognizesVerboseCommand(t *testing.T) {
	tests := []string{
		"/verbose",
		"/verbose quiet",
		"/verbose normal",
		"/verbose verbose",
	}
	for _, input := range tests {
		action, ok := ParseFeishuTextAction(input)
		if !ok {
			t.Fatalf("expected %q to be parsed", input)
		}
		if action.Kind != ActionVerboseCommand {
			t.Fatalf("input %q => kind %q, want %q", input, action.Kind, ActionVerboseCommand)
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
	if action.Kind != ActionVSCodeMigrateCommand {
		t.Fatalf("action kind = %q, want %q", action.Kind, ActionVSCodeMigrateCommand)
	}
}

func TestParseFeishuTextActionRecognizesSendFileCommand(t *testing.T) {
	action, ok := ParseFeishuTextAction("/sendfile")
	if !ok {
		t.Fatal("expected /sendfile to be parsed")
	}
	if action.Kind != ActionSendFile {
		t.Fatalf("action kind = %q, want %q", action.Kind, ActionSendFile)
	}
}

func TestParseFeishuTextActionRecognizesHistoryCommand(t *testing.T) {
	action, ok := ParseFeishuTextAction("/history")
	if !ok {
		t.Fatal("expected /history to be parsed")
	}
	if action.Kind != ActionShowHistory {
		t.Fatalf("action kind = %q, want %q", action.Kind, ActionShowHistory)
	}
}

func TestFeishuCommandCatalogsHideKillInstanceFromVisibleEntries(t *testing.T) {
	cases := []struct {
		name    string
		catalog FeishuCommandPageView
	}{
		{name: "help", catalog: FeishuCommandHelpPageView()},
		{name: "menu", catalog: FeishuCommandMenuPageView()},
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

func TestParseFeishuLegacyHeadlessCompatCommandsRejected(t *testing.T) {
	for _, input := range []string{"/newinstance", "/killinstance"} {
		if action, ok := ParseFeishuTextAction(input); ok {
			t.Fatalf("expected %q to be rejected, got %#v", input, action)
		}
	}
	for _, input := range []string{"newinstance", "new_instance", "killinstance", "kill_instance"} {
		if action, ok := ParseFeishuMenuAction(input); ok {
			t.Fatalf("expected %q menu alias to be rejected, got %#v", input, action)
		}
	}
}

func TestFeishuMenuVisibleCommandsHaveCanonicalSlashAndMenuParity(t *testing.T) {
	for _, def := range FeishuCommandDefinitions() {
		if !def.ShowInMenu {
			continue
		}
		slash := strings.TrimSpace(def.CanonicalSlash)
		if slash == "" {
			t.Fatalf("menu-visible command %q missing canonical slash", def.ID)
		}
		textAction, ok := ParseFeishuTextAction(slash)
		if !ok {
			t.Fatalf("menu-visible command %q slash %q is not parseable", def.ID, slash)
		}

		menuKey := strings.TrimSpace(def.CanonicalMenuKey)
		if menuKey == "" {
			t.Fatalf("menu-visible command %q missing canonical menu key", def.ID)
		}
		menuAction, ok := ParseFeishuMenuAction(menuKey)
		if !ok {
			t.Fatalf("menu-visible command %q menu key %q is not parseable", def.ID, menuKey)
		}
		if textAction.Kind != menuAction.Kind {
			t.Fatalf("menu-visible command %q slash/menu kind mismatch: %q vs %q", def.ID, textAction.Kind, menuAction.Kind)
		}
	}
}

func TestFeishuHelpVisibleCommandsHaveCanonicalSlashParsing(t *testing.T) {
	for _, def := range FeishuCommandDefinitions() {
		if !def.ShowInHelp {
			continue
		}
		slash := strings.TrimSpace(def.CanonicalSlash)
		if slash == "" {
			t.Fatalf("help-visible command %q missing canonical slash", def.ID)
		}
		if _, ok := ParseFeishuTextAction(slash); !ok {
			t.Fatalf("help-visible command %q slash %q is not parseable", def.ID, slash)
		}
	}
}

func TestFeishuRecommendedMenusStayInSuggestedOrder(t *testing.T) {
	got := FeishuRecommendedMenus()
	want := []FeishuRecommendedMenu{
		{Key: "menu", Name: "命令菜单", Description: "打开阶段感知的命令菜单首页。"},
		{Key: "stop", Name: "停止当前执行", Description: "中断当前执行，并丢弃飞书侧尚未发送的排队输入。"},
		{Key: "steerall", Name: "Steer All", Description: "把当前队列里可并入本轮执行的输入一次性并入当前 running turn。"},
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
	for _, catalog := range []FeishuCommandPageView{FeishuCommandHelpPageView(), FeishuCommandMenuPageView()} {
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

func TestFeishuCommandCatalogsIncludeSendFile(t *testing.T) {
	for _, catalog := range []FeishuCommandPageView{FeishuCommandHelpPageView(), FeishuCommandMenuPageView()} {
		found := false
		for _, section := range catalog.Sections {
			for _, entry := range section.Entries {
				for _, command := range entry.Commands {
					if command == "/sendfile" {
						found = true
					}
				}
			}
		}
		if !found {
			t.Fatalf("catalog %#v does not include /sendfile", catalog.Title)
		}
	}
}

func TestFeishuCommandCatalogsIncludeMode(t *testing.T) {
	for _, catalog := range []FeishuCommandPageView{FeishuCommandHelpPageView(), FeishuCommandMenuPageView()} {
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

func TestFeishuCommandCatalogsIncludeCron(t *testing.T) {
	for _, catalog := range []FeishuCommandPageView{FeishuCommandHelpPageView(), FeishuCommandMenuPageView()} {
		found := false
		for _, section := range catalog.Sections {
			for _, entry := range section.Entries {
				for _, command := range entry.Commands {
					if command == "/cron" {
						found = true
					}
				}
			}
		}
		if !found {
			t.Fatalf("catalog %#v does not include /cron", catalog.Title)
		}
	}
}

func TestFeishuCommandCatalogsIncludeVerbose(t *testing.T) {
	for _, catalog := range []FeishuCommandPageView{FeishuCommandHelpPageView(), FeishuCommandMenuPageView()} {
		found := false
		for _, section := range catalog.Sections {
			for _, entry := range section.Entries {
				for _, command := range entry.Commands {
					if command == "/verbose" {
						found = true
					}
				}
			}
		}
		if !found {
			t.Fatalf("catalog %#v does not include /verbose", catalog.Title)
		}
	}
}

func TestFeishuCommandCatalogsIncludeUpgrade(t *testing.T) {
	for _, catalog := range []FeishuCommandPageView{FeishuCommandHelpPageView(), FeishuCommandMenuPageView()} {
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
	catalog := FeishuCommandHelpPageView()
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
