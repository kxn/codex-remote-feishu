package daemon

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/adapter/relayws"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/surfaceresume"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestCodexTopicSettingWriteFailureRollsBackBeforeSuccessUI(t *testing.T) {
	tests := []struct {
		name            string
		text            string
		kind            control.ActionKind
		stamped         bool
		storeFailure    string
		wantCardTitle   string
		wantOldOverride state.CodexPromptOverrideRecord
	}{
		{
			name:            "stamped model card",
			text:            "/model gpt-5.6-terra",
			kind:            control.ActionModelCommand,
			stamped:         true,
			storeFailure:    "write",
			wantCardTitle:   "设置失败",
			wantOldOverride: state.CodexPromptOverrideRecord{Model: "gpt-5.5", ReasoningEffort: "high"},
		},
		{
			name:            "plain reasoning slash",
			text:            "/reasoning max",
			kind:            control.ActionReasoningCommand,
			storeFailure:    "write",
			wantCardTitle:   "设置失败",
			wantOldOverride: state.CodexPromptOverrideRecord{Model: "gpt-5.5", ReasoningEffort: "high"},
		},
		{
			name:            "nil store",
			text:            "/model gpt-5.6-terra",
			kind:            control.ActionModelCommand,
			storeFailure:    "nil",
			wantCardTitle:   "设置失败",
			wantOldOverride: state.CodexPromptOverrideRecord{Model: "gpt-5.5", ReasoningEffort: "high"},
		},
		{
			name:            "empty path store",
			text:            "/reasoning max",
			kind:            control.ActionReasoningCommand,
			stamped:         true,
			storeFailure:    "empty_path",
			wantCardTitle:   "设置失败",
			wantOldOverride: state.CodexPromptOverrideRecord{Model: "gpt-5.5", ReasoningEffort: "high"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			app, gateway, persistedPath := newCodexTopicSettingTestApp(t, tc.wantOldOverride, dynamicNonGPTCodexTopicSettingProfile())
			switch tc.storeFailure {
			case "nil":
				app.surfaceResumeRuntime.store = nil
			case "empty_path":
				store := surfaceresume.NewStore("")
				store.SetEntries(app.surfaceResumeRuntime.store.Entries())
				app.surfaceResumeRuntime.store = store
			default:
				failingParent := filepath.Join(t.TempDir(), "not-a-directory")
				if err := os.WriteFile(failingParent, []byte("block writes"), 0o600); err != nil {
					t.Fatalf("create deterministic surface store blocker: %v", err)
				}
				store := surfaceresume.NewStore(filepath.Join(failingParent, surfaceresume.StateFileName))
				store.SetEntries(app.surfaceResumeRuntime.store.Entries())
				app.surfaceResumeRuntime.store = store
			}

			action := control.Action{
				Kind:             tc.kind,
				GatewayID:        "app-1",
				SurfaceSessionID: "surface-1",
				ChatID:           "chat-1",
				ActorUserID:      "user-1",
				Text:             tc.text,
			}
			if tc.stamped {
				action.Inbound = &control.ActionInboundMeta{CardDaemonLifecycleID: app.daemonLifecycleID}
			}

			result := handleGatewayActionForTest(context.Background(), app, action)

			if got := app.service.Surface("surface-1").CodexPromptOverride; got != tc.wantOldOverride {
				t.Fatalf("in-memory Codex prompt override = %#v, want rollback to %#v", got, tc.wantOldOverride)
			}
			assertPersistedCodexPromptOverride(t, persistedPath, tc.wantOldOverride)
			if tc.stamped {
				if result == nil || result.ReplaceCurrentCard == nil {
					t.Fatalf("expected same-card persist failure, got %#v", result)
				}
				if len(gateway.operations) != 0 {
					t.Fatalf("stamped persist failure appended gateway operations: %#v", gateway.operations)
				}
				assertCodexTopicSettingPersistFailureOperation(t, *result.ReplaceCurrentCard, tc.wantCardTitle)
				return
			}
			if result != nil {
				t.Fatalf("plain slash unexpectedly replaced a current card: %#v", result)
			}
			if len(gateway.operations) != 1 {
				t.Fatalf("plain slash persist failure operations = %#v, want one failure notice", gateway.operations)
			}
			assertCodexTopicSettingPersistFailureOperation(t, gateway.operations[0], tc.wantCardTitle)
		})
	}
	t.Run("new surface without durable entry", func(t *testing.T) {
		stateDir := t.TempDir()
		gateway := &recordingGateway{}
		app := newRestoreHintTestApp(stateDir)
		app.gateway = gateway
		const surfaceID = "surface-new-setting"
		profile := dynamicNonGPTCodexTopicSettingProfile()
		app.service.MaterializeCodexProfiles([]state.CodexProfileSummary{profile})
		app.service.MaterializeSurfaceResumeWithCodexProfile(surfaceID, "app-1", "chat-1", "user-1", state.ProductModeNormal, agentproto.BackendCodex, profile.ID, "", state.SurfaceVerbosityNormal, state.PlanModeSettingOff)
		seedHeadlessInstance(app, "inst-1", "thread-1")
		app.service.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: surfaceID, ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
		persistedPath := surfaceresume.StatePath(stateDir)
		assertNoPersistedSurfaceResumeEntry(t, persistedPath, surfaceID)

		failingParent := filepath.Join(t.TempDir(), "not-a-directory")
		if err := os.WriteFile(failingParent, []byte("block writes"), 0o600); err != nil {
			t.Fatalf("create deterministic surface store blocker: %v", err)
		}
		app.surfaceResumeRuntime.store = surfaceresume.NewStore(filepath.Join(failingParent, surfaceresume.StateFileName))
		var logs strings.Builder
		oldLogOutput := log.Writer()
		log.SetOutput(&logs)
		t.Cleanup(func() { log.SetOutput(oldLogOutput) })

		result := handleGatewayActionForTest(context.Background(), app, control.Action{
			Kind: control.ActionModelCommand, GatewayID: "app-1", SurfaceSessionID: surfaceID, ChatID: "chat-1", ActorUserID: "user-1", Text: "/model gpt-5.6-terra",
			Inbound: &control.ActionInboundMeta{CardDaemonLifecycleID: app.daemonLifecycleID},
		})

		if got := app.service.Surface(surfaceID).CodexPromptOverride; got != (state.CodexPromptOverrideRecord{}) {
			t.Fatalf("new surface in-memory override = %#v, want empty rollback", got)
		}
		assertNoPersistedSurfaceResumeEntry(t, persistedPath, surfaceID)
		if result == nil || result.ReplaceCurrentCard == nil || len(gateway.operations) != 0 {
			t.Fatalf("new surface failure delivery = result %#v operations %#v", result, gateway.operations)
		}
		assertCodexTopicSettingPersistFailureOperation(t, *result.ReplaceCurrentCard, "设置失败")
		if strings.Contains(logs.String(), "persist surface resume state failed: surface="+surfaceID) {
			t.Fatalf("generic surface sync retried the rolled-back setting:\n%s", logs.String())
		}
	})
}

