package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/render"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

type recordingGateway struct {
	operations []feishu.Operation
}

func (g *recordingGateway) Start(context.Context, feishu.ActionHandler) error { return nil }

func (g *recordingGateway) Apply(_ context.Context, operations []feishu.Operation) error {
	g.operations = append(g.operations, operations...)
	return nil
}

type ctxCheckingGateway struct {
	ctxErr     error
	operations []feishu.Operation
}

func (g *ctxCheckingGateway) Start(context.Context, feishu.ActionHandler) error { return nil }

func (g *ctxCheckingGateway) Apply(ctx context.Context, operations []feishu.Operation) error {
	g.ctxErr = ctx.Err()
	g.operations = append(g.operations, operations...)
	return nil
}

type flakyGateway struct {
	failures   int
	operations []feishu.Operation
}

func (g *flakyGateway) Start(context.Context, feishu.ActionHandler) error { return nil }

func (g *flakyGateway) Apply(_ context.Context, operations []feishu.Operation) error {
	if g.failures > 0 {
		g.failures--
		return errors.New("lark temporarily unavailable")
	}
	g.operations = append(g.operations, operations...)
	return nil
}

type stubMarkdownPreviewer struct {
	requests []feishu.MarkdownPreviewRequest
	text     string
	err      error
}

func (s *stubMarkdownPreviewer) RewriteFinalBlock(_ context.Context, req feishu.MarkdownPreviewRequest) (render.Block, error) {
	s.requests = append(s.requests, req)
	block := req.Block
	if s.text != "" {
		block.Text = s.text
	}
	return block, s.err
}

func TestDaemonProjectsListAttachAndAssistantOutput(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})

	app.onHello(context.Background(), agentproto.Hello{
		Instance: agentproto.InstanceHello{
			InstanceID:    "inst-1",
			DisplayName:   "droid",
			WorkspaceRoot: "/data/dl/droid",
			WorkspaceKey:  "/data/dl/droid",
			ShortName:     "droid",
		},
	})
	app.onEvents(context.Background(), "inst-1", []agentproto.Event{{
		Kind:    agentproto.EventThreadsSnapshot,
		Threads: []agentproto.ThreadSnapshotRecord{{ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true}},
	}})
	app.onEvents(context.Background(), "inst-1", []agentproto.Event{{
		Kind:        agentproto.EventThreadFocused,
		ThreadID:    "thread-1",
		CWD:         "/data/dl/droid",
		FocusSource: "local_ui",
	}})

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "msg-1",
		Text:             "你好",
	})

	app.onEvents(context.Background(), "inst-1", []agentproto.Event{{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: "feishu:chat:1"},
	}})
	app.onEvents(context.Background(), "inst-1", []agentproto.Event{{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "item-1",
		Metadata: map[string]any{"text": "已收到：\n\n```text\nREADME.md\n```"},
	}})
	app.onEvents(context.Background(), "inst-1", []agentproto.Event{{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: "feishu:chat:1"},
	}})

	var hasListCard bool
	var hasTyping bool
	var hasFinalReplyCard bool
	for _, operation := range gateway.operations {
		switch {
		case operation.Kind == feishu.OperationSendCard && operation.CardTitle == "在线实例":
			hasListCard = true
		case operation.Kind == feishu.OperationAddReaction && operation.MessageID == "msg-1":
			hasTyping = true
		case operation.Kind == feishu.OperationSendCard && operation.CardTitle == "最终回复 · droid · 修复登录流程":
			hasFinalReplyCard = operation.CardBody == "已收到：\n\n```text\nREADME.md\n```"
		}
	}
	if !hasListCard {
		t.Fatalf("expected online instance card, got %#v", gateway.operations)
	}
	if !hasTyping {
		t.Fatalf("expected typing reaction, got %#v", gateway.operations)
	}
	if !hasFinalReplyCard {
		t.Fatalf("expected final assistant reply card, got %#v", gateway.operations)
	}
}

