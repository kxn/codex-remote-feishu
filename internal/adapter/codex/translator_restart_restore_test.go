package codex

import (
	"encoding/json"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestBuildChildRestartRestoreFrameReturnsNilWithoutFocusedThread(t *testing.T) {
	tr := NewTranslator("inst-1")
	frame, requestID, ok, err := tr.BuildChildRestartRestoreFrame("cmd-restart-1")
	if err != nil {
		t.Fatalf("BuildChildRestartRestoreFrame: %v", err)
	}
	if ok || len(frame) != 0 || requestID != "" {
		t.Fatalf("expected no restore frame without focused thread, got ok=%t request=%q frame=%q", ok, requestID, string(frame))
	}
}

func TestBuildChildRestartRestoreFrameSuppressesRestoreResponseAndThreadStarted(t *testing.T) {
	tr := NewTranslator("inst-1")
	if _, err := tr.ObserveClient([]byte(`{"method":"thread/resume","params":{"threadId":"thread-1","cwd":"/tmp/project"}}`)); err != nil {
		t.Fatalf("seed thread/resume: %v", err)
	}

	frame, requestID, ok, err := tr.BuildChildRestartRestoreFrame("cmd-restart-1")
	if err != nil {
		t.Fatalf("BuildChildRestartRestoreFrame: %v", err)
	}
	if !ok {
		t.Fatal("expected restore frame")
	}

	var payload map[string]any
	if err := json.Unmarshal(frame, &payload); err != nil {
		t.Fatalf("unmarshal restore frame: %v", err)
	}
	if payload["method"] != "thread/resume" {
		t.Fatalf("expected thread/resume restore frame, got %#v", payload)
	}
	params, _ := payload["params"].(map[string]any)
	if params["threadId"] != "thread-1" || params["cwd"] != "/tmp/project" {
		t.Fatalf("unexpected restore params: %#v", params)
	}

	result, err := tr.ObserveServer([]byte(`{"id":"` + requestID + `","result":{}}`))
	if err != nil {
		t.Fatalf("observe restore result: %v", err)
	}
	if !result.Suppress || len(result.Events) != 1 || len(result.OutboundToCodex) != 0 {
		t.Fatalf("expected restore response to be suppressed, got %#v", result)
	}
	if result.Events[0].Kind != agentproto.EventProcessChildRestartUpdated || result.Events[0].CommandID != "cmd-restart-1" || result.Events[0].Status != string(agentproto.ChildRestartStatusSucceeded) {
		t.Fatalf("unexpected restore success event: %#v", result.Events[0])
	}

	started, err := tr.ObserveServer([]byte(`{"method":"thread/started","params":{"thread":{"id":"thread-1","cwd":"/tmp/project","name":"修复登录流程"}}}`))
	if err != nil {
		t.Fatalf("observe thread/started: %v", err)
	}
	if !started.Suppress || len(started.Events) != 0 {
		t.Fatalf("expected restart thread/started to be suppressed, got %#v", started)
	}
	if tr.currentThreadID != "thread-1" {
		t.Fatalf("expected focused thread to stay restored, got %q", tr.currentThreadID)
	}
	if tr.knownThreadCWD["thread-1"] != "/tmp/project" {
		t.Fatalf("expected cwd to stay restored, got %#v", tr.knownThreadCWD)
	}
}

func TestCancelChildRestartRestoreDropsPendingRequest(t *testing.T) {
	tr := NewTranslator("inst-1")
	if _, err := tr.ObserveClient([]byte(`{"method":"thread/resume","params":{"threadId":"thread-1","cwd":"/tmp/project"}}`)); err != nil {
		t.Fatalf("seed thread/resume: %v", err)
	}

	_, requestID, ok, err := tr.BuildChildRestartRestoreFrame("cmd-restart-1")
	if err != nil {
		t.Fatalf("BuildChildRestartRestoreFrame: %v", err)
	}
	if !ok {
		t.Fatal("expected restore frame")
	}
	if _, exists := tr.pendingChildRestartRestore[requestID]; !exists {
		t.Fatalf("expected pending restore request %q", requestID)
	}

	tr.CancelChildRestartRestore(requestID)

	if _, exists := tr.pendingChildRestartRestore[requestID]; exists {
		t.Fatalf("expected pending restore request %q to be cleared", requestID)
	}
}

func TestBuildChildRestartRestoreFrameEmitsFailureEventOnRestoreError(t *testing.T) {
	tr := NewTranslator("inst-1")
	if _, err := tr.ObserveClient([]byte(`{"method":"thread/resume","params":{"threadId":"thread-1","cwd":"/tmp/project"}}`)); err != nil {
		t.Fatalf("seed thread/resume: %v", err)
	}

	_, requestID, ok, err := tr.BuildChildRestartRestoreFrame("cmd-restart-1")
	if err != nil {
		t.Fatalf("BuildChildRestartRestoreFrame: %v", err)
	}
	if !ok {
		t.Fatal("expected restore frame")
	}

	result, err := tr.ObserveServer([]byte(`{"id":"` + requestID + `","error":{"message":"restore failed"}}`))
	if err != nil {
		t.Fatalf("observe restore error: %v", err)
	}
	if !result.Suppress || len(result.Events) != 1 {
		t.Fatalf("expected suppressed restore error event, got %#v", result)
	}
	if result.Events[0].Kind != agentproto.EventProcessChildRestartUpdated || result.Events[0].Status != string(agentproto.ChildRestartStatusFailed) {
		t.Fatalf("unexpected restore failure event: %#v", result.Events[0])
	}
	if result.Events[0].Problem == nil || result.Events[0].Problem.CommandID != "cmd-restart-1" {
		t.Fatalf("expected failure problem with command id, got %#v", result.Events[0])
	}
}