func TestCodexTopicSettingPersistsBeforeSuccessUI(t *testing.T) {
	tests := []struct {
		name, text, successText string
		kind                    control.ActionKind
		stamped                 bool
		want                    state.CodexPromptOverrideRecord
	}{
		{name: "plain model slash", text: "/model gpt-5.6-terra", successText: "已更新飞书临时模型覆盖", kind: control.ActionModelCommand, want: state.CodexPromptOverrideRecord{Model: "gpt-5.6-terra", ReasoningEffort: "high"}},
		{name: "stamped reasoning card", text: "/reasoning max", successText: "已更新飞书临时推理强度覆盖", kind: control.ActionReasoningCommand, stamped: true, want: state.CodexPromptOverrideRecord{Model: "gpt-5.5", ReasoningEffort: "max"}},
	}
	old := state.CodexPromptOverrideRecord{Model: "gpt-5.5", ReasoningEffort: "high"}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			app, gateway, persistedPath := newCodexTopicSettingTestApp(t, old, dynamicNonGPTCodexTopicSettingProfile())
			app.surfaceResumeRuntime.groupTerminalFailureNotices = map[string]string{"surface-1": "old_failure"}
			app.surfaceResumeRuntime.vscodeDetachedPromptScanDue = false
			action := control.Action{Kind: tc.kind, GatewayID: "app-1", SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", Text: tc.text}
			if tc.stamped {
				action.Inbound = &control.ActionInboundMeta{CardDaemonLifecycleID: app.daemonLifecycleID}
			}

			result := handleGatewayActionForTest(context.Background(), app, action)

			if got := app.service.Surface("surface-1").CodexPromptOverride; got != tc.want {
				t.Fatalf("in-memory Codex prompt override = %#v, want %#v", got, tc.want)
			}
			assertPersistedCodexPromptOverride(t, persistedPath, tc.want)
			if _, ok := app.surfaceResumeRuntime.groupTerminalFailureNotices["surface-1"]; ok {
				t.Fatal("successful durable Put did not clear the terminal failure notice")
			}
			if !app.surfaceResumeRuntime.vscodeDetachedPromptScanDue {
				t.Fatal("successful durable Put did not schedule the VS Code detached prompt scan")
			}
			var operation feishu.Operation
			if tc.stamped {
				if result == nil || result.ReplaceCurrentCard == nil || len(gateway.operations) != 0 {
					t.Fatalf("stamped success delivery = result %#v operations %#v", result, gateway.operations)
				}
				operation = *result.ReplaceCurrentCard
			} else {
				if result != nil || len(gateway.operations) != 1 {
					t.Fatalf("plain success delivery = result %#v operations %#v", result, gateway.operations)
				}
				operation = gateway.operations[0]
			}
			text := operationCardText(operation)
			if !strings.Contains(text, tc.successText) || strings.Contains(text, codexTopicSettingPersistFailureText) {
				t.Fatalf("unexpected success UI: %#v", operation)
			}
		})
	}
}

