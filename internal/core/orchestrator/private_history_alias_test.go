package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestLegacyPrivateHistoryReadOnlySelectionIsVisibleButCannotStart(t *testing.T) {
	now := time.Now()
	s := newServiceForTest(&now)
	s.config.PrivateApplication = true
	s.EnablePrivateThreadPolicy()
	thread := state.ThreadRecord{ThreadID: "old-session", CWD: "/work", Name: "old"}
	s.SetPersistedThreadCatalog(&fakePersistedThreadCatalog{recentByBackend: map[agentproto.Backend][]state.ThreadRecord{agentproto.BackendCodex: {thread}}, byIDByBackend: map[agentproto.Backend]map[string]state.ThreadRecord{agentproto.BackendCodex: {thread.ThreadID: thread}}})
	surface := &state.SurfaceConsoleRecord{SurfaceSessionID: "surface", Backend: agentproto.BackendCodex}
	key := PrivateHistoryKey{Backend: agentproto.BackendCodex, Namespace: "root", Session: thread.ThreadID}
	policy := PrivateThreadPolicy{CatalogNamespaces: map[agentproto.Backend]string{agentproto.BackendCodex: "root"}, Ambiguous: map[PrivateHistoryKey]bool{{Backend: agentproto.BackendCodex, Session: thread.ThreadID}: true}, LegacyAliases: map[PrivateHistoryKey]bool{key: true}, LegacyReadOnly: map[PrivateHistoryKey]bool{key: true}}
	s.SetPrivateThreadPolicy(surface.SurfaceSessionID, policy)
	view := s.mergedThreadViewForBackend(surface, thread.ThreadID, agentproto.BackendCodex, true)
	if view == nil {
		t.Fatal("verified history hidden")
	}
	status, disabled := s.threadSelectionStatus(surface, view, true)
	if !disabled || !strings.Contains(status, "不可续接") {
		t.Fatalf("unexpected status %q disabled=%v", status, disabled)
	}
	if !s.privateLegacyHistoryReadOnly(surface, view) {
		t.Fatal("source uncertainty not projected")
	}
	events := s.useThread(surface, thread.ThreadID, true)
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "legacy_history_read_only" {
		t.Fatalf("use did not explain pending source: %+v", events)
	}
	policy.LegacyReadOnly[key] = false
	if s.privateLegacyHistoryReadOnly(surface, view) {
		t.Fatal("resolved execution remains held")
	}
	policy.ManagedDenied = map[PrivateHistoryKey]bool{key: true}
	s.SetPrivateThreadPolicy(surface.SurfaceSessionID, policy)
	if s.privateThreadVisible(surface.SurfaceSessionID, agentproto.BackendCodex, "", thread.ThreadID) {
		t.Fatal("foreign managed owner overridden")
	}
}
