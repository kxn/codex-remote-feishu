package orchestrator

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/testutil"
)

func TestItemBufferTextMaterializesLazily(t *testing.T) {
	buf := &itemBuffer{}

	buf.replaceText("hello")
	buf.appendText(", ")
	buf.appendText("world")

	if buf.textValue != "" {
		t.Fatalf("expected joined text cache to stay empty before materialization, got %q", buf.textValue)
	}
	if len(buf.textChunks) != 3 {
		t.Fatalf("expected three text chunks before materialization, got %#v", buf.textChunks)
	}

	if got := buf.text(); got != "hello, world" {
		t.Fatalf("buf.text() = %q, want %q", got, "hello, world")
	}
	if buf.textValue != "hello, world" {
		t.Fatalf("expected joined text cache after materialization, got %q", buf.textValue)
	}
	if len(buf.textChunks) != 1 || buf.textChunks[0] != "hello, world" {
		t.Fatalf("expected chunks to collapse after materialization, got %#v", buf.textChunks)
	}
}

func TestThreadTokenUsageUpdatePopulatesThreadStateAndFinalTurnSummary(t *testing.T) {
	now := time.Date(2026, 4, 12, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
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
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})
	if events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventThreadTokenUsageUpdated,
		ThreadID: "thread-1",
		TokenUsage: &agentproto.ThreadTokenUsage{
			Total: agentproto.TokenUsageBreakdown{
				TotalTokens:           300,
				InputTokens:           220,
				CachedInputTokens:     120,
				OutputTokens:          80,
				ReasoningOutputTokens: 30,
			},
		},
	}); len(events) != 0 {
		t.Fatalf("expected no direct UI event when recording baseline token usage, got %#v", events)
	}
	if events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "继续",
	}); len(events) == 0 {
		t.Fatal("expected queued remote events")
	}
	if events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	}); len(events) == 0 {
		t.Fatal("expected turn start events")
	}

	contextWindow := 1000
	if events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventThreadTokenUsageUpdated,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		TokenUsage: &agentproto.ThreadTokenUsage{
			Last: agentproto.TokenUsageBreakdown{
				TotalTokens:           190,
				InputTokens:           145,
				CachedInputTokens:     75,
				OutputTokens:          45,
				ReasoningOutputTokens: 18,
			},
			Total: agentproto.TokenUsageBreakdown{
				TotalTokens:           500,
				InputTokens:           370,
				CachedInputTokens:     200,
				OutputTokens:          130,
				ReasoningOutputTokens: 50,
			},
			ModelContextWindow: &contextWindow,
		},
	}); len(events) != 0 {
		t.Fatalf("expected no direct UI event on token usage update, got %#v", events)
	}

	thread := svc.root.Instances["inst-1"].Threads["thread-1"]
	if thread == nil || thread.TokenUsage == nil {
		t.Fatalf("expected thread usage state, got %#v", thread)
	}
	if thread.TokenUsage.Total.TotalTokens != 500 || thread.TokenUsage.Last.CachedInputTokens != 75 {
		t.Fatalf("unexpected thread token usage: %#v", thread.TokenUsage)
	}
	if binding := svc.turns.activeRemote["inst-1"]; binding == nil || !binding.HasLastUsage || binding.LastUsage.TotalTokens != 190 || !binding.HasStartTotalUsage || binding.StartTotalUsage.TotalTokens != 300 {
		t.Fatalf("expected active remote binding to capture turn usage baseline and last usage, got %#v", svc.turns.activeRemote["inst-1"])
	}

	now = now.Add(3400 * time.Millisecond)
	finished := completeRemoteTurnWithFinalText(t, svc, "turn-1", "completed", "", "已完成。", nil)
	var finalBlockEvent *control.UIEvent
	for i := range finished {
		event := finished[i]
		if event.Block != nil && event.Block.Final && event.Block.Text == "已完成。" {
			finalBlockEvent = &finished[i]
			break
		}
	}
	if finalBlockEvent == nil || finalBlockEvent.FinalTurnSummary == nil {
		t.Fatalf("expected final block with turn summary, got %#v", finished)
	}
	summary := finalBlockEvent.FinalTurnSummary
	if summary.Elapsed != 3400*time.Millisecond {
		t.Fatalf("unexpected elapsed summary: %#v", summary)
	}
	if summary.Usage == nil || summary.Usage.TotalTokens != 200 || summary.Usage.InputTokens != 150 || summary.Usage.CachedInputTokens != 80 || summary.Usage.OutputTokens != 50 || summary.Usage.ReasoningOutputTokens != 20 {
		t.Fatalf("unexpected final usage summary: %#v", summary)
	}
	if summary.ThreadUsage == nil || summary.ThreadUsage.TotalTokens != 500 || summary.ThreadUsage.InputTokens != 370 || summary.ThreadUsage.CachedInputTokens != 200 {
		t.Fatalf("unexpected thread cumulative usage summary: %#v", summary)
	}
	if summary.TotalTokensInContext != 190 {
		t.Fatalf("unexpected total tokens in context: %#v", summary)
	}
	if summary.ContextInputTokens == nil || *summary.ContextInputTokens != 145 {
		t.Fatalf("unexpected context input tokens: %#v", summary)
	}
	if summary.ModelContextWindow == nil || *summary.ModelContextWindow != 1000 {
		t.Fatalf("unexpected model context window: %#v", summary)
	}
}

func TestAttachPinsObservedFocusedThread(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
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

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "feishu:app-1:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})

	if len(events) < 2 {
		t.Fatalf("expected snapshot and notice, got %d events", len(events))
	}
	surface := svc.root.Surfaces["feishu:app-1:chat:1"]
	if surface.SelectedThreadID != "thread-1" {
		t.Fatalf("expected selected thread to be pinned, got %q", surface.SelectedThreadID)
	}
	if surface.RouteMode != state.RouteModePinned {
		t.Fatalf("expected route mode pinned, got %q", surface.RouteMode)
	}
}

func TestAttachFallsBackToActiveThreadWhenFocusedThreadUnknown(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:     "inst-1",
		DisplayName:    "droid",
		WorkspaceRoot:  "/data/dl/droid",
		WorkspaceKey:   "/data/dl/droid",
		ShortName:      "droid",
		Online:         true,
		ActiveThreadID: "thread-2",
		Threads: map[string]*state.ThreadRecord{
			"thread-2": {ThreadID: "thread-2", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})

	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "feishu:app-1:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})

	surface := svc.root.Surfaces["feishu:app-1:chat:1"]
	if surface.SelectedThreadID != "thread-2" {
		t.Fatalf("expected selected thread to fall back to active thread, got %q", surface.SelectedThreadID)
	}
	if surface.RouteMode != state.RouteModePinned {
		t.Fatalf("expected route mode pinned, got %q", surface.RouteMode)
	}
}