func TestCodexPrefixPersistFailureDoesNotEnqueueOrDispatch(t *testing.T) {
	for _, action := range []control.Action{
		{Kind: control.ActionTextMessage, Text: "[sol:high] check this"},
		{Kind: control.ActionModelCommand, Text: "/model gpt-6-sol high"},
		{Kind: control.ActionReasoningCommand, Text: "/reasoning low"},
	} {
		t.Run(action.Text, func(t *testing.T) {
			old := state.CodexPromptOverrideRecord{Model: "gpt-6-astra", ReasoningEffort: "high"}
			app, gateway, persistedPath := newCodexTopicSettingTestApp(t, old, state.CodexProfileSummary{ID: state.NativeCodexProfileID, Kind: state.CodexProfileKindNative, Available: true})
			app.surfaceResumeRuntime.store = nil
			action.GatewayID = "app-1"
			action.SurfaceSessionID = "surface-1"
			action.ChatID = "chat-1"
			action.ActorUserID = "user-1"
			action.MessageID = "msg-prefix"
			handleGatewayActionForTest(context.Background(), app, action)
			surface := app.service.Surface("surface-1")
			if surface.CodexPromptOverride != old || surface.ActiveQueueItemID != "" || len(surface.QueueItems) != 0 || len(surface.QueuedQueueItemIDs) != 0 {
				t.Fatalf("failed durable write mutated execution state: override=%#v active=%q queued=%d", surface.CodexPromptOverride, surface.ActiveQueueItemID, len(surface.QueueItems))
			}
			assertPersistedCodexPromptOverride(t, persistedPath, old)
			if len(gateway.operations) != 1 {
				t.Fatalf("expected failure notice only, got %#v", gateway.operations)
			}
			assertCodexTopicSettingPersistFailureOperation(t, gateway.operations[0], "设置失败")
		})
	}
}

