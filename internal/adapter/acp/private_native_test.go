package acp

import (
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"testing"
)

func TestPrivatePromptEvidenceRequiresMatchingACPResponse(t *testing.T) {
	tr := NewTranslator("inst", "/tmp/work")
	cmd := agentproto.Command{CommandID: "prompt", Kind: agentproto.CommandPromptSend, NativeBinding: &agentproto.NativeTurnBinding{}, Target: agentproto.Target{CWD: "/tmp/work", ExecutionMode: agentproto.PromptExecutionModeStartNew}, Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}}}}
	start, err := tr.TranslateCommand(cmd)
	if err != nil {
		t.Fatal(err)
	}
	first := decodeFrame(t, start.OutboundToChild[0])
	ready, err := tr.ObserveServer(mustLine(t, map[string]any{"jsonrpc": "2.0", "id": first["id"], "result": map[string]any{"sessionId": "session"}}))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ready.Events {
		if e.Kind == agentproto.EventNativeTurnBindingObserved {
			t.Fatal("accepted before prompt write")
		}
	}
	prompt := decodeFrame(t, ready.OutboundToChild[0])
	if prompt["method"] != "session/prompt" {
		t.Fatal(prompt)
	}
	unrelated, err := tr.ObserveServer(mustLine(t, map[string]any{"jsonrpc": "2.0", "id": "unrelated", "result": map[string]any{}}))
	if err != nil {
		t.Fatal(err)
	}
	if len(unrelated.Events) != 0 {
		t.Fatal("unrelated response accepted")
	}
	response, err := tr.ObserveServer(mustLine(t, map[string]any{"jsonrpc": "2.0", "id": prompt["id"], "result": map[string]any{"stopReason": "end_turn"}}))
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Events) < 2 || response.Events[0].Kind != agentproto.EventNativeTurnBindingObserved || response.Events[0].RequestID != prompt["id"] || response.Events[0].ThreadID != "session" || response.Events[0].CommandID != "prompt" {
		t.Fatalf("bad proof: %#v", response.Events)
	}
}

func TestPrivatePromptStreamProofRequiresOneExactPendingSession(t *testing.T) {
	tr := NewTranslator("inst", "/tmp/work")
	cmd := agentproto.Command{CommandID: "prompt", Kind: agentproto.CommandPromptSend, NativeBinding: &agentproto.NativeTurnBinding{}, Target: agentproto.Target{CWD: "/tmp/work", ExecutionMode: agentproto.PromptExecutionModeStartNew}, Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}}}}
	start, err := tr.TranslateCommand(cmd)
	if err != nil {
		t.Fatal(err)
	}
	frame := decodeFrame(t, start.OutboundToChild[0])
	ready, err := tr.ObserveServer(mustLine(t, map[string]any{"jsonrpc": "2.0", "id": frame["id"], "result": map[string]any{"sessionId": "session"}}))
	if err != nil {
		t.Fatal(err)
	}
	prompt := decodeFrame(t, ready.OutboundToChild[0])
	update := func(session, kind string) Result {
		r, e := tr.ObserveServer(mustLine(t, map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": session, "update": map[string]any{"sessionUpdate": kind, "content": map[string]any{"type": "text", "text": "hello"}}}}))
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	for _, e := range update("other", "agent_message_chunk").Events {
		if e.Kind == agentproto.EventNativeTurnBindingObserved {
			t.Fatal("other session accepted")
		}
	}
	for _, e := range update("session", "available_commands_update").Events {
		if e.Kind == agentproto.EventNativeTurnBindingObserved {
			t.Fatal("non-turn notification accepted")
		}
	}
	events := update("session", "agent_message_chunk").Events
	if len(events) == 0 || events[0].Kind != agentproto.EventNativeTurnBindingObserved || events[0].RequestID != prompt["id"] || events[0].CommandID != "prompt" {
		t.Fatalf("bad stream proof: %#v", events)
	}
	for _, e := range update("session", "agent_message_chunk").Events {
		if e.Kind == agentproto.EventNativeTurnBindingObserved {
			t.Fatal("duplicate acceptance")
		}
	}
}

func TestPrivateModelCatalogUsesExactHelperWithoutChangingActiveSession(t *testing.T) {
	tr := NewTranslator("inst", "/tmp/work")
	tr.currentSessionID = "active"
	cmd := agentproto.Command{CommandID: "models", Kind: agentproto.CommandModelList, NativeBinding: &agentproto.NativeTurnBinding{}, Target: agentproto.Target{CWD: "/tmp/work"}}
	out, err := tr.TranslateCommand(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.OutboundToChild) != 1 {
		t.Fatal("missing native catalog query")
	}
	frame := decodeFrame(t, out.OutboundToChild[0])
	if frame["method"] != "session/new" {
		t.Fatal(frame)
	}
	result, err := tr.ObserveServer(mustLine(t, map[string]any{"id": frame["id"], "result": map[string]any{"sessionId": "helper"}}))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 1 || result.Events[0].Kind != agentproto.EventModelCatalogUpdated || result.Events[0].RequestID != frame["id"] || result.Events[0].ThreadID != "helper" || tr.currentSessionID != "active" {
		t.Fatalf("catalog result: %+v, active=%s", result.Events, tr.currentSessionID)
	}
}
