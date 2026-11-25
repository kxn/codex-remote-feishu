package codex

import (
	"encoding/json"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"testing"
)

func TestProtocol171UserInputFlagsAndPermissionObject(t *testing.T) {
	tr := NewTranslator("instance")
	result, err := tr.ObserveServer([]byte(`{"id":"r1","method":"item/tool/requestUserInput","params":{"threadId":"thread","turnId":"turn","itemId":"item","isBlocking":false,"autoResolutionMs":500,"questions":[]}}`))
	if err != nil || len(result.Events) != 1 || result.Events[0].RequestPrompt == nil {
		t.Fatalf("request: %+v %v", result, err)
	}
	prompt := result.Events[0].RequestPrompt
	if prompt.IsBlocking == nil || *prompt.IsBlocking || prompt.AutoResolutionMs == nil || *prompt.AutoResolutionMs != 500 {
		t.Fatalf("missing input flags: %+v", prompt)
	}
	result, err = tr.ObserveServer([]byte(`{"id":"r2","method":"item/permissions/requestApproval","params":{"threadId":"thread","turnId":"turn","itemId":"item","cwd":"/workspace","startedAtMs":1,"permissions":{"fileSystem":{"read":["/workspace"]},"network":{"enabled":true}}}}`))
	if err != nil || len(result.Events) != 1 || result.Events[0].RequestPrompt == nil {
		t.Fatalf("request: %+v %v", result, err)
	}
	prompt = result.Events[0].RequestPrompt
	if prompt.Type != agentproto.RequestTypePermissionsRequestApproval || prompt.Permissions == nil || prompt.Permissions.Profile == nil {
		t.Fatalf("profile missing: %+v", prompt)
	}
}

func TestHistoryFailureThenRetryRequiresCompleteExactThread(t *testing.T) {
	for _, result := range []string{`{"thread":{"id":"other","turns":[]}}`, `{"thread":{"id":"thread"}}`} {
		tr := NewTranslator("instance")
		cmd := agentproto.Command{Kind: agentproto.CommandThreadHistoryRead, CommandID: "history", Target: agentproto.Target{ThreadID: "thread"}}
		first, err := tr.TranslateCommand(cmd)
		if err != nil {
			t.Fatal(err)
		}
		var rpc struct {
			ID string `json:"id"`
		}
		if err = json.Unmarshal(first[0], &rpc); err != nil {
			t.Fatal(err)
		}
		got, err := tr.ObserveServer([]byte(`{"id":"` + rpc.ID + `","result":` + result + `}`))
		if err != nil || len(got.Events) != 1 || got.Events[0].Status != "failed" || got.Events[0].ThreadHistory != nil {
			t.Fatalf("invalid response accepted: %+v %v", got, err)
		}
		second, err := tr.TranslateCommand(cmd)
		if err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(second[0], &rpc); err != nil {
			t.Fatal(err)
		}
		got, err = tr.ObserveServer([]byte(`{"id":"` + rpc.ID + `","result":{"thread":{"id":"thread","turns":[]}}}`))
		if err != nil || len(got.Events) != 1 || got.Events[0].Status != "completed" || got.Events[0].ThreadHistory == nil {
			t.Fatalf("valid retry failed: %+v %v", got, err)
		}
	}
}
