package control

import (
	"reflect"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
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
		"/upgrade codex",
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

func TestParseFeishuTextActionRecognizesBendToMyWillRollbackCommand(t *testing.T) {
	tests := []string{
		"/bendtomywill",
		"/bendtomywill rollback",
		"/bendtomywill rollback patch-thread-1-1",
	}
	for _, input := range tests {
		action, ok := ParseFeishuTextAction(input)
		if !ok {
			t.Fatalf("expected %q to be parsed", input)
		}
		if action.Kind != ActionTurnPatchCommand {
			t.Fatalf("input %q => kind %q, want %q", input, action.Kind, ActionTurnPatchCommand)
		}
		if action.Text != input {
			t.Fatalf("input %q => text %q, want raw command", input, action.Text)
		}
	}
}

func TestParseFeishuTextActionRecognizesAutoWhipCommand(t *testing.T) {
	tests := []string{
		"/autowhip",
		"/autowhip on",
		"/autowhip off",
	}
	for _, input := range tests {
		action, ok := ParseFeishuTextAction(input)
		if !ok {
			t.Fatalf("expected %q to be parsed", input)
		}
		if action.Kind != ActionAutoWhipCommand {
			t.Fatalf("input %q => kind %q, want %q", input, action.Kind, ActionAutoWhipCommand)
		}
		if action.Text != input {
			t.Fatalf("input %q => text %q, want raw command", input, action.Text)
		}
	}
}

func TestParseFeishuTextActionRecognizesAutoContinueCommand(t *testing.T) {
	tests := []string{
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

func TestParseFeishuTextActionRejectsLegacyRecoveryAliases(t *testing.T) {
	for _, input := range []string{
		"/recovery",
		"/recovery on",
		"/recovery off",
		"/autorecovery",
		"/autorecovery on",
		"/autorecovery off",
	} {
		if action, ok := ParseFeishuTextAction(input); ok {
			t.Fatalf("expected %q to be rejected, got %#v", input, action)
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

func TestParseFeishuTextActionRecognizesClaudeProfileCommand(t *testing.T) {
	tests := []string{
		"/claudeprofile",
		"/claudeprofile default",
		"/claudeprofile devseek",
	}
	for _, input := range tests {
		action, ok := ParseFeishuTextAction(input)
		if !ok {
			t.Fatalf("expected %q to be parsed", input)
		}
		if action.Kind != ActionClaudeProfileCommand {
			t.Fatalf("input %q => kind %q, want %q", input, action.Kind, ActionClaudeProfileCommand)
		}
		if action.Text != input {
			t.Fatalf("input %q => text %q, want raw command", input, action.Text)
		}
	}
}

func TestParseFeishuMenuActionRecognizesClaudeProfileCommand(t *testing.T) {
	action, ok := ParseFeishuMenuAction("claude_profile")
	if !ok {
		t.Fatal("expected claude_profile menu action to be parsed")
	}
	if action.Kind != ActionClaudeProfileCommand {
		t.Fatalf("action kind = %q, want %q", action.Kind, ActionClaudeProfileCommand)
	}
	if action.Text != "/claudeprofile" {
		t.Fatalf("action text = %q, want %q", action.Text, "/claudeprofile")
	}
	if action.CommandID != FeishuCommandClaudeProfile {
		t.Fatalf("command id = %q, want %q", action.CommandID, FeishuCommandClaudeProfile)
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

func TestParseFeishuMenuActionBuildsCanonicalTextFromDynamicRoutes(t *testing.T) {
	tests := []struct {
		eventKey   string
		wantKind   ActionKind
		wantText   string
		wantFamily string
	}{
		{eventKey: "reasoning_high", wantKind: ActionReasoningCommand, wantText: "/reasoning high", wantFamily: FeishuCommandReasoning},
		{eventKey: "model_gpt-5.4", wantKind: ActionModelCommand, wantText: "/model gpt-5.4", wantFamily: FeishuCommandModel},
		{eventKey: "access_confirm", wantKind: ActionAccessCommand, wantText: "/access confirm", wantFamily: FeishuCommandAccess},
		{eventKey: "plan-on", wantKind: ActionPlanCommand, wantText: "/plan on", wantFamily: FeishuCommandPlan},
		{eventKey: "upgrade_track_beta", wantKind: ActionUpgradeCommand, wantText: "/upgrade track beta", wantFamily: FeishuCommandUpgrade},
	}

	for _, tt := range tests {
		t.Run(tt.eventKey, func(t *testing.T) {
			action, ok := ParseFeishuMenuAction(tt.eventKey)
			if !ok {
				t.Fatalf("expected %q to be parsed", tt.eventKey)
			}
			if action.Kind != tt.wantKind {
				t.Fatalf("kind = %q, want %q", action.Kind, tt.wantKind)
			}
			if action.Text != tt.wantText {
				t.Fatalf("text = %q, want %q", action.Text, tt.wantText)
			}
			if action.CommandID != tt.wantFamily {
				t.Fatalf("command id = %q, want %q", action.CommandID, tt.wantFamily)
			}
		})
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

func TestParseFeishuTextActionRecognizesReviewCommand(t *testing.T) {
	tests := []string{"/review", "/review uncommitted", "/review commit", "/review commit abc1234"}
	for _, input := range tests {
		action, ok := ParseFeishuTextAction(input)
		if !ok {
			t.Fatalf("expected %q to be parsed", input)
		}
		if action.Kind != ActionReviewCommand {
			t.Fatalf("input %q => kind %q, want %q", input, action.Kind, ActionReviewCommand)
		}
		if action.Text != input {
			t.Fatalf("input %q => text %q, want %q", input, action.Text, input)
		}
		if action.CommandID != FeishuCommandReview {
			t.Fatalf("input %q => command id %q, want %q", input, action.CommandID, FeishuCommandReview)
		}
	}
}

func TestParseFeishuMenuActionRecognizesReviewCommand(t *testing.T) {
	tests := map[string]string{
		"review":             "/review",
		"reviewcommit":       "/review commit",
		"review_uncommitted": "/review uncommitted",
	}
	for key, wantText := range tests {
		action, ok := ParseFeishuMenuAction(key)
		if !ok {
			t.Fatalf("expected %q to be parsed", key)
		}
		if action.Kind != ActionReviewCommand {
			t.Fatalf("menu %q => kind %q, want %q", key, action.Kind, ActionReviewCommand)
		}
		if action.Text != wantText {
			t.Fatalf("menu %q => text %q, want %q", key, action.Text, wantText)
		}
		if action.CommandID != FeishuCommandReview {
			t.Fatalf("menu %q => command id %q, want %q", key, action.CommandID, FeishuCommandReview)
		}
	}
}

func TestFeishuCommandCatalogsHideKillInstanceFromVisibleEntries(t *testing.T) {
	cases := []struct {
		name    string
		catalog FeishuPageView
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

func TestFeishuCommandRegistryActionRoundTrip(t *testing.T) {
	tests := []struct {
		commandID string
		wantKind  ActionKind
		wantSlash string
	}{
		{commandID: FeishuCommandStop, wantKind: ActionStop, wantSlash: "/stop"},
		{commandID: FeishuCommandCompact, wantKind: ActionCompact, wantSlash: "/compact"},
		{commandID: FeishuCommandSteerAll, wantKind: ActionSteerAll, wantSlash: "/steerall"},
		{commandID: FeishuCommandNew, wantKind: ActionNewThread, wantSlash: "/new"},
		{commandID: FeishuCommandDetach, wantKind: ActionDetach, wantSlash: "/detach"},
		{commandID: FeishuCommandFollow, wantKind: ActionFollowLocal, wantSlash: "/follow"},
		{commandID: FeishuCommandPatch, wantKind: ActionTurnPatchCommand, wantSlash: "/bendtomywill"},
		{commandID: FeishuCommandWorkspaceNewWorktree, wantKind: ActionWorkspaceNewWorktree, wantSlash: "/workspace new worktree"},
	}

	for _, tt := range tests {
		t.Run(tt.commandID, func(t *testing.T) {
			kind, ok := ActionKindForFeishuCommandID(tt.commandID)
			if !ok || kind != tt.wantKind {
				t.Fatalf("ActionKindForFeishuCommandID(%q) = (%q, %v), want (%q, true)", tt.commandID, kind, ok, tt.wantKind)
			}

			commandID, ok := FeishuCommandIDForActionKind(tt.wantKind)
			if !ok || commandID != tt.commandID {
				t.Fatalf("FeishuCommandIDForActionKind(%q) = (%q, %v), want (%q, true)", tt.wantKind, commandID, ok, tt.commandID)
			}

			if got := BuildFeishuActionText(tt.wantKind, ""); got != tt.wantSlash {
				t.Fatalf("BuildFeishuActionText(%q) = %q, want %q", tt.wantKind, got, tt.wantSlash)
			}
		})
	}
}

func TestEveryFeishuCommandHasSinglePrimaryActionKind(t *testing.T) {
	for _, spec := range feishuCommandSpecs {
		kind, ok := feishuCommandPrimaryActionKind(spec)
		if !ok {
			t.Fatalf("command %q does not have a single primary action kind", spec.definition.ID)
		}
		if strings.TrimSpace(string(kind)) == "" {
			t.Fatalf("command %q has empty primary action kind", spec.definition.ID)
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
		{Key: "stop", Name: "停止推理", Description: "中断当前执行，并丢弃飞书侧尚未发送的排队输入。"},
		{Key: "steerall", Name: "全部加速", Description: "把当前队列里可并入本轮执行的输入一次性并入当前 running turn。"},
		{Key: "new", Name: "新建会话", Description: "仅 headless 模式可用：准备一个新会话，下一条消息会作为首条输入。"},
		{Key: "reasoning", Name: "推理强度", Description: "打开推理强度参数卡；如果知道完整 key，也可直接使用 `reasoning_high` 这类直达入口。"},
		{Key: "model", Name: "使用模型", Description: "打开模型卡片；如果知道完整 key，也可直接使用 `model_gpt-5.4` 这类直达入口。"},
		{Key: "access", Name: "执行权限", Description: "打开执行权限参数卡；如果知道完整 key，也可直接使用 `access_confirm` 这类直达入口。"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("recommended menus mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestFeishuCommandCatalogsIncludeAutoWhip(t *testing.T) {
	for _, catalog := range []FeishuPageView{FeishuCommandHelpPageView(), FeishuCommandMenuPageView()} {
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

func TestFeishuCommandCatalogsIncludeAutoContinue(t *testing.T) {
	for _, catalog := range []FeishuPageView{FeishuCommandHelpPageView(), FeishuCommandMenuPageView()} {
		found := false
		for _, section := range catalog.Sections {
			for _, entry := range section.Entries {
				for _, command := range entry.Commands {
					if command == "/autocontinue" {
						found = true
					}
				}
			}
		}
		if !found {
			t.Fatalf("catalog %#v does not include /autocontinue", catalog.Title)
		}
	}
}

func TestFeishuCommandCatalogsIncludeSendFile(t *testing.T) {
	for _, catalog := range []FeishuPageView{FeishuCommandHelpPageView(), FeishuCommandMenuPageView()} {
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
	for _, catalog := range []FeishuPageView{FeishuCommandHelpPageView(), FeishuCommandMenuPageView()} {
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
	for _, catalog := range []FeishuPageView{FeishuCommandHelpPageView(), FeishuCommandMenuPageView()} {
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
	for _, catalog := range []FeishuPageView{FeishuCommandHelpPageView(), FeishuCommandMenuPageView()} {
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
	for _, catalog := range []FeishuPageView{FeishuCommandHelpPageView(), FeishuCommandMenuPageView()} {
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
	for _, canonical := range []string{"/workspace list", "/access", "/reasoning", "/menu"} {
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

func TestFeishuCommandMenuGroupPageCarriesContextualVariantProvenance(t *testing.T) {
	page := BuildFeishuCommandMenuGroupPageViewForContext(FeishuCommandGroupSendSettings, CatalogContext{
		Backend:     agentproto.BackendClaude,
		ProductMode: "normal",
	})
	for _, section := range page.Sections {
		for _, entry := range section.Entries {
			if len(entry.Buttons) == 0 || len(entry.Commands) == 0 || entry.Commands[0] != "/model" {
				continue
			}
			button := entry.Buttons[0]
			if button.CommandID != FeishuCommandModel {
				t.Fatalf("command id = %q, want %q", button.CommandID, FeishuCommandModel)
			}
			if button.CatalogFamilyID != FeishuCommandModel {
				t.Fatalf("catalog family id = %q, want %q", button.CatalogFamilyID, FeishuCommandModel)
			}
			if button.CatalogVariantID != "model.claude.normal" {
				t.Fatalf("catalog variant id = %q, want %q", button.CatalogVariantID, "model.claude.normal")
			}
			if button.CatalogBackend != agentproto.BackendClaude {
				t.Fatalf("catalog backend = %q, want %q", button.CatalogBackend, agentproto.BackendClaude)
			}
			return
		}
	}
	t.Fatalf("expected contextual /model menu entry, got %#v", page.Sections)
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
