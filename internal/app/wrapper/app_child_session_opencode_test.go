package wrapper

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	acpadapter "github.com/kxn/codex-remote-feishu/internal/adapter/acp"
	"github.com/kxn/codex-remote-feishu/internal/config"
)

func TestBuildOpenCodeChildLaunchStripsWrapperModeAndKeepsProfileEnv(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://system-proxy.invalid")
	t.Setenv(config.OpenCodeConfigContentEnv, `{"model":"test/test-model"}`)
	t.Setenv(config.OpenCodeAuthContentEnv, `{"test":{"type":"api","key":"secret"}}`)

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
	if got := lookupEnv(env, config.OpenCodeAuthContentEnv); got != `{"test":{"type":"api","key":"secret"}}` {
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

func TestBootstrapOpenCodeACPWaitsForInitializeAndReplaysBufferedStdout(t *testing.T) {
	app := &App{config: Config{WorkspaceRoot: "/tmp/opencode-work"}}
	translator := acpadapter.NewTranslator("inst-opencode", "/tmp/opencode-work")
	var childStdin bytes.Buffer
	bufferedLine := []byte(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_1","update":{"sessionUpdate":"available_commands_update","availableCommands":[]}}}` + "\n")
	initializeResponse := []byte(`{"jsonrpc":"2.0","id":"relay-initialize-1","result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true}}}` + "\n")

	reader, err := app.bootstrapOpenCodeACP(translator, &childStdin, bytes.NewReader(append(bufferedLine, initializeResponse...)), nil, nil)
	if err != nil {
		t.Fatalf("bootstrapOpenCodeACP: %v", err)
	}
	var written map[string]any
	if err := json.Unmarshal(childStdin.Bytes(), &written); err != nil {
		t.Fatalf("unmarshal written initialize: %v", err)
	}
	if written["method"] != "initialize" || written["id"] != "relay-initialize-1" {
		t.Fatalf("written initialize frame = %#v", written)
	}
	replayed, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read replayed stdout: %v", err)
	}
	if string(replayed) != string(bufferedLine) {
		t.Fatalf("replayed stdout = %q, want %q", string(replayed), string(bufferedLine))
	}
}
