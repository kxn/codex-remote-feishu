package daemon

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/relayws"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestEventAffectsSurfaceResumeState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		event agentproto.Event
		want  bool
	}{
		{name: "item delta", event: agentproto.Event{Kind: agentproto.EventItemDelta}, want: false},
		{name: "item completed", event: agentproto.Event{Kind: agentproto.EventItemCompleted}, want: true},
		{name: "turn completed", event: agentproto.Event{Kind: agentproto.EventTurnCompleted}, want: true},
		{name: "thread discovered", event: agentproto.Event{Kind: agentproto.EventThreadDiscovered}, want: true},
		{name: "thread runtime status updated", event: agentproto.Event{Kind: agentproto.EventThreadRuntimeStatusUpdated}, want: true},
		{name: "threads snapshot", event: agentproto.Event{Kind: agentproto.EventThreadsSnapshot}, want: true},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := eventAffectsSurfaceResumeState(tc.event); got != tc.want {
				t.Fatalf("eventAffectsSurfaceResumeState(%s) = %t, want %t", tc.event.Kind, got, tc.want)
			}
		})
	}
}

func TestShouldLogAgentEvent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		event agentproto.Event
		want  bool
	}{
		{name: "item delta", event: agentproto.Event{Kind: agentproto.EventItemDelta}, want: false},
		{name: "item completed", event: agentproto.Event{Kind: agentproto.EventItemCompleted}, want: true},
		{name: "threads snapshot", event: agentproto.Event{Kind: agentproto.EventThreadsSnapshot}, want: true},
		{name: "system error", event: agentproto.Event{Kind: agentproto.EventSystemError}, want: true},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := shouldLogAgentEvent(tc.event); got != tc.want {
				t.Fatalf("shouldLogAgentEvent(%s) = %t, want %t", tc.event.Kind, got, tc.want)
			}
		})
	}
}