func dynamicNonGPTCodexTopicSettingProfile() state.CodexProfileSummary {
	return state.CodexProfileSummary{
		ID: "dynamic-non-gpt", Kind: state.CodexProfileKindAPI, BaseURL: "https://api.deepseek.com/", Model: "deepseek-v4-flash", ReasoningEffort: "high", Available: true,
	}
}

func newCodexTopicSettingTestApp(t *testing.T, initial state.CodexPromptOverrideRecord, profile state.CodexProfileSummary) (*App, *recordingGateway, string) {
	t.Helper()
	stateDir := t.TempDir()
	gateway := &recordingGateway{}
	app := newRestoreHintTestApp(stateDir)
	app.gateway = gateway
	app.service.MaterializeCodexProfiles([]state.CodexProfileSummary{profile})
	app.service.MaterializeSurfaceResumeWithCodexProfile("surface-1", "app-1", "chat-1", "user-1", state.ProductModeNormal, agentproto.BackendCodex, profile.ID, "", state.SurfaceVerbosityNormal, state.PlanModeSettingOff)
	seedHeadlessInstance(app, "inst-1", "thread-1")
	app.service.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	app.service.RestoreSurfaceCodexPromptOverride("surface-1", initial, time.Time{})
	app.mu.Lock()
	app.syncSurfaceResumeStateLocked(nil)
	app.mu.Unlock()
	path := surfaceresume.StatePath(stateDir)
	assertPersistedCodexPromptOverride(t, path, initial)
	return app, gateway, path
}

func assertPersistedCodexPromptOverride(t *testing.T, path string, want state.CodexPromptOverrideRecord) {
	t.Helper()
	store, err := surfaceresume.LoadStore(path)
	if err != nil {
		t.Fatalf("load persisted surface resume state: %v", err)
	}
	entry, ok := store.Get("surface-1")
	if !ok {
		t.Fatal("persisted surface resume entry missing")
	}
	if entry.CodexModelOverride != want.Model || entry.CodexReasoningEffortOverride != want.ReasoningEffort {
		t.Fatalf("persisted Codex prompt override = %q/%q, want %q/%q", entry.CodexModelOverride, entry.CodexReasoningEffortOverride, want.Model, want.ReasoningEffort)
	}
}

func assertNoPersistedSurfaceResumeEntry(t *testing.T, path, surfaceID string) {
	t.Helper()
	store, err := surfaceresume.LoadStore(path)
	if err != nil {
		t.Fatalf("load persisted surface resume state: %v", err)
	}
	if entry, ok := store.Get(surfaceID); ok {
		t.Fatalf("unexpected persisted surface resume entry: %#v", entry)
	}
}

func assertCodexTopicSettingPersistFailureOperation(t *testing.T, operation feishu.Operation, wantTitle string) {
	t.Helper()
	text := operationCardText(operation)
	if operation.CardTitle != wantTitle || !strings.Contains(text, "未能保存") || !strings.Contains(text, "当前配置未改变") {
		t.Fatalf("unexpected Codex topic setting persist failure UI: %#v", operation)
	}
	if strings.Contains(text, "已更新飞书临时") {
		t.Fatalf("persist failure leaked success UI: %#v", operation)
	}
}

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

