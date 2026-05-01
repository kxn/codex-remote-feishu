package wrapper

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func clearFeishuMCPBearerEnv(t *testing.T) {
	t.Helper()
	t.Setenv(feishuMCPBearerEnvName, "")
}

func TestBuildCodexChildLaunchAddsFeishuMCPForHeadless(t *testing.T) {
	clearFeishuMCPBearerEnv(t)
	statePath := writeToolServiceState(t, `{
  "url": "http://127.0.0.1:9702",
  "token": "secret-token",
  "tokenType": "bearer"
}`)
	app := New(Config{
		InstanceID:   "inst-1",
		Source:       "headless",
		RuntimePaths: relayruntime.Paths{ToolServiceFile: statePath},
	})

	args, env := app.buildCodexChildLaunch([]string{"app-server", "-c", `model="gpt-5"`})

	if len(args) != 7 {
		t.Fatalf("expected base args plus MCP overrides, got %d args: %#v", len(args), args)
	}
	if args[0] != "app-server" || args[1] != "-c" || args[2] != `model="gpt-5"` {
		t.Fatalf("expected base args to stay intact, got %#v", args[:3])
	}
	if args[3] != "-c" || args[4] != `mcp_servers.codex_remote_feishu.url="http://127.0.0.1:9702?codex_remote_instance_id=inst-1"` {
		t.Fatalf("unexpected url override args: %#v", args[3:5])
	}
	if args[5] != "-c" || args[6] != `mcp_servers.codex_remote_feishu.bearer_token_env_var="CODEX_REMOTE_FEISHU_MCP_BEARER"` {
		t.Fatalf("unexpected bearer override args: %#v", args[5:7])
	}
	if got := lookupEnv(env, feishuMCPBearerEnvName); got != "secret-token" {
		t.Fatalf("expected injected bearer env, got %q", got)
	}
}

func TestBuildCodexChildLaunchSkipsFeishuMCPForVSCodeSource(t *testing.T) {
	clearFeishuMCPBearerEnv(t)
	statePath := writeToolServiceState(t, `{
  "url": "http://127.0.0.1:9702",
  "token": "secret-token",
  "tokenType": "bearer"
}`)
	app := New(Config{
		Source:       "vscode",
		RuntimePaths: relayruntime.Paths{ToolServiceFile: statePath},
	})

	args, env := app.buildCodexChildLaunch([]string{"app-server"})

	if len(args) != 1 || args[0] != "app-server" {
		t.Fatalf("expected args to remain unchanged, got %#v", args)
	}
	if got := lookupEnv(env, feishuMCPBearerEnvName); got != "" {
		t.Fatalf("expected no injected bearer env for vscode source, got %q", got)
	}
}

func TestBuildCodexChildLaunchSkipsFeishuMCPWhenStateMissing(t *testing.T) {
	clearFeishuMCPBearerEnv(t)
	app := New(Config{
		InstanceID:   "inst-1",
		Source:       "headless",
		RuntimePaths: relayruntime.Paths{ToolServiceFile: filepath.Join(t.TempDir(), "missing.json")},
	})

	args, env := app.buildCodexChildLaunch([]string{"app-server"})

	if len(args) != 1 || args[0] != "app-server" {
		t.Fatalf("expected args to remain unchanged, got %#v", args)
	}
	if got := lookupEnv(env, feishuMCPBearerEnvName); got != "" {
		t.Fatalf("expected no injected bearer env when state is missing, got %q", got)
	}
}

func TestBuildCodexChildLaunchSkipsFeishuMCPForUnsupportedTokenType(t *testing.T) {
	clearFeishuMCPBearerEnv(t)
	statePath := writeToolServiceState(t, `{
  "url": "http://127.0.0.1:9702",
  "token": "secret-token",
  "tokenType": "basic"
}`)
	app := New(Config{
		InstanceID:   "inst-1",
		Source:       "headless",
		RuntimePaths: relayruntime.Paths{ToolServiceFile: statePath},
	})

	args, env := app.buildCodexChildLaunch([]string{"app-server"})

	if len(args) != 1 || args[0] != "app-server" {
		t.Fatalf("expected args to remain unchanged, got %#v", args)
	}
	if got := lookupEnv(env, feishuMCPBearerEnvName); got != "" {
		t.Fatalf("expected no injected bearer env for unsupported token type, got %q", got)
	}
}

