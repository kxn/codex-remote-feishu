package orchestrator

import (
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"strings"
	"testing"
	"time"
)

func TestPrivateThreadPolicyUsesExactHistoryAndTransfer(t *testing.T) {
	s := &Service{config: Config{PrivateApplication: true}}
	s.EnablePrivateThreadPolicy()
	key := PrivateHistoryKey{Backend: agentproto.BackendCodex, Namespace: "root-a", Session: "shared"}
	p := PrivateThreadPolicy{InstanceNamespaces: map[string]string{"a": "root-a", "b": "root-b"}, CatalogNamespaces: map[agentproto.Backend]string{agentproto.BackendCodex: "root-a"}, Denied: map[PrivateHistoryKey]bool{key: true}, ManagedDenied: map[PrivateHistoryKey]bool{}, Ambiguous: map[PrivateHistoryKey]bool{}, Transferred: map[PrivateHistoryKey]bool{}}
	s.SetPrivateThreadPolicy("surface", p)
	if s.privateThreadVisible("surface", agentproto.BackendCodex, "a", "shared") {
		t.Fatal("team session exposed")
	}
	if !s.privateThreadVisible("surface", agentproto.BackendCodex, "b", "shared") || !s.privateThreadVisible("surface", agentproto.BackendCodex, "a", "ordinary") {
		t.Fatal("unmanaged local session hidden")
	}
	p.Transferred[key] = true
	if !s.privateThreadVisible("surface", agentproto.BackendCodex, "a", "shared") {
		t.Fatal("transferred session hidden")
	}
	p.ManagedDenied[key] = true
	if s.privateThreadVisible("surface", agentproto.BackendCodex, "a", "shared") {
		t.Fatal("foreign current owner exposed despite old transfer")
	}
}

func TestPrivateThreadPolicyFiltersMergedSelectionSources(t *testing.T) {
	now := time.Now()
	s := newServiceForTest(&now)
	s.config.PrivateApplication = true
	s.EnablePrivateThreadPolicy()
	surface := &state.SurfaceConsoleRecord{SurfaceSessionID: "surface"}
	s.UpsertInstance(&state.InstanceRecord{InstanceID: "foreign", Online: true, WorkspaceRoot: "/work", WorkspaceKey: "/work", Threads: map[string]*state.ThreadRecord{"same": {ThreadID: "same", CWD: "/work", Name: "foreign"}, "team": {ThreadID: "team", CWD: "/work"}}})
	s.UpsertInstance(&state.InstanceRecord{InstanceID: "local", Online: true, WorkspaceRoot: "/work", WorkspaceKey: "/work", Threads: map[string]*state.ThreadRecord{"same": {ThreadID: "same", CWD: "/work", Name: "local"}, "ordinary": {ThreadID: "ordinary", CWD: "/work"}}})
	key := PrivateHistoryKey{Backend: agentproto.BackendCodex, Namespace: "foreign-root", Session: "same"}
	team := PrivateHistoryKey{Backend: agentproto.BackendCodex, Namespace: "foreign-root", Session: "team"}
	s.SetPrivateThreadPolicy("surface", PrivateThreadPolicy{InstanceNamespaces: map[string]string{"foreign": "foreign-root", "local": "local-root"}, Denied: map[PrivateHistoryKey]bool{team: true}, ManagedDenied: map[PrivateHistoryKey]bool{key: true}})
	views := s.mergedThreadViews(surface)
	found := map[string]*mergedThreadView{}
	for _, v := range views {
		found[v.ThreadID] = v
	}
	if found["team"] != nil || found["ordinary"] == nil || found["same"] == nil {
		t.Fatalf("incorrect list: %#v", found)
	}
	if found["same"].Inst.InstanceID != "local" || (found["same"].AnyVisibleInst != nil && found["same"].AnyVisibleInst.InstanceID == "foreign") {
		t.Fatal("foreign source used for selection")
	}
	if got := s.mergedThreadViewForBackend(surface, "team", agentproto.BackendCodex, true); got != nil {
		t.Fatal("direct selection exposes foreign team session")
	}
}

