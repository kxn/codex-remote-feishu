package wrapper

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestManagedMemoryPolicyPreservesHomeAndCredentials(t *testing.T) {
	t.Setenv("HOME", "/existing/provider-home")
	t.Setenv("CODEX_HOME", "/existing/codex-login")
	t.Setenv("CLAUDE_CODE_DISABLE_AUTO_MEMORY", "0")
	claude := newTestWorkApp(t, agentproto.BackendClaude)
	_, env := claude.buildClaudeChildLaunch(nil)
	if lookupEnv(env, "CLAUDE_CODE_DISABLE_AUTO_MEMORY") != "1" || lookupEnv(env, "HOME") != "/existing/provider-home" || lookupEnv(env, "CODEX_HOME") != "/existing/codex-login" {
		t.Fatal("memory shutdown modified provider identity or was overridden")
	}
	codex := newTestWorkApp(t, agentproto.BackendCodex)
	args, env := codex.buildCodexChildLaunch([]string{"app-server", "-c", "features.memories=true"})
	joined := strings.Join(args, " ")
	if !strings.HasSuffix(joined, "-c features.memories=false -c memories.use_memories=false -c memories.generate_memories=false") || lookupEnv(env, "CODEX_HOME") != "/existing/codex-login" {
		t.Fatal("managed memory policy was not the final explicit native override")
	}
	_, legacy := (&App{}).buildClaudeChildLaunch(nil)
	if lookupEnv(legacy, "CLAUDE_CODE_DISABLE_AUTO_MEMORY") != "0" {
		t.Fatal("legacy auto memory policy changed")
	}
}

func TestManagedOpenCodeInstructionsPreserveNativeProfileAndSecretEnv(t *testing.T) {
	t.Setenv(config.OpenCodeConfigContentEnv, `{"model":"provider/model","provider":{"provider":{"options":{"baseURL":"http://local"}}},"agent":{"review":{"prompt":"read only","tools":{"bash":false}}}}`)
	t.Setenv(config.OpenCodeAuthContentEnv, `{"provider":{"key":"separate-secret"}}`)
	app := newTestWorkApp(t, agentproto.BackendOpenCode)
	_, env, err := app.buildOpenCodeChildLaunch()
	if err != nil {
		t.Fatal(err)
	}
	var overlay map[string]any
	if err = json.Unmarshal([]byte(lookupEnv(env, config.OpenCodeConfigContentEnv)), &overlay); err != nil {
		t.Fatal(err)
	}
	if overlay["model"] != "provider/model" || overlay["provider"] == nil || lookupEnv(env, config.OpenCodeAuthContentEnv) != `{"provider":{"key":"separate-secret"}}` {
		t.Fatal("provider profile or auth was replaced")
	}
	agents := overlay["agent"].(map[string]any)
	for _, name := range []string{"build", "plan", "review"} {
		prompt := agents[name].(map[string]any)["prompt"].(string)
		if !strings.Contains(prompt, app.work.snapshot.InstructionBundle.Text) || strings.Contains(prompt, "separate-secret") {
			t.Fatal("native agent did not receive frozen nonsecret instruction bundle")
		}
	}
	if !strings.Contains(agents["review"].(map[string]any)["prompt"].(string), "read only") {
		t.Fatal("review policy disappeared")
	}
	t.Setenv(config.OpenCodeConfigContentEnv, `null`)
	if _, _, err = app.buildOpenCodeChildLaunch(); err == nil {
		t.Fatal("invalid profile bypassed required platform instructions")
	}
}

func TestNativeEscapeInventoryUsesCommandPosition(t *testing.T) {
	for _, command := range []string{"rg ssh config.go", "echo setsid", "git diff -- daemonize.go"} {
		if escapingNativeCommand.MatchString(command) {
			t.Fatalf("ordinary foreground command marked escaped: %q", command)
		}
	}
	for _, command := range []string{"setsid service &", "ssh remote-host 'start service'", "echo start; systemctl start unit", "docker run --detach server"} {
		if !escapingNativeCommand.MatchString(command) {
			t.Fatalf("external resource was not tracked: %q", command)
		}
	}
}
