package workmigration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestMigrationBackupChecksHashPermissionsAndRealDirectoryContainment(t *testing.T) {
	f := newFixture(t)
	raw := []byte(`{"legacy":"source survives"}`)
	path := filepath.Join(f.root, "backup.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	snapshot := baseSnapshot()
	snapshot.Backups = []Backup{{Path: path, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(raw))}}
	if _, err := f.coordinator.Prepare(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"legacy":"source changed!"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.coordinator.Prepare(context.Background(), snapshot); err == nil {
		t.Fatal("changed backup accepted")
	}
	if runtime.GOOS == "windows" {
		return
	}
	outside := t.TempDir()
	outsidePath := filepath.Join(outside, "outside.json")
	if err := os.WriteFile(outsidePath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(f.root, "outside-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	snapshot.Backups[0].Path = filepath.Join(link, "outside.json")
	if _, err := f.coordinator.Prepare(context.Background(), snapshot); err == nil {
		t.Fatal("parent symlink escaped controlled state directory")
	}
}

func TestMigrationLocationFenceResolvesParentSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink setup needs platform privilege")
	}
	f := newFixture(t)
	real := t.TempDir()
	alias := filepath.Join(f.root, "alias")
	if err := os.Symlink(real, alias); err != nil {
		t.Fatal(err)
	}
	snapshot := baseSnapshot()
	snapshot.Records = append(snapshot.Records, LegacyRecord{Key: "native", UnitKey: "old-private", Kind: "native", Raw: json.RawMessage(`{"unknown":true}`), Native: &LegacyNative{Backend: work.BackendCodex, Namespace: "codex", SessionID: "session", HostID: "local", CWD: filepath.Join(alias, "missing-child")}})
	captureAndFence(t, f, snapshot)
	f.tx(func(tx *workstore.Tx) error {
		if tx.AssertRuntimeResourceAvailable("cwd:local:"+filepath.Join(real, "missing-child")) == nil {
			t.Fatal("relative or parent symlink spelling bypassed a legacy native fence")
		}
		return nil
	})
}

func TestGroupPrimaryAndSharedNativeNeverBecomePrivateHistoryOwners(t *testing.T) {
	f := newFixture(t)
	first := f.verifiedOwner("first")
	second := f.verifiedOwner("second")
	native := &LegacyNative{Backend: work.BackendClaude, Namespace: "claude", SessionID: "shared-history"}
	snapshot := Snapshot{ID: "legacy-v1", Version: 1, Records: []LegacyRecord{
		{Key: "group", UnitKey: "group", Kind: "surface", ChatType: "group", Owner: &first, Raw: json.RawMessage(`{"primary":"old-primary"}`)},
		{Key: "native-first", UnitKey: "first", Kind: "native", ChatType: "p2p", Owner: &first, Native: native, Raw: json.RawMessage(`{"old":"first"}`)},
		{Key: "native-second", UnitKey: "second", Kind: "native", ChatType: "p2p", Owner: &second, Native: native, Raw: json.RawMessage(`{"old":"second"}`)},
	}}
	captureAndFence(t, f, snapshot)
	importAll(t, f, snapshot.ID)
	f.tx(func(tx *workstore.Tx) error {
		for _, key := range []string{"group", "native-first", "native-second"} {
			item, err := tx.GetMigrationItem(snapshot.ID, key)
			if err != nil {
				return err
			}
			if item.State != "quarantined" {
				t.Fatalf("ambiguous legacy source adopted: %s", key)
			}
		}
		scopes, err := tx.ListScopes()
		if err != nil {
			return err
		}
		for _, scope := range scopes {
			if scope.Kind == work.ScopeProject {
				t.Fatal("old group primary fabricated a project")
			}
			workers, err := tx.ListWorkers(scope.ID)
			if err != nil {
				return err
			}
			if len(workers) != 0 {
				t.Fatal("shared history acquired one unproven private owner")
			}
		}
		return nil
	})
}

func TestAdoptCannotChangeAnAlreadyImportedPrincipal(t *testing.T) {
	f := newFixture(t)
	snapshot := baseSnapshot()
	captureAndFence(t, f, snapshot)
	importAll(t, f, snapshot.ID)
	first := f.verifiedOwner("first")
	if _, err := f.coordinator.Adopt(context.Background(), snapshot.ID, "surface", first); err != nil {
		t.Fatal(err)
	}
	second := f.verifiedOwner("second")
	if _, err := f.coordinator.Adopt(context.Background(), snapshot.ID, "surface", second); !errors.Is(err, workstore.ErrIdentityConflict) {
		t.Fatalf("imported history owner changed: %v", err)
	}
}
