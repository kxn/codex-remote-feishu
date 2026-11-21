package workmigration

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type testHost struct{ incomplete bool }

func (h *testHost) Fence(_ context.Context, snapshot Snapshot) (FenceReceipt, error) {
	receipt := FenceReceipt{MigrationID: snapshot.ID, SourceFingerprint: snapshot.SourceFingerprint, LegacyWritersClosed: !h.incomplete, QueuesCaptured: true, IngressClosed: true, CronDisabled: true, InflightReconciled: true, Evidence: json.RawMessage(`{"source":"controlled-startup-fencer"}`)}
	for _, record := range snapshot.Records {
		if record.Native != nil {
			receipt.Native = append(receipt.Native, NativeFence{RecordKey: record.Key, Classification: "unknown", Evidence: json.RawMessage(`{"native_state":"unknown","namespace_and_cwd_must_remain_fenced":true}`)})
		}
	}
	return receipt, nil
}

type fixture struct {
	t           *testing.T
	store       *workstore.Store
	coordinator *Coordinator
	host        *testHost
	root, path  string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	f := &fixture{t: t, root: root, path: filepath.Join(root, "work.db"), host: &testHost{}}
	f.reopen()
	t.Cleanup(func() { f.store.Close() })
	return f
}
func (f *fixture) reopen() {
	f.t.Helper()
	if f.store != nil {
		if err := f.store.Close(); err != nil {
			f.t.Fatal(err)
		}
	}
	store, err := workstore.Open(context.Background(), f.path)
	if err != nil {
		f.t.Fatal(err)
	}
	f.store = store
	f.coordinator, err = New(store, f.host, Options{StateDir: f.root})
	if err != nil {
		f.t.Fatal(err)
	}
}
func (f *fixture) tx(fn func(*workstore.Tx) error) {
	f.t.Helper()
	if err := f.store.Within(context.Background(), fn); err != nil {
		f.t.Fatal(err)
	}
}
func (f *fixture) verifiedOwner(openID string) VerifiedOwner {
	f.t.Helper()
	var owner VerifiedOwner
	f.tx(func(tx *workstore.Tx) error {
		if _, err := tx.GetRole("role"); errors.Is(err, workstore.ErrNotFound) {
			if err = tx.PutRole(work.Role{ID: "role", Name: "Role", Description: "role", DescriptionVersion: 1, SettingsRevision: 1, Revision: 1}, 0); err != nil {
				return err
			}
		}
		now := time.Now().UTC()
		envelope := work.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", EventID: "event:" + openID, Tenant: "tenant", ChatID: "chat:" + openID, ChatType: "p2p", MessageID: "message:" + openID, SenderType: "user", SenderIDs: work.SenderIDs{OpenID: openID}, MessageType: "text", RawContent: `{"text":"observed SDK identity"}`, ReceivedAt: now}
		ingress, _, err := tx.AcceptRawIngress(envelope, work.IngressGateway{GatewayRef: "gateway", AppID: "app", RoleID: "role", BotGeneration: 1})
		if err != nil {
			return err
		}
		principal, _, err := tx.ResolvePrincipal(work.IdentityEvidence{Tenant: "tenant", AppID: "app", Generation: 1, IDs: envelope.SenderIDs, ObservationRef: ingress.ID})
		if err != nil {
			return err
		}
		owner = VerifiedOwner{Identity: work.IdentityScope{Tenant: "tenant", AppIdentityDomain: "app", IDType: "open_id", ExternalID: openID, Generation: 1}, PrincipalID: principal.ID, RoleID: "role", EvidenceRef: ingress.ID}
		return nil
	})
	return owner
}
func baseSnapshot() Snapshot {
	return Snapshot{ID: "legacy-v1", Version: 1, Records: []LegacyRecord{{Key: "surface", UnitKey: "old-private", Kind: "surface", ChatType: "p2p", Raw: json.RawMessage(`{"last_actor":"untyped-id-is-not-an-owner"}`)}, {Key: "cron", UnitKey: "old-cron", Kind: "cron", Raw: json.RawMessage(`{"schedule":"retired-readonly"}`)}}}
}
func captureAndFence(t *testing.T, f *fixture, snapshot Snapshot) {
	t.Helper()
	if _, err := f.coordinator.Prepare(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	if _, err := f.coordinator.Fence(context.Background(), snapshot.ID); err != nil {
		t.Fatal(err)
	}
}
func importAll(t *testing.T, f *fixture, id string) {
	t.Helper()
	for i := 0; i < 10; i++ {
		status, err := f.coordinator.Import(context.Background(), id, 1)
		if err != nil {
			t.Fatal(err)
		}
		if status.Pending == 0 {
			return
		}
	}
	t.Fatal("bounded import failed to advance")
}

func TestMigrationRestartsAtEveryCheckpointAndCommitsOneAuthority(t *testing.T) {
	f := newFixture(t)
	snapshot := baseSnapshot()
	if _, err := f.coordinator.Prepare(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	f.reopen()
	if _, err := f.coordinator.Import(context.Background(), snapshot.ID, 1); err == nil {
		t.Fatal("import before writer fence")
	}
	if _, err := f.coordinator.Commit(context.Background(), snapshot.ID); err == nil {
		t.Fatal("commit before capture imported")
	}
	f.host.incomplete = true
	if _, err := f.coordinator.Fence(context.Background(), snapshot.ID); err == nil {
		t.Fatal("partial host fencing accepted")
	}
	f.host.incomplete = false
	if _, err := f.coordinator.Fence(context.Background(), snapshot.ID); err != nil {
		t.Fatal(err)
	}
	f.reopen()
	status, err := f.coordinator.Import(context.Background(), snapshot.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if status.Pending != 1 {
		t.Fatal("import did not obey batch limit")
	}
	f.reopen()
	importAll(t, f, snapshot.ID)
	status, err = f.coordinator.Commit(context.Background(), snapshot.ID)
	if err != nil || status.AuthorityGeneration != 2 || status.Phase != "authority_committed" || status.Quarantined != 1 || status.Imported != 1 {
		t.Fatalf("bad cutover: %+v %v", status, err)
	}
	f.reopen()
	if _, err = f.coordinator.Commit(context.Background(), snapshot.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.coordinator.Prepare(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	changed := snapshot
	changed.Records = append([]LegacyRecord(nil), snapshot.Records...)
	changed.Records[0].Raw = json.RawMessage(`{"changed":true}`)
	if _, err = f.coordinator.Prepare(context.Background(), changed); !errors.Is(err, workstore.ErrIdempotencyConflict) {
		t.Fatalf("changed source replay accepted: %v", err)
	}
	f.tx(func(tx *workstore.Tx) error {
		scopes, err := tx.ListScopes()
		if err != nil {
			return err
		}
		if len(scopes) != 0 {
			t.Fatal("missing tenant/idtype fabricated personal ownership")
		}
		return nil
	})
}

func TestMigrationPreservesQueueAndNativeRecoveringWithoutFakeReadiness(t *testing.T) {
	f := newFixture(t)
	owner := f.verifiedOwner("human")
	snapshot := baseSnapshot()
	snapshot.Records[0].Owner = &owner
	snapshot.Records = append(snapshot.Records, LegacyRecord{Key: "queue", UnitKey: "old-private", Kind: "queue", ChatType: "p2p", Raw: json.RawMessage(`{"original_input":"preserve original source"}`), Owner: &owner, Queue: &LegacyQueue{OriginalID: "old-input-id", Text: "do not lose this input", SourceRef: "old-queue.json#7", ReceivedAt: time.Now().UTC()}}, LegacyRecord{Key: "native", UnitKey: "old-private", Kind: "native", ChatType: "p2p", Raw: json.RawMessage(`{"session":"native-history"}`), Owner: &owner, Native: &LegacyNative{Backend: work.BackendCodex, Namespace: "codex", SessionID: "native-session", InstanceID: "old-instance", HostID: "local", CWD: f.root}})
	captureAndFence(t, f, snapshot)
	importAll(t, f, snapshot.ID)
	f.tx(func(tx *workstore.Tx) error {
		unit, err := tx.GetMigrationUnit(snapshot.ID, "old-private")
		if err != nil {
			return err
		}
		inbox, err := tx.ListInbox(unit.ScopeID)
		if err != nil {
			return err
		}
		if len(inbox) != 1 || inbox[0].State != work.InboxPending {
			t.Fatal("legacy input lost or falsely consumed")
		}
		message, err := tx.GetMessage(inbox[0].MessageID)
		if err != nil {
			return err
		}
		body, err := tx.GetContent(unit.ScopeID, message.BodyRef)
		if err != nil {
			return err
		}
		if string(body.Data) != "do not lose this input" || message.DerivedFrom.ID != "old-input-id" {
			t.Fatal("legacy body or source ID changed")
		}
		workers, err := tx.ListWorkers(unit.ScopeID)
		if err != nil {
			return err
		}
		if len(workers) != 1 || workers[0].State != work.WorkerRecovering || workers[0].CarrierIncarnation != "" || workers[0].NativeSessionID != "native-session" {
			t.Fatal("legacy native fabricated a ready managed carrier")
		}
		return nil
	})
}

func TestUnknownNativeFencesOnlyExactLocationAndLegacyTerminalIsOnlyAudit(t *testing.T) {
	f := newFixture(t)
	cwd := f.t.TempDir()
	snapshot := baseSnapshot()
	snapshot.Records = append(snapshot.Records, LegacyRecord{Key: "native", UnitKey: "old-private", Kind: "native", ChatType: "p2p", Raw: json.RawMessage(`{"unknown":true}`), Native: &LegacyNative{Backend: work.BackendClaude, Namespace: "claude", SessionID: "old-session", HostID: "local", CWD: cwd}})
	captureAndFence(t, f, snapshot)
	f.reopen()
	f.tx(func(tx *workstore.Tx) error {
		if tx.AssertRuntimeResourceAvailable("cwd:local:"+cwd) == nil {
			t.Fatal("unknown old cwd was released")
		}
		return tx.AssertRuntimeResourceAvailable("cwd:local:" + f.root)
	})
	terminal := json.RawMessage(`{"kind":"turn.completed","threadId":"old-session","status":"completed"}`)
	if err := f.coordinator.ObserveLegacy(context.Background(), "old-instance", "event", terminal); err != nil {
		t.Fatal(err)
	}
	if err := f.coordinator.ObserveLegacy(context.Background(), "old-instance", "event", terminal); err != nil {
		t.Fatal(err)
	}
	f.reopen()
	f.tx(func(tx *workstore.Tx) error {
		events, err := tx.ListLegacyRuntimeObservations("old-instance", 0, 10)
		if err != nil {
			return err
		}
		if len(events) != 1 {
			t.Fatal("legacy observation replay duplicated")
		}
		if tx.AssertRuntimeResourceAvailable("cwd:local:"+cwd) == nil {
			t.Fatal("terminal alone fabricated full stop")
		}
		return nil
	})
	importAll(t, f, snapshot.ID)
	if _, err := f.coordinator.Commit(context.Background(), snapshot.ID); err != nil {
		t.Fatal("unrelated new scopes blocked by isolated old native", err)
	}
}

func TestVerifiedSDKAdoptionImportsQuarantinedInputOnce(t *testing.T) {
	f := newFixture(t)
	snapshot := baseSnapshot()
	snapshot.Records = append(snapshot.Records, LegacyRecord{Key: "queue", UnitKey: "old-private", Kind: "queue", ChatType: "p2p", Raw: json.RawMessage(`{"untyped_legacy_user":"human"}`), Queue: &LegacyQueue{OriginalID: "original", Text: "pending input", SourceRef: "queue.json#1", ReceivedAt: time.Now().UTC()}})
	captureAndFence(t, f, snapshot)
	importAll(t, f, snapshot.ID)
	if _, err := f.coordinator.Commit(context.Background(), snapshot.ID); err != nil {
		t.Fatal(err)
	}
	owner := f.verifiedOwner("actual-human")
	for i := 0; i < 2; i++ {
		if _, err := f.coordinator.Adopt(context.Background(), snapshot.ID, "queue", owner); err != nil {
			t.Fatal(err)
		}
	}
	f.tx(func(tx *workstore.Tx) error {
		unit, err := tx.GetMigrationUnit(snapshot.ID, "old-private")
		if err != nil {
			return err
		}
		inputs, err := tx.ListInbox(unit.ScopeID)
		if err != nil {
			return err
		}
		if len(inputs) != 1 || inputs[0].State != work.InboxPending {
			t.Fatal("SDK adoption duplicated or consumed original input")
		}
		item, err := tx.GetMigrationItem(snapshot.ID, "queue")
		if err != nil {
			return err
		}
		var original LegacyRecord
		if err = json.Unmarshal(item.Payload, &original); err != nil {
			return err
		}
		if original.Owner != nil || item.Owner == "" {
			t.Fatal("adoption overwrote raw source instead of recording new evidence")
		}
		return nil
	})
}

func TestMigrationCrashHelper(t *testing.T) {
	if os.Getenv("WORK_MIGRATION_CRASH_HELPER") != "1" {
		return
	}
	root := os.Getenv("WORK_MIGRATION_CRASH_ROOT")
	store, err := workstore.Open(context.Background(), filepath.Join(root, "work.db"))
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err := New(store, &testHost{}, Options{StateDir: root})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := baseSnapshot()
	if _, err = coordinator.Prepare(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	if _, err = coordinator.Fence(context.Background(), snapshot.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = coordinator.Import(context.Background(), snapshot.ID, 1); err != nil {
		t.Fatal(err)
	}
	os.Exit(17)
}
func TestMigrationRealProcessCrashKeepsImportedPrefixAndNoCutover(t *testing.T) {
	root := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestMigrationCrashHelper$")
	cmd.Env = append(os.Environ(), "WORK_MIGRATION_CRASH_HELPER=1", "WORK_MIGRATION_CRASH_ROOT="+root)
	if err := cmd.Run(); err == nil {
		t.Fatal("crash helper exited cleanly")
	}
	store, err := workstore.Open(context.Background(), filepath.Join(root, "work.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	coordinator, err := New(store, &testHost{}, Options{StateDir: root})
	if err != nil {
		t.Fatal(err)
	}
	status, err := coordinator.Status(context.Background(), "legacy-v1")
	if err != nil {
		t.Fatal(err)
	}
	if status.Phase != "quiescing" || status.Pending != 1 || status.AuthorityGeneration != 1 {
		t.Fatalf("crash fabricated cutover or lost prefix: %+v", status)
	}
}
