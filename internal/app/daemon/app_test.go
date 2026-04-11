package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

type timeoutMarkdownPreviewer struct {
	mu       sync.Mutex
	requests []feishu.FinalBlockPreviewRequest
	ctxErr   error
}

func (s *timeoutMarkdownPreviewer) RewriteFinalBlock(ctx context.Context, req feishu.FinalBlockPreviewRequest) (feishu.FinalBlockPreviewResult, error) {
	s.mu.Lock()
	s.requests = append(s.requests, req)
	s.mu.Unlock()
	<-ctx.Done()
	s.mu.Lock()
	s.ctxErr = ctx.Err()
	s.mu.Unlock()
	return feishu.FinalBlockPreviewResult{Block: req.Block}, ctx.Err()
}

func (s *timeoutMarkdownPreviewer) snapshot() ([]feishu.FinalBlockPreviewRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]feishu.FinalBlockPreviewRequest(nil), s.requests...), s.ctxErr
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

func operationCardButtons(operation feishu.Operation) []map[string]any {
	var buttons []map[string]any
	for _, element := range operation.CardElements {
		buttons = append(buttons, cardElementButtons(element)...)
	}
	return buttons
}

func cardElementButtons(element map[string]any) []map[string]any {
	switch element["tag"] {
	case "button":
		return []map[string]any{element}
	case "column_set":
		columns, _ := element["columns"].([]map[string]any)
		buttons := make([]map[string]any, 0, len(columns))
		for _, column := range columns {
			elements, _ := column["elements"].([]map[string]any)
			if len(elements) == 0 {
				continue
			}
			buttons = append(buttons, elements[0])
		}
		return buttons
	default:
		return nil
	}
}

func cardButtonPayload(button map[string]any) map[string]any {
	if value, _ := button["value"].(map[string]any); len(value) != 0 {
		return value
	}
	behaviors, _ := button["behaviors"].([]map[string]any)
	if len(behaviors) == 0 {
		return nil
	}
	value, _ := behaviors[0]["value"].(map[string]any)
	return value
}

func operationHasActionValue(operation feishu.Operation, kind, key, want string) bool {
	for _, button := range operationCardButtons(operation) {
		value := cardButtonPayload(button)
		if len(value) == 0 || value["kind"] != kind {
			continue
		}
		if key == "" {
			return true
		}
		if value[key] == want {
			return true
		}
	}
	return false
}

func TestHandleGatewayActionReplacesMenuCardForCardNavigation(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{
		PID:       42,
		StartedAt: time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC),
	})
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")

	result := app.HandleGatewayAction(context.Background(), control.Action{
		Kind:             control.ActionShowCommandMenu,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/menu send_settings",
		Inbound: &control.ActionInboundMeta{
			CardDaemonLifecycleID: app.daemonLifecycleID,
		},
	})

	if result == nil || result.ReplaceCurrentCard == nil {
		t.Fatalf("expected inline replacement result, got %#v", result)
	}
	if len(gateway.operations) != 0 {
		t.Fatalf("expected no appended gateway operations, got %#v", gateway.operations)
	}
	if result.ReplaceCurrentCard.CardTitle != "命令菜单" {
		t.Fatalf("unexpected replacement card: %#v", result.ReplaceCurrentCard)
	}
	if !operationHasActionValue(*result.ReplaceCurrentCard, "run_command", "command_text", "/menu") {
		t.Fatalf("expected replacement submenu card to include back-to-home command, got %#v", result.ReplaceCurrentCard.CardElements)
	}
}

func TestHandleGatewayActionReplacesBareModeCardForCardNavigation(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{
		PID:       42,
		StartedAt: time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC),
	})
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")

	result := app.HandleGatewayAction(context.Background(), control.Action{
		Kind:             control.ActionModeCommand,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/mode",
		Inbound: &control.ActionInboundMeta{
			CardDaemonLifecycleID: app.daemonLifecycleID,
		},
	})

	if result == nil || result.ReplaceCurrentCard == nil {
		t.Fatalf("expected inline replacement result, got %#v", result)
	}
	if len(gateway.operations) != 0 {
		t.Fatalf("expected no appended gateway operations, got %#v", gateway.operations)
	}
	if result.ReplaceCurrentCard.CardTitle != "切换模式" {
		t.Fatalf("unexpected replacement card title: %#v", result.ReplaceCurrentCard)
	}
	if !operationHasActionValue(*result.ReplaceCurrentCard, "run_command", "command_text", "/menu maintenance") {
		t.Fatalf("expected replacement mode card to include return action, got %#v", result.ReplaceCurrentCard.CardElements)
	}
}

