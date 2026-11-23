package workruntime

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestPrivateLiveSummaryRejectsHiddenHelperUnprovenAndForeignTransport(t *testing.T) {
	f := newFixture(t, work.BackendCodex, false, true)
	f.coordinator.options.EnqueueReconcile = func(string) {}
	setPrivateDisplayMode(f, "verbose")
	f.start()
	var before int
	f.tx(func(tx *workstore.Tx) error { outbox, e := tx.ListOutbox("scope"); before = len(outbox); return e })
	base := agentproto.Event{Kind: agentproto.EventItemDelta, ThreadID: "session", TurnID: "turn-1", ItemID: "summary", ItemKind: "reasoning_summary", Delta: "must not be rendered", Metadata: map[string]any{"publicReasoningSummary": true, "publicSummaryOffset": 0}}
	for _, kind := range []string{"hidden", "helper-traffic", "helper-initiator", "unknown-turn", "unknown-thread", "foreign-transport"} {
		event := base
		transport := f.carrier.OriginInstanceID
		switch kind {
		case "hidden":
			event.ItemKind = "reasoning_content"
		case "helper-traffic":
			event.TrafficClass = agentproto.TrafficClassInternalHelper
		case "helper-initiator":
			event.Initiator.Kind = agentproto.InitiatorInternalHelper
		case "unknown-turn":
			event.TurnID = "unproven"
		case "unknown-thread":
			event.ThreadID = "unproven"
		case "foreign-transport":
			transport = "unrelated-instance"
		}
		if e := f.coordinator.HandleEvent(t.Context(), transport, event); e != nil {
			t.Fatal(e)
		}
	}
	f.tx(func(tx *workstore.Tx) error {
		outbox, e := tx.ListOutbox("scope")
		if len(outbox) != before {
			t.Fatal("hidden/helper/unproven live event reached private output")
		}
		return e
	})
}

func TestGroupLiveSummaryNeverCreatesPrivatePresentation(t *testing.T) {
	f := newFixture(t, work.BackendCodex, false, true)
	owner := agentproto.ExecutionBinding{Version: 1, ScopeID: "group-scope", ContextID: "group-context", RoleID: "role", WorkerSessionID: "group-worker", SessionGeneration: 1, ControllerGeneration: 1, BotGeneration: 1}
	carrier := workstore.CarrierRegistration{Incarnation: "group-carrier", OriginInstanceID: "group-instance", Owner: owner, Backend: work.BackendCodex, InstanceGeneration: 1, ProcessGeneration: 1, LocationGeneration: 1}
	f.tx(func(tx *workstore.Tx) error {
		if e := tx.PutTeam(work.Team{ID: "group-team", Name: "Team", LeaderRoleID: "role", Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutScope(work.WorkScope{ID: owner.ScopeID, Kind: work.ScopeProject, ProjectID: "group-project", TeamID: "group-team", Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutProject(work.Project{ID: "group-project", Name: "Group", ScopeID: owner.ScopeID, TeamID: "group-team", LeaderRoleID: "role", State: work.ProjectActive, Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutContext(work.WorkContext{ID: owner.ContextID, ScopeID: owner.ScopeID, RoleID: "role", Workstream: "main", Revision: 1}, 0); e != nil {
			return e
		}
		return tx.PutWorker(work.WorkerSession{ID: owner.WorkerSessionID, ScopeID: owner.ScopeID, ContextID: owner.ContextID, RoleID: owner.RoleID, Backend: work.BackendCodex, State: work.WorkerReady, Generation: 1, Controller: work.ControllerAutomatic, ControllerGeneration: 1, InstanceID: carrier.OriginInstanceID, CarrierIncarnation: carrier.Incarnation, InstanceGeneration: 1, ProcessGeneration: 1, LocationGeneration: 1, Revision: 1}, 0)
	})
	if e := f.coordinator.RegisterCarrier(t.Context(), carrier); e != nil {
		t.Fatal(e)
	}
	f.tx(func(tx *workstore.Tx) error { return tx.SetCarrierConnected(carrier.Incarnation, true) })
	if e := f.coordinator.HandleEvent(t.Context(), carrier.OriginInstanceID, agentproto.Event{Kind: agentproto.EventItemDelta, ThreadID: "group-thread", TurnID: "group-turn", ItemID: "summary", ItemKind: "reasoning_summary", Delta: "public but group-streamed", Metadata: map[string]any{"publicReasoningSummary": true, "publicSummaryOffset": 0}}); e != nil {
		t.Fatal(e)
	}
	f.tx(func(tx *workstore.Tx) error {
		outbox, e := tx.ListOutbox(owner.ScopeID)
		if len(outbox) != 0 {
			t.Fatal("group delta generated private presentation")
		}
		return e
	})
}
