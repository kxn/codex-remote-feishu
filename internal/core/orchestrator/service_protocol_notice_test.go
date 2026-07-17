package orchestrator

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestProtocolNoticeRecordsStateOnlyWithoutUIEvents(t *testing.T) {
	now := time.Date(2026, 7, 17, 16, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", Loaded: true},
		},
	})

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventProtocolNotice,
		ThreadID: "thread-1",
		ProtocolNotice: &agentproto.ProtocolNotice{
			Method:   "configWarning",
			Kind:     "config",
			Severity: agentproto.ErrorSeverityWarning,
			ThreadID: "thread-1",
			Summary:  "Invalid config.",
			Details:  "Unknown key.",
			Path:     "/tmp/config.toml",
			Range:    "3:5-3:12",
		},
	})
	if len(events) != 0 {
		t.Fatalf("expected protocol notice to remain state-only, got %#v", events)
	}
	inst := svc.root.Instances["inst-1"]
	if len(inst.ProtocolNotices) != 1 {
		t.Fatalf("expected instance protocol notice, got %#v", inst.ProtocolNotices)
	}
	if notice := inst.ProtocolNotices[0]; notice.Method != "configWarning" || notice.Summary != "Invalid config." || notice.Path != "/tmp/config.toml" {
		t.Fatalf("unexpected instance notice: %#v", notice)
	}
	thread := inst.Threads["thread-1"]
	if len(thread.ProtocolNotices) != 1 || thread.ProtocolNotices[0].Method != "configWarning" {
		t.Fatalf("expected thread notice mirror, got %#v", thread.ProtocolNotices)
	}
}

func TestProtocolNoticeProjectsGuardianWarningToAffectedSurface(t *testing.T) {
	now := time.Date(2026, 7, 17, 16, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", Loaded: true},
		},
	})
	svc.root.Surfaces["surface-1"] = &state.SurfaceConsoleRecord{
		SurfaceSessionID:   "surface-1",
		GatewayID:          "gateway-1",
		ChatID:             "chat-1",
		ActorUserID:        "user-1",
		AttachedInstanceID: "inst-1",
		SelectedThreadID:   "thread-1",
		RouteMode:          state.RouteModePinned,
	}
	svc.threadClaims["thread-1"] = &threadClaimRecord{
		ThreadID:         "thread-1",
		InstanceID:       "inst-1",
		SurfaceSessionID: "surface-1",
	}

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventProtocolNotice,
		ThreadID: "thread-1",
		ProtocolNotice: &agentproto.ProtocolNotice{
			Method:   "guardianWarning",
			Kind:     "guardian",
			Severity: agentproto.ErrorSeverityWarning,
			ThreadID: "thread-1",
			Summary:  "This action was blocked by the guardian.",
			Details:  strings.Repeat("long details ", 20),
		},
	})

	if len(events) != 1 {
		t.Fatalf("expected one guardian notice event, got %#v", events)
	}
	event := events[0].Normalized()
	if event.Kind != eventcontract.KindNotice || event.SurfaceSessionID != "surface-1" {
		t.Fatalf("expected notice for affected surface, got %#v", event)
	}
	if event.Notice == nil || event.Notice.Code != "codex_guardian_warning" {
		t.Fatalf("unexpected notice payload: %#v", event.Notice)
	}
	if !strings.Contains(event.Notice.Text, "This action was blocked by the guardian.") {
		t.Fatalf("expected summary in notice text, got %q", event.Notice.Text)
	}
	if strings.Contains(event.Notice.Text, "long details") {
		t.Fatalf("expected long details to stay out of active notice, got %q", event.Notice.Text)
	}
	if event.Meta.MessageDelivery.Mutation != eventcontract.MessageMutationAppendOnly {
		t.Fatalf("expected append-only notice delivery, got %#v", event.Meta.MessageDelivery)
	}
}

func TestProtocolNoticeDoesNotProjectOrdinaryWarning(t *testing.T) {
	now := time.Date(2026, 7, 17, 16, 15, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID: "inst-1",
		Online:     true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Loaded: true},
		},
	})
	svc.root.Surfaces["surface-1"] = &state.SurfaceConsoleRecord{
		SurfaceSessionID:   "surface-1",
		GatewayID:          "gateway-1",
		AttachedInstanceID: "inst-1",
		SelectedThreadID:   "thread-1",
		RouteMode:          state.RouteModePinned,
	}
	svc.threadClaims["thread-1"] = &threadClaimRecord{ThreadID: "thread-1", InstanceID: "inst-1", SurfaceSessionID: "surface-1"}

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventProtocolNotice,
		ThreadID: "thread-1",
		ProtocolNotice: &agentproto.ProtocolNotice{
			Method:   "warning",
			Kind:     "warning",
			ThreadID: "thread-1",
			Summary:  "Non-blocking warning.",
		},
	})
	if len(events) != 0 {
		t.Fatalf("expected ordinary warning to remain state-only, got %#v", events)
	}
}

