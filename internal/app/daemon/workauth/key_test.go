package workauth

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestConcurrentKeyCreationPreservesOneAuthority(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth", "key")
	const count = 8
	signers := make([]*Signer, count)
	errs := make([]error, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) { defer wg.Done(); signers[i], errs[i] = LoadOrCreate(path, nil) }(i)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	token, err := signers[0].Issue(Claims{Purpose: "worker", RoleID: "r", BotGeneration: 1, ScopeID: "s", ContextID: "c", WorkerID: "w", SessionGeneration: 1, ControllerGeneration: 1}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	for _, signer := range signers {
		if _, err = signer.Verify(token); err != nil {
			t.Fatal("concurrent creator replaced authority:", err)
		}
	}
	reopened, err := LoadOrCreate(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = reopened.Verify(token); err != nil {
		t.Fatal("restart lost authority:", err)
	}
	if err = os.WriteFile(path, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadOrCreate(path, nil); err == nil {
		t.Fatal("corrupt key silently replaced")
	}
}