func TestDaemonNewThreadProjectsReadyState(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})

	app.onHello(context.Background(), agentproto.Hello{
		Instance: agentproto.InstanceHello{
			InstanceID:    "inst-1",
			DisplayName:   "droid",
			WorkspaceRoot: "/data/dl/droid",
			WorkspaceKey:  "/data/dl/droid",
			ShortName:     "droid",
		},
	})
	app.onEvents(context.Background(), "inst-1", []agentproto.Event{{
		Kind:    agentproto.EventThreadsSnapshot,
		Threads: []agentproto.ThreadSnapshotRecord{{ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true}},
	}})
	app.onEvents(context.Background(), "inst-1", []agentproto.Event{{
		Kind:        agentproto.EventThreadFocused,
		ThreadID:    "thread-1",
		CWD:         "/data/dl/droid",
		FocusSource: "local_ui",
	}})

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionNewThread,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	snapshot := app.service.SurfaceSnapshot("feishu:chat:1")
	if snapshot == nil || snapshot.Attachment.RouteMode != string(state.RouteModeNewThreadReady) || !snapshot.NextPrompt.CreateThread || snapshot.NextPrompt.CWD != "/data/dl/droid" {
		t.Fatalf("expected new-thread-ready snapshot, got %#v", snapshot)
	}

	var sawReadyCard bool
	for _, operation := range gateway.operations {
		if operation.Kind == feishu.OperationSendCard && strings.Contains(operation.CardBody, "新建会话（等待首条消息）") {
			sawReadyCard = true
			break
		}
	}
	if !sawReadyCard {
		t.Fatalf("expected gateway projection to include new-thread-ready state, got %#v", gateway.operations)
	}
}

func TestDaemonDecouplesGatewayApplyFromCanceledParentContext(t *testing.T) {
	gateway := &ctxCheckingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})

	app.onHello(context.Background(), agentproto.Hello{
		Instance: agentproto.InstanceHello{
			InstanceID:    "inst-1",
			DisplayName:   "droid",
			WorkspaceRoot: "/data/dl/droid",
			WorkspaceKey:  "/data/dl/droid",
			ShortName:     "droid",
		},
	})
	app.onEvents(context.Background(), "inst-1", []agentproto.Event{{
		Kind:    agentproto.EventThreadsSnapshot,
		Threads: []agentproto.ThreadSnapshotRecord{{ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true}},
	}})
	app.onEvents(context.Background(), "inst-1", []agentproto.Event{{
		Kind:        agentproto.EventThreadFocused,
		ThreadID:    "thread-1",
		CWD:         "/data/dl/droid",
		FocusSource: "local_ui",
	}})

	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	app.HandleAction(cancelledCtx, control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	if gateway.ctxErr != nil {
		t.Fatalf("expected gateway apply context to be decoupled from canceled parent, got %v", gateway.ctxErr)
	}
	if len(gateway.operations) == 0 {
		t.Fatalf("expected gateway operations, got %#v", gateway.operations)
	}
}

func TestDaemonRewritesFinalAssistantLinksViaMarkdownPreviewer(t *testing.T) {
	gateway := &recordingGateway{}
	previewer := &stubMarkdownPreviewer{text: "查看 [设计文档](https://preview/file-1)"}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	app.SetMarkdownPreviewer(previewer)

	app.onHello(context.Background(), agentproto.Hello{
		Instance: agentproto.InstanceHello{
			InstanceID:    "inst-1",
			DisplayName:   "droid",
			WorkspaceRoot: "/data/dl/droid",
			WorkspaceKey:  "/data/dl/droid",
			ShortName:     "droid",
		},
	})
	app.onEvents(context.Background(), "inst-1", []agentproto.Event{{
		Kind:    agentproto.EventThreadsSnapshot,
		Threads: []agentproto.ThreadSnapshotRecord{{ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true}},
	}})
	app.onEvents(context.Background(), "inst-1", []agentproto.Event{{
		Kind:        agentproto.EventThreadFocused,
		ThreadID:    "thread-1",
		CWD:         "/data/dl/droid",
		FocusSource: "local_ui",
	}})

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "ou_user",
		InstanceID:       "inst-1",
	})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "ou_user",
		MessageID:        "msg-1",
		Text:             "你好",
	})
	app.onEvents(context.Background(), "inst-1", []agentproto.Event{{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: "feishu:chat:1"},
	}})
	app.onEvents(context.Background(), "inst-1", []agentproto.Event{{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "item-1",
		Metadata: map[string]any{"text": "查看 [设计文档](/data/dl/droid/docs/design.md)"},
	}})
	app.onEvents(context.Background(), "inst-1", []agentproto.Event{{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: "feishu:chat:1"},
	}})

	if len(previewer.requests) != 1 {
		t.Fatalf("expected one preview rewrite request, got %#v", previewer.requests)
	}
	if previewer.requests[0].WorkspaceRoot != "/data/dl/droid" || previewer.requests[0].ThreadCWD != "/data/dl/droid" {
		t.Fatalf("unexpected preview context: %#v", previewer.requests[0])
	}

	var finalBody string
	for _, operation := range gateway.operations {
		if operation.Kind == feishu.OperationSendCard && operation.CardTitle == "最终回复 · droid · 修复登录流程" {
			finalBody = operation.CardBody
		}
	}
	if finalBody != "查看 [设计文档](https://preview/file-1)" {
		t.Fatalf("expected rewritten final reply body, got %#v", gateway.operations)
	}
}

