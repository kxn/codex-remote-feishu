package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
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
	requests    []feishu.FinalBlockPreviewRequest
	text        string
	supplements []feishu.PreviewSupplement
	err         error
}

func (s *stubMarkdownPreviewer) RewriteFinalBlock(_ context.Context, req feishu.FinalBlockPreviewRequest) (feishu.FinalBlockPreviewResult, error) {
	s.requests = append(s.requests, req)
	block := req.Block
	if s.text != "" {
		block.Text = s.text
	}
	return feishu.FinalBlockPreviewResult{
		Block:       block,
		Supplements: append([]feishu.PreviewSupplement(nil), s.supplements...),
	}, s.err
}

type lifecycleGateway struct {
	startedCh chan struct{}
	stoppedCh chan struct{}

	mu         sync.Mutex
	operations []feishu.Operation
}

func newLifecycleGateway() *lifecycleGateway {
	return &lifecycleGateway{
		startedCh: make(chan struct{}, 1),
		stoppedCh: make(chan struct{}, 1),
	}
}

func (g *lifecycleGateway) Start(ctx context.Context, _ feishu.ActionHandler) error {
	select {
	case g.startedCh <- struct{}{}:
	default:
	}
	<-ctx.Done()
	select {
	case g.stoppedCh <- struct{}{}:
	default:
	}
	return nil
}

func (g *lifecycleGateway) Apply(_ context.Context, operations []feishu.Operation) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.operations = append(g.operations, operations...)
	return nil
}

func (g *lifecycleGateway) snapshotOperations() []feishu.Operation {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]feishu.Operation(nil), g.operations...)
}

type timeoutThenRecordGateway struct {
	mu         sync.Mutex
	calls      int
	ctxErrs    []error
	operations []feishu.Operation
}

func (g *timeoutThenRecordGateway) Start(context.Context, feishu.ActionHandler) error { return nil }

func (g *timeoutThenRecordGateway) Apply(ctx context.Context, operations []feishu.Operation) error {
	g.mu.Lock()
	call := g.calls
	g.calls++
	g.mu.Unlock()

	if call == 0 {
		<-ctx.Done()
		g.mu.Lock()
		g.ctxErrs = append(g.ctxErrs, ctx.Err())
		g.mu.Unlock()
		return ctx.Err()
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	g.ctxErrs = append(g.ctxErrs, ctx.Err())
	g.operations = append(g.operations, operations...)
	return nil
}

func (g *timeoutThenRecordGateway) snapshot() ([]error, []feishu.Operation) {
	g.mu.Lock()
	defer g.mu.Unlock()
	errs := append([]error(nil), g.ctxErrs...)
	ops := append([]feishu.Operation(nil), g.operations...)
	return errs, ops
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
		case operation.Kind == feishu.OperationSendCard && operation.CardTitle == "在线 VS Code 实例":
			hasListCard = true
		case operation.Kind == feishu.OperationAddReaction && operation.MessageID == "msg-1":
			hasTyping = true
		case operation.Kind == feishu.OperationSendCard && strings.HasPrefix(operation.CardTitle, "最后答复"):
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

func TestDaemonRunGracefulShutdownDeliversFinalNoticeBeforeGatewayStops(t *testing.T) {
	gateway := newLifecycleGateway()
	app := New("127.0.0.1:0", "127.0.0.1:0", gateway, agentproto.ServerIdentity{})
	app.service.MaterializeSurface("surface-1", "", "chat-1", "user-1")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- app.Run(ctx)
	}()

	select {
	case <-gateway.startedCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for gateway start")
	}

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for daemon shutdown")
	}

	select {
	case <-gateway.stoppedCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for gateway stop")
	}

	operations := gateway.snapshotOperations()
	if len(operations) != 1 {
		t.Fatalf("expected one final notice operation, got %#v", operations)
	}
	if operations[0].Kind != feishu.OperationSendCard || operations[0].CardTitle != "系统提示" || operations[0].CardBody != daemonShutdownNoticeText {
		t.Fatalf("unexpected final notice operation: %#v", operations[0])
	}
	if operations[0].ReceiveID != "chat-1" || operations[0].ReceiveIDType != "chat_id" {
		t.Fatalf("unexpected final notice target: %#v", operations[0])
	}
}