func TestEventAffectsBotCapabilitySettingsState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		event agentproto.Event
		want  bool
	}{
		{name: "request resolved", event: agentproto.Event{Kind: agentproto.EventRequestResolved}, want: true},
		{name: "item delta", event: agentproto.Event{Kind: agentproto.EventItemDelta}, want: false},
		{name: "turn completed", event: agentproto.Event{Kind: agentproto.EventTurnCompleted}, want: false},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := eventAffectsBotCapabilitySettingsState(tc.event); got != tc.want {
				t.Fatalf("eventAffectsBotCapabilitySettingsState(%s) = %t, want %t", tc.event.Kind, got, tc.want)
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

func TestSyncSurfaceResumeStateForInstanceLockedScopesRecoveryState(t *testing.T) {
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
	if err := app.surfaceResumeRuntime.store.Delete("surface-1"); err != nil {
		app.mu.Unlock()
		t.Fatalf("delete surface-1 resume state: %v", err)
	}
	if err := app.surfaceResumeRuntime.store.Delete("surface-2"); err != nil {
		app.mu.Unlock()
		t.Fatalf("delete surface-2 resume state: %v", err)
	}
	delete(app.surfaceResumeRuntime.recovery, "surface-1")
	delete(app.surfaceResumeRuntime.recovery, "surface-2")
	app.syncSurfaceResumeStateForInstanceLocked("inst-1", nil)
	_, entry1 := app.surfaceResumeRuntime.store.Get("surface-1")
	_, entry2 := app.surfaceResumeRuntime.store.Get("surface-2")
	_, recovery1 := app.surfaceResumeRuntime.recovery["surface-1"]
	_, recovery2 := app.surfaceResumeRuntime.recovery["surface-2"]
	app.mu.Unlock()

	if !entry1 {
		t.Fatal("expected scoped surface resume sync to repopulate attached surface")
	}
	if entry2 {
		t.Fatal("expected scoped surface resume sync to skip unrelated attached surface")
	}
	if !recovery1 {
		t.Fatal("expected scoped sync to repopulate attached recovery state")
	}
	if recovery2 {
		t.Fatal("expected scoped sync to skip unrelated recovery state")
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
	if err := app.surfaceResumeRuntime.store.Delete("surface-1"); err != nil {
		app.mu.Unlock()
		t.Fatalf("delete surface-1 resume state: %v", err)
	}
	if err := app.surfaceResumeRuntime.store.Delete("surface-2"); err != nil {
		app.mu.Unlock()
		t.Fatalf("delete surface-2 resume state: %v", err)
	}
	app.syncSurfaceResumeStateForInstanceLocked("inst-1", nil)
	_, entry1 := app.surfaceResumeRuntime.store.Get("surface-1")
	_, entry2 := app.surfaceResumeRuntime.store.Get("surface-2")
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

func TestCodexInitialClearPersistsSettingPresence(t *testing.T) {
	app, _, path := newCodexTopicSettingTestApp(t, state.CodexPromptOverrideRecord{}, state.CodexProfileSummary{ID: state.NativeCodexProfileID, Kind: state.CodexProfileKindNative, Available: true})
	handleGatewayActionForTest(context.Background(), app, control.Action{Kind: control.ActionModelCommand, GatewayID: "app-1", SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", Text: "/model clear"})
	store, err := surfaceresume.LoadStore(path)
	if err != nil {
		t.Fatal(err)
	}
	entry, _ := store.Get("surface-1")
	if entry.CodexPromptOverrideUpdatedAt.IsZero() || entry.CodexModelOverride != "" || entry.CodexReasoningEffortOverride != "" {
		t.Fatalf("initial clear lost explicit presence: %#v", entry)
	}
	stamp := entry.CodexPromptOverrideUpdatedAt
	app.mu.Lock()
	app.syncSurfaceResumeStateLocked(nil)
	app.mu.Unlock()
	store, err = surfaceresume.LoadStore(path)
	if err != nil {
		t.Fatal(err)
	}
	entry, _ = store.Get("surface-1")
	if !entry.CodexPromptOverrideUpdatedAt.Equal(stamp) {
		t.Fatalf("ordinary route sync advanced topic setting clock: %#v", entry)
	}
	restarted := newRestoreHintTestApp(filepath.Dir(path))
	surface := restarted.service.Surface("surface-1")
	if surface == nil || !surface.CodexPromptOverrideUpdatedAt.Equal(stamp) {
		t.Fatalf("restart lost explicit clear stamp: %#v", surface)
	}
}
