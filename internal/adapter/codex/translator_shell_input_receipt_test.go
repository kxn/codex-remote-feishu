package codex

import (
	"encoding/json"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"testing"
)

func TestManagedShellOperationRequiresActualCorrelatedNativeResponse(t *testing.T) {
	for _, tc := range []struct{ name, body, status string }{{"acknowledged", `"result":{}`, "acknowledged"}, {"rejected", `"error":{"code":-1,"message":"not supported"}`, "failed"}, {"unknown", `"something":{}`, "unknown"}, {"no-turn-proof", `"result":{"turnId":"another"}`, "acknowledged"}} {
		t.Run(tc.name, func(t *testing.T) {
			translator := NewTranslator("instance")
			translator.activeTurnByThread["thread"] = "turn"
			command := agentproto.Command{CommandID: "shell-admission", Kind: agentproto.CommandThreadShellCommand, Target: agentproto.Target{ThreadID: "thread", TurnID: "turn"}, ShellCommand: agentproto.ShellCommand{Command: "fixed-private-payload-read", OperationID: "shell-operation"}}
			frames, err := translator.TranslateCommand(command)
			if err != nil {
				t.Fatal(err)
			}
			var frame struct {
				ID string `json:"id"`
			}
			if err = json.Unmarshal(frames[0], &frame); err != nil {
				t.Fatal(err)
			}
			unrelated, err := translator.ObserveServer([]byte(`{"id":"other-rpc","result":{}}`))
			if err != nil {
				t.Fatal(err)
			}
			for _, event := range unrelated.Events {
				if event.Kind == agentproto.EventNativeTurnInputAccepted {
					t.Fatal("unrelated RPC accepted shell input")
				}
			}
			result, err := translator.ObserveServer([]byte(fmt.Sprintf(`{"id":%q,%s}`, frame.ID, tc.body)))
			if err != nil {
				t.Fatal(err)
			}
			if tc.status == "" {
				if len(result.Events) != 0 {
					t.Fatalf("RPC reply invented shell turn proof: %+v", result.Events)
				}
				return
			}
			if len(result.Events) != 1 || result.Events[0].CommandID != command.CommandID || result.Events[0].RequestID != frame.ID || result.Events[0].Status != tc.status {
				t.Fatalf("actual shell receipt %+v", result.Events)
			}
			if result.Events[0].Kind != agentproto.EventNativeShellRPCResult || result.Events[0].TurnID != "" {
				t.Fatalf("shell RPC invented model input/turn proof: %+v", result.Events)
			}

		})
	}
}
