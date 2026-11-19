package codexstate

import (
	"database/sql"
	"fmt"
	"testing"
)

// Desktop renames update name independently from the legacy title and activity
// timestamp. Both list and exact-thread reads must see the latest saved name.
func TestSQLiteThreadCatalogReadsDesktopRenamesWithoutActivityChange(t *testing.T) {
	dbPath := createThreadCatalogTestDB(t)
	db, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("ALTER TABLE threads ADD COLUMN name TEXT"); err != nil {
		t.Fatal(err)
	}
	catalog := NewSQLiteThreadCatalog(dbPath, SQLiteThreadCatalogOptions{Logf: func(string, ...any) {}})
	for _, name := range []string{"Memory", "Memory 部署"} {
		if _, err := db.Exec("UPDATE threads SET name = ? WHERE id = 'thread-1'", name); err != nil {
			t.Fatal(err)
		}
		thread, err := catalog.ThreadByID("thread-1")
		if err != nil {
			t.Fatal(err)
		}
		if thread == nil || thread.Name != name || thread.FirstUserMessage != "第一条消息" {
			t.Fatalf("exact lookup must use desktop name %q and preserve original input, got %#v", name, thread)
		}
		names, err := catalog.ExplicitThreadNames([]string{"thread-1"})
		if err != nil || names["thread-1"] != name {
			t.Fatalf("batch lookup must see desktop rename %q, got %v (%v)", name, names, err)
		}
		threads, err := catalog.RecentThreads(10)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, listed := range threads {
			if listed.ThreadID == "thread-1" {
				found = true
				if listed.Name != name || !listed.LastUsedAt.Equal(thread.LastUsedAt) {
					t.Fatalf("list must use the same latest name without changing recency: %#v", listed)
				}
			}
		}
		if !found {
			t.Fatal("renamed thread disappeared from recent list")
		}
	}
}

func TestSQLiteThreadCatalogOptionalNameKeepsLegacyFallback(t *testing.T) {
	for _, tc := range []struct {
		label     string
		addColumn bool
		name      any
	}{
		{label: "older schema"},
		{label: "null name", addColumn: true},
		{label: "empty name", addColumn: true, name: ""},
		{label: "blank name", addColumn: true, name: "  \t\n"},
	} {
		t.Run(tc.label, func(t *testing.T) {
			dbPath := createThreadCatalogTestDB(t)
			db, err := sql.Open("sqlite", "file:"+dbPath)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if tc.addColumn {
				if _, err := db.Exec("ALTER TABLE threads ADD COLUMN name TEXT"); err != nil {
					t.Fatal(err)
				}
				if _, err := db.Exec("UPDATE threads SET name = ? WHERE id = 'thread-1'", tc.name); err != nil {
					t.Fatal(err)
				}
			}
			catalog := NewSQLiteThreadCatalog(dbPath, SQLiteThreadCatalogOptions{Logf: func(string, ...any) {}})
			thread, err := catalog.ThreadByID("thread-1")
			if err != nil {
				t.Fatal(err)
			}
			if thread == nil || thread.Name != "修复登录流程" {
				t.Fatalf("unnamed/legacy thread must keep its existing title, got %#v", thread)
			}
			if _, err := catalog.RecentThreads(10); err != nil {
				t.Fatalf("optional name column must not break list reads: %v", err)
			}
			if _, err := catalog.RecentWorkspaces(10); err != nil {
				t.Fatalf("optional name column must not break workspace reads: %v", err)
			}
			names, err := catalog.ExplicitThreadNames([]string{"thread-1"})
			if err != nil || len(names) != 0 {
				t.Fatalf("legacy/default title must not become an explicit rename: %v (%v)", names, err)
			}
		})
	}
}

func TestSQLiteThreadCatalogExplicitNamesUsesBoundedFilteredIdentityReads(t *testing.T) {
	dbPath := createThreadCatalogTestDB(t)
	db, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("ALTER TABLE threads ADD COLUMN name TEXT"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE threads SET name = '桌面新名'"); err != nil {
		t.Fatal(err)
	}
	// More than SQLite's parameter limit ensures an unbounded IN query fails.
	ids := make([]string, 33_000)
	for i := range ids {
		ids[i] = fmt.Sprintf("not-found-%d", i)
	}
	ids = append(ids, "thread-1", "thread-3", "thread-2", "thread-subagent", "thread-probe", "thread-exec", "thread-mcp", "thread-cron", "' OR 1=1 --")
	catalog := NewSQLiteThreadCatalog(dbPath, SQLiteThreadCatalogOptions{Logf: func(string, ...any) {}})
	names, err := catalog.ExplicitThreadNames(ids)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 || names["thread-1"] != "桌面新名" || names["thread-3"] != "桌面新名" {
		t.Fatalf("batch names must include later batches and preserve visibility filters: %v", names)
	}
}

func TestSQLiteThreadCatalogSeesNameColumnAddedAfterFirstRead(t *testing.T) {
	dbPath := createThreadCatalogTestDB(t)
	catalog := NewSQLiteThreadCatalog(dbPath, SQLiteThreadCatalogOptions{Logf: func(string, ...any) {}})
	if _, err := catalog.ThreadByID("thread-1"); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("ALTER TABLE threads ADD COLUMN name TEXT"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE threads SET name = 'UserIntent' WHERE id = 'thread-1'"); err != nil {
		t.Fatal(err)
	}
	thread, err := catalog.ThreadByID("thread-1")
	if err != nil {
		t.Fatal(err)
	}
	if thread == nil || thread.Name != "UserIntent" {
		t.Fatalf("same catalog must pick up the upgraded name column, got %#v", thread)
	}
}
