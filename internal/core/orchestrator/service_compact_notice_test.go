package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestContextCompactionRendersSingleNoticeOnAttachedSurface(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 0, 0, 0, time.UTC)
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
	svc.root.Surfaces["surface-1"].Verbosity = state.SurfaceVerbosityVerbose

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "compact-1",
		ItemKind: "context_compaction",
	})

	if len(events) != 1 || events[0].Kind != eventcontract.KindExecCommandProgress || events[0].ExecCommandProgress == nil {
		t.Fatalf("expected compact progress event, got %#v", events)
	}
	progress := events[0].ExecCommandProgress
	if len(progress.Entries) != 1 || progress.Entries[0].Kind != "context_compaction" || progress.Entries[0].Label != "压缩" || progress.Entries[0].Summary != "上下文已压缩。" {
		t.Fatalf("unexpected compact progress payload: %#v", progress)
	}
	if len(progress.Timeline) != 1 || progress.Timeline[0].Kind != "context_compaction" || progress.Timeline[0].Summary != "上下文已压缩。" {
		t.Fatalf("expected compact progress to populate canonical timeline, got %#v", progress.Timeline)
	}
	if replay := svc.root.Instances["inst-1"].Threads["thread-1"].UndeliveredReplay; replay != nil {
		t.Fatalf("expected delivered compact notice not to leave replay, got %#v", replay)
	}
}

func TestContextCompactionNormalVerbosityShowsAttachedSurfaceCard(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 2, 0, 0, time.UTC)
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
	svc.root.Surfaces["surface-1"].Verbosity = state.SurfaceVerbosityNormal

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "compact-1",
		ItemKind: "context_compaction",
	})
	if len(events) != 1 || events[0].Kind != eventcontract.KindExecCommandProgress || events[0].ExecCommandProgress == nil {
		t.Fatalf("expected normal verbosity to show compact card, got %#v", events)
	}
	progress := events[0].ExecCommandProgress
	if len(progress.Entries) != 1 || progress.Entries[0].Kind != "context_compaction" || progress.Entries[0].Summary != "上下文已压缩。" {
		t.Fatalf("unexpected compact progress payload in normal verbosity: %#v", progress)
	}
	if replay := svc.root.Instances["inst-1"].Threads["thread-1"].UndeliveredReplay; replay != nil {
		t.Fatalf("expected attached compact completion not to leave replay, got %#v", replay)
	}
	if svc.root.Surfaces["surface-1"].ActiveExecProgress == nil {
		t.Fatal("expected normal verbosity to retain shared progress state for the active card")
	}
}

func TestManualCompactStillUsesForegroundOwnerCardOutsideVerbose(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 3, 0, 0, time.UTC)
	svc := newCompactServiceFixture(&now)
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ThreadID: "thread-1"})

	start := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionCompact,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	catalog, _ := requireCompactStartEvents(t, start)
	svc.RecordOwnerCardFlowMessage("surface-1", catalog.TrackingKey, "om-compact-notice")
	svc.BindPendingRemoteCommand("surface-1", "cmd-compact-notice")
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-compact-notice",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: "surface-1"},
	})

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-compact-notice",
		ItemID:   "compact-notice",
		ItemKind: "context_compaction",
	})
	if len(events) != 1 {
		t.Fatalf("expected explicit compact to emit one owner-card update outside verbose, got %#v", events)
	}
	got := commandCatalogFromEvent(t, events[0])
	if got.MessageID != "om-compact-notice" || got.Title != "上下文已压缩" || got.ThemeKey != "success" {
		t.Fatalf("unexpected explicit compact owner-card update: %#v", got)
	}
}