func TestPrivateOpenCodeCatalogAndDirectSelectionShareOwnershipPolicy(t *testing.T) {
	now := time.Now()
	s := newServiceForTest(&now)
	s.config.PrivateApplication = true
	s.EnablePrivateThreadPolicy()
	records := []state.ThreadRecord{{ThreadID: "local", CWD: "/work"}, {ThreadID: "team", CWD: "/work"}, {ThreadID: "other", CWD: "/work"}}
	s.SetPersistedThreadCatalog(&fakePersistedThreadCatalog{recentByBackend: map[agentproto.Backend][]state.ThreadRecord{agentproto.BackendOpenCode: records}, byIDByBackend: map[agentproto.Backend]map[string]state.ThreadRecord{agentproto.BackendOpenCode: {"local": records[0], "team": records[1], "other": records[2]}}})
	surface := &state.SurfaceConsoleRecord{SurfaceSessionID: "s"}
	team := PrivateHistoryKey{Backend: agentproto.BackendOpenCode, Namespace: "root", Session: "team"}
	other := PrivateHistoryKey{Backend: agentproto.BackendOpenCode, Namespace: "root", Session: "other"}
	s.SetPrivateThreadPolicy("s", PrivateThreadPolicy{CatalogNamespaces: map[agentproto.Backend]string{agentproto.BackendOpenCode: "root"}, Denied: map[PrivateHistoryKey]bool{team: true}, ManagedDenied: map[PrivateHistoryKey]bool{other: true}})
	views := map[string]*mergedThreadView{}
	s.mergePersistedRecentThreadsForBackend(surface, views, agentproto.BackendOpenCode, true)
	if len(views) != 1 || views["local"] == nil {
		t.Fatalf("catalog not filtered: %#v", views)
	}
	if s.mergedThreadViewForBackend(surface, "local", agentproto.BackendOpenCode, true) == nil || s.mergedThreadViewForBackend(surface, "team", agentproto.BackendOpenCode, true) != nil || s.mergedThreadViewForBackend(surface, "other", agentproto.BackendOpenCode, true) != nil {
		t.Fatal("direct selection diverged from catalog")
	}
}

func TestPrivateOpenCodeOldVersionVisibleDuringAutoRestore(t *testing.T) {
	now := time.Now()
	s := newServiceForTest(&now)
	s.MaterializeSurfaceResumeContract("s", "app", "chat", "user", state.HeadlessCodexSurfaceBackendContract("default"), state.SurfaceVerbosityNormal, state.PlanModeSettingOff)
	s.root.Surfaces["s"].PendingHeadless = &state.HeadlessLaunchRecord{InstanceID: "instance", Purpose: state.HeadlessLaunchPurposeThreadRestore, AutoRestore: true}
	events := s.HandleHeadlessLaunchFailed("s", "instance", agentproto.ErrorInfo{Code: "opencode_upgrade_required"})
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "opencode_upgrade_required" || !strings.Contains(events[0].Notice.Text, "2.1.4") {
		t.Fatalf("missing upgrade notice: %+v", events)
	}
	if s.root.Surfaces["s"].PendingHeadless != nil {
		t.Fatal("pending launch not cleared")
	}
}

func TestUnavailablePrivateHistoryRemainsClosedWithExactRetiredCarrier(t *testing.T) {
	s := NewService(nil, Config{PrivateApplication: true}, nil)
	s.EnablePrivateThreadPolicy()
	s.SetPrivateThreadPolicy("s", PrivateThreadPolicy{Unavailable: "inventory unavailable"})
	s.RecordPrivateRetiredSession("s", agentproto.BackendCodex, "old", "namespace", "owned")
	if s.privateThreadVisible("s", agentproto.BackendCodex, "old", "owned") || s.privateThreadVisible("s", agentproto.BackendCodex, "old", "foreign") {
		t.Fatal("retired replacement proof exposed history")
	}
	p := s.privateThreadPolicies["s"]
	if !p.ExactRetired[PrivateHistoryKey{Backend: agentproto.BackendCodex, Namespace: "namespace", Session: "owned"}] {
		t.Fatal("exact retired proof missing")
	}
	if p.ExactRetired[PrivateHistoryKey{Backend: agentproto.BackendCodex, Namespace: "namespace", Session: "foreign"}] {
		t.Fatal("foreign session gained retired proof")
	}
	fresh := NewService(nil, Config{PrivateApplication: true}, nil)
	fresh.RecordPrivateRetiredSession("s", agentproto.BackendCodex, "old", "namespace", "owned")
	if fresh.privateThreadVisible("s", agentproto.BackendCodex, "old", "foreign") {
		t.Fatal("retired proof opened unverified catalog")
	}
}