func TestDaemonShutdownContinuesFinalNoticeFanoutAfterTimeout(t *testing.T) {
	gateway := &timeoutThenRecordGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	app.shutdownGracePeriod = 80 * time.Millisecond
	app.shutdownNoticeTimeout = 25 * time.Millisecond
	app.service.MaterializeSurface("surface-1", "", "chat-1", "user-1")
	app.service.MaterializeSurface("surface-2", "", "chat-2", "user-2")

	start := time.Now()
	if err := app.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown returned error: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed > 250*time.Millisecond {
		t.Fatalf("expected bounded shutdown notice fanout, elapsed=%s", elapsed)
	}

	errs, operations := gateway.snapshot()
	if len(errs) != 2 || !errors.Is(errs[0], context.DeadlineExceeded) || errs[1] != nil {
		t.Fatalf("unexpected shutdown notice errors: %#v", errs)
	}
	if len(operations) != 1 {
		t.Fatalf("expected second surface notice to still be delivered, got %#v", operations)
	}
	if operations[0].ReceiveID != "chat-2" || operations[0].CardBody != daemonShutdownNoticeText {
		t.Fatalf("unexpected shutdown notice delivery: %#v", operations[0])
	}
}

func TestDaemonIgnoresActionsAfterShutdownStarts(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	app.service.MaterializeSurface("surface-1", "", "chat-1", "user-1")

	if err := app.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown returned error: %v", err)
	}
	before := len(gateway.operations)

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	if len(gateway.operations) != before {
		t.Fatalf("expected shutdown gate to suppress new actions, got %#v", gateway.operations[before:])
	}
}

func TestDaemonRewritesFinalAssistantLinksViaMarkdownPreviewer(t *testing.T) {
	gateway := &recordingGateway{}
	previewer := &stubMarkdownPreviewer{text: "查看 [设计文档](https://preview/file-1)"}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	app.SetFinalBlockPreviewer(previewer)

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
		if operation.Kind == feishu.OperationSendCard && strings.HasPrefix(operation.CardTitle, "最后答复") {
			finalBody = operation.CardBody
		}
	}
	if finalBody != "查看 [设计文档](https://preview/file-1)" {
		t.Fatalf("expected rewritten final reply body, got %#v", gateway.operations)
	}
}

func TestDaemonProjectsPreviewSupplementsAfterFinalReply(t *testing.T) {
	gateway := &recordingGateway{}
	previewer := &stubMarkdownPreviewer{
		text: "查看 [设计文档](https://preview/file-1)",
		supplements: []feishu.PreviewSupplement{{
			Kind: "card",
			Data: map[string]any{
				"title": "补充预览",
				"body":  "这里预留后续的下载按钮或内嵌内容。",
			},
		}},
	}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	app.SetFinalBlockPreviewer(previewer)

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

	var (
		finalBody      string
		supplementBody string
	)
	for _, operation := range gateway.operations {
		if operation.Kind != feishu.OperationSendCard {
			continue
		}
		switch {
		case strings.HasPrefix(operation.CardTitle, "最后答复"):
			finalBody = operation.CardBody
		case operation.CardTitle == "补充预览":
			supplementBody = operation.CardBody
		}
	}
	if finalBody != "查看 [设计文档](https://preview/file-1)" {
		t.Fatalf("expected rewritten final reply body, got %#v", gateway.operations)
	}
	if supplementBody != "这里预留后续的下载按钮或内嵌内容。" {
		t.Fatalf("expected preview supplement card to be projected, got %#v", gateway.operations)
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
	if got.Kind != feishu.OperationSendCard || got.CardTitle != "在线 VS Code 实例" {
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

func TestDaemonAcceptedSteerRemovesQueueReactionAndAddsThumbsUp(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})

	var commands []agentproto.Command
	app.sendAgentCommand = func(instanceID string, command agentproto.Command) error {
		if instanceID != "inst-1" {
			t.Fatalf("unexpected command target: %s", instanceID)
		}
		commands = append(commands, command)
		return nil
	}

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
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "msg-active",
		Text:             "先开始",
	})
	app.onEvents(context.Background(), "inst-1", []agentproto.Event{{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	}})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionImageMessage,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "msg-img",
		LocalPath:        "/tmp/queued.png",
		MIMEType:         "image/png",
	})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "msg-queued",
		Text:             "补充信息",
	})

	beforeAck := len(gateway.operations)
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionReactionCreated,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		TargetMessageID:  "msg-queued",
		ReactionType:     "ThumbsUp",
	})
	if len(commands) < 2 {
		t.Fatalf("expected steer command to be dispatched, got %#v", commands)
	}
	steer := commands[len(commands)-1]
	if steer.Kind != agentproto.CommandTurnSteer {
		t.Fatalf("expected last command to be turn.steer, got %#v", steer)
	}

	app.onCommandAck(context.Background(), "inst-1", agentproto.CommandAck{
		CommandID: steer.CommandID,
		Accepted:  true,
	})
	ops := gateway.operations[beforeAck:]
	if len(ops) != 4 {
		t.Fatalf("expected queue-off + thumbs-up for text and image sources, got %#v", ops)
	}
	want := map[string]map[string]bool{
		"msg-queued": {"remove:OneSecond": false, "add:THUMBSUP": false},
		"msg-img":    {"remove:OneSecond": false, "add:THUMBSUP": false},
	}
	for _, op := range ops {
		switch op.Kind {
		case feishu.OperationRemoveReaction:
			if op.EmojiType == "OneSecond" {
				want[op.MessageID]["remove:OneSecond"] = true
			}
		case feishu.OperationAddReaction:
			if op.EmojiType == "THUMBSUP" {
				want[op.MessageID]["add:THUMBSUP"] = true
			}
		}
	}
	for messageID, checks := range want {
		for label, ok := range checks {
			if !ok {
				t.Fatalf("expected %s for %s, got %#v", label, messageID, ops)
			}
		}
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
	if !strings.Contains(gateway.operations[0].CardTitle, "链路错误") || !strings.Contains(gateway.operations[0].CardBody, "位置：<text_tag color='neutral'>gateway_apply</text_tag>") {
		t.Fatalf("expected queued gateway failure notice first, got %#v", gateway.operations[0])
	}
	if gateway.operations[1].CardTitle == "" || !strings.Contains(gateway.operations[1].CardBody, "当前没有在线 VS Code 实例") {
		t.Fatalf("expected current response card after queued notice, got %#v", gateway.operations[1])
	}
}