func TestHandleGatewayActionKeepsParameterApplyAppendOnly(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{
		PID:       42,
		StartedAt: time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC),
	})
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")

	result := app.HandleGatewayAction(context.Background(), control.Action{
		Kind:             control.ActionAutoContinueCommand,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/autocontinue on",
		Inbound: &control.ActionInboundMeta{
			CardDaemonLifecycleID: app.daemonLifecycleID,
		},
	})

	if result != nil {
		t.Fatalf("expected append-only behavior for parameter apply, got %#v", result)
	}
	if len(gateway.operations) != 1 {
		t.Fatalf("expected one appended gateway operation, got %#v", gateway.operations)
	}
	if gateway.operations[0].CardTitle != "系统提示" {
		t.Fatalf("unexpected appended card: %#v", gateway.operations[0])
	}
}

func TestHandleGatewayActionReplacesScopedThreadCardForCardNavigation(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{
		PID:       42,
		StartedAt: time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC),
	})
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "dl",
		WorkspaceRoot: "/data/dl",
		WorkspaceKey:  "/data/dl",
		ShortName:     "dl",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "会话1", CWD: "/data/dl", LastUsedAt: time.Date(2026, 4, 10, 10, 1, 0, 0, time.UTC)},
			"thread-2": {ThreadID: "thread-2", Name: "会话2", CWD: "/data/dl", LastUsedAt: time.Date(2026, 4, 10, 10, 2, 0, 0, time.UTC)},
			"thread-3": {ThreadID: "thread-3", Name: "会话3", CWD: "/data/dl", LastUsedAt: time.Date(2026, 4, 10, 10, 3, 0, 0, time.UTC)},
			"thread-4": {ThreadID: "thread-4", Name: "会话4", CWD: "/data/dl", LastUsedAt: time.Date(2026, 4, 10, 10, 4, 0, 0, time.UTC)},
			"thread-5": {ThreadID: "thread-5", Name: "会话5", CWD: "/data/dl", LastUsedAt: time.Date(2026, 4, 10, 10, 5, 0, 0, time.UTC)},
			"thread-6": {ThreadID: "thread-6", Name: "会话6", CWD: "/data/dl", LastUsedAt: time.Date(2026, 4, 10, 10, 6, 0, 0, time.UTC)},
		},
	})
	app.service.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})

	result := app.HandleGatewayAction(context.Background(), control.Action{
		Kind:             control.ActionShowScopedThreads,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Inbound: &control.ActionInboundMeta{
			CardDaemonLifecycleID: app.daemonLifecycleID,
		},
	})

	if result == nil || result.ReplaceCurrentCard == nil {
		t.Fatalf("expected inline replacement result, got %#v", result)
	}
	if len(gateway.operations) != 0 {
		t.Fatalf("expected no appended gateway operations, got %#v", gateway.operations)
	}
	if result.ReplaceCurrentCard.CardTitle != "当前工作区全部会话" {
		t.Fatalf("unexpected replacement card title: %#v", result.ReplaceCurrentCard)
	}
	if !operationHasActionValue(*result.ReplaceCurrentCard, "show_threads", "", "") {
		t.Fatalf("expected replacement scoped-all card to include return action, got %#v", result.ReplaceCurrentCard.CardElements)
	}
}