func TestWorkspaceSelectionEventCarriesFeishuTargetPickerContext(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	if len(events) != 1 || events[0].FeishuTargetPickerView == nil {
		t.Fatalf("expected target picker event, got %#v", events)
	}
	if events[0].FeishuTargetPickerContext == nil {
		t.Fatalf("expected feishu target picker context, got %#v", events[0])
	}
	if events[0].FeishuTargetPickerContext.DTOOwner != control.FeishuUIDTOwnerTargetPicker {
		t.Fatalf("unexpected dto owner: %#v", events[0].FeishuTargetPickerContext)
	}
	if events[0].FeishuTargetPickerContext.Source != control.TargetPickerRequestSourceList || events[0].FeishuTargetPickerContext.Title != "选择工作区与会话" {
		t.Fatalf("unexpected target picker context: %#v", events[0].FeishuTargetPickerContext)
	}
	if events[0].FeishuTargetPickerContext.Surface.ProductMode != string(state.ProductModeNormal) || events[0].FeishuTargetPickerContext.Surface.CallbackPayloadOwner != control.FeishuUICallbackPayloadOwnerAdapter {
		t.Fatalf("unexpected surface context: %#v", events[0].FeishuTargetPickerContext.Surface)
	}
}

func TestApplyFeishuUIIntentBuildsModeCatalog(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)

	events := svc.ApplyFeishuUIIntent(control.Action{
		Kind:             control.ActionModeCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/mode",
	}, control.FeishuUIIntent{
		Kind:    control.FeishuUIIntentShowModeCatalog,
		RawText: "/mode",
	})
	if len(events) != 1 {
		t.Fatalf("expected mode catalog event, got %#v", events)
	}
	catalog := commandCatalogFromEvent(t, events[0])
	if catalog.Title != "切换模式" {
		t.Fatalf("unexpected mode catalog: %#v", catalog)
	}
	if events[0].FeishuCommandView == nil || events[0].FeishuCommandView.Config == nil || events[0].FeishuCommandView.Config.CommandID != control.FeishuCommandMode {
		t.Fatalf("expected feishu command view for mode catalog, got %#v", events[0].FeishuCommandView)
	}
	if events[0].FeishuCommandContext == nil || events[0].FeishuCommandContext.DTOOwner != control.FeishuUIDTOwnerCommand || events[0].FeishuCommandContext.CommandID != control.FeishuCommandMode {
		t.Fatalf("expected feishu command context for mode catalog, got %#v", events[0].FeishuCommandContext)
	}
}

func TestApplyFeishuUIIntentBuildsVerboseCatalog(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)

	events := svc.ApplyFeishuUIIntent(control.Action{
		Kind:             control.ActionVerboseCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/verbose",
	}, control.FeishuUIIntent{
		Kind:    control.FeishuUIIntentShowVerboseCatalog,
		RawText: "/verbose",
	})
	if len(events) != 1 {
		t.Fatalf("expected verbose catalog event, got %#v", events)
	}
	catalog := commandCatalogFromEvent(t, events[0])
	if catalog.Title != "前端详细程度" {
		t.Fatalf("unexpected verbose catalog: %#v", catalog)
	}
	if events[0].FeishuCommandView == nil || events[0].FeishuCommandView.Config == nil || events[0].FeishuCommandView.Config.CommandID != control.FeishuCommandVerbose {
		t.Fatalf("expected feishu command view for verbose catalog, got %#v", events[0].FeishuCommandView)
	}
	if got := events[0].FeishuCommandView.Config.CurrentValue; got != string(state.SurfaceVerbosityNormal) {
		t.Fatalf("expected default verbosity current value, got %q", got)
	}
}

func TestApplySurfaceActionModeInvalidArgsReturnsCommandView(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModeCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/mode nope",
	})
	if len(events) != 1 || events[0].FeishuCommandView == nil || events[0].FeishuCommandView.Config == nil {
		t.Fatalf("expected invalid mode to reopen command view, got %#v", events)
	}
	if got := events[0].FeishuCommandView.Config.StatusKind; got != "error" {
		t.Fatalf("expected error status kind, got %#v", events[0].FeishuCommandView.Config)
	}
	if got := events[0].FeishuCommandView.Config.FormDefaultValue; got != "nope" {
		t.Fatalf("expected invalid mode args to stay in form, got %#v", events[0].FeishuCommandView.Config)
	}
	catalog := commandCatalogFromEvent(t, events[0])
	if catalog.Title != "切换模式" {
		t.Fatalf("unexpected mode error catalog: %#v", catalog)
	}
}

func TestApplySurfaceActionModelInvalidReasoningReturnsCommandView(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	surface := svc.root.Surfaces["surface-1"]
	surface.AttachedInstanceID = "inst-1"
	svc.root.Instances["inst-1"] = &state.InstanceRecord{InstanceID: "inst-1"}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModelCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/model gpt-5.4 nope",
	})
	if len(events) != 1 || events[0].FeishuCommandView == nil || events[0].FeishuCommandView.Config == nil {
		t.Fatalf("expected invalid model args to reopen command view, got %#v", events)
	}
	if got := events[0].FeishuCommandView.Config.StatusKind; got != "error" {
		t.Fatalf("expected error status kind, got %#v", events[0].FeishuCommandView.Config)
	}
	if got := events[0].FeishuCommandView.Config.FormDefaultValue; got != "gpt-5.4 nope" {
		t.Fatalf("expected invalid model args to stay in form, got %#v", events[0].FeishuCommandView.Config)
	}
	catalog := commandCatalogFromEvent(t, events[0])
	if catalog.Title != "模型" {
		t.Fatalf("unexpected model error catalog: %#v", catalog)
	}
}

func TestApplySurfaceActionVerboseCommandUpdatesSurface(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionVerboseCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/verbose quiet",
	})
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "surface_verbose_updated" {
		t.Fatalf("expected verbosity updated notice, got %#v", events)
	}
	surface := svc.root.Surfaces["surface-1"]
	if surface == nil {
		t.Fatal("expected surface to exist")
	}
	if surface.Verbosity != state.SurfaceVerbosityQuiet {
		t.Fatalf("expected surface verbosity quiet, got %q", surface.Verbosity)
	}
}

func TestQuietVerbosityHidesPlanButKeepsFinal(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoContinueSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityQuiet

	startRemoteTurnForAutoContinueTest(t, svc, "msg-1", "处理一下", "turn-1")

	planEvents := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventTurnPlanUpdated,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		PlanSnapshot: &agentproto.TurnPlanSnapshot{
			Explanation: "先分析问题。",
			Steps: []agentproto.TurnPlanStep{
				{Step: "分析", Status: agentproto.TurnPlanStepStatusInProgress},
			},
		},
	})
	if len(planEvents) != 0 {
		t.Fatalf("expected quiet verbosity to suppress plan events, got %#v", planEvents)
	}

	finished := completeRemoteTurnWithFinalText(t, svc, "turn-1", "completed", "", "最终结果", nil)
	foundFinal := false
	for _, event := range finished {
		if event.Kind == control.UIEventBlockCommitted && event.Block != nil && event.Block.Final && event.Block.Text == "最终结果" {
			foundFinal = true
		}
		if event.Kind == control.UIEventPlanUpdated {
			t.Fatalf("did not expect quiet verbosity to leak plan event in final sequence: %#v", finished)
		}
	}
	if !foundFinal {
		t.Fatalf("expected final block to remain visible, got %#v", finished)
	}
}

