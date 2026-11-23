package workstore

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestProfileModelCatalogPersistsLastGoodAcrossFailureAndRestart(t *testing.T) {
	store, path := openTestStore(t)
	now := time.Date(2026, 11, 23, 1, 0, 0, 0, time.UTC)
	observed := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC) // no freshness TTL on durable reads
	snapshot := agentproto.ModelCatalogSnapshot{RefreshedAt: observed, NextCursor: "native-next", Entries: []agentproto.ModelCatalogEntry{{ID: "first", Model: "first", SupportedReasoningEfforts: []agentproto.ReasoningEffortOption{{ReasoningEffort: "high"}}}, {ID: "second", Model: "second"}}}
	var good ProfileModelCatalog
	requireTx(t, store, func(tx *Tx) error {
		if _, err := tx.GetProfileModelCatalog("connection-page"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("initial error=%v", err)
		}
		pending, err := tx.BeginProfileModelCatalogRefresh("connection-page", "attempt-a", now)
		if err != nil {
			return err
		}
		if !pending.Refreshing || pending.Revision != 1 {
			t.Fatal("refresh attempt not durable")
		}
		good, err = tx.CompleteProfileModelCatalogRefresh("connection-page", "attempt-a", &snapshot, "native-command-a", "", now)
		if err != nil {
			return err
		}
		if good.Refreshing || good.EvidenceID != "native-command-a" || good.RefreshedAt != observed {
			t.Fatal("native successful result not preserved")
		}
		pending, err = tx.BeginProfileModelCatalogRefresh("connection-page", "attempt-b", now.Add(time.Minute))
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(pending.Snapshot, good.Snapshot) || pending.EvidenceID != good.EvidenceID || pending.RefreshedAt != good.RefreshedAt {
			t.Fatal("refresh start erased last good page")
		}
		broken := agentproto.ModelCatalogSnapshot{Entries: []agentproto.ModelCatalogEntry{{ID: "untrusted-partial"}}, ErrorMessage: "native timeout"}
		failed, err := tx.CompleteProfileModelCatalogRefresh("connection-page", "attempt-b", &broken, "wrong-evidence", "", now.Add(2*time.Minute))
		if err != nil {
			return err
		}
		if failed.Refreshing || failed.LastError != "native timeout" || !reflect.DeepEqual(failed.Snapshot, good.Snapshot) || failed.EvidenceID != good.EvidenceID || failed.RefreshedAt != good.RefreshedAt {
			t.Fatal("failure replaced confirmed models or provenance")
		}
		return nil
	})
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reopened.Close() })
	requireTx(t, reopened, func(tx *Tx) error {
		restored, err := tx.GetProfileModelCatalog("connection-page")
		if err != nil {
			return err
		}
		if restored.LastError != "native timeout" || !reflect.DeepEqual(restored.Snapshot, good.Snapshot) || restored.EvidenceID != good.EvidenceID || restored.RefreshedAt != observed {
			t.Fatal("restart/old age erased last good catalog")
		}
		pending, err := tx.BeginProfileModelCatalogRefresh("connection-page", "attempt-c", now.Add(3*time.Minute))
		if err != nil {
			return err
		}
		if !pending.Refreshing || len(pending.Snapshot.Entries) != 2 {
			t.Fatal("new query replaced last good instead of retaining it")
		}
		return nil
	})
	if err = reopened.Close(); err != nil {
		t.Fatal(err)
	}
	afterCrash, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { afterCrash.Close() })
	requireTx(t, afterCrash, func(tx *Tx) error {
		pending, err := tx.GetProfileModelCatalog("connection-page")
		if err != nil {
			return err
		}
		if !pending.Refreshing || pending.LastAttemptID != "attempt-c" || len(pending.Snapshot.Entries) != 2 {
			t.Fatal("inflight refresh lost durable last good state on restart")
		}
		return nil
	})
}

func TestProfileModelCatalogFencesOldAttemptsAndNamespaces(t *testing.T) {
	s, _ := openTestStore(t)
	now := time.Now().UTC()
	first := agentproto.ModelCatalogSnapshot{Entries: []agentproto.ModelCatalogEntry{{ID: "current-model"}}}
	requireTx(t, s, func(tx *Tx) error {
		if _, err := tx.BeginProfileModelCatalogRefresh("same-connection-page", "old", now); err != nil {
			return err
		}
		latest, err := tx.BeginProfileModelCatalogRefresh("same-connection-page", "new", now.Add(time.Second))
		if err != nil {
			return err
		}
		if _, err = tx.CompleteProfileModelCatalogRefresh("same-connection-page", "old", &first, "late-native", "", now.Add(2*time.Second)); !errors.Is(err, ErrConflict) {
			t.Fatal("late old query overwrote newer query")
		}
		unchanged, err := tx.GetProfileModelCatalog(latest.Key)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(latest, unchanged) {
			t.Fatal("rejected late result changed durable state")
		}
		completed, err := tx.CompleteProfileModelCatalogRefresh(latest.Key, "new", &first, "actual-native", "", now.Add(3*time.Second))
		if err != nil {
			return err
		}
		duplicate, err := tx.CompleteProfileModelCatalogRefresh(latest.Key, "new", nil, "", "late timeout", now.Add(4*time.Second))
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(completed, duplicate) {
			t.Fatal("duplicate completion changed first completed result")
		}
		for _, namespace := range []string{"other-login-page", "same-connection-next-cursor"} {
			pending, err := tx.BeginProfileModelCatalogRefresh(namespace, "isolated", now)
			if err != nil {
				return err
			}
			if len(pending.Snapshot.Entries) != 0 || pending.EvidenceID != "" {
				t.Fatal("catalog leaked across semantic namespace/cursor")
			}
			if _, err = tx.CompleteProfileModelCatalogRefresh(namespace, "isolated", nil, "", "query failed", now); err != nil {
				return err
			}
		}
		actual, err := tx.GetProfileModelCatalog(latest.Key)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(actual, completed) {
			t.Fatal("other namespace failure mutated known page")
		}
		return nil
	})
}

func TestProfileModelCatalogRefreshTransactionAndExistingSchema(t *testing.T) {
	s, path := openTestStore(t)
	now := time.Now().UTC()
	sentinel := errors.New("rollback")
	err := s.Within(t.Context(), func(tx *Tx) error {
		if _, err := tx.BeginProfileModelCatalogRefresh("rolled-back", "probe", now); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	requireTx(t, s, func(tx *Tx) error {
		if _, err := tx.GetProfileModelCatalog("rolled-back"); !errors.Is(err, ErrNotFound) {
			t.Fatal("rolled back query attempt survived")
		}
		// Simulate an existing version-1 installation from before this additive table.
		_, err := tx.tx.ExecContext(tx.ctx, "DROP TABLE profile_model_catalogs")
		return err
	})
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reopened.Close() })
	requireTx(t, reopened, func(tx *Tx) error {
		pending, err := tx.BeginProfileModelCatalogRefresh("upgraded", "query", now)
		if err != nil {
			return err
		}
		if pending.Revision != 1 || !pending.Refreshing {
			t.Fatal("existing schema did not gain independent catalog storage")
		}
		return nil
	})
}
