package codex

import (
	"encoding/json"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/pathcanon"
)

func TestTranslateThreadCompactStart(t *testing.T) {
	tr := NewTranslator("inst-1")
	if _, err := tr.ObserveClient([]byte(`{"method":"thread/resume","params":{"threadId":"thread-1","cwd":"/tmp/project"}}`)); err != nil {
		t.Fatalf("observe current thread: %v", err)
	}
	commands, err := tr.TranslateCommand(agentproto.Command{
		Kind:   agentproto.CommandThreadCompactStart,
		Origin: agentproto.Origin{Surface: "surface-1"},
		Target: agentproto.Target{ThreadID: "thread-1"},
	})
	if err != nil {
		t.Fatalf("translate compact command: %v", err)
	}
	if len(commands) != 1 {
		t.Fatalf("expected one compact command, got %#v", commands)
	}
	var payload map[string]any
	if err := json.Unmarshal(commands[0], &payload); err != nil {
		t.Fatalf("unmarshal compact command: %v", err)
	}
	if payload["method"] != "thread/compact/start" {
		t.Fatalf("expected thread/compact/start payload, got %#v", payload)
	}
	params, _ := payload["params"].(map[string]any)
	if params["threadId"] != "thread-1" {
		t.Fatalf("unexpected compact params: %#v", params)
	}
}

func TestTranslateThreadCompactStartResumesTargetThreadWhenNotCurrent(t *testing.T) {
	tr := NewTranslator("inst-1")
	if _, err := tr.ObserveClient([]byte(`{"method":"thread/resume","params":{"threadId":"thread-current","cwd":"/tmp/current"}}`)); err != nil {
		t.Fatalf("observe current thread: %v", err)
	}

	commands, err := tr.TranslateCommand(agentproto.Command{
		Kind:   agentproto.CommandThreadCompactStart,
		Origin: agentproto.Origin{Surface: "surface-1"},
		Target: agentproto.Target{ThreadID: "thread-1", CWD: "/tmp/project"},
	})
	if err != nil {
		t.Fatalf("translate compact command: %v", err)
	}
	if len(commands) != 1 {
		t.Fatalf("expected one compact prepare command, got %#v", commands)
	}
	var resumePayload map[string]any
	if err := json.Unmarshal(commands[0], &resumePayload); err != nil {
		t.Fatalf("unmarshal resume command: %v", err)
	}
	if resumePayload["method"] != "thread/resume" {
		t.Fatalf("expected thread/resume payload, got %#v", resumePayload)
	}
	resumeParams, _ := resumePayload["params"].(map[string]any)
	if resumeParams["threadId"] != "thread-1" || resumeParams["cwd"] != pathcanon.Native("/tmp/project") {
		t.Fatalf("unexpected resume params: %#v", resumeParams)
	}

	result, err := tr.ObserveServer([]byte(`{"id":"relay-thread-resume-0","result":{"thread":{"id":"thread-1","cwd":"/tmp/project"}}}`))
	if err != nil {
		t.Fatalf("observe thread resume response: %v", err)
	}
	if !result.Suppress || len(result.OutboundToCodex) != 1 {
		t.Fatalf("expected suppressed compact followup, got %#v", result)
	}
	var compactPayload map[string]any
	if err := json.Unmarshal(result.OutboundToCodex[0], &compactPayload); err != nil {
		t.Fatalf("unmarshal compact followup: %v", err)
	}
	if compactPayload["method"] != "thread/compact/start" {
		t.Fatalf("expected compact followup payload, got %#v", compactPayload)
	}
	compactParams, _ := compactPayload["params"].(map[string]any)
	if compactParams["threadId"] != "thread-1" {
		t.Fatalf("unexpected compact followup params: %#v", compactParams)
	}

	next, err := tr.TranslateCommand(agentproto.Command{
		Kind:   agentproto.CommandThreadCompactStart,
		Origin: agentproto.Origin{Surface: "surface-1"},
		Target: agentproto.Target{ThreadID: "thread-1"},
	})
	if err != nil {
		t.Fatalf("translate compact command after resume: %v", err)
	}
	if len(next) != 1 {
		t.Fatalf("expected one direct compact command, got %#v", next)
	}
	var directPayload map[string]any
	if err := json.Unmarshal(next[0], &directPayload); err != nil {
		t.Fatalf("unmarshal direct compact command: %v", err)
	}
	if directPayload["method"] != "thread/compact/start" {
		t.Fatalf("expected direct compact payload after resume, got %#v", directPayload)
	}
}

