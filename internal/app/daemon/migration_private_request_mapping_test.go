package daemon

import (
	"encoding/json"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestNativeRequestComponentRequiresExactHeldCarrierAndGeneration(t *testing.T) {
	row := func(id string, v any) workstore.PersonalExportRecord {
		b, e := json.Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		return workstore.PersonalExportRecord{Key: id, Payload: b}
	}
	w := work.WorkerSession{ID: "worker", ScopeID: "scope", ContextID: "front", RoleID: "role", Generation: 2, ControllerGeneration: 3, CarrierIncarnation: "carrier"}
	a := work.ExecutionAttempt{ID: "attempt", ScopeID: "scope", ContextID: "front", RunRef: work.RunRef{WorkerID: "worker", Generation: 2, RunID: "run"}}
	b := agentproto.ExecutionBinding{Version: 1, ScopeID: "scope", ContextID: "front", RoleID: "role", WorkerSessionID: "worker", SessionGeneration: 2, ControllerGeneration: 3, BotGeneration: 1, AttemptID: "attempt", RunRef: "run"}
	req := workstore.NativeRequest{ID: "request", ScopeID: "scope", CarrierIncarnation: "carrier", Binding: b, NativeRequestID: "native", State: "open"}
	owner := b
	owner.AttemptID, owner.RunRef = "", ""
	carrier := workstore.CarrierRegistration{Incarnation: "carrier", Owner: owner}
	rows := map[string][]workstore.PersonalExportRecord{"workers": {row("worker", w)}, "attempts": {row("attempt", a)}, "carrier_registrations": {row("carrier", map[string]any{"registration": carrier})}, "native_ui_requests": {row("request", req)}}
	held, err := verifiedPersonalRequestComponents(rows, map[string]bool{"attempt": true})
	if err != nil || !held["request"] {
		t.Fatalf("exact request not isolated: %+v %v", held, err)
	}
	for _, mutate := range []func(*workstore.NativeRequest){func(r *workstore.NativeRequest) { r.CarrierIncarnation = "old" }, func(r *workstore.NativeRequest) { r.Binding.SessionGeneration = 1 }, func(r *workstore.NativeRequest) { r.Binding.RunRef = "other" }, func(r *workstore.NativeRequest) { r.Binding.BotGeneration++ }} {
		changed := req
		mutate(&changed)
		rows["native_ui_requests"] = []workstore.PersonalExportRecord{row("request", changed)}
		held, err = verifiedPersonalRequestComponents(rows, map[string]bool{"attempt": true})
		if err != nil || held["request"] {
			t.Fatalf("foreign request accepted: %+v %v", held, err)
		}
	}
}
