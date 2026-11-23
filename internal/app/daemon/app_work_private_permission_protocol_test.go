package daemon

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/adapter/claude"
	"github.com/kxn/codex-remote-feishu/internal/adapter/codex"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestPrivateDefaultFullReachesCodexResumedTurnAndClaudePermissionProtocol(t *testing.T) {
	access := teamPrivateDefaultAccess(work.WorkScope{Kind: work.ScopePersonal}, work.WorkContext{Purpose: work.PurposeBusiness}, state.BotCapabilitySettingsRecord{})
	command := agentproto.Command{CommandID: "private-prompt", Kind: agentproto.CommandPromptSend, Origin: agentproto.Origin{ChatID: "private"}, Target: agentproto.Target{ThreadID: "existing", CWD: "/repo", ExecutionMode: agentproto.PromptExecutionModeResumeExisting}, Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}}}, Overrides: agentproto.PromptOverrides{AccessMode: access}, CodexResume: &agentproto.CodexResumePolicy{Mode: agentproto.CodexResumeApplyTargetProfile, ModelProviderID: "openai", DeveloperInstruction: "updated private memory"}}
	decode := func(raw []byte) map[string]any {
		t.Helper()
		var frame map[string]any
		if err := json.Unmarshal(raw, &frame); err != nil {
			t.Fatal(err)
		}
		return frame
	}
	tr := codex.NewTranslator("private-carrier")
	frames, err := tr.TranslateCommand(command)
	if err != nil {
		t.Fatal(err)
	}
	resume := decode(frames[0])
	params := resume["params"].(map[string]any)
	if resume["method"] != "thread/resume" || params["threadId"] != "existing" || params["developerInstructions"] != "updated private memory" {
		t.Fatal("resume lost original history or new memory")
	}
	result, err := tr.ObserveServer([]byte(fmt.Sprintf(`{"id":%q,"result":{"thread":{"id":"existing"}}}`, resume["id"])))
	if err != nil {
		t.Fatal(err)
	}
	turn := decode(result.OutboundToCodex[0])
	p := turn["params"].(map[string]any)
	sandbox, _ := p["sandboxPolicy"].(map[string]any)
	if turn["method"] != "turn/start" || p["approvalPolicy"] != "never" || sandbox["type"] != "dangerFullAccess" {
		t.Fatalf("full default did not reach actual native turn: approval=%v sandbox=%v", p["approvalPolicy"], sandbox)
	}
	ct := claude.NewTranslator("private-claude")
	if _, err := ct.ObserveServer([]byte(`{"type":"system","subtype":"init","session_id":"existing","permissionMode":"default"}`)); err != nil {
		t.Fatal(err)
	}
	frames, err = ct.TranslateCommand(command)
	if err != nil {
		t.Fatal(err)
	}
	frame := decode(frames[0])
	request, _ := frame["request"].(map[string]any)
	if request["subtype"] != "set_permission_mode" || request["mode"] != "bypassPermissions" {
		t.Fatal("full default did not reach actual Claude permission RPC")
	}
}
