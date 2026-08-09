package wrapper

import (
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/config"
)

func TestBuildOpenCodeChildLaunchStripsWrapperModeAndKeepsProfileEnv(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://system-proxy.invalid")
	t.Setenv(config.OpenCodeConfigContentEnv, `{"model":"test/test-model"}`)
	t.Setenv(config.OpenCodeAuthContentEnv, `{"provider":{"test":{"apiKey":"secret"}}}`)

	workspaceRoot := "/tmp/opencode-work"
	app := &App{config: Config{
		Args:          []string{"opencode-acp", "acp", "--cwd", workspaceRoot},
		WorkspaceRoot: workspaceRoot,
		ChildProxyEnv: []string{"HTTPS_PROXY=http://child-proxy.invalid"},
	}}

	args, env := app.buildOpenCodeChildLaunch()
	if strings.Join(args, "\x00") != strings.Join([]string{"acp", "--cwd", workspaceRoot}, "\x00") {
		t.Fatalf("opencode child args = %#v", args)
	}
	if got := lookupEnv(env, "HTTP_PROXY"); got != "" {
		t.Fatalf("system proxy leaked into opencode child env: %q", got)
	}
	if got := lookupEnv(env, "HTTPS_PROXY"); got != "http://child-proxy.invalid" {
		t.Fatalf("child proxy env = %q", got)
	}
	if got := lookupEnv(env, config.OpenCodeConfigContentEnv); got != `{"model":"test/test-model"}` {
		t.Fatalf("opencode config env = %q", got)
	}
	if got := lookupEnv(env, config.OpenCodeAuthContentEnv); got != `{"provider":{"test":{"apiKey":"secret"}}}` {
		t.Fatalf("opencode auth env = %q", got)
	}
}

func TestBuildOpenCodeChildLaunchDefaultsToACPWorkspaceArgs(t *testing.T) {
	workspaceRoot := "/tmp/opencode-default-work"
	app := &App{config: Config{WorkspaceRoot: workspaceRoot}}

	args, _ := app.buildOpenCodeChildLaunch()
	if strings.Join(args, "\x00") != strings.Join([]string{"acp", "--cwd", workspaceRoot}, "\x00") {
		t.Fatalf("default opencode child args = %#v", args)
	}
}
