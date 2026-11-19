package codex

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestAsyncUserInputPreservesNativeAgentMessageQuestions(t *testing.T) {
	tr := NewTranslator("inst-1")
	result, err := tr.ObserveServer([]byte(`{"method":"item/completed","params":{"threadId":"thread-1","turnId":"turn-1","item":{"id":"item-1","type":"agentMessage","text":"允许切换吗？","phase":"final_answer","delivery":"async","questions":[{"title":"允许切换吗？","options":["允许全部空闲后切换","先保留候选"]}]}}}`))
	if err != nil || len(result.Events) != 1 {
		t.Fatalf("native item/completed: result=%#v err=%v", result, err)
	}
	event := result.Events[0]
	if event.Kind != agentproto.EventItemCompleted || event.ItemKind != "agent_message" || event.RequestPrompt != nil {
		t.Fatalf("async message must remain an item, not a pending RPC: %#v", event)
	}
	if event.Metadata["delivery"] != "async" || event.Metadata["questions"] == nil {
		t.Fatalf("lost native structured questions: %#v", event.Metadata)
	}
}