func TestDaemonFallsBackToActorRouteForColdStartMenuActions(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})

	app.onHello(context.Background(), agentproto.Hello{
		Instance: agentproto.InstanceHello{
			InstanceID:    "inst-1",
			DisplayName:   "droid",
			WorkspaceRoot: "/data/dl/droid",
			WorkspaceKey:  "/data/dl/droid",
			ShortName:     "droid",
		},
	})

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "feishu:user:ou_1",
		ActorUserID:      "ou_1",
	})

	if len(gateway.operations) != 1 {
		t.Fatalf("expected one operation, got %#v", gateway.operations)
	}
	got := gateway.operations[0]
	if got.Kind != feishu.OperationSendCard || got.CardTitle != "在线实例" {
		t.Fatalf("unexpected operation: %#v", got)
	}
	if got.ReceiveID != "ou_1" || got.ReceiveIDType != "open_id" {
		t.Fatalf("expected actor fallback route, got %#v", got)
	}
}

func TestDaemonNotifiesAttachedSurfaceWhenInstanceDisconnects(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})

	app.onHello(context.Background(), agentproto.Hello{
		Instance: agentproto.InstanceHello{
			InstanceID:    "inst-1",
			DisplayName:   "droid",
			WorkspaceRoot: "/data/dl/droid",
			WorkspaceKey:  "/data/dl/droid",
			ShortName:     "droid",
		},
	})

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})

	before := len(gateway.operations)
	app.onDisconnect(context.Background(), "inst-1")

	var hasOfflineNotice bool
	for _, operation := range gateway.operations[before:] {
		switch {
		case operation.Kind == feishu.OperationSendCard && operation.CardTitle == "系统提示" && operation.CardBody == "当前接管实例已离线：droid":
			hasOfflineNotice = true
		}
	}
	if !hasOfflineNotice {
		t.Fatalf("expected offline notice, got %#v", gateway.operations[before:])
	}
}

func TestDaemonTickResumesQueuedRemoteInputAfterLocalTurnCompletes(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})

	app.onHello(context.Background(), agentproto.Hello{
		Instance: agentproto.InstanceHello{
			InstanceID:    "inst-1",
			DisplayName:   "droid",
			WorkspaceRoot: "/data/dl/droid",
			WorkspaceKey:  "/data/dl/droid",
			ShortName:     "droid",
		},
	})
	app.onEvents(context.Background(), "inst-1", []agentproto.Event{{
		Kind:    agentproto.EventThreadsSnapshot,
		Threads: []agentproto.ThreadSnapshotRecord{{ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true}},
	}})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		ThreadID:         "thread-1",
	})

	app.onEvents(context.Background(), "inst-1", []agentproto.Event{{
		Kind:     agentproto.EventLocalInteractionObserved,
		ThreadID: "thread-1",
		CWD:      "/data/dl/droid",
		Action:   "turn_start",
	}})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "msg-queued",
		Text:             "列一下目录",
	})
	app.onEvents(context.Background(), "inst-1", []agentproto.Event{{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	}})
	app.onTick(context.Background(), time.Now().Add(2*time.Second))

	var hasTyping bool
	for _, operation := range gateway.operations {
		if operation.Kind == feishu.OperationAddReaction && operation.MessageID == "msg-queued" {
			hasTyping = true
		}
	}
	if !hasTyping {
		t.Fatalf("expected queued message to resume dispatch after tick, got %#v", gateway.operations)
	}
}

