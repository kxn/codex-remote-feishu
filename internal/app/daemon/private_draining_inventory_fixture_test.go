package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
)

func logControlledDrainInventory(t *testing.T, path string, carrier hostinventory.ProcessIdentity) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Logf("inventory open error=%v", err)
		return
	}
	defer db.Close()
	var raw []byte
	if err = db.QueryRow(`SELECT payload FROM inventory_state LIMIT 1`).Scan(&raw); err != nil {
		t.Logf("inventory read error=%v", err)
		return
	}
	var envelope struct {
		State json.RawMessage `json:"state"`
	}
	if err = json.Unmarshal(raw, &envelope); err != nil {
		t.Logf("inventory envelope decode error=%v", err)
		return
	}
	var data struct {
		Scopes        []hostinventory.Snapshot   `json:"scopes"`
		Tasks         map[string]bool            `json:"tasks"`
		Turns         map[string]json.RawMessage `json:"turns"`
		PendingInputs map[string]string          `json:"pending_inputs"`
	}
	if err = json.Unmarshal(envelope.State, &data); err != nil {
		t.Logf("inventory decode error=%v", err)
		return
	}
	t.Logf("controlled inventory scopes=%d tasks=%d turns=%d pending=%d", len(data.Scopes), len(data.Tasks), len(data.Turns), len(data.PendingInputs))
	for _, saved := range data.Scopes {
		t.Logf("saved scope complete=%v alive=%v controlled=%v waited=%v exit=%v process_count=%d", saved.Complete, saved.RootAlive, saved.ControlledStop, saved.WaitObserved, saved.RootExitVerified, len(saved.ProcessIDs))
		scope, e := hostinventory.Restore(saved)
		if e != nil {
			t.Logf("scope restore error=%v", e)
			continue
		}
		observed := scope.Inspect()
		t.Logf("live scope complete=%v alive=%v controlled=%v waited=%v exit=%v process_count=%d reason=%s", observed.Complete, observed.RootAlive, observed.ControlledStop, observed.WaitObserved, observed.RootExitVerified, len(observed.ProcessIDs), observed.Reason)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		e = scope.ObserveRootExitWithCarrier(ctx, carrier)
		cancel()
		after := scope.Inspect()
		t.Logf("after observed exit error=%v complete=%v alive=%v controlled=%v waited=%v exit=%v process_count=%d reason=%s", e, after.Complete, after.RootAlive, after.ControlledStop, after.WaitObserved, after.RootExitVerified, len(after.ProcessIDs), after.Reason)
		_ = scope.Close()
	}
}
