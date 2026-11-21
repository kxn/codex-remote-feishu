package workmigration

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestLegacyHelloUnknownHostFencesPossibleHostsUntilExactAttribution(t *testing.T) {
	f := newFixture(t)
	snapshot := baseSnapshot()
	captureAndFence(t, f, snapshot)
	importAll(t, f, snapshot.ID)
	if _, err := f.coordinator.Commit(context.Background(), snapshot.ID); err != nil {
		t.Fatal(err)
	}
	hello := agentproto.Hello{Instance: agentproto.InstanceHello{InstanceID: "old-live", Backend: agentproto.BackendCodex, WorkspaceRoot: "/unattributed/repo"}}
	if err := f.coordinator.ObserveLegacyHello(context.Background(), hello, HostAttribution{}); err != nil {
		t.Fatal(err)
	}
	var record string
	f.tx(func(tx *workstore.Tx) error {
		for _, host := range []string{"local", "remote"} {
			if tx.AssertRuntimeResourceAvailable("cwd:"+host+":/unattributed/repo") == nil {
				t.Fatalf("unverified host bypassed fence on %s", host)
			}
		}
		if err := tx.AssertRuntimeResourceAvailable("cwd:local:/independent/repo"); err != nil {
			t.Fatalf("unrelated path blocked: %v", err)
		}
		fences, err := tx.ListMigrationResourceFences(snapshot.ID, "", 256)
		for _, fence := range fences {
			if fence.Reason == "legacy_live_native_unknown" {
				record = fence.RecordKey
			}
		}
		return err
	})
	if record == "" {
		t.Fatal("missing durable legacy fence")
	}
	// A late terminal is an audit observation, never background/host proof.
	if err := f.coordinator.ObserveLegacy(context.Background(), "old-live", "event", json.RawMessage(`{"kind":"turn.completed"}`)); err != nil {
		t.Fatal(err)
	}
	f.reopen()
	f.tx(func(tx *workstore.Tx) error {
		if tx.AssertRuntimeResourceAvailable("cwd:local:/unattributed/repo") == nil {
			t.Fatal("terminal or restart cleared unknown fence")
		}
		return nil
	})
	if err := f.coordinator.AttributeLegacyHost(context.Background(), snapshot.ID, record, HostAttribution{HostID: "remote", Evidence: json.RawMessage(`{"source":"authenticated-host-launch-ledger","instance":"old-live"}`)}); err != nil {
		t.Fatal(err)
	}
	f.tx(func(tx *workstore.Tx) error {
		if err := tx.AssertRuntimeResourceAvailable("cwd:local:/unattributed/repo"); err != nil {
			t.Fatalf("proven remote host still blocks unrelated local path: %v", err)
		}
		if tx.AssertRuntimeResourceAvailable("cwd:remote:/unattributed/repo") == nil {
			t.Fatal("host attribution falsely proved native quiescence")
		}
		return nil
	})
}