func TestNormalVerbosityKeepsPlanUpdates(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoContinueSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityNormal

	startRemoteTurnForAutoContinueTest(t, svc, "msg-1", "处理一下", "turn-1")

	planEvents := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventTurnPlanUpdated,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		PlanSnapshot: &agentproto.TurnPlanSnapshot{
			Explanation: "先分析问题。",
			Steps: []agentproto.TurnPlanStep{
				{Step: "分析", Status: agentproto.TurnPlanStepStatusInProgress},
			},
		},
	})
	if len(planEvents) != 1 || planEvents[0].Kind != control.UIEventPlanUpdated {
		t.Fatalf("expected normal verbosity to keep plan event, got %#v", planEvents)
	}
	if planEvents[0].SourceMessageID != "msg-1" {
		t.Fatalf("expected plan update to inherit turn reply anchor, got %#v", planEvents[0])
	}
}

func TestVerbosityFilterNeverDropsDaemonOrAgentCommands(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	svc.root.Surfaces["surface-1"].Verbosity = state.SurfaceVerbosityQuiet

	events := svc.filterEventsForSurfaceVisibility([]control.UIEvent{
		{
			Kind:             control.UIEventAgentCommand,
			SurfaceSessionID: "surface-1",
			Command:          &agentproto.Command{Kind: agentproto.CommandPromptSend},
		},
		{
			Kind:             control.UIEventDaemonCommand,
			SurfaceSessionID: "surface-1",
			DaemonCommand:    &control.DaemonCommand{Kind: control.DaemonCommandDebug},
		},
	})
	if len(events) != 2 {
		t.Fatalf("expected control-flow commands to bypass verbosity filter, got %#v", events)
	}
}

func TestAttachBusyInstanceRejectsSecondSurface(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
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
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionModeCommand, SurfaceSessionID: "surface-2", ChatID: "chat-2", ActorUserID: "user-2", Text: "/mode vscode"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-2",
		ChatID:           "chat-2",
		ActorUserID:      "user-2",
		InstanceID:       "inst-1",
	})

	surface := svc.root.Surfaces["surface-2"]
	if surface.AttachedInstanceID != "" || surface.SelectedThreadID != "" || surface.RouteMode != state.RouteModeUnbound {
		t.Fatalf("expected second surface to remain detached, got attached=%q selected=%q route=%q", surface.AttachedInstanceID, surface.SelectedThreadID, surface.RouteMode)
	}
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "instance_busy" {
		t.Fatalf("expected instance_busy notice, got %#v", events)
	}
}

func TestAttachWithoutDefaultThreadEntersUnboundAndPromptsUse(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.AttachedInstanceID != "inst-1" || surface.SelectedThreadID != "" || surface.RouteMode != state.RouteModeUnbound {
		t.Fatalf("expected surface to enter attached_unbound, got attached=%q selected=%q route=%q", surface.AttachedInstanceID, surface.SelectedThreadID, surface.RouteMode)
	}
	var sawNotice, sawPrompt bool
	for _, event := range events {
		if event.Notice != nil && event.Notice.Code == "attached" && strings.Contains(event.Notice.Text, "/use") {
			sawNotice = true
		}
		if prompt, ok := eventSelectionPrompt(event); ok && prompt.Kind == control.SelectionPromptUseThread {
			sawPrompt = true
		}
	}
	if !sawNotice || !sawPrompt {
		t.Fatalf("expected attach notice plus /use prompt, got %#v", events)
	}
}

func TestAttachWorkspaceEntersUnboundAndPromptsUse(t *testing.T) {
	now := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
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

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachWorkspace,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		WorkspaceKey:     "/data/dl/droid",
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.AttachedInstanceID != "inst-1" || !testutil.SamePath(surface.ClaimedWorkspaceKey, "/data/dl/droid") {
		t.Fatalf("expected workspace attach to claim droid, got %#v", surface)
	}
	if surface.SelectedThreadID != "" || surface.RouteMode != state.RouteModeUnbound {
		t.Fatalf("expected workspace attach to land unbound, got selected=%q route=%q", surface.SelectedThreadID, surface.RouteMode)
	}
	var sawNotice, sawPrompt bool
	for _, event := range events {
		if event.Notice != nil && event.Notice.Code == "workspace_attached" && strings.Contains(event.Notice.Text, "/use") {
			sawNotice = true
		}
		if prompt, ok := eventSelectionPrompt(event); ok && prompt.Kind == control.SelectionPromptUseThread {
			sawPrompt = true
		}
	}
	if !sawNotice || !sawPrompt {
		t.Fatalf("expected workspace attach notice plus /use prompt, got %#v", events)
	}
}

func TestAttachWorkspaceSwitchClearsPinnedThread(t *testing.T) {
	now := time.Date(2026, 4, 9, 12, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
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
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-2",
		DisplayName:             "web",
		WorkspaceRoot:           "/data/dl/web",
		WorkspaceKey:            "/data/dl/web",
		ShortName:               "web",
		Online:                  true,
		ObservedFocusedThreadID: "thread-2",
		Threads: map[string]*state.ThreadRecord{
			"thread-2": {ThreadID: "thread-2", Name: "修样式", CWD: "/data/dl/web"},
		},
	})

	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachWorkspace,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		WorkspaceKey:     "/data/dl/droid",
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ThreadID:         "thread-1",
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachWorkspace,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		WorkspaceKey:     "/data/dl/web",
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.AttachedInstanceID != "inst-2" || !testutil.SamePath(surface.ClaimedWorkspaceKey, "/data/dl/web") {
		t.Fatalf("expected workspace switch to move to web, got %#v", surface)
	}
	if surface.SelectedThreadID != "" || surface.RouteMode != state.RouteModeUnbound {
		t.Fatalf("expected workspace switch to clear pinned thread, got selected=%q route=%q", surface.SelectedThreadID, surface.RouteMode)
	}
	var sawNotice, sawPrompt bool
	for _, event := range events {
		if event.Notice != nil && event.Notice.Code == "workspace_switched" && strings.Contains(event.Notice.Text, "/use") {
			sawNotice = true
		}
		if prompt, ok := eventSelectionPrompt(event); ok && prompt.Kind == control.SelectionPromptUseThread {
			sawPrompt = true
		}
	}
	if !sawNotice || !sawPrompt {
		t.Fatalf("expected workspace switch notice plus /use prompt, got %#v", events)
	}
}

func TestAttachWorkspaceSwitchBlockedByQueuedWork(t *testing.T) {
	now := time.Date(2026, 4, 9, 12, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
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
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-2",
		DisplayName:   "web",
		WorkspaceRoot: "/data/dl/web",
		WorkspaceKey:  "/data/dl/web",
		ShortName:     "web",
		Online:        true,
	})

	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachWorkspace,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		WorkspaceKey:     "/data/dl/droid",
	})
	surface := svc.root.Surfaces["surface-1"]
	surface.QueueItems["item-1"] = &state.QueueItemRecord{ID: "item-1", Status: state.QueueItemQueued}
	surface.QueuedQueueItemIDs = []string{"item-1"}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachWorkspace,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		WorkspaceKey:     "/data/dl/web",
	})

	if surface.AttachedInstanceID != "inst-1" || !testutil.SamePath(surface.ClaimedWorkspaceKey, "/data/dl/droid") {
		t.Fatalf("expected blocked switch to keep current workspace, got %#v", surface)
	}
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "thread_switch_queued" {
		t.Fatalf("expected queued switch block notice, got %#v", events)
	}
}