func TestProtocolNoticeProjectsSevereConfigWarningOnly(t *testing.T) {
	now := time.Date(2026, 7, 17, 16, 20, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID: "inst-1",
		Online:     true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Loaded: true},
		},
	})
	svc.root.Surfaces["surface-1"] = &state.SurfaceConsoleRecord{
		SurfaceSessionID:   "surface-1",
		GatewayID:          "gateway-1",
		AttachedInstanceID: "inst-1",
		SelectedThreadID:   "thread-1",
		RouteMode:          state.RouteModePinned,
	}
	svc.threadClaims["thread-1"] = &threadClaimRecord{ThreadID: "thread-1", InstanceID: "inst-1", SurfaceSessionID: "surface-1"}

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventProtocolNotice,
		ThreadID: "thread-1",
		ProtocolNotice: &agentproto.ProtocolNotice{
			Method:   "configWarning",
			Kind:     "config",
			ThreadID: "thread-1",
			Summary:  "Failed to parse config.",
			Details:  "invalid TOML",
			Path:     "/tmp/config.toml",
		},
	})
	if len(events) != 1 {
		t.Fatalf("expected one severe config warning notice, got %#v", events)
	}
	if events[0].Notice == nil || events[0].Notice.Code != "codex_config_warning" {
		t.Fatalf("unexpected severe config notice: %#v", events[0].Notice)
	}

	events = svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventProtocolNotice,
		ThreadID: "thread-1",
		ProtocolNotice: &agentproto.ProtocolNotice{
			Method:   "configWarning",
			Kind:     "config",
			ThreadID: "thread-1",
			Summary:  "Deprecated config key.",
			Details:  "This key will be removed later.",
		},
	})
	if len(events) != 0 {
		t.Fatalf("expected non-severe config warning to remain state-only, got %#v", events)
	}
}

func TestProtocolNoticeDoesNotProjectWithoutAffectedSurface(t *testing.T) {
	now := time.Date(2026, 7, 17, 16, 25, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID: "inst-1",
		Online:     true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Loaded: true},
		},
	})

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventProtocolNotice,
		ThreadID: "thread-1",
		ProtocolNotice: &agentproto.ProtocolNotice{
			Method:   "guardianWarning",
			Kind:     "guardian",
			ThreadID: "thread-1",
			Summary:  "Blocked.",
		},
	})
	if len(events) != 0 {
		t.Fatalf("expected no active notice without affected surface, got %#v", events)
	}
}

func TestProtocolNoticeGuardianWarningCooldownDeduplicatesSameKey(t *testing.T) {
	now := time.Date(2026, 7, 17, 16, 30, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID: "inst-1",
		Online:     true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Loaded: true},
		},
	})
	svc.root.Surfaces["surface-1"] = &state.SurfaceConsoleRecord{
		SurfaceSessionID:   "surface-1",
		GatewayID:          "gateway-1",
		AttachedInstanceID: "inst-1",
		SelectedThreadID:   "thread-1",
		RouteMode:          state.RouteModePinned,
	}
	svc.threadClaims["thread-1"] = &threadClaimRecord{ThreadID: "thread-1", InstanceID: "inst-1", SurfaceSessionID: "surface-1"}
	event := agentproto.Event{
		Kind:     agentproto.EventProtocolNotice,
		ThreadID: "thread-1",
		ProtocolNotice: &agentproto.ProtocolNotice{
			Method:   "guardianWarning",
			Kind:     "guardian",
			ThreadID: "thread-1",
			Summary:  "Blocked by guardian.",
		},
	}

	if events := svc.ApplyAgentEvent("inst-1", event); len(events) != 1 {
		t.Fatalf("expected first guardian notice, got %#v", events)
	}
	if events := svc.ApplyAgentEvent("inst-1", event); len(events) != 0 {
		t.Fatalf("expected duplicate guardian notice to be cooled down, got %#v", events)
	}
	now = now.Add(11 * time.Minute)
	if events := svc.ApplyAgentEvent("inst-1", event); len(events) != 1 {
		t.Fatalf("expected guardian notice after cooldown, got %#v", events)
	}
}

func TestProtocolNoticeStateIsBounded(t *testing.T) {
	now := time.Date(2026, 7, 17, 16, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID: "inst-1",
		Online:     true,
		Threads:    map[string]*state.ThreadRecord{},
	})

	for i := 0; i < maxProtocolNoticesPerScope+5; i++ {
		svc.ApplyAgentEvent("inst-1", agentproto.Event{
			Kind:     agentproto.EventProtocolNotice,
			ThreadID: "thread-1",
			ProtocolNotice: &agentproto.ProtocolNotice{
				Method:   "warning",
				Kind:     "warning",
				ThreadID: "thread-1",
				Summary:  fmt.Sprintf("warning-%02d", i),
			},
		})
	}
	inst := svc.root.Instances["inst-1"]
	if len(inst.ProtocolNotices) != maxProtocolNoticesPerScope {
		t.Fatalf("expected bounded instance notices, got %d", len(inst.ProtocolNotices))
	}
	if got := inst.ProtocolNotices[0].Summary; got != "warning-05" {
		t.Fatalf("expected oldest retained notice to be warning-05, got %q", got)
	}
	thread := inst.Threads["thread-1"]
	if len(thread.ProtocolNotices) != maxProtocolNoticesPerScope {
		t.Fatalf("expected bounded thread notices, got %d", len(thread.ProtocolNotices))
	}
}
