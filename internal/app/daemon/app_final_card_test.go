package daemon

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/render"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type messageIDAssigningGateway struct {
	mu         sync.Mutex
	next       int
	operations []feishu.Operation
	notify     chan struct{}
}

func (g *messageIDAssigningGateway) Start(context.Context, feishu.ActionHandler) error { return nil }

func (g *messageIDAssigningGateway) Apply(_ context.Context, operations []feishu.Operation) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	for i := range operations {
		if operations[i].Kind != feishu.OperationSendCard || operations[i].MessageID != "" {
			continue
		}
		g.next++
		operations[i].MessageID = fmt.Sprintf("om-card-%d", g.next)
	}
	g.operations = append(g.operations, operations...)
	if g.notify != nil {
		select {
		case g.notify <- struct{}{}:
		default:
		}
	}
	return nil
}

func (g *messageIDAssigningGateway) snapshotOperations() []feishu.Operation {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]feishu.Operation(nil), g.operations...)
}

func (g *messageIDAssigningGateway) waitForOperationCount(n int, timeout time.Duration) []feishu.Operation {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ops := g.snapshotOperations()
		if len(ops) >= n {
			return ops
		}
		if g.notify == nil {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		select {
		case <-g.notify:
		case <-time.After(10 * time.Millisecond):
		}
	}
	return g.snapshotOperations()
}

type secondChancePreviewer struct {
	mu          sync.Mutex
	calls       int
	secondText  string
	secondErr   error
	secondGate  chan struct{}
	secondStart chan struct{}
	secondDone  chan struct{}
}

func (s *secondChancePreviewer) RewriteFinalBlock(ctx context.Context, req feishu.FinalBlockPreviewRequest) (feishu.FinalBlockPreviewResult, error) {
	s.mu.Lock()
	s.calls++
	call := s.calls
	secondGate := s.secondGate
	secondStart := s.secondStart
	secondDone := s.secondDone
	secondText := s.secondText
	secondErr := s.secondErr
	s.mu.Unlock()

	if call == 1 {
		<-ctx.Done()
		return feishu.FinalBlockPreviewResult{Block: req.Block}, ctx.Err()
	}
	if secondStart != nil {
		select {
		case secondStart <- struct{}{}:
		default:
		}
	}
	if secondGate != nil {
		select {
		case <-secondGate:
		case <-ctx.Done():
			if secondDone != nil {
				close(secondDone)
			}
			return feishu.FinalBlockPreviewResult{Block: req.Block}, ctx.Err()
		}
	}
	block := req.Block
	if secondText != "" {
		block.Text = secondText
	}
	if secondDone != nil {
		close(secondDone)
	}
	return feishu.FinalBlockPreviewResult{Block: block}, secondErr
}

func materializeAttachedSurfaceForFinalCardTest(app *App, surfaceID, gatewayID, chatID, actorUserID, instanceID, workspaceKey string) {
	app.service.MaterializeSurface(surfaceID, gatewayID, chatID, actorUserID)
	for _, surface := range app.service.Surfaces() {
		if surface == nil || surface.SurfaceSessionID != surfaceID {
			continue
		}
		surface.AttachedInstanceID = instanceID
		surface.ClaimedWorkspaceKey = workspaceKey
		return
	}
}

func TestDeliverUIEventRecordsFinalCardAnchorFromPrimaryFinalReply(t *testing.T) {
	gateway := &messageIDAssigningGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	app.SetFinalBlockPreviewer(&stubMarkdownPreviewer{
		supplements: []feishu.PreviewSupplement{{
			Kind: "card",
			Data: map[string]any{
				"title": "补充信息",
				"body":  "preview link",
			},
		}},
	})

	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {
				ThreadID: "thread-1",
				CWD:      "/data/dl/droid",
				Loaded:   true,
			},
		},
	})
	materializeAttachedSurfaceForFinalCardTest(app, "feishu:chat:1", "app-1", "chat-1", "ou_user", "inst-1", "/data/dl/droid")

	event := control.UIEvent{
		Kind:             control.UIEventBlockCommitted,
		SurfaceSessionID: "feishu:chat:1",
		SourceMessageID:  "msg-1",
		Block: &render.Block{
			Kind:       render.BlockAssistantMarkdown,
			InstanceID: "inst-1",
			ThreadID:   "thread-1",
			TurnID:     "turn-1",
			ItemID:     "item-1",
			Text:       "已经处理完成。",
			Final:      true,
		},
	}
	if err := app.deliverUIEventWithContext(context.Background(), event); err != nil {
		t.Fatalf("deliver final block: %v", err)
	}

	got := app.service.LookupFinalCardForBlock("feishu:chat:1", *event.Block, app.daemonLifecycleID)
	if got == nil {
		t.Fatal("expected retained final card anchor")
	}
	if got.MessageID != "om-card-1" || got.SourceMessageID != "msg-1" {
		t.Fatalf("unexpected final card anchor: %#v", got)
	}
	ops := gateway.snapshotOperations()
	if len(ops) != 2 {
		t.Fatalf("expected final card plus supplement send ops, got %#v", ops)
	}
	if ops[0].MessageID != "om-card-1" || ops[1].MessageID != "om-card-2" {
		t.Fatalf("unexpected sent message ids: %#v", ops)
	}
}

