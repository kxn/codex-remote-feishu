package orchestrator

import (
	"fmt"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
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
