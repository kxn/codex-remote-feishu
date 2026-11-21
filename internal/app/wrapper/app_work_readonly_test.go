package wrapper

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestReviewCodexFinalNativeFramesCannotEscalateSandbox(t *testing.T) {
	app := newTestWorkApp(t, agentproto.BackendCodex)
	app.work.snapshot.ReadOnly = true
	command := testWorkCommand(app)
	command.Overrides.AccessMode = agentproto.AccessModeFullAccess
	result, err := app.runtime.TranslateCommand(command)
	if err != nil {
		t.Fatal(err)
	}
	frames := flattenRuntimeCommandPhaseFrames(result.Phases)
	if len(frames) != 1 {
		t.Fatalf("initial frames=%d", len(frames))
	}
	var start map[string]any
	if err = json.Unmarshal(frames[0], &start); err != nil {
		t.Fatal(err)
	}
	params := start["params"].(map[string]any)
	if params["sandbox"] != "read-only" || params["approvalPolicy"] != "never" {
		t.Fatalf("review thread escalated: %#v", params)
	}
	response, _ := json.Marshal(map[string]any{"id": start["id"], "result": map[string]any{"thread": map[string]any{"id": "review-session"}}})
	observed, err := app.runtime.ObserveServer(response)
	if err != nil {
		t.Fatal(err)
	}
	if len(observed.OutboundToChild) != 1 {
		t.Fatalf("turn followup=%d", len(observed.OutboundToChild))
	}
	var turn map[string]any
	if err = json.Unmarshal(observed.OutboundToChild[0], &turn); err != nil {
		t.Fatal(err)
	}
	params = turn["params"].(map[string]any)
	sandbox := params["sandboxPolicy"].(map[string]any)
	if sandbox["type"] != "readOnly" || sandbox["networkAccess"] != false || params["approvalPolicy"] != "never" {
		t.Fatalf("review turn escalated: %#v", params)
	}
	args, _ := app.buildCodexChildLaunch([]string{"app-server", "-c", "features.goals=true"})
	for _, policy := range []string{"features.goals=false", "features.hooks=false", "mcp_servers={}"} {
		if !strings.Contains(strings.Join(args, " "), policy) {
			t.Fatalf("missing review launch constraint %s", policy)
		}
	}
	command.Kind = agentproto.CommandThreadGoalSet
	if _, err = app.prepareWorkCommand(command); err == nil {
		t.Fatal("review enabled continuation")
	}
}

func TestReviewClaudeRestrictsActualLaunchAndPreservesProviderSettings(t *testing.T) {
	t.Setenv(config.ClaudeRuntimeSettingsJSONEnv, `{"env":{"ANTHROPIC_BASE_URL":"https://provider.example","ANTHROPIC_AUTH_TOKEN":"profile-auth"}}`)
	app := newTestWorkApp(t, agentproto.BackendClaude)
	app.work.snapshot.ReadOnly = true
	args, _ := app.buildClaudeChildLaunch(nil)
	assertArgPair(t, args, "--tools", "Read,Glob,Grep")
	assertArgPair(t, args, "--mcp-config", `{"mcpServers":{}}`)
	assertNotContainsArg(t, args, "--allow-dangerously-skip-permissions")
	assertNotContainsArg(t, args, "--dangerously-skip-permissions")
	var settings map[string]any
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--settings" {
			if err := json.Unmarshal([]byte(args[i+1]), &settings); err != nil {
				t.Fatal(err)
			}
		}
	}
	if settings["disableAllHooks"] != true || settings["env"].(map[string]any)["ANTHROPIC_AUTH_TOKEN"] != "profile-auth" {
		t.Fatal("review policy lost provider env or retained hooks")
	}
}

func TestReviewOpenCodeOverridesBothNativePermissionSchemas(t *testing.T) {
	t.Setenv(config.OpenCodeConfigContentEnv, `{"permission":{"*":"allow"},"agent":{"custom":{"mode":"primary","permission":{"bash":"allow"}}},"agents":{"other":{"permissions":[{"action":"*","resource":"*","effect":"allow"}]}}}`)
	app := newTestWorkApp(t, agentproto.BackendOpenCode)
	app.work.snapshot.ReadOnly = true
	_, env, err := app.buildOpenCodeChildLaunch()
	if err != nil {
		t.Fatal(err)
	}
	var overlay map[string]any
	if err = json.Unmarshal([]byte(lookupEnv(env, config.OpenCodeConfigContentEnv)), &overlay); err != nil {
		t.Fatal(err)
	}
	for _, group := range []string{"agent", "agents"} {
		for name, raw := range overlay[group].(map[string]any) {
			agent := raw.(map[string]any)
			if agent["permission"].(map[string]any)["*"] != "deny" || agent["tools"].(map[string]any)["*"] != false {
				t.Fatalf("%s.%s bypasses review", group, name)
			}
			rules := agent["permissions"].([]any)
			if rules[0].(map[string]any)["effect"] != "deny" {
				t.Fatal("V2 agent can escalate")
			}
		}
	}
}