func TestListWorkspacesMarksBusyClaimedWorkspaceDisabled(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
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
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-2",
		DisplayName:   "web",
		WorkspaceRoot: "/data/dl/web",
		WorkspaceKey:  "/data/dl/web",
		ShortName:     "web",
		Source:        "vscode",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-2": {ThreadID: "thread-2", Name: "修样式", CWD: "/data/dl/web"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "surface-2",
		ChatID:           "chat-2",
		ActorUserID:      "user-2",
	})

	if len(events) != 1 {
		t.Fatalf("expected one target picker event, got %#v", events)
	}
	view := targetPickerFromEvent(t, events[0])
	if view.Source != control.TargetPickerRequestSourceList || len(view.WorkspaceOptions) != 1 {
		t.Fatalf("unexpected target picker view: %#v", view)
	}
	if view.SelectedWorkspaceKey != "/data/dl/web" {
		t.Fatalf("expected only free workspace to remain selectable, got %#v", view)
	}
	if _, ok := targetPickerModeOption(view, control.FeishuTargetPickerModeAddWorkspace); !ok || !view.ShowModeSwitch {
		t.Fatalf("expected add-workspace mode switch to appear in list picker, got %#v", view)
	}
	freeOption, ok := targetPickerWorkspaceOption(view, "/data/dl/web")
	if !ok || strings.Contains(freeOption.MetaText, "当前被其他飞书会话接管") {
		t.Fatalf("expected busy workspace to be omitted from picker, got %#v", view.WorkspaceOptions)
	}
	if _, ok := targetPickerSessionOption(view, targetPickerThreadValue("thread-2")); !ok {
		t.Fatalf("expected free workspace session to stay selectable, got %#v", view.SessionOptions)
	}
	if _, ok := targetPickerSessionOption(view, targetPickerNewThreadValue); !ok {
		t.Fatalf("expected new-thread option for free workspace, got %#v", view.SessionOptions)
	}
}

func TestListWorkspacesShowsCurrentSummaryAndSortsAttachableFirst(t *testing.T) {
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-droid",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-current": {ThreadID: "thread-current", Name: "当前工作", CWD: "/data/dl/droid", LastUsedAt: now.Add(-5 * time.Minute)},
		},
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-web",
		DisplayName:             "web",
		WorkspaceRoot:           "/data/dl/web",
		WorkspaceKey:            "/data/dl/web",
		ShortName:               "web",
		Online:                  true,
		ObservedFocusedThreadID: "thread-web",
		Threads: map[string]*state.ThreadRecord{
			"thread-web": {ThreadID: "thread-web", Name: "整理样式", CWD: "/data/dl/web", LastUsedAt: now.Add(-2 * time.Minute)},
		},
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-ops",
		DisplayName:   "ops",
		WorkspaceRoot: "/data/dl/ops",
		WorkspaceKey:  "/data/dl/ops",
		ShortName:     "ops",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-ops": {ThreadID: "thread-ops", Name: "值班处理", CWD: "/data/dl/ops", LastUsedAt: now.Add(-1 * time.Hour)},
		},
	})

	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachWorkspace,
		SurfaceSessionID: "surface-current",
		ChatID:           "chat-current",
		ActorUserID:      "user-current",
		WorkspaceKey:     "/data/dl/droid",
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachWorkspace,
		SurfaceSessionID: "surface-busy",
		ChatID:           "chat-busy",
		ActorUserID:      "user-busy",
		WorkspaceKey:     "/data/dl/ops",
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "surface-current",
		ChatID:           "chat-current",
		ActorUserID:      "user-current",
	})

	if len(events) != 1 {
		t.Fatalf("expected one target picker event, got %#v", events)
	}
	view := targetPickerFromEvent(t, events[0])
	if view.Source != control.TargetPickerRequestSourceList || view.SelectedWorkspaceKey != "/data/dl/droid" {
		t.Fatalf("expected current workspace to stay selected by default, got %#v", view)
	}
	if len(view.WorkspaceOptions) != 2 {
		t.Fatalf("expected busy workspace to be omitted while attachable/current stay visible, got %#v", view.WorkspaceOptions)
	}
	webOption, ok := targetPickerWorkspaceOption(view, "/data/dl/web")
	if !ok || !strings.Contains(webOption.MetaText, "有 VS Code 活动") {
		t.Fatalf("expected recent attachable workspace to stay visible, got %#v", view.WorkspaceOptions)
	}
	if _, ok := targetPickerWorkspaceOption(view, "/data/dl/droid"); !ok {
		t.Fatalf("expected current workspace to remain in options, got %#v", view.WorkspaceOptions)
	}
	if _, ok := targetPickerSessionOption(view, targetPickerThreadValue("thread-current")); !ok {
		t.Fatalf("expected current workspace sessions to populate default picker view, got %#v", view.SessionOptions)
	}
}