func TestHandleGatewayActionReplacesWorkspaceThreadCardForCardNavigation(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{
		PID:       42,
		StartedAt: time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC),
	})
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "proj1",
		WorkspaceRoot: "/data/dl/proj1",
		WorkspaceKey:  "/data/dl/proj1",
		ShortName:     "proj1",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "会话1", CWD: "/data/dl/proj1", LastUsedAt: time.Date(2026, 4, 10, 10, 1, 0, 0, time.UTC)},
			"thread-2": {ThreadID: "thread-2", Name: "会话2", CWD: "/data/dl/proj1", LastUsedAt: time.Date(2026, 4, 10, 10, 2, 0, 0, time.UTC)},
		},
	})

	result := app.HandleGatewayAction(context.Background(), control.Action{
		Kind:             control.ActionShowWorkspaceThreads,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		WorkspaceKey:     "/data/dl/proj1",
		Inbound: &control.ActionInboundMeta{
			CardDaemonLifecycleID: app.daemonLifecycleID,
		},
	})

	if result == nil || result.ReplaceCurrentCard == nil {
		t.Fatalf("expected inline replacement result, got %#v", result)
	}
	if len(gateway.operations) != 0 {
		t.Fatalf("expected no appended gateway operations, got %#v", gateway.operations)
	}
	if result.ReplaceCurrentCard.CardTitle != "proj1 全部会话" {
		t.Fatalf("unexpected replacement card title: %#v", result.ReplaceCurrentCard)
	}
	if !operationHasActionValue(*result.ReplaceCurrentCard, "show_all_threads", "", "") {
		t.Fatalf("expected replacement workspace card to include return action, got %#v", result.ReplaceCurrentCard.CardElements)
	}
}

func TestHandleGatewayActionReplacesExpandedWorkspaceListCardForCardNavigation(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{
		PID:       42,
		StartedAt: time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC),
	})
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	for i := 0; i < 6; i++ {
		key := fmt.Sprintf("/data/dl/proj-%d", i)
		app.service.UpsertInstance(&state.InstanceRecord{
			InstanceID:    fmt.Sprintf("inst-%d", i),
			DisplayName:   fmt.Sprintf("proj-%d", i),
			WorkspaceRoot: key,
			WorkspaceKey:  key,
			ShortName:     fmt.Sprintf("proj-%d", i),
			Online:        true,
			Threads: map[string]*state.ThreadRecord{
				fmt.Sprintf("thread-%d", i): {
					ThreadID:   fmt.Sprintf("thread-%d", i),
					Name:       fmt.Sprintf("会话-%d", i),
					CWD:        key,
					LastUsedAt: time.Date(2026, 4, 10, 10, i, 0, 0, time.UTC),
				},
			},
		})
	}

	result := app.HandleGatewayAction(context.Background(), control.Action{
		Kind:             control.ActionShowAllWorkspaces,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Inbound: &control.ActionInboundMeta{
			CardDaemonLifecycleID: app.daemonLifecycleID,
		},
	})

	if result == nil || result.ReplaceCurrentCard == nil {
		t.Fatalf("expected inline replacement result, got %#v", result)
	}
	if len(gateway.operations) != 0 {
		t.Fatalf("expected no appended gateway operations, got %#v", gateway.operations)
	}
	if result.ReplaceCurrentCard.CardTitle != "全部工作区" {
		t.Fatalf("unexpected replacement card title: %#v", result.ReplaceCurrentCard)
	}
	if !operationHasActionValue(*result.ReplaceCurrentCard, "show_recent_workspaces", "", "") {
		t.Fatalf("expected expanded workspace card to include return action, got %#v", result.ReplaceCurrentCard.CardElements)
	}
}

