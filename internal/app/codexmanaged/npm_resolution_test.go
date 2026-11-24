package codexmanaged

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNPMResolutionHonorsEffectiveAgePolicy(t *testing.T) {
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("npm unavailable")
	}
	now := time.Now().UTC()
	old := now.Add(-72 * time.Hour).Format(time.RFC3339)
	recent := now.Add(-time.Hour).Format(time.RFC3339)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"name":"@openai/codex","dist-tags":{"latest":"0.172.0"},"versions":{"0.171.0":{"name":"@openai/codex","version":"0.171.0"},"0.172.0":{"name":"@openai/codex","version":"0.172.0"}},"time":{"0.171.0":%q,"0.172.0":%q}}`, old, recent)
	}))
	defer server.Close()
	root := t.TempDir()
	userConfig := filepath.Join(root, "npmrc")
	env := append(os.Environ(), "NPM_CONFIG_USERCONFIG="+userConfig, "NPM_CONFIG_CACHE="+filepath.Join(root, "cache"), "NPM_CONFIG_GLOBALCONFIG="+filepath.Join(root, "global"))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := os.WriteFile(userConfig, []byte("min-release-age=2\n"), 0600); err != nil {
		t.Fatal(err)
	}
	policy1, err := npmPolicyFingerprint(ctx, env)
	if err != nil {
		t.Fatal(err)
	}
	v, err := resolveLatestWithNPMRegistry(ctx, env, server.URL)
	if err != nil || v != "0.171.0" {
		t.Fatalf("policy version=%q error=%v", v, err)
	}
	if err := os.WriteFile(userConfig, []byte("min-release-age=0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	policy2, err := npmPolicyFingerprint(ctx, env)
	if err != nil || policy1 == policy2 {
		t.Fatalf("policy change: %v", err)
	}
	v, err = resolveLatestWithNPMRegistry(ctx, env, server.URL)
	if err != nil || v != "0.172.0" {
		t.Fatalf("unrestricted version=%q error=%v", v, err)
	}
}

func TestLatestVersionCacheTracksPolicy(t *testing.T) {
	root := t.TempDir()
	calls := 0
	policy := "old"
	ops := fakeOperations("0.171.0")
	ops.policy = func(context.Context) (string, error) { return policy, nil }
	ops.lookup = func(context.Context) (string, error) { calls++; return "0.171.0", nil }
	// An old cache without policy was written by earlier releases.
	b, _ := json.Marshal(checkRecord{Latest: "0.172.0", CheckedAt: time.Now().UTC()})
	if err := os.WriteFile(filepath.Join(root, "check.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := latestVersion(context.Background(), root, ops); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("lookups=%d", calls)
	}
	policy = "new"
	if _, err := latestVersion(context.Background(), root, ops); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("policy change not observed: %d", calls)
	}
	if strings.Contains(policy, "token") {
		t.Fatal("unexpected policy")
	}
}