func TestListWorkspacesUsesVisibleThreadCWDsForBroadHeadlessPool(t *testing.T) {
	now := time.Date(2026, 4, 9, 19, 30, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	for i := 1; i <= 2; i++ {
		svc.UpsertInstance(&state.InstanceRecord{
			InstanceID:    fmt.Sprintf("inst-headless-%d", i),
			DisplayName:   fmt.Sprintf("headless-%d", i),
			WorkspaceRoot: "/data/dl",
			WorkspaceKey:  "/data/dl",
			ShortName:     "dl",
			Source:        "headless",
			Managed:       true,
			Online:        true,
			Threads: map[string]*state.ThreadRecord{
				fmt.Sprintf("thread-fs-%d", i): {ThreadID: fmt.Sprintf("thread-fs-%d", i), Name: "atlas", CWD: "/data/dl/atlas", Loaded: true},
				fmt.Sprintf("thread-sf-%d", i): {ThreadID: fmt.Sprintf("thread-sf-%d", i), Name: "harbor", CWD: "/data/dl/harbor", Loaded: true},
			},
		})
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	if len(events) != 1 {
		t.Fatalf("expected one target picker event, got %#v", events)
	}
	view := targetPickerFromEvent(t, events[0])
	if view.Source != control.TargetPickerRequestSourceList || view.Title != "选择工作区与会话" {
		t.Fatalf("unexpected target picker view: %#v", view)
	}
	if len(view.WorkspaceOptions) != 2 {
		t.Fatalf("expected two real workspaces instead of broad instance root, got %#v", view.WorkspaceOptions)
	}
	got := map[string]bool{}
	for _, option := range view.WorkspaceOptions {
		if option.Synthetic {
			continue
		}
		got[option.Value] = true
	}
	if !got[testutil.WorkspacePath("data", "dl", "atlas")] || !got[testutil.WorkspacePath("data", "dl", "harbor")] || got[testutil.WorkspacePath("data", "dl")] {
		t.Fatalf("expected thread cwd workspaces only, got %#v", view.WorkspaceOptions)
	}
}

func TestListWorkspacesShowsPersistedOnlyWorkspaceAsRecoverable(t *testing.T) {
	now := time.Date(2026, 4, 10, 14, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.SetPersistedThreadCatalog(&fakePersistedThreadCatalog{
		recent: []state.ThreadRecord{
			{
				ThreadID:   "thread-picdetect",
				Name:       "排查图片识别",
				Preview:    "sqlite only",
				CWD:        "/data/dl/picdetect",
				Loaded:     true,
				LastUsedAt: now.Add(-3 * time.Minute),
			},
		},
		byID: map[string]state.ThreadRecord{
			"thread-picdetect": {
				ThreadID:   "thread-picdetect",
				Name:       "排查图片识别",
				Preview:    "sqlite only",
				CWD:        "/data/dl/picdetect",
				Loaded:     true,
				LastUsedAt: now.Add(-3 * time.Minute),
			},
		},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	if len(events) != 1 {
		t.Fatalf("expected one target picker event, got %#v", events)
	}
	view := targetPickerFromEvent(t, events[0])
	if len(view.WorkspaceOptions) != 1 {
		t.Fatalf("expected one recoverable workspace option, got %#v", view.WorkspaceOptions)
	}
	option, ok := targetPickerWorkspaceOption(view, "/data/dl/picdetect")
	if !ok {
		t.Fatalf("expected persisted-only workspace to remain visible, got %#v", view.WorkspaceOptions)
	}
	if !testutil.SamePath(option.Value, "/data/dl/picdetect") || !option.RecoverableOnly {
		t.Fatalf("expected persisted-only workspace to remain recoverable-only, got %#v", option)
	}
	if option.MetaText != "3分前 · 后台可恢复" {
		t.Fatalf("expected recoverable workspace meta, got %#v", option.MetaText)
	}
	if view.SelectedWorkspaceKey != "/data/dl/picdetect" {
		t.Fatalf("expected recoverable workspace to be selected, got %#v", view)
	}
	if _, ok := targetPickerSessionOption(view, targetPickerThreadValue("thread-picdetect")); !ok {
		t.Fatalf("expected persisted thread to stay selectable, got %#v", view.SessionOptions)
	}
}

func TestBuildWorkspaceSelectionModelFallsBackToLastGoodPersistedSnapshot(t *testing.T) {
	now := time.Date(2026, 4, 14, 9, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	catalog := &fakePersistedThreadCatalog{
		recent: []state.ThreadRecord{
			{
				ThreadID:   "thread-picdetect",
				Name:       "排查图片识别",
				CWD:        "/data/dl/picdetect",
				LastUsedAt: now.Add(-2 * time.Minute),
			},
		},
		recentWorkspaces: map[string]time.Time{
			"/data/dl/picdetect": now.Add(-2 * time.Minute),
		},
		byID: map[string]state.ThreadRecord{},
	}
	svc.SetPersistedThreadCatalog(catalog)

	surface := svc.ensureSurface(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	model, events := svc.buildWorkspaceSelectionModel(surface, 1)
	if len(events) != 0 || model == nil {
		t.Fatalf("expected initial workspace selection model, got model=%#v events=%#v", model, events)
	}

	catalog.recentErr = errors.New("database is locked")
	catalog.recentWorkspacesErr = errors.New("database is locked")
	model, events = svc.buildWorkspaceSelectionModel(surface, 1)
	if len(events) != 0 || model == nil {
		t.Fatalf("expected fallback workspace selection model, got model=%#v events=%#v", model, events)
	}

	found := false
	for i := range model.Entries {
		if testutil.SamePath(model.Entries[i].WorkspaceKey, "/data/dl/picdetect") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected fallback snapshot to keep persisted workspace visible, got %#v", model.Entries)
	}
}

func TestShowAllWorkspacesUsesSamePagedWorkspacePrompt(t *testing.T) {
	now := time.Date(2026, 4, 10, 14, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	for i := 0; i < 6; i++ {
		key := fmt.Sprintf("/data/dl/proj-%d", i)
		svc.UpsertInstance(&state.InstanceRecord{
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
					LastUsedAt: now.Add(-time.Duration(i) * time.Minute),
				},
			},
		})
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowAllWorkspaces,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	if len(events) != 1 {
		t.Fatalf("expected one target picker event, got %#v", events)
	}
	view := targetPickerFromEvent(t, events[0])
	if view.Source != control.TargetPickerRequestSourceList || view.Title != "选择工作区与会话" {
		t.Fatalf("unexpected target picker view: %#v", view)
	}
	if len(view.WorkspaceOptions) != 6 {
		t.Fatalf("expected all workspaces in a single target picker, got %#v", view.WorkspaceOptions)
	}
}

func TestSendFileActionOpensFilePickerInsideCurrentWorkspace(t *testing.T) {
	now := time.Date(2026, 4, 14, 9, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	workspaceRoot := t.TempDir()
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "headless",
		WorkspaceRoot: workspaceRoot,
		WorkspaceKey:  workspaceRoot,
		Source:        "headless",
		Managed:       true,
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionSendFile,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	if len(events) != 1 || events[0].FeishuPathPickerView == nil {
		t.Fatalf("expected file picker event, got %#v", events)
	}
	view := events[0].FeishuPathPickerView
	if view.Mode != control.PathPickerModeFile || view.Title != "选择要发送的文件" || !testutil.SamePath(view.RootPath, workspaceRoot) {
		t.Fatalf("unexpected send-file picker view: %#v", view)
	}
}

func TestSendFileActionRequiresAttachedWorkspace(t *testing.T) {
	now := time.Date(2026, 4, 14, 9, 6, 0, 0, time.UTC)
	svc := newServiceForTest(&now)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionSendFile,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "send_file_requires_workspace" {
		t.Fatalf("expected requires-workspace notice, got %#v", events)
	}
}

func TestSendFilePickerConfirmDispatchesDaemonCommand(t *testing.T) {
	now := time.Date(2026, 4, 14, 9, 7, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	workspaceRoot := t.TempDir()
	filePath := filepath.Join(workspaceRoot, "report.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "headless",
		WorkspaceRoot: workspaceRoot,
		WorkspaceKey:  workspaceRoot,
		Source:        "headless",
		Managed:       true,
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})
	openEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionSendFile,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	view := openEvents[0].FeishuPathPickerView
	selectEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionPathPickerSelect,
		SurfaceSessionID: "surface-1",
		ActorUserID:      "user-1",
		PickerID:         view.PickerID,
		PickerEntry:      "report.txt",
	})
	if len(selectEvents) != 1 || selectEvents[0].FeishuPathPickerView == nil || !selectEvents[0].FeishuPathPickerView.CanConfirm {
		t.Fatalf("expected selectable file picker state, got %#v", selectEvents)
	}
	confirmEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionPathPickerConfirm,
		SurfaceSessionID: "surface-1",
		ActorUserID:      "user-1",
		PickerID:         view.PickerID,
	})
	if len(confirmEvents) != 1 || confirmEvents[0].DaemonCommand == nil {
		t.Fatalf("expected daemon command after picker confirm, got %#v", confirmEvents)
	}
	command := confirmEvents[0].DaemonCommand
	if command.Kind != control.DaemonCommandSendIMFile || !testutil.SamePath(command.LocalPath, filePath) {
		t.Fatalf("unexpected send-file daemon command: %#v", command)
	}
}

func TestFreshWorkspaceHeadlessConnectsAndAllowsFirstTextToCreateThread(t *testing.T) {
	now := time.Date(2026, 4, 14, 9, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := svc.ensureSurface(control.Action{
		Kind:             control.ActionStatus,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	workspaceRoot := t.TempDir()

	startEvents := svc.startFreshWorkspaceHeadless(surface, workspaceRoot)
	if len(startEvents) != 2 || startEvents[1].DaemonCommand == nil || startEvents[1].DaemonCommand.Kind != control.DaemonCommandStartHeadless {
		t.Fatalf("expected fresh workspace headless start, got %#v", startEvents)
	}
	pending := svc.root.Surfaces["surface-1"].PendingHeadless
	if pending == nil || pending.Purpose != state.HeadlessLaunchPurposeFreshWorkspace || !testutil.SamePath(pending.ThreadCWD, workspaceRoot) {
		t.Fatalf("unexpected pending workspace launch: %#v", pending)
	}

	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    pending.InstanceID,
		DisplayName:   "headless",
		WorkspaceRoot: workspaceRoot,
		WorkspaceKey:  workspaceRoot,
		Source:        "headless",
		Managed:       true,
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	connectEvents := svc.ApplyInstanceConnected(pending.InstanceID)
	if len(connectEvents) == 0 {
		t.Fatal("expected workspace attach notice after headless connect")
	}
	snapshot := svc.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.Attachment.InstanceID != pending.InstanceID || snapshot.PendingHeadless.InstanceID != "" || !testutil.SamePath(snapshot.WorkspaceKey, workspaceRoot) {
		t.Fatalf("expected attached fresh workspace snapshot, got %#v", snapshot)
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-1",
		Text:             "开始处理",
		Inputs:           []agentproto.Input{{Type: agentproto.InputText, Text: "开始处理"}},
	})
	var promptSend *agentproto.Command
	for _, event := range events {
		if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend {
			promptSend = event.Command
		}
	}
	if promptSend == nil || !promptSend.Target.CreateThreadIfMissing || !testutil.SamePath(promptSend.Target.CWD, workspaceRoot) {
		t.Fatalf("expected first text to create thread in fresh workspace, got %#v", events)
	}
}

func TestAttachWorkspaceUsesThreadWorkspaceFromBroadHeadlessPool(t *testing.T) {
	now := time.Date(2026, 4, 9, 19, 35, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-headless-1",
		DisplayName:   "headless-1",
		WorkspaceRoot: "/data/dl",
		WorkspaceKey:  "/data/dl",
		ShortName:     "dl",
		Source:        "headless",
		Managed:       true,
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-fs": {ThreadID: "thread-fs", Name: "修复 relay", CWD: "/data/dl/atlas", Loaded: true},
			"thread-sf": {ThreadID: "thread-sf", Name: "整理 harbor", CWD: "/data/dl/harbor", Loaded: true},
		},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachWorkspace,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		WorkspaceKey:     "/data/dl/atlas",
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.AttachedInstanceID != "inst-headless-1" || !testutil.SamePath(surface.ClaimedWorkspaceKey, "/data/dl/atlas") {
		t.Fatalf("expected workspace attach to resolve via thread cwd, got %#v", surface)
	}
	if surface.SelectedThreadID != "" || surface.RouteMode != state.RouteModeUnbound {
		t.Fatalf("expected broad-pool workspace attach to remain unbound, got %#v", surface)
	}
	var threadPrompt *control.FeishuDirectSelectionPrompt
	for _, event := range events {
		if prompt, ok := eventSelectionPrompt(event); ok && prompt.Kind == control.SelectionPromptUseThread {
			threadPrompt = prompt
			break
		}
	}
	if threadPrompt == nil || len(threadPrompt.Options) != 1 || threadPrompt.Options[0].OptionID != "thread-fs" {
		t.Fatalf("expected /use prompt to be scoped to selected workspace, got %#v", events)
	}
}

func TestShowWorkspaceThreadsSupportsPersistedOnlyWorkspace(t *testing.T) {
	now := time.Date(2026, 4, 10, 14, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.SetPersistedThreadCatalog(&fakePersistedThreadCatalog{
		recent: []state.ThreadRecord{
			{
				ThreadID:   "thread-picdetect",
				Name:       "最新排查",
				Preview:    "sqlite only",
				CWD:        "/data/dl/picdetect",
				Loaded:     true,
				LastUsedAt: now.Add(-2 * time.Minute),
			},
			{
				ThreadID:   "thread-other",
				Name:       "其他工作区",
				Preview:    "other",
				CWD:        "/data/dl/other",
				Loaded:     true,
				LastUsedAt: now.Add(-1 * time.Minute),
			},
		},
		byID: map[string]state.ThreadRecord{
			"thread-picdetect": {
				ThreadID:   "thread-picdetect",
				Name:       "最新排查",
				Preview:    "sqlite only",
				CWD:        "/data/dl/picdetect",
				Loaded:     true,
				LastUsedAt: now.Add(-2 * time.Minute),
			},
		},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowWorkspaceThreads,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		WorkspaceKey:     "/data/dl/picdetect",
	})

	if len(events) != 1 {
		t.Fatalf("expected workspace target picker, got %#v", events)
	}
	view := targetPickerFromEvent(t, events[0])
	if view.Source != control.TargetPickerRequestSourceWorkspace || !testutil.SamePath(view.SelectedWorkspaceKey, "/data/dl/picdetect") {
		t.Fatalf("unexpected persisted-only workspace target picker: %#v", view)
	}
	if view.Page != control.FeishuTargetPickerPageMode || !view.ShowModeSwitch || !view.CanConfirm || view.ConfirmLabel != "下一步" {
		t.Fatalf("expected persisted-only workspace picker to start on mode page, got %#v", view)
	}
	if _, ok := targetPickerSessionOption(view, targetPickerThreadValue("thread-picdetect")); !ok {
		t.Fatalf("expected persisted-only thread option to remain recoverable, got %#v", view.SessionOptions)
	}
}

func TestUseBusyIdleThreadShowsKickPromptAndConfirmTransfersClaim(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
			"thread-2": {ThreadID: "thread-2", Name: "整理日志", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.root.Surfaces["surface-2"] = &state.SurfaceConsoleRecord{
		SurfaceSessionID:   "surface-2",
		ProductMode:        state.ProductModeVSCode,
		AttachedInstanceID: "inst-1",
		RouteMode:          state.RouteModeUnbound,
		QueueItems:         map[string]*state.QueueItemRecord{},
		StagedImages:       map[string]*state.StagedImageRecord{},
		PendingRequests:    map[string]*state.RequestPromptRecord{},
	}
	svc.instanceClaims["inst-1"] = &instanceClaimRecord{InstanceID: "inst-1", SurfaceSessionID: "surface-1"}

	promptEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-2",
		ThreadID:         "thread-1",
	})
	if len(promptEvents) != 1 {
		t.Fatalf("expected kick confirmation prompt, got %#v", promptEvents)
	}
	if prompt := selectionPromptFromEvent(t, promptEvents[0]); prompt.Kind != control.SelectionPromptKickThread {
		t.Fatalf("expected kick confirmation prompt, got %#v", promptEvents)
	}

	confirm := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionConfirmKickThread,
		SurfaceSessionID: "surface-2",
		ThreadID:         "thread-1",
	})

	first := svc.root.Surfaces["surface-1"]
	second := svc.root.Surfaces["surface-2"]
	if first.SelectedThreadID != "" || first.RouteMode != state.RouteModeUnbound {
		t.Fatalf("expected victim surface to become unbound, got selected=%q route=%q", first.SelectedThreadID, first.RouteMode)
	}
	if second.SelectedThreadID != "thread-1" || second.RouteMode != state.RouteModeFollowLocal {
		t.Fatalf("expected requesting surface to claim thread-1, got selected=%q route=%q", second.SelectedThreadID, second.RouteMode)
	}
	var sawVictimNotice, sawWinnerNotice bool
	for _, event := range confirm {
		if event.SurfaceSessionID == "surface-1" && event.Notice != nil && event.Notice.Code == "thread_claim_lost" {
			sawVictimNotice = true
		}
		if event.SurfaceSessionID == "surface-2" && event.Notice != nil && event.Notice.Code == "thread_kicked" {
			sawWinnerNotice = true
		}
	}
	if !sawVictimNotice || !sawWinnerNotice {
		t.Fatalf("expected kick notices for both surfaces, got %#v", confirm)
	}
}

func TestUseBusyRunningThreadRejectsKick(t *testing.T) {
	now := time.Date(2026, 4, 4, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
			"thread-2": {ThreadID: "thread-2", Name: "整理日志", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.root.Surfaces["surface-2"] = &state.SurfaceConsoleRecord{
		SurfaceSessionID:   "surface-2",
		ProductMode:        state.ProductModeVSCode,
		AttachedInstanceID: "inst-1",
		RouteMode:          state.RouteModeUnbound,
		QueueItems:         map[string]*state.QueueItemRecord{},
		StagedImages:       map[string]*state.StagedImageRecord{},
		PendingRequests:    map[string]*state.RequestPromptRecord{},
	}
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "你好",
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-2",
		ThreadID:         "thread-1",
	})
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "thread_busy_running" {
		t.Fatalf("expected busy running thread to reject kick, got %#v", events)
	}
}

func TestNormalModeListWithoutOnlineWorkspacesShowsCreateWorkspacePicker(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "feishu:app-1:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	if len(events) != 1 || events[0].FeishuTargetPickerView == nil {
		t.Fatalf("expected one target picker event, got %#v", events)
	}
	view := targetPickerFromEvent(t, events[0])
	if view.Source != control.TargetPickerRequestSourceList {
		t.Fatalf("expected /list picker source, got %#v", view)
	}
	if len(view.WorkspaceOptions) != 0 {
		t.Fatalf("expected no existing workspace options when runtime is empty, got %#v", view.WorkspaceOptions)
	}
	if view.SelectedMode != control.FeishuTargetPickerModeAddWorkspace || view.SelectedSource != control.FeishuTargetPickerSourceLocalDirectory {
		t.Fatalf("expected empty runtime to preselect add-workspace/local-directory flow, got %#v", view)
	}
	if view.Page != control.FeishuTargetPickerPageMode || !view.ShowModeSwitch || !view.CanConfirm || view.ConfirmLabel != "下一步" {
		t.Fatalf("expected empty runtime to stay on mode page instead of auto-jumping, got %#v", view)
	}
}

func TestVSCodeModeListWithoutOnlineInstancesReturnsNotice(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModeCommand,
		SurfaceSessionID: "feishu:app-1:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/mode vscode",
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "feishu:app-1:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "no_online_instances" {
		t.Fatalf("expected no_online_instances notice, got %#v", events)
	}
	if !strings.Contains(events[0].Notice.Text, "当前没有在线 VS Code 实例") {
		t.Fatalf("expected vscode-specific empty state notice, got %#v", events[0].Notice)
	}
}

func TestStatusMaterializesSurfaceWithDefaultNormalMode(t *testing.T) {
	now := time.Date(2026, 4, 9, 10, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionStatus,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	if len(events) != 1 || events[0].Snapshot == nil {
		t.Fatalf("expected one snapshot event, got %#v", events)
	}
	surface := svc.root.Surfaces["surface-1"]
	if surface == nil {
		t.Fatal("expected surface to be materialized")
	}
	if surface.ProductMode != state.ProductModeNormal {
		t.Fatalf("expected default product mode normal, got %q", surface.ProductMode)
	}
	if events[0].Snapshot.ProductMode != "normal" {
		t.Fatalf("expected snapshot product mode normal, got %#v", events[0].Snapshot)
	}
}

func TestModeCommandSwitchesDetachedSurface(t *testing.T) {
	now := time.Date(2026, 4, 9, 10, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionStatus, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1"})
	surface := svc.root.Surfaces["surface-1"]
	surface.PromptOverride = state.ModelConfigRecord{Model: "gpt-5.4"}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModeCommand,
		SurfaceSessionID: "surface-1",
		Text:             "/mode vscode",
	})

	if surface.ProductMode != state.ProductModeVSCode {
		t.Fatalf("expected product mode vscode, got %q", surface.ProductMode)
	}
	if surface.AttachedInstanceID != "" || surface.SelectedThreadID != "" || surface.RouteMode != state.RouteModeUnbound {
		t.Fatalf("expected detached unbound surface after mode switch, got %#v", surface)
	}
	if surface.PromptOverride != (state.ModelConfigRecord{}) {
		t.Fatalf("expected prompt override to be cleared, got %#v", surface.PromptOverride)
	}
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "surface_mode_switched" {
		t.Fatalf("expected surface_mode_switched notice, got %#v", events)
	}
}

func TestModeCommandDetachesIdleAttachmentBeforeSwitching(t *testing.T) {
	now := time.Date(2026, 4, 9, 10, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
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
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModeCommand,
		SurfaceSessionID: "surface-1",
		Text:             "/mode vscode",
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.ProductMode != state.ProductModeVSCode {
		t.Fatalf("expected product mode vscode, got %q", surface.ProductMode)
	}
	if surface.AttachedInstanceID != "" || surface.SelectedThreadID != "" || surface.RouteMode != state.RouteModeUnbound {
		t.Fatalf("expected mode switch to detach attached surface, got %#v", surface)
	}
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "surface_mode_switched" {
		t.Fatalf("expected surface_mode_switched notice, got %#v", events)
	}
}

func TestModeCommandCancelsPendingHeadlessBeforeSwitching(t *testing.T) {
	now := time.Date(2026, 4, 9, 10, 11, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
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
	start := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		ThreadID:         "thread-1",
	})
	if len(start) != 2 || start[1].DaemonCommand == nil || start[1].DaemonCommand.Kind != control.DaemonCommandStartHeadless {
		t.Fatalf("expected detached /use to start headless launch, got %#v", start)
	}
	pending := svc.root.Surfaces["surface-1"].PendingHeadless
	if pending == nil {
		t.Fatal("expected pending headless before mode switch")
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModeCommand,
		SurfaceSessionID: "surface-1",
		Text:             "/mode vscode",
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.ProductMode != state.ProductModeVSCode {
		t.Fatalf("expected product mode vscode, got %q", surface.ProductMode)
	}
	if surface.AttachedInstanceID != "" || surface.SelectedThreadID != "" || surface.RouteMode != state.RouteModeUnbound || surface.PendingHeadless != nil {
		t.Fatalf("expected mode switch to fully clear pending headless state, got %#v", surface)
	}
	if len(events) != 2 || events[0].DaemonCommand == nil || events[0].DaemonCommand.Kind != control.DaemonCommandKillHeadless || events[1].Notice == nil || events[1].Notice.Code != "surface_mode_switched" {
		t.Fatalf("expected kill + switched notice, got %#v", events)
	}
	if events[0].DaemonCommand.InstanceID != pending.InstanceID || events[0].DaemonCommand.ThreadID != pending.ThreadID {
		t.Fatalf("expected mode switch to kill pending headless launch, got %#v", events[0].DaemonCommand)
	}
}

func TestModeCommandRejectsSwitchWhileWorkIsRunning(t *testing.T) {
	now := time.Date(2026, 4, 9, 10, 15, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
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
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "你好",
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModeCommand,
		SurfaceSessionID: "surface-1",
		Text:             "/mode vscode",
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.ProductMode != state.ProductModeNormal {
		t.Fatalf("expected mode to remain normal while busy, got %q", surface.ProductMode)
	}
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "surface_mode_busy" {
		t.Fatalf("expected surface_mode_busy notice, got %#v", events)
	}
}

func TestVSCodeAttachCanSwitchInstanceWithoutDetach(t *testing.T) {
	now := time.Date(2026, 4, 9, 11, 13, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	materializeVSCodeSurfaceForTest(svc, "surface-1")
	svc.UpsertInstance(&state.InstanceRecord{
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
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-2",
		DisplayName:             "web",
		WorkspaceRoot:           "/data/dl/web",
		WorkspaceKey:            "/data/dl/web",
		ShortName:               "web",
		Online:                  true,
		ObservedFocusedThreadID: "thread-2",
		Threads: map[string]*state.ThreadRecord{
			"thread-2": {ThreadID: "thread-2", Name: "整理样式", CWD: "/data/dl/web", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionImageMessage, SurfaceSessionID: "surface-1", MessageID: "msg-img", LocalPath: "/tmp/img.png", MIMEType: "image/png"})
	svc.root.Surfaces["surface-1"].PromptOverride = state.ModelConfigRecord{Model: "gpt-5.4"}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-2",
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.AttachedInstanceID != "inst-2" || surface.SelectedThreadID != "thread-2" || surface.RouteMode != state.RouteModeFollowLocal {
		t.Fatalf("expected vscode attach switch to rebind follow-local on new instance, got %#v", surface)
	}
	if surface.PromptOverride != (state.ModelConfigRecord{}) {
		t.Fatalf("expected attach switch to clear prompt override, got %#v", surface.PromptOverride)
	}
	if len(surface.StagedImages) != 0 {
		t.Fatalf("expected attach switch to discard staged images, got %#v", surface.StagedImages)
	}
	if svc.instanceClaimSurface("inst-1") != nil || svc.instanceClaimSurface("inst-2") == nil {
		t.Fatalf("expected instance claim to move to switched target")
	}
	var sawSwitchNotice bool
	for _, event := range events {
		if event.Notice != nil && event.Notice.Code == "attached" && strings.Contains(event.Notice.Text, "已切换到") {
			sawSwitchNotice = true
		}
	}
	if !sawSwitchNotice {
		t.Fatalf("expected switch notice, got %#v", events)
	}
}

func TestShowAllThreadsDisablesWorkspaceClaimedThreadInNormalMode(t *testing.T) {
	now := time.Date(2026, 4, 9, 11, 15, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid-a",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid-a",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-2",
		DisplayName:             "droid-b",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid-b",
		Online:                  true,
		ObservedFocusedThreadID: "thread-2",
		Threads: map[string]*state.ThreadRecord{
			"thread-2": {ThreadID: "thread-2", Name: "整理日志", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowAllThreads,
		SurfaceSessionID: "surface-2",
		ChatID:           "chat-2",
		ActorUserID:      "user-2",
	})

	if len(events) != 1 || events[0].FeishuTargetPickerView == nil {
		t.Fatalf("expected add-workspace picker instead of unavailable notice, got %#v", events)
	}
	view := targetPickerFromEvent(t, events[0])
	if view.SelectedMode != control.FeishuTargetPickerModeAddWorkspace || view.SelectedSource != control.FeishuTargetPickerSourceLocalDirectory {
		t.Fatalf("expected claimed-only workspace case to fall back to add-workspace flow, got %#v", view)
	}
}

func TestDetachReleasesWorkspaceClaim(t *testing.T) {
	now := time.Date(2026, 4, 9, 11, 20, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid-a",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid-a",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-2",
		DisplayName:             "droid-b",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid-b",
		Online:                  true,
		ObservedFocusedThreadID: "thread-2",
		Threads: map[string]*state.ThreadRecord{
			"thread-2": {ThreadID: "thread-2", Name: "整理日志", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionDetach, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-2",
		ChatID:           "chat-2",
		ActorUserID:      "user-2",
		InstanceID:       "inst-2",
	})

	surface := svc.root.Surfaces["surface-2"]
	if surface.AttachedInstanceID != "inst-2" || surface.ClaimedWorkspaceKey != "/data/dl/droid" {
		t.Fatalf("expected workspace claim to be released for second attach, got %#v", surface)
	}
	if len(events) == 0 || events[0].Notice == nil || events[0].Notice.Code != "attached" {
		t.Fatalf("expected attach success after detach release, got %#v", events)
	}
}

func TestNormalModeListIncludesHeadlessWorkspace(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-headless-1",
		DisplayName:   "headless",
		WorkspaceRoot: "/data/dl/runtime/headless",
		WorkspaceKey:  "/data/dl/runtime/headless",
		ShortName:     "headless",
		Source:        "headless",
		Managed:       true,
		Online:        true,
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "feishu:app-1:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	if len(events) != 1 {
		t.Fatalf("expected one target picker for headless-only runtime, got %#v", events)
	}
	view := targetPickerFromEvent(t, events[0])
	if view.Source != control.TargetPickerRequestSourceList || view.Title != "选择工作区与会话" {
		t.Fatalf("unexpected target picker: %#v", view)
	}
	if len(view.WorkspaceOptions) != 1 {
		t.Fatalf("expected only headless workspace in existing-workspace mode, got %#v", view.WorkspaceOptions)
	}
	if _, ok := targetPickerWorkspaceOption(view, "/data/dl/runtime/headless"); !ok {
		t.Fatalf("expected only headless workspace in target picker, got %#v", view.WorkspaceOptions)
	}
	if len(view.SessionOptions) != 1 || view.SessionOptions[0].Value != targetPickerNewThreadValue {
		t.Fatalf("expected headless-only workspace to offer direct new thread, got %#v", view.SessionOptions)
	}
}