func TestHandleGatewayActionReplacesExpandedThreadWorkspaceCardForCardNavigation(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{
		PID:       42,
		StartedAt: time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC),
	})
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	for i := 0; i < 6; i++ {
		key := fmt.Sprintf("/data/dl/proj-%d", i)
		app.service.UpsertInstance(&state.InstanceRecord{
			InstanceID:    fmt.Sprintf("inst-%d", i),
			DisplayName:   fmt.Sprintf("proj-%d", i),
			WorkspaceRoot: key,
			WorkspaceKey:  key,
			ShortName:     fmt.Sprintf("proj-%d", i),
			Online:        true,
			Threads: map[string]*state.ThreadRecord{
				fmt.Sprintf("thread-%d", i): {
					ThreadID:   fmt.Sprintf("thread-%d", i),
					Name:       fmt.Sprintf("会话-%d", i),
					CWD:        key,
					LastUsedAt: time.Date(2026, 4, 10, 10, i, 0, 0, time.UTC),
				},
			},
		})
	}

	result := app.HandleGatewayAction(context.Background(), control.Action{
		Kind:             control.ActionShowAllThreadWorkspaces,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Inbound: &control.ActionInboundMeta{
			CardDaemonLifecycleID: app.daemonLifecycleID,
		},
	})

	if result == nil || result.ReplaceCurrentCard == nil {
		t.Fatalf("expected inline replacement result, got %#v", result)
	}
	if len(gateway.operations) != 0 {
		t.Fatalf("expected no appended gateway operations, got %#v", gateway.operations)
	}
	if result.ReplaceCurrentCard.CardTitle != "全部会话" {
		t.Fatalf("unexpected replacement card title: %#v", result.ReplaceCurrentCard)
	}
	if !operationHasActionValue(*result.ReplaceCurrentCard, "show_recent_thread_workspaces", "", "") {
		t.Fatalf("expected expanded thread workspace card to include return action, got %#v", result.ReplaceCurrentCard.CardElements)
	}
}