func TestIngressPumpRoundRobinKeepsPerInstanceFIFO(t *testing.T) {
	pump := newIngressPump()
	for _, item := range []ingressWorkItem{
		{
			instanceID: "inst-a",
			kind:       ingressWorkEvents,
			events:     []agentproto.Event{{Kind: agentproto.EventItemDelta, ItemID: "a-1"}},
		},
		{
			instanceID: "inst-a",
			kind:       ingressWorkEvents,
			events:     []agentproto.Event{{Kind: agentproto.EventItemDelta, ItemID: "a-2"}},
		},
		{
			instanceID: "inst-b",
			kind:       ingressWorkEvents,
			events:     []agentproto.Event{{Kind: agentproto.EventItemDelta, ItemID: "b-1"}},
		},
	} {
		if err := pump.Enqueue(item); err != nil {
			t.Fatalf("enqueue: %v", err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer func() {
		pump.Close()
		pump.Wait()
	}()

	gotCh := make(chan string, 3)
	go func() {
		err := pump.Run(ctx, func(item ingressWorkItem) {
			gotCh <- item.instanceID + ":" + item.events[0].ItemID
		})
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("pump run: %v", err)
		}
	}()

	want := []string{"inst-a:a-1", "inst-b:b-1", "inst-a:a-2"}
	for _, expected := range want {
		select {
		case got := <-gotCh:
			if got != expected {
				t.Fatalf("processing order = %q, want %q", got, expected)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for %s", expected)
		}
	}
}

func TestIngressPumpEnqueueDoesNotBlockOnSlowHandler(t *testing.T) {
	pump := newIngressPump()
	if err := pump.Enqueue(ingressWorkItem{
		instanceID: "inst-a",
		kind:       ingressWorkEvents,
		events:     []agentproto.Event{{Kind: agentproto.EventItemDelta, ItemID: "a-1"}},
	}); err != nil {
		t.Fatalf("enqueue first item: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer func() {
		pump.Close()
		pump.Wait()
	}()

	started := make(chan struct{})
	release := make(chan struct{})
	processed := make(chan string, 2)
	go func() {
		err := pump.Run(ctx, func(item ingressWorkItem) {
			if item.events[0].ItemID == "a-1" {
				close(started)
				<-release
			}
			processed <- item.events[0].ItemID
		})
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("pump run: %v", err)
		}
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for slow handler to start")
	}

	enqueueDone := make(chan error, 1)
	go func() {
		enqueueDone <- pump.Enqueue(ingressWorkItem{
			instanceID: "inst-a",
			kind:       ingressWorkEvents,
			events:     []agentproto.Event{{Kind: agentproto.EventItemDelta, ItemID: "a-2"}},
		})
	}()

	select {
	case err := <-enqueueDone:
		if err != nil {
			t.Fatalf("enqueue second item: %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("enqueue blocked on slow handler")
	}

	close(release)

	for _, expected := range []string{"a-1", "a-2"} {
		select {
		case got := <-processed:
			if got != expected {
				t.Fatalf("processed item = %s, want %s", got, expected)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for processed item %s", expected)
		}
	}
}

func TestAppRelayCallbacksUseIngressPump(t *testing.T) {
	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app.startIngressPump(ctx, nil)
	defer app.stopIngressPump()

	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	app.rememberRelayConnection("inst-1", 1)

	app.enqueueEvents(context.Background(), relayws.ConnectionMeta{ConnectionID: 1}, "inst-1", []agentproto.Event{{
		Kind: agentproto.EventThreadsSnapshot,
		Threads: []agentproto.ThreadSnapshotRecord{{
			ThreadID: "thread-1",
			Name:     "修复登录流程",
			CWD:      "/data/dl/droid",
			Loaded:   true,
		}},
	}})

	waitForDaemonCondition(t, 2*time.Second, func() bool {
		app.mu.Lock()
		defer app.mu.Unlock()
		inst := app.service.Instance("inst-1")
		return inst != nil && inst.Threads["thread-1"] != nil
	})
}

func TestSyncSurfaceResumeStateForInstanceLockedScopesHeadlessRecoveryState(t *testing.T) {
	t.Parallel()

	app := newRestoreHintTestApp(t.TempDir())
	seedHeadlessInstance(app, "inst-1", "thread-1")
	seedHeadlessInstance(app, "inst-2", "thread-2")

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionStatus,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionAttachInstance,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionStatus,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-2",
		ChatID:           "chat-2",
		ActorUserID:      "user-2",
	})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionAttachInstance,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-2",
		ChatID:           "chat-2",
		ActorUserID:      "user-2",
		InstanceID:       "inst-2",
	})

	app.mu.Lock()
	if err := app.surfaceResumeState.Delete("surface-1"); err != nil {
		app.mu.Unlock()
		t.Fatalf("delete surface-1 resume state: %v", err)
	}
	if err := app.surfaceResumeState.Delete("surface-2"); err != nil {
		app.mu.Unlock()
		t.Fatalf("delete surface-2 resume state: %v", err)
	}
	delete(app.headlessRestoreState, "surface-1")
	delete(app.headlessRestoreState, "surface-2")
	app.syncSurfaceResumeStateForInstanceLocked("inst-1", nil)
	_, entry1 := app.surfaceResumeState.Get("surface-1")
	_, entry2 := app.surfaceResumeState.Get("surface-2")
	_, recovery1 := app.headlessRestoreState["surface-1"]
	_, recovery2 := app.headlessRestoreState["surface-2"]
	app.mu.Unlock()

	if !entry1 {
		t.Fatal("expected scoped surface resume sync to repopulate attached surface")
	}
	if entry2 {
		t.Fatal("expected scoped surface resume sync to skip unrelated attached surface")
	}
	if !recovery1 {
		t.Fatal("expected scoped sync to repopulate attached headless recovery state")
	}
	if recovery2 {
		t.Fatal("expected scoped sync to skip unrelated headless recovery state")
	}
}

func TestSyncSurfaceResumeStateForInstanceLockedScopesToAttachedInstance(t *testing.T) {
	t.Parallel()

	app := newRestoreHintTestApp(t.TempDir())
	seedHeadlessInstance(app, "inst-1", "thread-1")
	seedHeadlessInstance(app, "inst-2", "thread-2")

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionStatus,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionAttachInstance,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionStatus,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-2",
		ChatID:           "chat-2",
		ActorUserID:      "user-2",
	})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionAttachInstance,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-2",
		ChatID:           "chat-2",
		ActorUserID:      "user-2",
		InstanceID:       "inst-2",
	})

	app.mu.Lock()
	if err := app.surfaceResumeState.Delete("surface-1"); err != nil {
		app.mu.Unlock()
		t.Fatalf("delete surface-1 resume state: %v", err)
	}
	if err := app.surfaceResumeState.Delete("surface-2"); err != nil {
		app.mu.Unlock()
		t.Fatalf("delete surface-2 resume state: %v", err)
	}
	app.syncSurfaceResumeStateForInstanceLocked("inst-1", nil)
	_, entry1 := app.surfaceResumeState.Get("surface-1")
	_, entry2 := app.surfaceResumeState.Get("surface-2")
	app.mu.Unlock()

	if !entry1 {
		t.Fatal("expected scoped surface resume sync to repopulate attached surface")
	}
	if entry2 {
		t.Fatal("expected scoped surface resume sync to skip unrelated attached surface")
	}
}

