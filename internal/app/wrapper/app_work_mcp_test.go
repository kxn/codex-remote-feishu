package wrapper

import (
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestManagedLaunchDoesNotPublishLegacyFeishuMCPOrBearer(t *testing.T) {
	t.Setenv(feishuMCPBearerEnvName, "inherited-old-bearer")
	t.Setenv(config.WorkSessionTokenEnv, "new-scoped-cli-token")
	t.Setenv(config.OpenCodeConfigContentEnv, `{"mcp":{"user-server":{"type":"remote","url":"https://user.example/mcp"}}}`)
	state := writeToolServiceState(t, `{"url":"http://127.0.0.1:9702/legacy-tools","token":"legacy-file-secret","tokenType":"bearer"}`)
	for _, backend := range []agentproto.Backend{agentproto.BackendCodex, agentproto.BackendClaude, agentproto.BackendOpenCode} {
		t.Run(string(backend), func(t *testing.T) {
			app := newTestWorkApp(t, backend)
			app.config.RuntimePaths.ToolServiceFile = state
			var args, env []string
			switch backend {
			case agentproto.BackendCodex:
				args, env = app.buildCodexChildLaunch([]string{"app-server", "-c", `mcp_servers.user_server.url="https://user.example/mcp"`})
			case agentproto.BackendClaude:
				args, env = app.buildClaudeChildLaunch(nil)
			case agentproto.BackendOpenCode:
				var err error
				args, env, err = app.buildOpenCodeChildLaunch()
				if err != nil {
					t.Fatal(err)
				}
			}
			all := strings.Join(append(args, env...), "\n")
			for _, forbidden := range []string{"legacy-tools", "legacy-file-secret", "inherited-old-bearer", feishuMCPBearerEnvName} {
				if strings.Contains(all, forbidden) {
					t.Fatalf("legacy MCP leaked %s", forbidden)
				}
			}
			if lookupEnv(env, config.WorkSessionTokenEnv) != "new-scoped-cli-token" {
				t.Fatal("new CLI credential was stripped")
			}
			if len(app.openCodeFeishuMCPServers()) != 0 {
				t.Fatal("legacy MCP entered ACP initialize")
			}
			if backend == agentproto.BackendCodex && !strings.Contains(strings.Join(args, " "), "user.example/mcp") {
				t.Fatal("user MCP configuration changed")
			}
			if backend == agentproto.BackendOpenCode && !strings.Contains(lookupEnv(env, config.OpenCodeConfigContentEnv), "user.example/mcp") {
				t.Fatal("user MCP configuration changed")
			}
		})
	}
}