func TestDaemonHelloCanonicalizesWorkspaceMetadata(t *testing.T) {
	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	app.sendAgentCommand = func(string, agentproto.Command) error { return nil }

	app.onHello(context.Background(), agentproto.Hello{
		Instance: agentproto.InstanceHello{
			InstanceID:    "inst-1",
			DisplayName:   "droid",
			WorkspaceRoot: " /data/dl/work/../droid/ ",
			Source:        "vscode",
		},
	})

	inst := app.service.Instance("inst-1")
	if inst == nil {
		t.Fatal("expected instance after hello")
	}
	if inst.WorkspaceRoot != "/data/dl/droid" || inst.WorkspaceKey != "/data/dl/droid" {
		t.Fatalf("expected canonical workspace metadata, got %#v", inst)
	}
	if inst.ShortName != "droid" {
		t.Fatalf("expected canonical short name, got %#v", inst)
	}
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
		case operation.Kind == feishu.OperationSendCard && operation.CardTitle == "工作区列表":
			hasListCard = true
		case operation.Kind == feishu.OperationAddReaction && operation.MessageID == "msg-1":
			hasTyping = true
		case operation.Kind == feishu.OperationSendCard && strings.HasPrefix(operation.CardTitle, "最后答复"):
			hasFinalReplyCard = operation.CardBody == "已收到：\n\n```text\nREADME.md\n```"
		}
	}
	if !hasListCard {
		t.Fatalf("expected workspace list card, got %#v", gateway.operations)
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
		if operation.Kind == feishu.OperationSendCard && strings.Contains(operation.CardBody, "已准备新建会话") {
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

func TestDaemonContinuesFinalReplyAfterPreviewTimeout(t *testing.T) {
	gateway := &ctxCheckingGateway{}
	previewer := &timeoutMarkdownPreviewer{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	app.SetFinalBlockPreviewer(previewer)
	app.finalPreviewTimeout = 10 * time.Millisecond
	app.gatewayApplyTimeout = 200 * time.Millisecond

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

	requests, previewCtxErr := previewer.snapshot()
	if len(requests) != 1 {
		t.Fatalf("expected one preview request, got %#v", requests)
	}
	if !errors.Is(previewCtxErr, context.DeadlineExceeded) {
		t.Fatalf("expected preview timeout, got %v", previewCtxErr)
	}
	if gateway.ctxErr != nil {
		t.Fatalf("expected final gateway apply to use a fresh context, got %v", gateway.ctxErr)
	}

	var finalBody string
	for _, operation := range gateway.operations {
		if operation.Kind == feishu.OperationSendCard && strings.HasPrefix(operation.CardTitle, "最后答复") {
			finalBody = operation.CardBody
		}
	}
	if finalBody != "查看 [设计文档](/data/dl/droid/docs/design.md)" {
		t.Fatalf("expected original final body after preview timeout, got %#v", gateway.operations)
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
	if got.Kind != feishu.OperationSendCard || got.CardTitle != "工作区列表" {
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
		case operation.Kind == feishu.OperationSendCard && operation.CardTitle == "系统提示" && operation.CardBody == "当前接管的工作区已离线：/data/dl/droid":
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

func TestDaemonTickSyncsFeishuTimeSensitiveForPendingInput(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})

	userSurfaceID := "feishu:app-1:user:ou_user-1"
	chatSurfaceID := "feishu:app-1:chat:oc_group-1"
	app.service.MaterializeSurface(userSurfaceID, "app-1", "oc_p2p-1", "ou_user-1")
	app.service.MaterializeSurface(chatSurfaceID, "app-1", "oc_group-1", "ou_user-1")

	surfaces := app.service.Surfaces()
	for _, surface := range surfaces {
		switch surface.SurfaceSessionID {
		case userSurfaceID, chatSurfaceID:
			surface.PendingRequests = map[string]*state.RequestPromptRecord{
				"req-1": {RequestID: "req-1"},
			}
		}
	}

	app.onTick(context.Background(), time.Now().UTC())

	if len(gateway.operations) != 1 {
		t.Fatalf("operations = %#v, want exactly one time-sensitive enable for user surface", gateway.operations)
	}
	if gateway.operations[0].Kind != feishu.OperationSetTimeSensitive {
		t.Fatalf("first operation kind = %q, want %q", gateway.operations[0].Kind, feishu.OperationSetTimeSensitive)
	}
	if !gateway.operations[0].TimeSensitive {
		t.Fatalf("expected first operation to enable time-sensitive state, got %#v", gateway.operations[0])
	}
	if gateway.operations[0].ReceiveID != "ou_user-1" || gateway.operations[0].ReceiveIDType != "open_id" {
		t.Fatalf("unexpected user target for time-sensitive enable: %#v", gateway.operations[0])
	}

	app.onTick(context.Background(), time.Now().UTC().Add(time.Second))
	if len(gateway.operations) != 1 {
		t.Fatalf("second tick should not resend unchanged time-sensitive state, got %#v", gateway.operations)
	}

	for _, surface := range app.service.Surfaces() {
		if surface.SurfaceSessionID == userSurfaceID {
			surface.PendingRequests = nil
		}
	}

	app.onTick(context.Background(), time.Now().UTC().Add(2*time.Second))

	if len(gateway.operations) != 2 {
		t.Fatalf("operations after clearing pending input = %#v, want one enable and one disable", gateway.operations)
	}
	if gateway.operations[1].Kind != feishu.OperationSetTimeSensitive || gateway.operations[1].TimeSensitive {
		t.Fatalf("second operation = %#v, want time-sensitive disable", gateway.operations[1])
	}
	if gateway.operations[1].ReceiveID != "ou_user-1" || gateway.operations[1].ReceiveIDType != "open_id" {
		t.Fatalf("unexpected user target for time-sensitive disable: %#v", gateway.operations[1])
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
	if gateway.operations[1].CardTitle == "" || !strings.Contains(gateway.operations[1].CardBody, "当前没有可接管的工作区") {
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

func TestDaemonRejectsOldRemovedCardAndShowsConcreteLegacyCommand(t *testing.T) {
	gateway := &recordingGateway{}
	startedAt := time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{PID: 42, StartedAt: startedAt})

	seedAttachedSurfaceForInboundTests(app)

	before := len(gateway.operations)
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionRemovedCommand,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "resume_headless_thread",
		Inbound: &control.ActionInboundMeta{
			CardDaemonLifecycleID: "older-life",
		},
	})

	delta := gateway.operations[before:]
	assertSingleRejectedNotice(t, delta, "旧卡片已过期", "重新发送对应命令获取新卡片")
	if !strings.Contains(delta[0].CardBody, "/newinstance") {
		t.Fatalf("expected expired removed-card notice to mention /newinstance, got %#v", delta)
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

func containsEnvEntry(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
