package daemon

import (
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"strings"
	"testing"
)

// Each step is dispatched through the SDK callback and durable interaction lane;
// the caller's HTTP server accepts only PATCH of the original menu message.
func exercisePrivateMenuSDKPages(t *testing.T, home, fixedProfile string, follow func(string, string, string, string, map[string]any) string) (string, int32) {
	t.Helper()
	count := int32(0)
	step := func(card, action, argument, want string, form map[string]any) string {
		t.Helper()
		count++
		next := follow(card, fmt.Sprintf("all-pages-%d", count), action, argument, form)
		if want != "" && !strings.Contains(next, want) {
			t.Fatalf("%s page missing %q: %s", action, want, next)
		}
		return next
	}
	card := step(home, "private.basic", "", "基本命令", nil)
	card = step(card, "private.status", "", "当前工作状态", nil)
	card = step(card, "private.basic", "", "基本命令", nil)
	card = step(card, "private.menu", "", "工作菜单", nil)
	card = step(card, "private.tools", "", "常用工具", nil)
	card = step(card, "private.file-help", "", "file_path", nil)
	card = step(card, "private.tools", "", "常用工具", nil)
	card = step(card, "private.review", "", "review_commit", nil)
	card = step(card, "private.tools", "", "常用工具", nil)
	card = step(card, "private.goal-help", "", "会话目标", nil)
	card = step(card, "private.goal-new", "", "goal_request", nil)
	card = step(card, "private.goal-refresh", "", "会话目标", nil)
	card = step(card, "private.goal-edit", "", "goal_request", nil)
	card = step(card, "private.goal-refresh", "", "会话目标", nil)
	if strings.Contains(card, `"action":"private.goal-clear"`) {
		t.Fatal("no native session offered goal clear")
	}
	card = step(card, "private.tools", "", "常用工具", nil)
	card = step(card, "automation.autowhip.status", "", "当前设置：关闭", nil)
	card = step(card, "automation.autowhip.on", "", "当前设置：开启", nil)
	card = step(card, "automation.autowhip.off", "", "当前设置：关闭", nil)
	card = step(card, "private.tools", "", "常用工具", nil)
	card = step(card, "private.menu", "", "工作菜单", nil)
	card = step(card, "private.admin", "", "系统管理", nil)
	card = step(card, "private.help", "", "命令帮助", nil)
	card = step(card, "private.menu", "", "工作菜单", nil)
	card = step(card, "private.settings", "", "参数设置", nil)
	for _, page := range []string{"backend", "codex-profile", "reasoning", "access", "plan", "verbose"} {
		card = step(card, "settings.page", page, "返回参数设置", nil)
		if value := map[string]string{"reasoning": "high", "access": "confirm", "plan": "on", "verbose": "normal"}[page]; value != "" {
			card = step(card, "settings.apply", "", "已保存", map[string]any{"selection": value})
		}
		card = step(card, "settings.page", "overview", "参数设置", nil)
	}
	card = step(card, "settings.page", "codex-profile", "Codex 配置", nil)
	card = step(card, "settings.apply", "", "已保存", map[string]any{"selection": state.NativeCodexProfileID})
	card = step(card, "settings.page", "overview", "参数设置", nil)
	card = step(card, "settings.page", "model", "manual_model", nil)
	card = step(card, "settings.model-manual", "", "custom-model high", map[string]any{"manual_model": "custom-model high"})
	card = step(card, "settings.page", "overview", "参数设置", nil)
	card = step(card, "settings.page", "codex-profile", "Codex 配置", nil)
	card = step(card, "settings.apply", "", "已保存", map[string]any{"selection": fixedProfile})
	card = step(card, "settings.page", "overview", "参数设置", nil)
	card = step(card, "automation.autocontinue.status", "", "当前设置：关闭", nil)
	card = step(card, "automation.autocontinue.on", "", "当前设置：开启", nil)
	card = step(card, "private.settings", "", "断线后自动继续：开启", nil)
	card = step(card, "automation.autocontinue.status", "", "当前设置：开启", nil)
	card = step(card, "automation.autocontinue.off", "", "当前设置：关闭", nil)
	card = step(card, "private.settings", "", "断线后自动继续：关闭", nil)
	card = step(card, "private.menu", "", "工作菜单", nil)
	card = step(card, "private.workspace", "", "工作区与会话", nil)
	card = step(card, "private.projects", "", "返回工作区与会话", nil)
	card = step(card, "private.workspace", "", "工作区与会话", nil)
	card = step(card, "private.menu", "", "工作菜单", nil)
	return card, count
}

func exercisePrivateMenuSDKShortcuts(t *testing.T, event func(string, string, map[string]any) string, runMenus func() error, publish func() string) {
	t.Helper()
	for _, tt := range []struct{ key, title string }{
		{"status", "当前工作状态"}, {"workspace", "选择执行目录"}, {"list", "已有会话"}, {"project", "返回工作区与会话"},
		{"settings", "参数设置"}, {"model", "模型"}, {"reasoning", "推理强度"}, {"mode", "执行后端"},
		{"access", "执行权限"}, {"plan", "计划模式"}, {"verbose", "消息详细程度"},
		{"codexprofile", "Codex 配置"}, {"claudeprofile", "Claude 配置"}, {"opencodeprofile", "OpenCode 配置"},
		{"autowhip_status", "自动检查未完成工作"}, {"autocontinue_status", "断线后自动继续"}, {"attach", "这个菜单入口已不可用"},
	} {
		event("shortcut-"+tt.key, "application.bot.menu_v6", map[string]any{"event_key": tt.key, "operator": map[string]any{"operator_id": map[string]string{"open_id": "ou_human"}}})
		if err := runMenus(); err != nil {
			t.Fatalf("%s menu: %v", tt.key, err)
		}
		if card := publish(); !strings.Contains(card, tt.title) {
			t.Fatalf("%s shortcut missing %q: %s", tt.key, tt.title, card)
		}
	}
}