func TestDaemonRemovedNewInstanceCommandShowsMigrationNotice(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	started := false
	app.startHeadless = func(opts relayruntime.HeadlessLaunchOptions) (int, error) {
		started = true
		return 0, nil
	}

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionRemovedCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/newinstance",
	})

	snapshot := app.service.SurfaceSnapshot("surface-1")
	if started {
		t.Fatal("expected removed command not to start headless")
	}
	if snapshot == nil || snapshot.PendingHeadless.InstanceID != "" {
		t.Fatalf("expected no pending headless snapshot, got %#v", snapshot)
	}
	if len(gateway.operations) != 1 {
		t.Fatalf("expected one migration notice operation, got %#v", gateway.operations)
	}
	if gateway.operations[0].Kind != feishu.OperationSendCard || !strings.Contains(gateway.operations[0].CardBody, "/use") {
		t.Fatalf("expected migration notice card, got %#v", gateway.operations[0])
	}
}

func TestDaemonRejectsOldStopMenuBeforeHandling(t *testing.T) {
	gateway := &recordingGateway{}
	startedAt := time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{PID: 42, StartedAt: startedAt})

	seedAttachedSurfaceForInboundTests(app)
	inst := app.service.Instance("inst-1")
	inst.ActiveThreadID = "thread-1"
	inst.ActiveTurnID = "turn-1"

	var sent []agentproto.Command
	app.sendAgentCommand = func(_ string, command agentproto.Command) error {
		sent = append(sent, command)
		return nil
	}

	before := len(gateway.operations)
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionStop,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Inbound: &control.ActionInboundMeta{
			MenuClickTime: startedAt.Add(-3 * time.Minute),
		},
	})

	if len(sent) != 0 {
		t.Fatalf("expected old stop not to send interrupt command, got %#v", sent)
	}
	if inst.ActiveTurnID != "turn-1" {
		t.Fatalf("expected old stop not to mutate active turn, got %#v", inst)
	}
	delta := gateway.operations[before:]
	assertSingleRejectedNotice(t, delta, "旧动作已忽略", "重新发送消息、命令或重新点击菜单")
	if !strings.Contains(delta[0].CardBody, "/stop") {
		t.Fatalf("expected old stop notice to mention /stop, got %#v", delta)
	}
}