func TestObserveTurnStartedAfterCompactUsesRemoteSurfaceInitiator(t *testing.T) {
	tr := NewTranslator("inst-1")
	if _, err := tr.ObserveClient([]byte(`{"method":"thread/resume","params":{"threadId":"thread-1","cwd":"/tmp/project"}}`)); err != nil {
		t.Fatalf("observe current thread: %v", err)
	}
	_, err := tr.TranslateCommand(agentproto.Command{
		Kind:   agentproto.CommandThreadCompactStart,
		Origin: agentproto.Origin{Surface: "surface-1"},
		Target: agentproto.Target{ThreadID: "thread-1"},
	})
	if err != nil {
		t.Fatalf("translate compact command: %v", err)
	}

	result, err := tr.ObserveServer([]byte(`{"method":"turn/started","params":{"threadId":"thread-1","turn":{"id":"turn-compact-1"}}}`))
	if err != nil {
		t.Fatalf("observe turn started: %v", err)
	}
	if len(result.Events) != 1 {
		t.Fatalf("expected one event, got %#v", result.Events)
	}
	if result.Events[0].Initiator.Kind != agentproto.InitiatorRemoteSurface || result.Events[0].Initiator.SurfaceSessionID != "surface-1" {
		t.Fatalf("unexpected initiator after compact start: %#v", result.Events[0].Initiator)
	}
}

func TestObserveCompactStartErrorEmitsSystemError(t *testing.T) {
	tr := NewTranslator("inst-1")
	if _, err := tr.ObserveClient([]byte(`{"method":"thread/resume","params":{"threadId":"thread-1","cwd":"/tmp/project"}}`)); err != nil {
		t.Fatalf("observe current thread: %v", err)
	}
	_, err := tr.TranslateCommand(agentproto.Command{
		Kind:   agentproto.CommandThreadCompactStart,
		Origin: agentproto.Origin{Surface: "surface-1"},
		Target: agentproto.Target{ThreadID: "thread-1"},
	})
	if err != nil {
		t.Fatalf("translate compact command: %v", err)
	}

	result, err := tr.ObserveServer([]byte(`{"id":"relay-thread-compact-start-0","error":{"message":"thread busy"}}`))
	if err != nil {
		t.Fatalf("observe compact error: %v", err)
	}
	if len(result.Events) != 1 || result.Events[0].Kind != agentproto.EventSystemError || result.Events[0].Problem == nil {
		t.Fatalf("expected compact start system error, got %#v", result.Events)
	}
	if result.Events[0].Problem.Operation != "thread.compact.start" || result.Events[0].Problem.SurfaceSessionID != "surface-1" {
		t.Fatalf("unexpected compact start problem: %#v", result.Events[0].Problem)
	}
}

func TestObserveCompactThreadResumeErrorEmitsSystemError(t *testing.T) {
	tr := NewTranslator("inst-1")
	if _, err := tr.ObserveClient([]byte(`{"method":"thread/resume","params":{"threadId":"thread-current","cwd":"/tmp/current"}}`)); err != nil {
		t.Fatalf("observe current thread: %v", err)
	}
	_, err := tr.TranslateCommand(agentproto.Command{
		Kind:   agentproto.CommandThreadCompactStart,
		Origin: agentproto.Origin{Surface: "surface-1"},
		Target: agentproto.Target{ThreadID: "thread-1"},
	})
	if err != nil {
		t.Fatalf("translate compact command: %v", err)
	}

	result, err := tr.ObserveServer([]byte(`{"id":"relay-thread-resume-0","error":{"message":"thread not found"}}`))
	if err != nil {
		t.Fatalf("observe thread resume error: %v", err)
	}
	if len(result.Events) != 1 || result.Events[0].Kind != agentproto.EventSystemError || result.Events[0].Problem == nil {
		t.Fatalf("expected compact resume system error, got %#v", result.Events)
	}
	if result.Events[0].Problem.Operation != "thread.compact.start" ||
		result.Events[0].Problem.ThreadID != "thread-1" ||
		result.Events[0].Problem.SurfaceSessionID != "surface-1" {
		t.Fatalf("unexpected compact resume problem: %#v", result.Events[0].Problem)
	}
}
