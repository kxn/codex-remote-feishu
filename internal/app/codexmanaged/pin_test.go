package codexmanaged

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExactUpgradePinPreventsAutomaticLatestDrift(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "verified-codex")
	body, _ := json.Marshal(exactPin{Binary: binary, Version: "0.2.0"})
	if err := os.WriteFile(filepath.Join(root, "exact-version.json"), body, 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	ops := operations{lookup: func(context.Context) (string, error) { calls++; return "9.0.0", nil }, install: func(context.Context, string, string) error {
		t.Fatal("pinned runtime started another install")
		return nil
	}, readVersion: func(_ context.Context, path string) (string, error) {
		if path == binary {
			return "0.2.0", nil
		}
		return "0.1.0", nil
	}, now: time.Now}
	selected, err := ensure(context.Background(), "configured-codex", root, ops)
	if err != nil {
		t.Fatal(err)
	}
	if selected != binary || calls != 0 {
		t.Fatalf("pin drifted: binary=%s lookups=%d", selected, calls)
	}
	ops.readVersion = func(context.Context, string) (string, error) { return "0.3.0", nil }
	if _, err = ensure(context.Background(), "configured-codex", root, ops); err == nil {
		t.Fatal("modified pinned binary silently accepted")
	}
}
