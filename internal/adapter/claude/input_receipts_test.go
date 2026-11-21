package claude

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestNativeUserUUIDLifecycleBindsQueuedSteerAfterPreviousResult(t *testing.T) {
	tr := NewTranslator("wrapper")
	if _, err := tr.ObserveServer([]byte(`{"type":"system","subtype":"init","session_id":"session","permissionMode":"default"}`)); err != nil {
		t.Fatal(err)
	}
	command := agentproto.Command{CommandID: "first", Kind: agentproto.CommandPromptSend, Target: agentproto.Target{ThreadID: "session"}, Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "start"}}}}
	if _, err := tr.TranslateCommand(command); err != nil {
		t.Fatal(err)
	}
	tr.RegisterUserCommandUUID("47a986d9-f93a-4e0d-bf3d-e70cc48f8910", command)
	result, err := tr.ObserveServer([]byte(`{"type":"command_lifecycle","command_uuid":"unregistered","state":"started","session_id":"session"}`))
	if err != nil || len(result.Events) != 0 {
		t.Fatal("unregistered UUID was attributed to newest command")
	}
	result, err = tr.ObserveServer([]byte(`{"type":"command_lifecycle","command_uuid":"47a986d9-f93a-4e0d-bf3d-e70cc48f8910","state":"started","session_id":"session"}`))
	if err != nil || len(result.Events) != 1 || result.Events[0].CommandID != "first" || result.Events[0].Kind != agentproto.EventNativeInputReceipt {
		t.Fatal("exact native lifecycle was not normalized")
	}
	started, err := tr.ObserveServer([]byte(`{"type":"assistant","session_id":"session","message":{"id":"message-1","content":[{"type":"text","text":"working"}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	var turnID string
	for _, event := range started.Events {
		if event.Kind == agentproto.EventTurnStarted {
			turnID = event.TurnID
			if event.Metadata["turnIdSource"] != "synthetic_from_observed_user_uuid" {
				t.Fatal("turn ID source was not explicit")
			}
		}
	}
	if turnID != "relay-claude-turn-47a986d9-f93a-4e0d-bf3d-e70cc48f8910" {
		t.Fatalf("unstable synthetic ID %q", turnID)
	}
	steer := command
	steer.CommandID = "supplement"
	steer.Kind = agentproto.CommandTurnSteer
	steer.Target.TurnID = turnID
	if _, err := tr.TranslateCommand(steer); err != nil {
		t.Fatal(err)
	}
	tr.RegisterUserCommandUUID("9c6d77cd-7829-4bb0-997c-a375d843027c", steer)
	if _, err := tr.ObserveServer([]byte(`{"type":"result","subtype":"success","session_id":"session","result":"first finished","is_error":false}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := tr.ObserveServer([]byte(`{"type":"command_lifecycle","command_uuid":"9c6d77cd-7829-4bb0-997c-a375d843027c","state":"started","session_id":"session"}`)); err != nil {
		t.Fatal(err)
	}
	started, err = tr.ObserveServer([]byte(`{"type":"assistant","session_id":"session","message":{"id":"message-2","content":[{"type":"text","text":"following queued supplement"}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range started.Events {
		if event.Kind == agentproto.EventTurnStarted {
			found = true
			if event.CommandID != "supplement" || event.TurnID != "relay-claude-turn-9c6d77cd-7829-4bb0-997c-a375d843027c" {
				t.Fatal("queued steer was assigned to previous/latest turn")
			}
		}
	}
	if !found {
		t.Fatal("native UUID started did not establish the next synthetic turn")
	}
}

func TestCancelledUserUUIDDoesNotRemainQueued(t *testing.T) {
	tr := NewTranslator("wrapper")
	command := agentproto.Command{CommandID: "cancelled-input", Kind: agentproto.CommandPromptSend, Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "input"}}}}
	if _, err := tr.TranslateCommand(command); err != nil {
		t.Fatal(err)
	}
	tr.RegisterUserCommandUUID("b46642c4-c0b4-43b8-9474-18fce3f24a44", command)
	result, err := tr.ObserveServer([]byte(`{"type":"command_lifecycle","command_uuid":"b46642c4-c0b4-43b8-9474-18fce3f24a44","state":"cancelled","session_id":"session"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.pendingTurns) != 0 || len(result.Events) != 2 || result.Events[1].Kind != agentproto.EventTurnCompleted || result.Events[1].CommandID != "cancelled-input" || result.Events[1].Status != "interrupted" {
		t.Fatal("native cancelled input left a phantom pending turn")
	}
}
