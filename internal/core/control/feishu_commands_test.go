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
		{Key: "list", Name: "列出实例", Description: "列出当前在线的 VS Code 实例，并提供接管入口。"},
		{Key: "status", Name: "当前状态", Description: "查看当前接管状态、输入目标和飞书侧临时覆盖。"},
		{Key: "threads", Name: "切换会话", Description: "展示最近可见会话，并切换后续输入目标。"},
		{Key: "stop", Name: "停止当前执行", Description: "中断当前执行，并丢弃飞书侧尚未发送的排队输入。"},
		{Key: "reason_low", Name: "推理 Low", Description: "只覆盖下一条消息的推理强度为 low。"},
		{Key: "reason_medium", Name: "推理 Medium", Description: "只覆盖下一条消息的推理强度为 medium。"},
		{Key: "reason_high", Name: "推理 High", Description: "只覆盖下一条消息的推理强度为 high。"},
		{Key: "reason_xhigh", Name: "推理 XHigh", Description: "只覆盖下一条消息的推理强度为 xhigh。"},
		{Key: "access_full", Name: "执行权限 Full", Description: "只覆盖下一条消息的执行权限为 full。"},
		{Key: "access_confirm", Name: "执行权限 Confirm", Description: "只覆盖下一条消息的执行权限为 confirm。"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("recommended menus mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}