func TestContextCompactionStoresReplayWhenNoSurfaceAndReplaysOnce(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 5, 0, 0, time.UTC)
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

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "compact-1",
		ItemKind: "context_compaction",
	})
	if len(events) != 0 {
		t.Fatalf("expected no UI events without attached surface, got %#v", events)
	}
	replay := svc.root.Instances["inst-1"].Threads["thread-1"].UndeliveredReplay
	if replay == nil || replay.Kind != state.ThreadReplayNotice || replay.NoticeCode != "context_compacted" || replay.NoticeText != "上下文已压缩。" {
		t.Fatalf("expected compact replay to be stored, got %#v", replay)
	}

	surface := svc.ensureSurface(control.Action{
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	surface.Verbosity = state.SurfaceVerbosityVerbose
	attach := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})
	if len(attach) == 0 {
		t.Fatalf("expected attach to replay stored compact notice, got %#v", attach)
	}
	var sawProgress bool
	for _, event := range attach {
		if event.ExecCommandProgress != nil && len(event.ExecCommandProgress.Entries) == 1 {
			entry := event.ExecCommandProgress.Entries[0]
			if entry.Kind == "context_compaction" && entry.Label == "压缩" && entry.Summary == "上下文已压缩。" {
				sawProgress = true
			}
		}
	}
	if !sawProgress {
		t.Fatalf("expected attach to replay compact progress, got %#v", attach)
	}
	if replay := svc.root.Instances["inst-1"].Threads["thread-1"].UndeliveredReplay; replay != nil {
		t.Fatalf("expected replay to be drained after attach, got %#v", replay)
	}
	if extra := svc.replayThreadUpdate(svc.root.Surfaces["surface-1"], svc.root.Instances["inst-1"], "thread-1"); len(extra) != 0 {
		t.Fatalf("expected compact replay to be one-shot, got %#v", extra)
	}
}

func TestContextCompactionReplayShowsSharedProgressWhenAttachedSurfaceNormal(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 7, 0, 0, time.UTC)
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

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "compact-1",
		ItemKind: "context_compaction",
	})

	surface := svc.ensureSurface(control.Action{
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	surface.Verbosity = state.SurfaceVerbosityNormal

	attach := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})
	var sawProgress bool
	for _, event := range attach {
		if event.ExecCommandProgress != nil && len(event.ExecCommandProgress.Entries) == 1 {
			entry := event.ExecCommandProgress.Entries[0]
			if entry.Kind == "context_compaction" && entry.Summary == "上下文已压缩。" {
				sawProgress = true
			}
		}
	}
	if !sawProgress {
		t.Fatalf("expected normal attach to replay compact progress, got %#v", attach)
	}
	if replay := svc.root.Instances["inst-1"].Threads["thread-1"].UndeliveredReplay; replay != nil {
		t.Fatalf("expected replay to drain after normal attach, got %#v", replay)
	}
}

func TestContextCompactionReplayStaysSilentWhenAttachedSurfaceQuiet(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 7, 30, 0, time.UTC)
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

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "compact-1",
		ItemKind: "context_compaction",
	})

	surface := svc.ensureSurface(control.Action{
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	surface.Verbosity = state.SurfaceVerbosityQuiet

	attach := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})
	for _, event := range attach {
		if event.Notice != nil && event.Notice.Code == "context_compacted" {
			t.Fatalf("expected quiet compact replay to stay silent, got %#v", attach)
		}
		if event.ExecCommandProgress != nil {
			t.Fatalf("expected quiet compact replay not to emit shared progress, got %#v", attach)
		}
	}
	if replay := svc.root.Instances["inst-1"].Threads["thread-1"].UndeliveredReplay; replay != nil {
		t.Fatalf("expected suppressed quiet replay to drain after attach, got %#v", replay)
	}
}

func TestCompactReplayKeepsStoredReplyAnchor(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 9, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose
	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "整理一下", "turn-1")

	svc.storeThreadReplayTurnNotice(svc.root.Instances["inst-1"], "thread-1", "turn-1", compactCompletionNotice())
	svc.root.Instances["inst-1"].ActiveTurnID = ""

	events := svc.replayThreadUpdate(surface, svc.root.Instances["inst-1"], "thread-1")
	if len(events) != 1 || events[0].ExecCommandProgress == nil {
		t.Fatalf("expected one compact replay progress event, got %#v", events)
	}
	if events[0].SourceMessageID != "msg-1" {
		t.Fatalf("expected compact replay to keep source anchor, got %#v", events[0])
	}
}