func waitForDaemonCondition(t *testing.T, timeout time.Duration, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !check() {
		t.Fatal("condition not satisfied before timeout")
	}
}

func TestDaemonShutdownWithoutIngressPumpStartReturns(t *testing.T) {
	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = app.Shutdown(context.Background())
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown blocked without ingress pump start")
	}
}

func TestIngressPumpCloseRejectsNewWork(t *testing.T) {
	pump := newIngressPump()
	pump.Close()
	if err := pump.Enqueue(ingressWorkItem{
		instanceID: "inst-a",
		kind:       ingressWorkDisconnect,
	}); !errors.Is(err, errIngressPumpClosed) {
		t.Fatalf("expected closed pump error, got %v", err)
	}
}

func TestIngressPumpRejectsDataWhenInstanceQueueFull(t *testing.T) {
	pump := newIngressPump()
	pump.maxPerInstance = 1

	first := ingressWorkItem{
		instanceID:   "inst-a",
		connectionID: 1,
		kind:         ingressWorkEvents,
		events:       []agentproto.Event{{Kind: agentproto.EventItemDelta, ItemID: "a-1"}},
	}
	second := ingressWorkItem{
		instanceID:   "inst-a",
		connectionID: 1,
		kind:         ingressWorkEvents,
		events:       []agentproto.Event{{Kind: agentproto.EventItemDelta, ItemID: "a-2"}},
	}
	if err := pump.Enqueue(first); err != nil {
		t.Fatalf("enqueue first item: %v", err)
	}
	if err := pump.Enqueue(second); !errors.Is(err, errIngressQueueFull) {
		t.Fatalf("expected queue full error, got %v", err)
	}
	if err := pump.Enqueue(ingressWorkItem{
		instanceID:   "inst-a",
		connectionID: 2,
		kind:         ingressWorkHello,
		hello:        &agentproto.Hello{Instance: agentproto.InstanceHello{InstanceID: "inst-a"}},
	}); err != nil {
		t.Fatalf("expected hello to bypass full queue, got %v", err)
	}
}

func TestAppStopIngressPumpWaitsForRunner(t *testing.T) {
	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	app.startIngressPump(ctx, nil)

	done := make(chan struct{})
	go func() {
		defer close(done)
		app.stopIngressPump()
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stopIngressPump blocked")
	}
}

func TestIngressPumpRunReturnsOnContextCancel(t *testing.T) {
	pump := newIngressPump()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := pump.Run(ctx, func(ingressWorkItem) {})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v, want context canceled", err)
	}
}