func TestDaemonProjectsQueuedAndDiscardedReactionsForRecalledMessage(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})

	app.onHello(context.Background(), agentproto.Hello{
		Instance: agentproto.InstanceHello{
			InstanceID:    "inst-1",
			DisplayName:   "droid",
			WorkspaceRoot: "/data/dl/droid",
			WorkspaceKey:  "/data/dl/droid",
			ShortName:     "droid",
		},
	})
	app.onEvents(context.Background(), "inst-1", []agentproto.Event{{
		Kind:    agentproto.EventThreadsSnapshot,
		Threads: []agentproto.ThreadSnapshotRecord{{ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true}},
	}})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		ThreadID:         "thread-1",
	})

	app.onEvents(context.Background(), "inst-1", []agentproto.Event{{
		Kind:     agentproto.EventLocalInteractionObserved,
		ThreadID: "thread-1",
		CWD:      "/data/dl/droid",
		Action:   "turn_start",
	}})

	beforeQueue := len(gateway.operations)
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "msg-queued",
		Text:             "先排队",
	})
	queueOps := gateway.operations[beforeQueue:]
	if len(queueOps) == 0 || queueOps[0].Kind != feishu.OperationAddReaction || queueOps[0].EmojiType != "OneSecond" {
		t.Fatalf("expected queued message to receive OneSecond reaction, got %#v", queueOps)
	}

	beforeRecall := len(gateway.operations)
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionMessageRecalled,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		TargetMessageID:  "msg-queued",
	})
	recallOps := gateway.operations[beforeRecall:]
	if len(recallOps) != 2 {
		t.Fatalf("expected queue reaction removal plus discard reaction, got %#v", recallOps)
	}
	if recallOps[0].Kind != feishu.OperationRemoveReaction || recallOps[0].EmojiType != "OneSecond" {
		t.Fatalf("expected first recall op to remove queue reaction, got %#v", recallOps)
	}
	if recallOps[1].Kind != feishu.OperationAddReaction || recallOps[1].EmojiType != "ThumbsDown" {
		t.Fatalf("expected second recall op to add discard reaction, got %#v", recallOps)
	}
}

func TestDaemonStatusExportsSurfacesAndRemoteTurnState(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})

	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	app.service.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})
	app.service.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "msg-1",
		Text:             "你好",
	})

	req := httptest.NewRequest("GET", "/v1/status", nil)
	rec := httptest.NewRecorder()
	app.handleStatus(rec, req)

	var payload struct {
		Instances          []struct{ InstanceID string }
		Surfaces           []struct{ SurfaceSessionID, AttachedInstanceID, ActiveQueueItemID string }
		PendingRemoteTurns []struct {
			InstanceID       string
			SurfaceSessionID string
			QueueItemID      string
			SourceMessageID  string
			Status           string
		}
		ActiveRemoteTurns []struct{}
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode status payload: %v", err)
	}
	if len(payload.Instances) != 1 || payload.Instances[0].InstanceID != "inst-1" {
		t.Fatalf("expected one instance in status payload, got %#v", payload.Instances)
	}
	if len(payload.Surfaces) != 1 || payload.Surfaces[0].SurfaceSessionID != "feishu:chat:1" || payload.Surfaces[0].AttachedInstanceID != "inst-1" {
		t.Fatalf("expected attached surface in status payload, got %#v", payload.Surfaces)
	}
	if len(payload.PendingRemoteTurns) != 1 || payload.PendingRemoteTurns[0].SurfaceSessionID != "feishu:chat:1" || payload.PendingRemoteTurns[0].SourceMessageID != "msg-1" || payload.PendingRemoteTurns[0].Status != "dispatching" {
		t.Fatalf("expected pending remote turn in status payload, got %#v", payload.PendingRemoteTurns)
	}
	if len(payload.ActiveRemoteTurns) != 0 {
		t.Fatalf("expected no active remote turns before turn/started, got %#v", payload.ActiveRemoteTurns)
	}
}

