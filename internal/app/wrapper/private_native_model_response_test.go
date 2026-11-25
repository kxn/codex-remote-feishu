package wrapper

import (
	"encoding/json"
	"fmt"
	"io"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestPrivatePlanResponseMissingModelIsBoundFailure(t *testing.T) {
	app := privateTestApp(t, agentproto.BackendCodex)
	binding := agentproto.NativeTurnBinding{Version: 1, Owner: app.privateNative.snapshot.Owner, ExecutionID: "turn-owner"}
	cmd := agentproto.Command{CommandID: "missing-model", Kind: agentproto.CommandPromptSend, NativeBinding: &binding, Target: agentproto.Target{CWD: app.privateNative.snapshot.CWD, ExecutionMode: agentproto.PromptExecutionModeStartNew}, Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hi"}}}, Overrides: agentproto.PromptOverrides{PlanMode: "on"}}
	if _, err := app.prepareWorkCommand(cmd); err != nil {
		t.Fatal(err)
	}
	if _, err := app.prepareJournalCommand(cmd); err != nil {
		t.Fatal(err)
	}
	translated, err := app.runtime.TranslateCommand(cmd)
	if err != nil {
		t.Fatal(err)
	}
	frames := flattenRuntimeCommandPhaseFrames(translated.Phases)
	if len(frames) != 1 {
		t.Fatalf("frames=%d", len(frames))
	}
	id := nativeFrameRequestID(frames[0])
	if err = writeJournaledChildFrame(io.Discard, frames[0], app.runtime); err != nil {
		t.Fatal(err)
	}
	response, _ := json.Marshal(map[string]any{"id": id, "result": map[string]any{"thread": map[string]any{"id": "thread"}}})
	observed, err := app.runtime.ObserveServer(response)
	if err != nil {
		t.Fatal(err)
	}
	if len(observed.OutboundToChild) != 0 {
		t.Fatal("invalid turn frame was sent")
	}
	found := false
	for _, event := range observed.Events {
		if event.Status == "accepted" {
			t.Fatal("fabricated acceptance")
		}
		if event.Kind == agentproto.EventTurnCompleted && event.Status == "failed" {
			if event.NativeBinding == nil || *event.NativeBinding != binding || event.RequestID != id || event.TurnCompletionOrigin != agentproto.TurnCompletionOriginThreadStartMissingModel || event.ErrorMessage == "" {
				t.Fatalf("unbound failure: %+v", event)
			}
			found = true
		}
	}
	if !found {
		t.Fatal(fmt.Sprintf("missing explicit protocol error: %+v", observed.Events))
	}
}
