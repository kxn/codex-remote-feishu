package daemon

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/render"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type reentrantAppLockPreviewer struct {
	app *App

	mu       sync.Mutex
	requests []feishu.FinalBlockPreviewRequest
}

type reentrantAppLockGateway struct {
	app *App

	mu         sync.Mutex
	operations []feishu.Operation
	err        error
}

func (s *reentrantAppLockPreviewer) RewriteFinalBlock(_ context.Context, req feishu.FinalBlockPreviewRequest) (feishu.FinalBlockPreviewResult, error) {
	s.mu.Lock()
	s.requests = append(s.requests, req)
	s.mu.Unlock()

	locked := make(chan struct{})
	go func() {
		s.app.mu.Lock()
		s.app.mu.Unlock()
		close(locked)
	}()

	select {
	case <-locked:
		return feishu.FinalBlockPreviewResult{Block: req.Block}, nil
	case <-time.After(250 * time.Millisecond):
		return feishu.FinalBlockPreviewResult{Block: req.Block}, errors.New("previewer could not reacquire app lock")
	}
}

func (s *reentrantAppLockPreviewer) snapshot() []feishu.FinalBlockPreviewRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]feishu.FinalBlockPreviewRequest(nil), s.requests...)
}

func (g *reentrantAppLockGateway) Start(context.Context, feishu.ActionHandler) error { return nil }

func (g *reentrantAppLockGateway) Apply(_ context.Context, operations []feishu.Operation) error {
	g.mu.Lock()
	g.operations = append(g.operations, operations...)
	g.mu.Unlock()

	locked := make(chan struct{})
	go func() {
		g.app.mu.Lock()
		g.app.mu.Unlock()
		close(locked)
	}()

	select {
	case <-locked:
		return nil
	case <-time.After(250 * time.Millisecond):
		err := errors.New("gateway could not reacquire app lock")
		g.mu.Lock()
		g.err = err
		g.mu.Unlock()
		return err
	}
}

func (g *reentrantAppLockGateway) snapshot() ([]feishu.Operation, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]feishu.Operation(nil), g.operations...), g.err
}

func TestHandleUIEventsReleasesAppLockDuringFinalPreviewRewrite(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	previewer := &reentrantAppLockPreviewer{app: app}
	app.SetFinalBlockPreviewer(previewer)

	app.service.MaterializeSurface("feishu:chat:1", "app-1", "chat-1", "ou_user")
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

	done := make(chan struct{})
	go func() {
		app.mu.Lock()
		defer app.mu.Unlock()
		app.handleUIEventsLocked(context.Background(), []control.UIEvent{{
			Kind:             control.UIEventBlockCommitted,
			SurfaceSessionID: "feishu:chat:1",
			SourceMessageID:  "msg-1",
			Block: &render.Block{
				Kind:       render.BlockAssistantMarkdown,
				InstanceID: "inst-1",
				ThreadID:   "thread-1",
				TurnID:     "turn-1",
				ItemID:     "item-1",
				Text:       "最终结果",
				Final:      true,
			},
		}})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handleUIEvents timed out while previewer reentered app lock")
	}

	if requests := previewer.snapshot(); len(requests) != 1 {
		t.Fatalf("expected one preview rewrite request, got %#v", requests)
	}
	if len(gateway.operations) == 0 {
		t.Fatalf("expected final reply to be delivered after preview rewrite")
	}
}

func TestHandleUIEventsReleasesAppLockDuringGatewayApply(t *testing.T) {
	gateway := &reentrantAppLockGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	gateway.app = app
	app.service.MaterializeSurface("feishu:chat:1", "app-1", "chat-1", "ou_user")

	done := make(chan struct{})
	go func() {
		app.mu.Lock()
		defer app.mu.Unlock()
		app.handleUIEventsLocked(context.Background(), []control.UIEvent{{
			Kind:             control.UIEventNotice,
			SurfaceSessionID: "feishu:chat:1",
			Notice: &control.Notice{
				Code:  "gateway_notice",
				Title: "Gateway",
				Text:  "test",
			},
		}})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handleUIEvents timed out while gateway apply reentered app lock")
	}

	operations, err := gateway.snapshot()
	if err != nil {
		t.Fatalf("gateway apply should have completed without app-lock recursion failure: %v", err)
	}
	if len(operations) == 0 {
		t.Fatalf("expected notice delivery operations, got %#v", operations)
	}
}

func TestHandleUIEventsReleasesAppLockDuringRelaySend(t *testing.T) {
	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		WorkspaceRoot: "/tmp/workspace",
		WorkspaceKey:  "/tmp/workspace",
		Source:        "headless",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		GatewayID:        "app-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})

	var sendErr error
	app.sendAgentCommand = func(string, agentproto.Command) error {
		locked := make(chan struct{})
		go func() {
			app.mu.Lock()
			app.mu.Unlock()
			close(locked)
		}()

		select {
		case <-locked:
			return nil
		case <-time.After(250 * time.Millisecond):
			sendErr = errors.New("relay send could not reacquire app lock")
			return sendErr
		}
	}

	done := make(chan struct{})
	go func() {
		app.mu.Lock()
		defer app.mu.Unlock()
		app.handleUIEventsLocked(context.Background(), []control.UIEvent{{
			Kind:             control.UIEventAgentCommand,
			SurfaceSessionID: "surface-1",
			Command: &agentproto.Command{
				Kind: agentproto.CommandPromptSend,
			},
		}})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handleUIEvents timed out while relay send reentered app lock")
	}
	if sendErr != nil {
		t.Fatalf("relay send should have completed without app-lock recursion failure: %v", sendErr)
	}
}
