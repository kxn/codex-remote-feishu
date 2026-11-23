package workruntime

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workpublish"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/renderer"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestPrivateLegacyVisibleReplyParity(t *testing.T) {
	now := time.Date(2026, 4, 12, 13, 0, 0, 0, time.UTC)
	legacy := orchestrator.NewService(func() time.Time { return now }, orchestrator.Config{TurnHandoffWait: 800 * time.Millisecond, GitAvailable: true}, renderer.NewPlanner())
	legacy.UpsertInstance(&state.InstanceRecord{InstanceID: "instance", DisplayName: "worker", WorkspaceRoot: "/tmp/work", WorkspaceKey: "/tmp/work", Online: true, ObservedFocusedThreadID: "session", Threads: map[string]*state.ThreadRecord{"session": {ThreadID: "session", CWD: "/tmp/work", Loaded: true}}})
	for _, action := range []control.Action{
		{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface", ChatID: "chat", ActorUserID: "user", InstanceID: "instance"},
		{Kind: control.ActionUseThread, SurfaceSessionID: "surface", ChatID: "chat", ActorUserID: "user", ThreadID: "session"},
		{Kind: control.ActionTextMessage, SurfaceSessionID: "surface", ChatID: "chat", ActorUserID: "user", MessageID: "human", Text: "do work"},
	} {
		legacy.ApplySurfaceAction(action)
	}
	legacy.ApplyAgentEvent("instance", agentproto.Event{Kind: agentproto.EventTurnStarted, ThreadID: "session", TurnID: "turn-1", Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: "surface"}})
	f := newFixture(t, work.BackendCodex, false, true)
	f.coordinator.options.EnqueueReconcile = func(string) {}
	f.start()
	sdk := startPrivateSDK(t, false)
	publisher, err := workpublish.New(f.store, privateSDKResolver{sdk.api}, func() time.Time { return f.now })
	if err != nil {
		t.Fatal(err)
	}
	projector := feishu.NewProjector()
	var legacyCards []string
	for _, event := range []agentproto.Event{
		{Kind: agentproto.EventItemCompleted, ItemKind: "agent_message", ItemID: "final", Metadata: map[string]any{"text": "Shared final answer.", "phase": "final_answer"}},
		{Kind: agentproto.EventTurnCompleted, Status: "completed"},
	} {
		event.ThreadID, event.TurnID = "session", "turn-1"
		for _, projected := range legacy.ApplyAgentEvent("instance", event) {
			for _, op := range projector.ProjectEvent("chat", projected) {
				if op.Kind == feishu.OperationSendCard || op.Kind == feishu.OperationSendText {
					data, _ := json.Marshal(op)
					legacyCards = append(legacyCards, string(data)+op.FinalSourceBody())
				}
			}
		}
		f.now = f.now.Add(time.Second)
		if err := f.handle(event); err != nil {
			t.Fatal(err)
		}
		drainPrivatePublisher(t, publisher)
	}
	privateCards := sdk.requests()
	if len(legacyCards) != 1 || len(privateCards) != len(legacyCards) {
		t.Fatalf("legacy cards %v; private requests %+v", legacyCards, privateCards)
	}
	if privateCards[0].method != "POST" || !strings.Contains(legacyCards[0], "Shared final answer.") || !strings.Contains(privateCards[0].body, "Shared final answer.") {
		t.Fatalf("legacy %v; private %+v", legacyCards, privateCards)
	}
	drainPrivatePublisher(t, publisher)
	if len(sdk.requests()) != len(privateCards) {
		t.Fatal("completion published an extra card on a later drain")
	}
}
