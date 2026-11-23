package wrapper

import (
	"encoding/json"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestPrivateClaudeResumeAllowsFullAndRefreshesInstruction(t *testing.T) {
	app := newTestWorkApp(t, agentproto.BackendClaude)
	app.work.snapshot.InstructionBundle.Text = "updated private memory and instructions"
	args, _ := app.buildClaudeChildLaunch(&claudeLaunchResumeTarget{ThreadID: "existing-private-session", CWD: app.config.WorkspaceRoot})
	assertArgPair(t, args, "--resume", "existing-private-session")
	assertContainsArg(t, args, "--allow-dangerously-skip-permissions")
	assertNotContainsArg(t, args, "--permission-mode")
	raw, err := app.claudeBootstrapInitializeFrame()
	if err != nil {
		t.Fatal(err)
	}
	var frame struct {
		Request struct {
			AppendSystemPrompt string `json:"appendSystemPrompt"`
		} `json:"request"`
	}
	if err = json.Unmarshal(raw, &frame); err != nil {
		t.Fatal(err)
	}
	if frame.Request.AppendSystemPrompt != app.work.snapshot.InstructionBundle.Text {
		t.Fatal("resumed Claude lost updated frozen private instructions")
	}
}
