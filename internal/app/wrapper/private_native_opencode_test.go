package wrapper

import (
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"io"
	"testing"
)

func TestPrivateOpenCodeStreamHasJournaledExactPromptFrame(t *testing.T) {
	a := privateTestApp(t, agentproto.BackendOpenCode)
	binding := agentproto.NativeTurnBinding{Version: 1, Owner: a.privateNative.snapshot.Owner, ExecutionID: "execution"}
	cmd := agentproto.Command{CommandID: "prompt", Kind: agentproto.CommandPromptSend, NativeBinding: &binding, Target: agentproto.Target{CWD: a.privateNative.snapshot.CWD, ExecutionMode: agentproto.PromptExecutionModeStartNew}, Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}}}}
	if _, err := a.prepareJournalCommand(cmd); err != nil {
		t.Fatal(err)
	}
	out, err := a.runtime.TranslateCommand(cmd)
	if err != nil {
		t.Fatal(err)
	}
	frames := flattenRuntimeCommandPhaseFrames(out.Phases)
	if len(frames) != 1 {
		t.Fatalf("frames %d", len(frames))
	}
	first := nativeFrameRequestID(frames[0])
	if err = writeJournaledChildFrame(io.Discard, frames[0], a.runtime); err != nil {
		t.Fatal(err)
	}
	ready, err := a.runtime.ObserveServer([]byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%q,"result":{"sessionId":"session"}}`, first)))
	if err != nil {
		t.Fatal(err)
	}
	if len(ready.OutboundToChild) != 1 {
		t.Fatalf("prompt frames %d", len(ready.OutboundToChild))
	}
	frame := ready.OutboundToChild[0]
	request := nativeFrameRequestID(frame)
	if err = writeJournaledChildFrame(io.Discard, frame, a.runtime); err != nil {
		t.Fatal(err)
	}
	out2, err := a.runtime.ObserveServer([]byte(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"session","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"hello"}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(out2.Events) == 0 || out2.Events[0].Kind != agentproto.EventNativeTurnBindingObserved || out2.Events[0].RequestID != request || out2.Events[0].NativeBinding == nil || *out2.Events[0].NativeBinding != binding {
		t.Fatalf("proof %+v", out2.Events)
	}
	receipt, err := a.privateNative.journal.CommandReceipt("prompt")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range receipt.Frames {
		if f.NativeRequestID == request && f.NativeMethod == "session/prompt" && f.Stage == "written" {
			found = true
		}
	}
	if !found {
		t.Fatalf("prompt frame absent: %+v", receipt)
	}
}
