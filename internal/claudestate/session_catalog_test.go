package claudestate

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/claudesessionstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestSessionMetaToThreadRecordKeepsUnmappedObservedPermissionTruth(t *testing.T) {
	thread := sessionMetaToThreadRecord(claudesessionstore.SessionMeta{
		ID:           "thread-1",
		Title:        "修复登录流程",
		WorkspaceKey: "/data/dl/droid",
		CWD:          "/data/dl/droid",
		ObservedPermission: &agentproto.ObservedPermissionState{
			NativeMode:     "dontAsk",
			ProjectionKind: agentproto.ObservedPermissionProjectionKindUnmapped,
		},
		UpdatedAt: time.Date(2026, 5, 26, 10, 0, 0, 0, time.UTC),
	})
	if thread == nil {
		t.Fatal("expected thread record")
	}
	if thread.ObservedPermission == nil || thread.ObservedPermission.NativeMode != "dontAsk" {
		t.Fatalf("expected thread to keep unmapped observed permission truth, got %#v", thread)
	}
	if thread.ObservedAccessMode != "" || thread.ObservedPlanMode != "" {
		t.Fatalf("expected unmapped observed permission to avoid fake coarse projection, got %#v", thread)
	}
}