func TestDaemonFlushesQueuedGatewayFailureNoticeOnNextSuccess(t *testing.T) {
	gateway := &flakyGateway{failures: 1}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	if len(gateway.operations) != 0 {
		t.Fatalf("expected first gateway apply to fail without delivered operations, got %#v", gateway.operations)
	}

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	if len(gateway.operations) < 2 {
		t.Fatalf("expected queued error notice and current card after recovery, got %#v", gateway.operations)
	}
	if !strings.Contains(gateway.operations[0].CardTitle, "链路错误") || !strings.Contains(gateway.operations[0].CardBody, "位置：`gateway_apply`") {
		t.Fatalf("expected queued gateway failure notice first, got %#v", gateway.operations[0])
	}
	if gateway.operations[1].CardTitle == "" || !strings.Contains(gateway.operations[1].CardBody, "当前没有在线实例") {
		t.Fatalf("expected current response card after queued notice, got %#v", gateway.operations[1])
	}
}

func TestDaemonStartsHeadlessAndPromptsForResumeAfterRefresh(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	stateDir := t.TempDir()
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		BinaryPath: "/tmp/codex-remote",
		ConfigPath: "/tmp/config.json",
		BaseEnv:    []string{"PATH=/usr/bin"},
		Paths: relayruntime.Paths{
			LogsDir:  t.TempDir(),
			StateDir: stateDir,
		},
	})

	var captured relayruntime.HeadlessLaunchOptions
	app.startHeadless = func(opts relayruntime.HeadlessLaunchOptions) (int, error) {
		captured = opts
		return 4321, nil
	}

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionNewInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	snapshot := app.service.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.PendingHeadless.InstanceID == "" {
		t.Fatalf("expected pending headless snapshot, got %#v", snapshot)
	}
	if captured.WorkDir != stateDir || captured.InstanceID != snapshot.PendingHeadless.InstanceID {
		t.Fatalf("unexpected headless launch opts: %#v", captured)
	}
	if !containsEnvEntry(captured.Env, "CODEX_REMOTE_INSTANCE_SOURCE=headless") || !containsEnvEntry(captured.Env, "CODEX_REMOTE_INSTANCE_MANAGED=1") {
		t.Fatalf("expected headless env overrides, got %#v", captured.Env)
	}
	if !containsEnvEntry(captured.Env, "CODEX_REMOTE_INSTANCE_DISPLAY_NAME=headless") {
		t.Fatalf("expected headless display name override, got %#v", captured.Env)
	}

	app.onHello(context.Background(), agentproto.Hello{
		Instance: agentproto.InstanceHello{
			InstanceID:    snapshot.PendingHeadless.InstanceID,
			DisplayName:   "headless",
			WorkspaceRoot: stateDir,
			WorkspaceKey:  stateDir,
			ShortName:     "headless",
			Source:        "headless",
			Managed:       true,
			PID:           4321,
		},
	})

	snapshot = app.service.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.Attachment.InstanceID == "" || snapshot.Attachment.Source != "headless" || !snapshot.Attachment.Managed || snapshot.Attachment.SelectedThreadID != "" {
		t.Fatalf("expected auto-attached headless snapshot, got %#v", snapshot)
	}
	if snapshot.PendingHeadless.Status != string(state.HeadlessLaunchSelecting) {
		t.Fatalf("expected pending selection state, got %#v", snapshot.PendingHeadless)
	}

	app.onEvents(context.Background(), snapshot.Attachment.InstanceID, []agentproto.Event{{
		Kind: agentproto.EventThreadsSnapshot,
		Threads: []agentproto.ThreadSnapshotRecord{{
			ThreadID:  "thread-1",
			Name:      "修复登录流程",
			Preview:   "修登录",
			CWD:       "/data/dl/droid",
			Loaded:    true,
			ListOrder: 1,
		}},
	}})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionResumeHeadless,
		SurfaceSessionID: "surface-1",
		ThreadID:         "thread-1",
	})

	snapshot = app.service.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.Attachment.SelectedThreadID != "thread-1" || snapshot.PendingHeadless.InstanceID != "" {
		t.Fatalf("expected selected headless thread after prompt resolution, got %#v", snapshot)
	}
}

