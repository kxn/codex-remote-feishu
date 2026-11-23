package wrapper

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/adapter/codex"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestIndependentShellUsesActualNativeTurnAfterProtocolTurnSwitch(t *testing.T) {
	translator := codex.NewTranslator("shell-native")
	if _, err := translator.ObserveServer([]byte(`{"method":"turn/started","params":{"threadId":"session","turn":{"id":"old-model-turn"}}}`)); err != nil {
		t.Fatal(err)
	}
	runtime := newShellCommandRuntime(t.TempDir(), "independent-shell")
	owner := &agentproto.ExecutionBinding{Version: 1, ScopeID: "scope", ContextID: "context", RoleID: "role", WorkerSessionID: "worker", SessionGeneration: 1, ControllerGeneration: 1, BotGeneration: 1}
	command, cleanup, err := runtime.prepare(agentproto.Command{CommandID: "actual-shell-command", Kind: agentproto.CommandThreadShellCommand, Execution: owner, Target: agentproto.Target{ThreadID: "session", TurnID: "old-model-turn"}, ShellCommand: agentproto.ShellCommand{OperationID: "independent-shell-op", Payload: "<queued_input_bundle_v1>\n{\"schema\":\"queued_input_bundle.v1\",\"text\":\"human request\"}\n</queued_input_bundle_v1>\n"}})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup(false)
	frames, err := translator.TranslateCommand(command)
	if err != nil {
		t.Fatal(err)
	}
	var frame struct {
		ID     string         `json:"id"`
		Params map[string]any `json:"params"`
	}
	if err = json.Unmarshal(frames[0], &frame); err != nil {
		t.Fatal(err)
	}
	if frame.Params["turnId"] != nil || frame.Params["expectedTurnId"] != nil {
		t.Fatal("test did not exercise actual session-level shell RPC")
	}
	// Actual native notifications change the turn after the command was emitted,
	// before its response and item. Merely checking a translator cache cannot fix it.
	for _, raw := range []string{`{"method":"turn/completed","params":{"threadId":"session","turn":{"id":"old-model-turn","status":"completed"}}}`, `{"method":"turn/started","params":{"threadId":"session","turn":{"id":"independent-shell-turn"}}}`} {
		if _, err = translator.ObserveServer([]byte(raw)); err != nil {
			t.Fatal(err)
		}
	}
	reply, err := translator.ObserveServer([]byte(fmt.Sprintf(`{"id":%q,"result":{}}`, frame.ID)))
	if err != nil {
		t.Fatal(err)
	}
	if len(reply.Events) != 1 || reply.Events[0].Kind != agentproto.EventNativeShellRPCResult || reply.Events[0].Status != "acknowledged" || reply.Events[0].TurnID != "" {
		t.Fatalf("empty RPC invented turn proof: %+v", reply.Events)
	}
	raw, _ := json.Marshal(map[string]any{"method": "item/completed", "params": map[string]any{"threadId": "session", "turnId": "independent-shell-turn", "item": map[string]any{"id": "real-native-shell-item", "type": "commandExecution", "source": "userShell", "command": command.ShellCommand.Command, "status": "completed", "exitCode": 0, "aggregatedOutput": "actual shell output"}}})
	observed, err := translator.ObserveServer(raw)
	if err != nil {
		t.Fatal(err)
	}
	events := runtime.observeEvents(observed.Events)
	if len(events) != 1 || events[0].Kind != agentproto.EventNativeShellItemObserved || events[0].CommandID != command.CommandID || events[0].TurnID != "independent-shell-turn" || events[0].ThreadID != "session" || events[0].Action != "completed" || events[0].Execution == nil || events[0].Execution.AttemptID != "" || events[0].Execution.RunRef != "" {
		t.Fatalf("actual shell item lost independent ownership: %+v", events)
	}
	if _, exists := events[0].Metadata["userShellOutput"]; exists {
		t.Fatal("shell payload output unexpectedly retained")
	}
}