func TestDaemonRejectsOldTextDetachCommandAndKeepsAttachment(t *testing.T) {
	gateway := &recordingGateway{}
	startedAt := time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{PID: 42, StartedAt: startedAt})

	seedAttachedSurfaceForInboundTests(app)

	before := len(gateway.operations)
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionDetach,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/detach",
		Inbound: &control.ActionInboundMeta{
			MessageCreateTime: startedAt.Add(-3 * time.Minute),
		},
	})

	snapshot := app.service.SurfaceSnapshot("feishu:chat:1")
	if snapshot == nil || snapshot.Attachment.InstanceID != "inst-1" {
		t.Fatalf("expected old detach command not to detach surface, got %#v", snapshot)
	}
	delta := gateway.operations[before:]
	assertSingleRejectedNotice(t, delta, "旧动作已忽略", "重新发送消息、命令或重新点击菜单")
	if !strings.Contains(delta[0].CardBody, "/detach") {
		t.Fatalf("expected old detach notice to mention /detach, got %#v", delta)
	}
}

func TestDaemonRejectsOldTextMessageBeforeQueueing(t *testing.T) {
	gateway := &recordingGateway{}
	startedAt := time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{PID: 42, StartedAt: startedAt})

	seedSelectedThreadSurfaceForInboundTests(app)

	var sent []agentproto.Command
	app.sendAgentCommand = func(_ string, command agentproto.Command) error {
		sent = append(sent, command)
		return nil
	}

	before := len(gateway.operations)
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "msg-old",
		Text:             "这是一条重启前的旧消息",
		Inbound: &control.ActionInboundMeta{
			MessageCreateTime: startedAt.Add(-3 * time.Minute),
		},
	})

	if len(sent) != 0 {
		t.Fatalf("expected old text message not to dispatch commands, got %#v", sent)
	}
	snapshot := app.service.SurfaceSnapshot("feishu:chat:1")
	if snapshot == nil || snapshot.Dispatch.QueuedCount != 0 || snapshot.Dispatch.ActiveItemStatus != "" {
		t.Fatalf("expected old text message not to queue input, got %#v", snapshot)
	}
	delta := gateway.operations[before:]
	assertSingleRejectedNotice(t, delta, "旧动作已忽略", "重新发送消息、命令或重新点击菜单")
	if delta[0].Kind != feishu.OperationSendCard {
		t.Fatalf("expected old text rejection to send only notice card, got %#v", delta)
	}
	if !strings.Contains(delta[0].CardBody, "这是一条重启前的旧消息") {
		t.Fatalf("expected old text rejection to mention message preview, got %#v", delta)
	}
}

func TestDaemonRejectsOldCardDetachAndShowsExpiredNotice(t *testing.T) {
	gateway := &recordingGateway{}
	startedAt := time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{PID: 42, StartedAt: startedAt})

	seedAttachedSurfaceForInboundTests(app)

	before := len(gateway.operations)
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionDetach,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Inbound: &control.ActionInboundMeta{
			CardDaemonLifecycleID: "older-life",
		},
	})

	snapshot := app.service.SurfaceSnapshot("feishu:chat:1")
	if snapshot == nil || snapshot.Attachment.InstanceID != "inst-1" {
		t.Fatalf("expected old card callback not to detach surface, got %#v", snapshot)
	}
	delta := gateway.operations[before:]
	assertSingleRejectedNotice(t, delta, "旧卡片已过期", "重新发送对应命令获取新卡片")
	if !strings.Contains(delta[0].CardBody, "/detach") {
		t.Fatalf("expected expired card notice to mention /detach, got %#v", delta)
	}
}

func seedAttachedSurfaceForInboundTests(app *App) {
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
}

func seedSelectedThreadSurfaceForInboundTests(app *App) {
	seedAttachedSurfaceForInboundTests(app)
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		ThreadID:         "thread-1",
	})
}

func assertSingleRejectedNotice(t *testing.T, ops []feishu.Operation, title, bodySubstring string) {
	t.Helper()
	if len(ops) != 1 {
		t.Fatalf("expected one rejection notice operation, got %#v", ops)
	}
	if ops[0].Kind != feishu.OperationSendCard || ops[0].CardTitle != title || !strings.Contains(ops[0].CardBody, bodySubstring) {
		t.Fatalf("unexpected rejection notice operation: %#v", ops[0])
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
	app.sendAgentCommand = func(string, agentproto.Command) error { return nil }
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{IdleTTL: time.Minute, KillGrace: time.Second, MinIdle: 0})

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

	base := time.Now().UTC()
	app.onTick(context.Background(), base)
	app.onTick(context.Background(), base.Add(2*time.Minute))

	if stoppedPID != 2468 {
		t.Fatalf("expected idle managed headless pid to stop, got %d", stoppedPID)
	}
	if app.service.Instance("inst-headless-2") != nil {
		t.Fatalf("expected idle managed headless instance to be removed, got %#v", app.service.Instance("inst-headless-2"))
	}
}