func TestBuildClaudeChildLaunchAddsFeishuMCPForHeadless(t *testing.T) {
	clearFeishuMCPBearerEnv(t)
	statePath := writeToolServiceState(t, `{
  "url": "http://127.0.0.1:9702",
  "token": "secret-token",
  "tokenType": "bearer"
}`)
	configPath := filepath.Join(t.TempDir(), "claude-mcp.json")
	app := New(Config{
		InstanceID: "inst-1",
		Source:     "headless",
		RuntimePaths: relayruntime.Paths{
			ToolServiceFile:     statePath,
			ClaudeMCPConfigFile: configPath,
		},
	})

	args, env := app.buildClaudeChildLaunch(nil)

	if containsArg(args, "--strict-mcp-config") {
		t.Fatalf("did not expect --strict-mcp-config in Claude launch args: %#v", args)
	}
	index := indexOfArg(args, "--mcp-config")
	if index < 0 || index+1 >= len(args) {
		t.Fatalf("expected Claude launch args to include --mcp-config path, got %#v", args)
	}
	if args[index+1] != configPath {
		t.Fatalf("expected mcp config path %q, got %q", configPath, args[index+1])
	}
	if strings.Contains(strings.Join(args, "\x00"), "secret-token") {
		t.Fatalf("Claude launch args leaked bearer token: %#v", args)
	}
	if got := lookupEnv(env, feishuMCPBearerEnvName); got != "secret-token" {
		t.Fatalf("expected injected bearer env, got %q", got)
	}

	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read Claude MCP config: %v", err)
	}
	if strings.Contains(string(raw), "secret-token") {
		t.Fatalf("Claude MCP config leaked bearer token: %s", string(raw))
	}
	if !strings.Contains(string(raw), "Bearer ${"+feishuMCPBearerEnvName+"}") {
		t.Fatalf("expected env placeholder in Claude MCP config, got %s", string(raw))
	}

	var payload struct {
		MCPServers map[string]struct {
			Type    string            `json:"type"`
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode Claude MCP config: %v", err)
	}
	server, ok := payload.MCPServers[feishuMCPServerID]
	if !ok {
		t.Fatalf("expected %s server in Claude MCP config: %#v", feishuMCPServerID, payload)
	}
	if server.Type != "http" || server.URL != "http://127.0.0.1:9702?codex_remote_instance_id=inst-1" {
		t.Fatalf("unexpected Claude MCP server config: %#v", server)
	}
	if got := server.Headers["Authorization"]; got != "Bearer ${"+feishuMCPBearerEnvName+"}" {
		t.Fatalf("unexpected authorization header placeholder: %q", got)
	}
}

func TestBuildClaudeChildLaunchSkipsFeishuMCPForVSCodeSource(t *testing.T) {
	clearFeishuMCPBearerEnv(t)
	statePath := writeToolServiceState(t, `{
  "url": "http://127.0.0.1:9702",
  "token": "secret-token",
  "tokenType": "bearer"
}`)
	configPath := filepath.Join(t.TempDir(), "claude-mcp.json")
	app := New(Config{
		Source: "vscode",
		RuntimePaths: relayruntime.Paths{
			ToolServiceFile:     statePath,
			ClaudeMCPConfigFile: configPath,
		},
	})

	args, env := app.buildClaudeChildLaunch(nil)

	if containsArg(args, "--mcp-config") {
		t.Fatalf("expected no --mcp-config for vscode source, got %#v", args)
	}
	if got := lookupEnv(env, feishuMCPBearerEnvName); got != "" {
		t.Fatalf("expected no injected bearer env for vscode source, got %q", got)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("expected no Claude MCP config file for vscode source, stat err=%v", err)
	}
}

func TestBuildClaudeChildLaunchSkipsFeishuMCPWhenStateMissing(t *testing.T) {
	clearFeishuMCPBearerEnv(t)
	configPath := filepath.Join(t.TempDir(), "claude-mcp.json")
	app := New(Config{
		InstanceID: "inst-1",
		Source:     "headless",
		RuntimePaths: relayruntime.Paths{
			ToolServiceFile:     filepath.Join(t.TempDir(), "missing.json"),
			ClaudeMCPConfigFile: configPath,
		},
	})

	args, env := app.buildClaudeChildLaunch(nil)

	if containsArg(args, "--mcp-config") {
		t.Fatalf("expected no --mcp-config when state is missing, got %#v", args)
	}
	if got := lookupEnv(env, feishuMCPBearerEnvName); got != "" {
		t.Fatalf("expected no injected bearer env when state is missing, got %q", got)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("expected no Claude MCP config file when state is missing, stat err=%v", err)
	}
}

func TestBuildClaudeChildLaunchSkipsFeishuMCPWhenConfigPathMissing(t *testing.T) {
	clearFeishuMCPBearerEnv(t)
	statePath := writeToolServiceState(t, `{
  "url": "http://127.0.0.1:9702",
  "token": "secret-token",
  "tokenType": "bearer"
}`)
	app := New(Config{
		InstanceID:   "inst-1",
		Source:       "headless",
		RuntimePaths: relayruntime.Paths{ToolServiceFile: statePath},
	})

	args, env := app.buildClaudeChildLaunch(nil)

	if containsArg(args, "--mcp-config") {
		t.Fatalf("expected no --mcp-config without runtime config path, got %#v", args)
	}
	if got := lookupEnv(env, feishuMCPBearerEnvName); got != "" {
		t.Fatalf("expected no bearer env when Claude config path is missing, got %q", got)
	}
}

func writeToolServiceState(t *testing.T, raw string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tool-service.json")
	if err := os.WriteFile(path, []byte(strings.TrimSpace(raw)+"\n"), 0o600); err != nil {
		t.Fatalf("write tool service state: %v", err)
	}
	return path
}

func lookupEnv(env []string, key string) string {
	for _, item := range env {
		k, v, ok := strings.Cut(item, "=")
		if ok && k == key {
			return v
		}
	}
	return ""
}

func indexOfArg(args []string, target string) int {
	for index, arg := range args {
		if arg == target {
			return index
		}
	}
	return -1
}

func containsArg(args []string, target string) bool {
	return indexOfArg(args, target) >= 0
}
