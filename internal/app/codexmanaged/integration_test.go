package codexmanaged

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/execlaunch"
)

// This opt-in test downloads a private official npm package and only invokes
// --version. It never starts an app-server or changes service lifecycle state.
func TestEnsureRealPrivateInstall(t *testing.T) {
	configured := os.Getenv("CODEX_MANAGED_INTEGRATION_BINARY")
	if configured == "" {
		t.Skip("set CODEX_MANAGED_INTEGRATION_BINARY to an older real Codex CLI")
	}
	t.Setenv("CODEX_HOME", t.TempDir())
	env := os.Environ()
	before, err := readBinaryVersionWithEnv(context.Background(), configured, env)
	if err != nil {
		t.Fatal(err)
	}
	configuredVersion, err := parseVersion(before)
	if err != nil {
		t.Fatal(err)
	}
	globalPackage, globalMetadata := globalPackageSnapshot(t, env)
	root := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	binary, err := EnsureWithEnv(ctx, configured, root, env)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(binary, filepath.Join(root, "releases")+string(os.PathSeparator)) {
		t.Fatalf("did not install private update: configured=%s selected=%s", before, binary)
	}
	actual, err := readBinaryVersionWithEnv(ctx, binary, env)
	if err != nil {
		t.Fatal(err)
	}
	installedVersion, err := parseVersion(actual)
	if err != nil || configuredVersion.newerThan(installedVersion) {
		t.Fatalf("private version %s older than configured %s: %v", actual, before, err)
	}
	info, err := os.Stat(binary)
	if err != nil {
		t.Fatal(err)
	}
	cacheBefore, err := os.ReadFile(filepath.Join(root, "check.json"))
	if err != nil {
		t.Fatal(err)
	}
	again, err := EnsureWithEnv(ctx, configured, root, env)
	if err != nil || again != binary {
		t.Fatalf("second selection=%s err=%v", again, err)
	}
	infoAgain, err := os.Stat(binary)
	if err != nil || !os.SameFile(info, infoAgain) || !info.ModTime().Equal(infoAgain.ModTime()) {
		t.Fatalf("cached binary was replaced: %v", err)
	}
	cacheAfter, err := os.ReadFile(filepath.Join(root, "check.json"))
	if err != nil || string(cacheAfter) != string(cacheBefore) {
		t.Fatalf("fresh lookup cache was rewritten: %v", err)
	}
	if after, err := readBinaryVersionWithEnv(ctx, configured, env); err != nil || after != before {
		t.Fatalf("configured CLI changed: before=%s after=%s err=%v", before, after, err)
	}
	if after, err := os.ReadFile(globalPackage); err != nil || string(after) != string(globalMetadata) {
		t.Fatalf("global npm package metadata changed: %v", err)
	}
	if releases, err := os.ReadDir(filepath.Join(root, "releases")); err != nil || len(releases) != 1 {
		t.Fatalf("unexpected releases after second selection: count=%d err=%v", len(releases), err)
	}
	t.Logf("private Codex %s installed and reused; original CLI %s and global npm package unchanged", actual, before)
}

func globalPackageSnapshot(t *testing.T, env []string) (string, []byte) {
	t.Helper()
	npm, err := executableFromEnv("npm", env)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := execlaunch.CommandContext(ctx, npm, "root", "-g")
	cmd.Env = append([]string{}, env...)
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("snapshot global npm package root: %v", err)
	}
	path := filepath.Join(strings.TrimSpace(string(output)), "@openai", "codex", "package.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("snapshot global npm Codex package: %v", err)
	}
	return path, raw
}