func TestDaemonIdleManagedHeadlessRefreshesOnInterval(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		IdleTTL:             time.Hour,
		KillGrace:           time.Second,
		IdleRefreshInterval: 5 * time.Minute,
		IdleRefreshTimeout:  time.Minute,
	})

	var commands []agentproto.Command
	app.sendAgentCommand = func(instanceID string, command agentproto.Command) error {
		if instanceID != "inst-headless-2" {
			t.Fatalf("unexpected command target: %s", instanceID)
		}
		commands = append(commands, command)
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
	if len(commands) != 1 || commands[0].Kind != agentproto.CommandThreadsRefresh {
		t.Fatalf("expected initial hello refresh, got %#v", commands)
	}

	app.onEvents(context.Background(), "inst-headless-2", []agentproto.Event{{
		Kind: agentproto.EventThreadsSnapshot,
	}})
	base := time.Now().UTC()
	app.managedHeadless["inst-headless-2"].LastRefreshCompletedAt = base
	app.managedHeadless["inst-headless-2"].RefreshInFlight = false
	app.managedHeadless["inst-headless-2"].RefreshCommandID = ""

	app.onTick(context.Background(), base.Add(2*time.Minute))
	if len(commands) != 1 {
		t.Fatalf("expected no idle refresh before interval, got %#v", commands)
	}

	app.onTick(context.Background(), base.Add(6*time.Minute))
	if len(commands) != 2 || commands[1].Kind != agentproto.CommandThreadsRefresh {
		t.Fatalf("expected scheduled idle refresh after interval, got %#v", commands)
	}
	if managed := app.managedHeadless["inst-headless-2"]; managed == nil || managed.Status != managedHeadlessStatusIdle || !managed.RefreshInFlight {
		t.Fatalf("expected idle managed headless to track in-flight refresh, got %#v", managed)
	}
}

func TestDaemonShutdownStopsManagedHeadlessAndRemovesRuntimeState(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{KillGrace: time.Second})

	var stopped []int
	app.stopProcess = func(pid int, _ time.Duration) error {
		stopped = append(stopped, pid)
		return nil
	}

	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-headless-1",
		DisplayName:   "headless",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		Source:        "headless",
		Managed:       true,
		PID:           4321,
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	app.managedHeadless["inst-headless-1"] = &managedHeadlessProcess{
		InstanceID:    "inst-headless-1",
		PID:           4321,
		WorkspaceRoot: "/data/dl/droid",
		DisplayName:   "headless",
		Status:        managedHeadlessStatusBusy,
	}

	if err := app.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if len(stopped) != 1 || stopped[0] != 4321 {
		t.Fatalf("expected managed headless pid 4321 to stop, got %#v", stopped)
	}
	if len(app.managedHeadless) != 0 {
		t.Fatalf("expected managed headless map cleared, got %#v", app.managedHeadless)
	}
	if app.service.Instance("inst-headless-1") != nil {
		t.Fatalf("expected managed headless instance removed from service, got %#v", app.service.Instance("inst-headless-1"))
	}
}

func TestDaemonShutdownContinuesManagedHeadlessCleanupAfterStopError(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{KillGrace: time.Second})

	var stopped []int
	app.stopProcess = func(pid int, _ time.Duration) error {
		stopped = append(stopped, pid)
		if pid == 1111 {
			return errors.New("terminate failed")
		}
		return nil
	}

	for _, inst := range []*state.InstanceRecord{
		{
			InstanceID:    "inst-headless-1",
			DisplayName:   "headless-1",
			WorkspaceRoot: "/data/dl/droid",
			WorkspaceKey:  "/data/dl/droid",
			Source:        "headless",
			Managed:       true,
			PID:           1111,
			Threads:       map[string]*state.ThreadRecord{},
		},
		{
			InstanceID:    "inst-headless-2",
			DisplayName:   "headless-2",
			WorkspaceRoot: "/data/dl/droid",
			WorkspaceKey:  "/data/dl/droid",
			Source:        "headless",
			Managed:       true,
			PID:           2222,
			Threads:       map[string]*state.ThreadRecord{},
		},
	} {
		app.service.UpsertInstance(inst)
	}
	app.managedHeadless["inst-headless-1"] = &managedHeadlessProcess{InstanceID: "inst-headless-1", PID: 1111}
	app.managedHeadless["inst-headless-2"] = &managedHeadlessProcess{InstanceID: "inst-headless-2", PID: 2222}

	err := app.Shutdown(context.Background())
	if err == nil || !strings.Contains(err.Error(), "inst-headless-1") {
		t.Fatalf("expected shutdown cleanup error for first managed headless, got %v", err)
	}
	if len(stopped) != 2 {
		t.Fatalf("expected both managed headless processes to be attempted, got %#v", stopped)
	}
	if len(app.managedHeadless) != 0 {
		t.Fatalf("expected managed headless map cleared after cleanup, got %#v", app.managedHeadless)
	}
	if app.service.Instance("inst-headless-1") != nil || app.service.Instance("inst-headless-2") != nil {
		t.Fatalf("expected managed headless service state removed after cleanup, got %#v %#v", app.service.Instance("inst-headless-1"), app.service.Instance("inst-headless-2"))
	}
}