func TestDaemonStartsPreselectedHeadlessForGlobalThreadUse(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	stateDir := t.TempDir()
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		BinaryPath: "/tmp/codex-remote",
		ConfigPath: "/tmp/config.json",
		BaseEnv:    []string{"PATH=/usr/bin"},
		Paths: relayruntime.Paths{
			LogsDir:  t.TempDir(),
			StateDir: stateDir,
		},
	})
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-offline",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        false,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})

	var captured relayruntime.HeadlessLaunchOptions
	app.startHeadless = func(opts relayruntime.HeadlessLaunchOptions) (int, error) {
		captured = opts
		return 4321, nil
	}

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		ThreadID:         "thread-1",
	})

	snapshot := app.service.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.PendingHeadless.ThreadID != "thread-1" || snapshot.PendingHeadless.ThreadCWD != "/data/dl/droid" {
		t.Fatalf("expected pending preselected headless snapshot, got %#v", snapshot)
	}
	if captured.WorkDir != "/data/dl/droid" || captured.InstanceID != snapshot.PendingHeadless.InstanceID {
		t.Fatalf("unexpected preselected headless launch opts: %#v", captured)
	}
	if containsEnvEntry(captured.Env, "CODEX_REMOTE_INSTANCE_DISPLAY_NAME=headless") {
		t.Fatalf("did not expect default headless display override when thread cwd is known, got %#v", captured.Env)
	}

	app.onHello(context.Background(), agentproto.Hello{
		Instance: agentproto.InstanceHello{
			InstanceID:    snapshot.PendingHeadless.InstanceID,
			DisplayName:   "headless",
			WorkspaceRoot: "/data/dl/droid",
			WorkspaceKey:  "/data/dl/droid",
			ShortName:     "headless",
			Source:        "headless",
			Managed:       true,
			PID:           4321,
		},
	})

	snapshot = app.service.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.Attachment.InstanceID == "" || snapshot.Attachment.SelectedThreadID != "thread-1" || snapshot.PendingHeadless.InstanceID != "" {
		t.Fatalf("expected preselected headless hello to auto-attach target thread, got %#v", snapshot)
	}
}

func TestDaemonKillInstanceStopsManagedHeadlessProcess(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{IdleTTL: time.Hour, KillGrace: time.Second})

	stoppedPID := 0
	app.stopProcess = func(pid int, _ time.Duration) error {
		stoppedPID = pid
		return nil
	}

	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-headless-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		Source:        "headless",
		Managed:       true,
		PID:           4321,
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	app.managedHeadless["inst-headless-1"] = &managedHeadlessProcess{InstanceID: "inst-headless-1", PID: 4321, StartedAt: time.Now()}
	app.service.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-headless-1"})

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ThreadID:         "thread-1",
	})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionKillInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	if stoppedPID != 4321 {
		t.Fatalf("expected managed headless pid to stop, got %d", stoppedPID)
	}
	snapshot := app.service.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.Attachment.InstanceID != "" {
		t.Fatalf("expected surface to detach after kill, got %#v", snapshot)
	}
	if app.service.Instance("inst-headless-1") != nil {
		t.Fatalf("expected managed headless instance to be removed after kill, got %#v", app.service.Instance("inst-headless-1"))
	}
}

func TestDaemonIdleHeadlessCleanupStopsDetachedManagedInstance(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{IdleTTL: time.Minute, KillGrace: time.Second})

	stoppedPID := 0
	app.stopProcess = func(pid int, _ time.Duration) error {
		stoppedPID = pid
		return nil
	}

	app.onHello(context.Background(), agentproto.Hello{
		Instance: agentproto.InstanceHello{
			InstanceID:    "inst-headless-2",
			DisplayName:   "droid",
			WorkspaceRoot: "/data/dl/droid",
			WorkspaceKey:  "/data/dl/droid",
			ShortName:     "droid",
			Source:        "headless",
			Managed:       true,
			PID:           2468,
		},
	})

	base := time.Date(2026, 4, 5, 13, 0, 0, 0, time.UTC)
	app.onTick(context.Background(), base)
	app.onTick(context.Background(), base.Add(2*time.Minute))

	if stoppedPID != 2468 {
		t.Fatalf("expected idle managed headless pid to stop, got %d", stoppedPID)
	}
	if app.service.Instance("inst-headless-2") != nil {
		t.Fatalf("expected idle managed headless instance to be removed, got %#v", app.service.Instance("inst-headless-2"))
	}
}

func containsEnvEntry(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