func TestDeliverUIEventSecondChanceFinalPatchUpdatesSameCardAfterPreviewTimeout(t *testing.T) {
	gateway := &messageIDAssigningGateway{notify: make(chan struct{}, 8)}
	previewer := &secondChancePreviewer{
		secondText: "查看 [设计文档](https://preview/file-1)",
		secondDone: make(chan struct{}),
	}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	app.SetFinalBlockPreviewer(previewer)
	app.finalPreviewTimeout = 10 * time.Millisecond
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {
				ThreadID: "thread-1",
				CWD:      "/data/dl/droid",
				Loaded:   true,
			},
		},
	})
	materializeAttachedSurfaceForFinalCardTest(app, "feishu:chat:1", "app-1", "chat-1", "ou_user", "inst-1", "/data/dl/droid")

	event := control.UIEvent{
		Kind:             control.UIEventBlockCommitted,
		SurfaceSessionID: "feishu:chat:1",
		SourceMessageID:  "msg-1",
		Block: &render.Block{
			Kind:       render.BlockAssistantMarkdown,
			InstanceID: "inst-1",
			ThreadID:   "thread-1",
			TurnID:     "turn-1",
			ItemID:     "item-1",
			Text:       "查看 [设计文档](/data/dl/droid/docs/design.md)",
			Final:      true,
		},
	}
	if err := app.deliverUIEventWithContext(context.Background(), event); err != nil {
		t.Fatalf("deliver final block: %v", err)
	}
	ops := gateway.waitForOperationCount(2, 2*time.Second)
	if len(ops) != 2 {
		t.Fatalf("expected final send plus async patch, got %#v", ops)
	}
	if ops[0].Kind != feishu.OperationSendCard || ops[0].MessageID != "om-card-1" {
		t.Fatalf("expected initial final send on first card, got %#v", ops)
	}
	if !strings.Contains(ops[0].CardBody, "`/data/dl/droid/docs/design.md`") {
		t.Fatalf("expected initial final body to use timeout fallback, got %#v", ops[0])
	}
	if ops[1].Kind != feishu.OperationUpdateCard || ops[1].MessageID != "om-card-1" {
		t.Fatalf("expected async patch to target same final card, got %#v", ops[1])
	}
	if ops[1].CardBody != "查看 [设计文档](https://preview/file-1)" {
		t.Fatalf("expected async patch body to use materialized preview link, got %#v", ops[1])
	}
}

func TestDeliverUIEventSecondChanceFinalPatchSkipsWhenNoImprovement(t *testing.T) {
	gateway := &messageIDAssigningGateway{notify: make(chan struct{}, 8)}
	previewer := &secondChancePreviewer{
		secondDone: make(chan struct{}),
	}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	app.SetFinalBlockPreviewer(previewer)
	app.finalPreviewTimeout = 10 * time.Millisecond
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {
				ThreadID: "thread-1",
				CWD:      "/data/dl/droid",
				Loaded:   true,
			},
		},
	})
	materializeAttachedSurfaceForFinalCardTest(app, "feishu:chat:1", "app-1", "chat-1", "ou_user", "inst-1", "/data/dl/droid")

	event := control.UIEvent{
		Kind:             control.UIEventBlockCommitted,
		SurfaceSessionID: "feishu:chat:1",
		SourceMessageID:  "msg-1",
		Block: &render.Block{
			Kind:       render.BlockAssistantMarkdown,
			InstanceID: "inst-1",
			ThreadID:   "thread-1",
			TurnID:     "turn-1",
			ItemID:     "item-1",
			Text:       "已经处理完成。",
			Final:      true,
		},
	}
	if err := app.deliverUIEventWithContext(context.Background(), event); err != nil {
		t.Fatalf("deliver final block: %v", err)
	}
	select {
	case <-previewer.secondDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for second-chance preview attempt")
	}
	ops := gateway.snapshotOperations()
	if len(ops) != 1 {
		t.Fatalf("expected no patch when second chance result is unchanged, got %#v", ops)
	}
}

func TestDeliverUIEventSecondChanceFinalPatchSkipsAfterDetach(t *testing.T) {
	gateway := &messageIDAssigningGateway{notify: make(chan struct{}, 8)}
	previewer := &secondChancePreviewer{
		secondText:  "查看 [设计文档](https://preview/file-1)",
		secondGate:  make(chan struct{}),
		secondStart: make(chan struct{}, 1),
		secondDone:  make(chan struct{}),
	}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	app.SetFinalBlockPreviewer(previewer)
	app.finalPreviewTimeout = 10 * time.Millisecond
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {
				ThreadID: "thread-1",
				CWD:      "/data/dl/droid",
				Loaded:   true,
			},
		},
	})
	materializeAttachedSurfaceForFinalCardTest(app, "feishu:chat:1", "app-1", "chat-1", "ou_user", "inst-1", "/data/dl/droid")

	event := control.UIEvent{
		Kind:             control.UIEventBlockCommitted,
		SurfaceSessionID: "feishu:chat:1",
		SourceMessageID:  "msg-1",
		Block: &render.Block{
			Kind:       render.BlockAssistantMarkdown,
			InstanceID: "inst-1",
			ThreadID:   "thread-1",
			TurnID:     "turn-1",
			ItemID:     "item-1",
			Text:       "查看 [设计文档](/data/dl/droid/docs/design.md)",
			Final:      true,
		},
	}
	if err := app.deliverUIEventWithContext(context.Background(), event); err != nil {
		t.Fatalf("deliver final block: %v", err)
	}
	select {
	case <-previewer.secondStart:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for second-chance preview attempt to start")
	}
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionDetach,
		SurfaceSessionID: "feishu:chat:1",
	})
	close(previewer.secondGate)
	select {
	case <-previewer.secondDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for gated second-chance preview attempt")
	}
	ops := gateway.snapshotOperations()
	for _, op := range ops {
		if op.Kind == feishu.OperationUpdateCard && op.MessageID == "om-card-1" {
			t.Fatalf("expected detach to suppress second-chance patch, got %#v", ops)
		}
	}
}