func TestDaemonPrewarmsManagedHeadlessToMinIdle(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	stateDir := t.TempDir()
	var launches []relayruntime.HeadlessLaunchOptions
	app.startHeadless = func(opts relayruntime.HeadlessLaunchOptions) (int, error) {
		launches = append(launches, opts)
		return 5000 + len(launches), nil
	}
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		BinaryPath: "/tmp/codex-remote",
		ConfigPath: "/tmp/config.json",
		Paths: relayruntime.Paths{
			StateDir: stateDir,
			LogsDir:  t.TempDir(),
		},
		MinIdle: 1,
	})

	now := time.Now().UTC()
	app.onTick(context.Background(), now)
	if len(launches) != 1 {
		t.Fatalf("expected one prewarm launch, got %#v", launches)
	}
	if launches[0].WorkDir != stateDir {
		t.Fatalf("expected prewarm workdir to use state dir, got %#v", launches[0])
	}
	if !containsEnvEntry(launches[0].Env, "CODEX_REMOTE_INSTANCE_SOURCE=headless") || !containsEnvEntry(launches[0].Env, "CODEX_REMOTE_INSTANCE_MANAGED=1") {
		t.Fatalf("expected managed headless prewarm env, got %#v", launches[0].Env)
	}
	if len(app.managedHeadless) != 1 {
		t.Fatalf("expected one managed headless record, got %#v", app.managedHeadless)
	}
	for _, managed := range app.managedHeadless {
		if managed.Status != managedHeadlessStatusStarting || managed.WorkspaceRoot != stateDir || managed.DisplayName != "headless" {
			t.Fatalf("unexpected prewarmed managed headless record: %#v", managed)
		}
	}

	app.onTick(context.Background(), now.Add(10*time.Second))
	if len(launches) != 1 {
		t.Fatalf("expected fresh starting instance to count toward min-idle, got %#v", launches)
	}
}

func TestDaemonPrewarmsReplacementWhenOfflineManagedHeadlessDoesNotCount(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	stateDir := t.TempDir()
	app.sendAgentCommand = func(string, agentproto.Command) error { return nil }
	var launches []relayruntime.HeadlessLaunchOptions
	app.startHeadless = func(opts relayruntime.HeadlessLaunchOptions) (int, error) {
		launches = append(launches, opts)
		return 6000 + len(launches), nil
	}
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		BinaryPath: "/tmp/codex-remote",
		ConfigPath: "/tmp/config.json",
		Paths: relayruntime.Paths{
			StateDir: stateDir,
			LogsDir:  t.TempDir(),
		},
		StartTTL: time.Minute,
		MinIdle:  1,
	})

	app.onHello(context.Background(), agentproto.Hello{
		Instance: agentproto.InstanceHello{
			InstanceID:    "inst-headless-old",
			DisplayName:   "old",
			WorkspaceRoot: stateDir,
			WorkspaceKey:  stateDir,
			ShortName:     "old",
			Source:        "headless",
			Managed:       true,
			PID:           2468,
		},
	})
	app.onDisconnect(context.Background(), "inst-headless-old")

	app.onTick(context.Background(), time.Now().UTC())
	if len(launches) != 1 {
		t.Fatalf("expected offline managed headless to trigger replacement prewarm, got %#v", launches)
	}
	if len(app.managedHeadless) != 2 {
		t.Fatalf("expected offline member to remain visible alongside replacement, got %#v", app.managedHeadless)
	}
	if app.managedHeadless["inst-headless-old"] == nil || app.managedHeadless["inst-headless-old"].Status != managedHeadlessStatusOffline {
		t.Fatalf("expected original member to stay offline, got %#v", app.managedHeadless["inst-headless-old"])
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
