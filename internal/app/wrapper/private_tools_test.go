package wrapper

import (
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/toolservicecontract"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func TestPrivateToolPublicationUsesSeparateBearerAndNeverTeamContext(t *testing.T) {
	path := writeToolServiceState(t, `{"url":"http://127.0.0.1:9702","token":"legacy-global","tokenType":"bearer"}`)
	t.Setenv(agentproto.PrivateNativeCredentialEnv, "private-carrier-secret")
	a := &App{config: Config{InstanceID: "instance", Source: "headless", RuntimePaths: relayruntime.Paths{ToolServiceFile: path, StateDir: t.TempDir()}}, privateNative: &privateNativeState{}}
	info, ok := a.readFeishuMCPPublicationInfo()
	if !ok || info.Token != toolservicecontract.PrivateBearer("private-carrier-secret") || info.Token == "legacy-global" || info.Token == "private-carrier-secret" {
		t.Fatal("private tool did not get its domain-separated capability")
	}
	for _, launch := range []func([]string, []string) ([]string, []string){a.applyCodexFeishuMCPPublication, a.applyClaudeFeishuMCPPublication} {
		_, env := launch(nil, nil)
		if lookupEnv(env, feishuMCPBearerEnvName) != info.Token {
			t.Fatal("backend did not receive private bearer")
		}
	}
	t.Setenv(agentproto.PrivateNativeCredentialEnv, "")
	if _, ok = a.readFeishuMCPPublicationInfo(); ok {
		t.Fatal("missing private credential fell back to global")
	}
	a.work = &workExecutionState{}
	if _, ok = a.readFeishuMCPPublicationInfo(); ok {
		t.Fatal("team received private publication tools")
	}
}

func TestPrivateManagedMemoryKeepsOriginalHomeAndNativeInstructions(t *testing.T) {
	t.Setenv("HOME", "/existing/home")
	t.Setenv("CODEX_HOME", "/existing/codex")
	t.Setenv("CLAUDE_CODE_DISABLE_AUTO_MEMORY", "0")
	for _, backend := range []agentproto.Backend{agentproto.BackendClaude, agentproto.BackendCodex} {
		a := &App{config: Config{Backend: backend}, privateNative: &privateNativeState{}}
		if backend == agentproto.BackendCodex {
			args, env := a.buildCodexChildLaunch([]string{"app-server", "-c", `developer_instructions="existing user instructions"`})
			if !strings.Contains(strings.Join(args, " "), `developer_instructions="existing user instructions"`) || !strings.Contains(strings.Join(args, " "), "memories.generate_memories=false") {
				t.Fatal("private memory replaced instructions or left native memory enabled")
			}
			if lookupEnv(env, "HOME") != "/existing/home" || lookupEnv(env, "CODEX_HOME") != "/existing/codex" {
				t.Fatal("memory changed profile homes")
			}
		} else {
			_, env := a.buildClaudeChildLaunch(nil)
			if lookupEnv(env, "CLAUDE_CODE_DISABLE_AUTO_MEMORY") != "1" || lookupEnv(env, "HOME") != "/existing/home" {
				t.Fatal("private memory lost its native boundary")
			}
		}
	}
}
