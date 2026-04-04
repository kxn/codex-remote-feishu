package harness

import (
	"strings"
	"testing"

	"fschannel/internal/core/control"
	"fschannel/testkit/mockcodex"
)

func TestRemotePromptWithoutSelectedThreadCreatesThreadAndProjectsReply(t *testing.T) {
	h := New()
	inst := h.Service.Instance(h.InstanceID)
	inst.ObservedFocusedThreadID = ""
	inst.ActiveThreadID = ""
	h.Codex.OmitFinalText = true

	if err := h.ApplyAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       h.InstanceID,
	}); err != nil {
		t.Fatalf("attach instance: %v", err)
	}

	h.Codex.Responder = func(turn mockcodex.TurnStart) string {
		return "您好"
	}

	if err := h.ApplyAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "msg-1",
		Text:             "你好",
	}); err != nil {
		t.Fatalf("send text: %v", err)
	}

	snapshot := h.Service.SurfaceSnapshot("feishu:chat:1")
	if snapshot == nil {
		t.Fatal("expected surface snapshot to exist")
	}
	if snapshot.Attachment.SelectedThreadID == "" {
		t.Fatal("expected new thread to be selected after remote turn starts")
	}

	var foundReply bool
	for _, block := range h.Feishu.Blocks {
		if strings.Contains(block.Text, "您好") {
			foundReply = true
			break
		}
	}
	if !foundReply {
		t.Fatalf("expected assistant reply to be projected, blocks=%#v notices=%#v", h.Feishu.Blocks, h.Feishu.Notices)
	}
}

func TestPinnedThreadRemainsPromptTargetAfterLocalFocusChanges(t *testing.T) {
	h := New()
	h.Codex.SeedThread("thread-2", "/data/dl/other", "另一个会话")
	if err := h.ApplyAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       h.InstanceID,
	}); err != nil {
		t.Fatalf("attach instance: %v", err)
	}
	if err := h.LocalClient([]byte(`{"method":"thread/resume","params":{"threadId":"thread-2","cwd":"/data/dl/other"}}` + "\n")); err != nil {
		t.Fatalf("local thread resume: %v", err)
	}

	h.Codex.Responder = func(turn mockcodex.TurnStart) string {
		if turn.ThreadID != "thread-1" {
			t.Fatalf("expected remote prompt to stay on pinned thread-1, got %q", turn.ThreadID)
		}
		return "已路由到 pinned thread"
	}

	if err := h.ApplyAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "msg-1",
		Text:             "你好",
	}); err != nil {
		t.Fatalf("send text: %v", err)
	}
}

func TestStructuredLocalHelperTurnIsIgnoredWhenAttached(t *testing.T) {
	h := New()
	if err := h.ApplyAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       h.InstanceID,
	}); err != nil {
		t.Fatalf("attach instance: %v", err)
	}

	blockCount := len(h.Feishu.Blocks)
	if err := h.LocalClient([]byte(`{"id":"helper-turn-1","method":"turn/start","params":{"threadId":"thread-1","cwd":"/data/dl/droid","outputSchema":{"type":"object","properties":{"title":{"type":"string"},"body":{"type":"string"}}}}}` + "\n")); err != nil {
		t.Fatalf("local helper turn: %v", err)
	}

	if len(h.Feishu.Blocks) != blockCount {
		t.Fatalf("expected structured helper turn to stay hidden from feishu, got blocks=%#v notices=%#v", h.Feishu.Blocks[blockCount:], h.Feishu.Notices)
	}
}

func TestStructuredLocalHelperTurnDoesNotPoisonRemoteReply(t *testing.T) {
	h := New()
	h.Codex.Responder = func(turn mockcodex.TurnStart) string {
		if h.Codex.LastTurnStart["outputSchema"] != nil {
			return "{\"title\":\"当前目录是 `/data/dl`。\",\"body\":\"```text\\nREADME.md\\n```\"}"
		}
		return "当前目录是 `/data/dl`。\n\n```text\nREADME.md\n```"
	}
	if err := h.LocalClient([]byte(`{"id":"helper-turn-1","method":"turn/start","params":{"threadId":"thread-1","cwd":"/data/dl/droid","outputSchema":{"type":"object","properties":{"title":{"type":"string"},"body":{"type":"string"}}}}}` + "\n")); err != nil {
		t.Fatalf("local helper turn: %v", err)
	}
	if err := h.ApplyAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       h.InstanceID,
	}); err != nil {
		t.Fatalf("attach instance: %v", err)
	}
	if err := h.ApplyAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "msg-1",
		Text:             "列一下目录",
	}); err != nil {
		t.Fatalf("send text: %v", err)
	}

	foundNormal := false
	for _, block := range h.Feishu.Blocks {
		if strings.Contains(block.Text, `{"title":`) {
			t.Fatalf("expected remote reply to stay plain text, got blocks=%#v", h.Feishu.Blocks)
		}
		if strings.Contains(block.Text, "当前目录是 `/data/dl`。") {
			foundNormal = true
		}
	}
	if !foundNormal {
		t.Fatalf("expected normalized remote reply block, got %#v", h.Feishu.Blocks)
	}
}