func TestIngressPumpConcurrentEnqueueIsSafe(t *testing.T) {
	pump := newIngressPump()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer func() {
		pump.Close()
		pump.Wait()
	}()

	var wg sync.WaitGroup
	gotCh := make(chan string, 4)
	go func() {
		err := pump.Run(ctx, func(item ingressWorkItem) {
			gotCh <- item.instanceID + ":" + item.kind.String()
		})
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("pump run: %v", err)
		}
	}()

	items := []ingressWorkItem{
		{instanceID: "inst-a", kind: ingressWorkDisconnect},
		{instanceID: "inst-b", kind: ingressWorkDisconnect},
		{instanceID: "inst-a", kind: ingressWorkDisconnect},
		{instanceID: "inst-b", kind: ingressWorkDisconnect},
	}
	wg.Add(len(items))
	for _, item := range items {
		go func(item ingressWorkItem) {
			defer wg.Done()
			if err := pump.Enqueue(item); err != nil {
				t.Errorf("enqueue: %v", err)
			}
		}(item)
	}
	wg.Wait()

	for range items {
		select {
		case <-gotCh:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for concurrent items")
		}
	}
}

func (k ingressWorkKind) String() string {
	return string(k)
}

func TestAppProcessIngressDropsStaleConnectionItems(t *testing.T) {
	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID: "inst-1",
		Threads:    map[string]*state.ThreadRecord{},
	})
	app.rememberRelayConnection("inst-1", 2)

	app.processIngressWork(ingressWorkItem{
		instanceID:   "inst-1",
		connectionID: 1,
		kind:         ingressWorkEvents,
		events: []agentproto.Event{{
			Kind: agentproto.EventThreadsSnapshot,
			Threads: []agentproto.ThreadSnapshotRecord{{
				ThreadID: "thread-stale",
				Name:     "stale",
				Loaded:   true,
			}},
		}},
	})

	stats := app.ingress.Stats("inst-1")
	if stats.StaleDropCount != 1 {
		t.Fatalf("expected stale drop count to increment, got %#v", stats)
	}
	if thread := app.service.Instance("inst-1").Threads["thread-stale"]; thread != nil {
		t.Fatalf("expected stale ingress item to be dropped, got %#v", thread)
	}
}

func TestAppHandleIngressOverloadKeepsAttachmentUntilPreservedTurnCompletes(t *testing.T) {
	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	app.service.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	app.service.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ThreadID: "thread-1"})
	app.service.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, SurfaceSessionID: "surface-1", MessageID: "msg-1", Text: "你好"})
	app.service.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})
	app.service.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, SurfaceSessionID: "surface-1", MessageID: "msg-2", Text: "第二条"})

	app.rememberRelayConnection("inst-1", 7)
	app.handleIngressOverload("inst-1", 7)
	app.processIngressWork(ingressWorkItem{
		instanceID:   "inst-1",
		connectionID: 7,
		kind:         ingressWorkDisconnect,
	})

	surface := app.service.SurfaceSnapshot("surface-1")
	if surface == nil || surface.Attachment.InstanceID != "inst-1" || surface.Attachment.SelectedThreadID != "thread-1" {
		t.Fatalf("expected transport degrade to keep attachment and selected thread, got %#v", surface)
	}

	recovery := app.service.ApplyInstanceConnected("inst-1")
	if len(recovery) != 0 {
		t.Fatalf("expected reconnect to wait for preserved turn completion, got %#v", recovery)
	}
	active := app.service.ActiveRemoteTurns()
	if len(active) != 1 || active[0].SourceMessageID != "msg-1" || active[0].TurnID != "turn-1" {
		t.Fatalf("expected in-flight turn to stay active across reconnect, got %#v", active)
	}

	resumed := app.service.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})
	var sawPromptSend bool
	for _, event := range resumed {
		if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend {
			sawPromptSend = true
		}
	}
	if !sawPromptSend {
		t.Fatalf("expected preserved turn completion to re-dispatch queued work, got %#v", resumed)
	}

	pending := app.service.PendingRemoteTurns()
	if len(pending) != 1 || pending[0].SourceMessageID != "msg-2" {
		t.Fatalf("expected queued work to resume after preserved turn completion, got %#v", pending)
	}
}
