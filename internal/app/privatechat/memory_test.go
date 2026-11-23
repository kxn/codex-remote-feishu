package privatechat

import (
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

func memoryJournalFixture(t *testing.T) (*Journal, Owner, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "private.db")
	j, err := OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	o := Owner{TenantID: "tenant", AppID: "bot", GatewayID: "gateway", SurfaceID: "private", ChatID: "chat", UserID: "user"}
	a := New(nil, orchestrator.Config{})
	if err = a.RegisterOwner(o); err != nil {
		t.Fatal(err)
	}
	core, err := a.Service().ExportPrivateCheckpoint(o.SurfaceID)
	if err != nil {
		t.Fatal(err)
	}
	if err = j.CommitCheckpoint(t.Context(), Checkpoint{Owner: o, Generation: 1, Revision: 1, Core: core}, 0, nil); err != nil {
		t.Fatal(err)
	}
	return j, o, path
}

func TestPrivateMemoryRevisionRevocationAndRestart(t *testing.T) {
	j, o, path := memoryJournalFixture(t)
	source := json.RawMessage(`[{"kind":"private_agent","carrier":"first"}]`)
	req := MemoryMutation{OperationID: "save", ID: "reply-language", Content: "用中文回答", Applicability: "回复语言"}
	m, err := j.MutateMemory(t.Context(), o, 1, req, source)
	if err != nil || m.Revision != 1 {
		t.Fatalf("write: %+v %v", m, err)
	}
	if _, err = j.MutateMemory(t.Context(), o, 1, MemoryMutation{OperationID: "stale", ID: req.ID, Content: "stale", Applicability: "reply"}, source); err == nil {
		t.Fatal("stale create overwrote memory")
	}
	foreign := o
	foreign.UserID = "other"
	if _, err = j.ReadMemory(t.Context(), foreign, 1, req.ID, 1); err == nil {
		t.Fatal("foreign user read private memory")
	}
	if _, err = j.MutateMemory(t.Context(), foreign, 1, req, source); err == nil {
		t.Fatal("unregistered owner wrote memory")
	}
	revise := req
	revise.OperationID = "revise"
	revise.ExpectedRevision = 1
	revise.Content = "优先简短中文回答"
	if _, err = j.MutateMemory(t.Context(), o, 1, revise, source); err != nil {
		t.Fatal(err)
	}
	if _, err = j.ReadMemory(t.Context(), o, 1, req.ID, 1); err == nil {
		t.Fatal("old revision remained discoverable")
	}
	revoke := MemoryMutation{OperationID: "forget", ID: req.ID, ExpectedRevision: 2, Revoke: true}
	if _, err = j.MutateMemory(t.Context(), o, 1, revoke, source); err != nil {
		t.Fatal(err)
	}
	if err = j.Close(); err != nil {
		t.Fatal(err)
	}
	j, err = OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	index, _, err := j.ListMemoryIndex(t.Context(), o, 1, "", 20)
	if err != nil || len(index) != 0 {
		t.Fatalf("revoked index after reopen: %+v %v", index, err)
	}
	if _, err = j.ReadMemory(t.Context(), o, 1, req.ID, 2); err == nil {
		t.Fatal("revoked body returned after restart")
	}
	// Retried mutation returns its original receipt without resurrecting state.
	if result, err := j.MutateMemory(t.Context(), o, 1, req, source); err != nil || result.Revision != 1 {
		t.Fatalf("replay: %+v %v", result, err)
	}
	index, _, err = j.ListMemoryIndex(t.Context(), o, 1, "", 20)
	if err != nil || len(index) != 0 {
		t.Fatal("replay resurrected revoked memory")
	}
	var versions int
	if err = j.db.QueryRow(`SELECT count(*) FROM private_memory_versions`).Scan(&versions); err != nil || versions != 3 {
		t.Fatalf("history=%d %v", versions, err)
	}
}

func TestPrivateMemoryConcurrentCASAndBoundedIndex(t *testing.T) {
	j, o, _ := memoryJournalFixture(t)
	defer j.Close()
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, id := range []string{"one", "two"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			_, err := j.MutateMemory(t.Context(), o, 1, MemoryMutation{OperationID: id, ID: "decision", Content: id, Applicability: "decision"}, json.RawMessage(`[]`))
			results <- err
		}(id)
	}
	wg.Wait()
	close(results)
	successful := 0
	for err := range results {
		if err == nil {
			successful++
		}
	}
	if successful != 1 {
		t.Fatalf("concurrent creates=%d", successful)
	}
	if _, err := j.MutateMemory(t.Context(), o, 1, MemoryMutation{OperationID: "second", ID: "z-other", Content: "large body", Applicability: "other"}, json.RawMessage(`[]`)); err != nil {
		t.Fatal(err)
	}
	index, next, err := j.ListMemoryIndex(t.Context(), o, 1, "", 1)
	if err != nil || len(index) != 1 || index[0].Content != "" || next != "decision" {
		t.Fatalf("index: %+v %s %v", index, next, err)
	}
	index, _, err = j.ListMemoryIndex(t.Context(), o, 1, next, 1)
	if err != nil || len(index) != 1 || index[0].ID != "z-other" {
		t.Fatalf("next page: %+v %v", index, err)
	}
}
