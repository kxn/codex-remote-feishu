package codexmanaged

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/execlaunch"
)

func fakeOperations(latest string) operations {
	return operations{
		lookup: func(context.Context) (string, error) { return latest, nil },
		readVersion: func(_ context.Context, binary string) (string, error) {
			raw, err := os.ReadFile(binary)
			return string(raw), err
		},
		install: func(_ context.Context, prefix, version string) error { return writeFakeInstall(prefix, version) },
		now:     time.Now,
	}
}

func writeFakeInstall(prefix, version string) error {
	triple, platform, executable := platformPackage()
	binary := filepath.Join(prefix, "node_modules", "@openai", platform, "vendor", triple, "bin", executable)
	if err := os.MkdirAll(filepath.Dir(binary), 0o700); err != nil {
		return err
	}
	return os.WriteFile(binary, []byte(version), 0o700)
}

func configuredFile(t *testing.T, version string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "configured-codex")
	if err := os.WriteFile(path, []byte(version), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestEnsurePublishesPrivateInstallAndUsesFreshCheck(t *testing.T) {
	root := t.TempDir()
	configured := configuredFile(t, "0.153.4")
	ops := fakeOperations("0.159.3")
	lookups, installs := 0, 0
	ops.lookup = func(context.Context) (string, error) { lookups++; return "0.159.3", nil }
	ops.install = func(_ context.Context, prefix, target string) error {
		installs++
		if filepath.Dir(prefix) != root || !strings.HasPrefix(filepath.Base(prefix), ".install-") {
			t.Fatalf("not a private staging dir: %s", prefix)
		}
		return writeFakeInstall(prefix, target)
	}
	first, err := ensure(context.Background(), configured, root, ops)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(first, filepath.Join(root, "releases", "0.159.3")+string(os.PathSeparator)) {
		t.Fatalf("unexpected published path %s", first)
	}
	second, err := ensure(context.Background(), configured, root, ops)
	if err != nil {
		t.Fatal(err)
	}
	if second != first || lookups != 1 || installs != 1 {
		t.Fatalf("second=%s first=%s lookups=%d installs=%d", second, first, lookups, installs)
	}
	if raw, _ := os.ReadFile(configured); string(raw) != "0.153.4" {
		t.Fatal("configured binary changed")
	}
	if matches, _ := filepath.Glob(filepath.Join(root, ".install-*")); len(matches) != 0 {
		t.Fatalf("staging survived: %v", matches)
	}
}

func TestEnsureFailedUpdateDoesNotLaunchOldBinaryAndRetriesInstall(t *testing.T) {
	root := t.TempDir()
	ops := fakeOperations("0.159.3")
	lookups, installs := 0, 0
	ops.lookup = func(context.Context) (string, error) { lookups++; return "0.159.3", nil }
	ops.install = func(context.Context, string, string) error { installs++; return errors.New("install failed") }
	configured := configuredFile(t, "0.153.4")
	for i := 0; i < 2; i++ {
		binary, err := ensure(context.Background(), configured, root, ops)
		if err == nil || binary != "" || !strings.Contains(err.Error(), "install private Codex 0.159.3") {
			t.Fatalf("binary=%s err=%v", binary, err)
		}
	}
	if lookups != 1 || installs != 2 {
		t.Fatalf("lookups=%d installs=%d", lookups, installs)
	}
	if matches, _ := filepath.Glob(filepath.Join(root, ".install-*")); len(matches) != 0 {
		t.Fatalf("staging survived: %v", matches)
	}
}

func TestEnsureOfflineUsesVerifiedPrivateAndDoesNotCacheFailure(t *testing.T) {
	root := t.TempDir()
	if err := writeFakeInstall(filepath.Join(root, "releases", "0.159.3"), "0.159.3"); err != nil {
		t.Fatal(err)
	}
	ops := fakeOperations("")
	calls := 0
	ops.lookup = func(context.Context) (string, error) { calls++; return "", errors.New("offline") }
	configured := configuredFile(t, "0.153.4")
	for i := 0; i < 2; i++ {
		binary, err := ensure(context.Background(), configured, root, ops)
		if err != nil || !strings.Contains(binary, filepath.Join("releases", "0.159.3")) {
			t.Fatalf("binary=%s err=%v", binary, err)
		}
	}
	if calls != 2 {
		t.Fatalf("failure was cached: calls=%d", calls)
	}
	if _, err := os.Stat(filepath.Join(root, "check.json")); !os.IsNotExist(err) {
		t.Fatalf("failed lookup wrote cache: %v", err)
	}
	binary, err := ensure(context.Background(), configuredFile(t, "garbage"), t.TempDir(), ops)
	if binary != "" || err == nil {
		t.Fatalf("unverified fallback: %s %v", binary, err)
	}
}

func TestEnsureNoDowngradeAndPreviewChoice(t *testing.T) {
	for _, configured := range []string{"0.160.0", "0.160.0-alpha.1", "0.159.2-alpha.1"} {
		t.Run(configured, func(t *testing.T) {
			ops := fakeOperations("0.159.3")
			ops.install = func(context.Context, string, string) error { t.Fatal("unexpected install"); return nil }
			path := configuredFile(t, configured)
			binary, err := ensure(context.Background(), path, t.TempDir(), ops)
			if err != nil || binary != path {
				t.Fatalf("binary=%s err=%v", binary, err)
			}
		})
	}
	root := t.TempDir()
	if err := writeFakeInstall(filepath.Join(root, "releases", "0.160.0"), "0.160.0"); err != nil {
		t.Fatal(err)
	}
	binary, err := ensure(context.Background(), configuredFile(t, "0.153.4"), root, fakeOperations("0.159.3"))
	if err != nil || !strings.Contains(binary, filepath.Join("releases", "0.160.0")) {
		t.Fatalf("private downgrade: binary=%s err=%v", binary, err)
	}
}

func TestEnsureRejectsWrongInstalledVersionAndUnsafeMetadata(t *testing.T) {
	root := t.TempDir()
	ops := fakeOperations("0.159.3")
	ops.install = func(_ context.Context, prefix, _ string) error { return writeFakeInstall(prefix, "0.157.1") }
	binary, err := ensure(context.Background(), configuredFile(t, "0.153.4"), root, ops)
	if binary != "" || err == nil || !strings.Contains(err.Error(), "expected 0.159.3") {
		t.Fatalf("binary=%s err=%v", binary, err)
	}
	if entries, _ := os.ReadDir(filepath.Join(root, "releases")); len(entries) != 0 {
		t.Fatal("unverified release published")
	}
	for _, unsafe := range []string{"../../escape", "0.159.3/../escape", "0.159.3;bad", "0.159.3-alpha.1"} {
		ops := fakeOperations(unsafe)
		ops.install = func(context.Context, string, string) error { t.Fatalf("unsafe install target %s", unsafe); return nil }
		_, err := ensure(context.Background(), "", t.TempDir(), ops)
		if err == nil {
			t.Fatalf("accepted %s", unsafe)
		}
	}
}

func TestEnsureExpiredSuccessfulCheckQueriesAgain(t *testing.T) {
	root := t.TempDir()
	clock := time.Now()
	ops := fakeOperations("0.159.3")
	ops.now = func() time.Time { return clock }
	configured := configuredFile(t, "0.159.3")
	if _, err := ensure(context.Background(), configured, root, ops); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(checkInterval)
	ops.lookup = func(context.Context) (string, error) { return "0.160.0", nil }
	binary, err := ensure(context.Background(), configured, root, ops)
	if err != nil || !strings.Contains(binary, filepath.Join("releases", "0.160.0")) {
		t.Fatalf("binary=%s err=%v", binary, err)
	}
}

func TestEnsureQuarantinesInvalidReleaseAndRepairsIt(t *testing.T) {
	root := t.TempDir()
	release := filepath.Join(root, "releases", "0.159.3")
	if err := writeFakeInstall(release, "0.157.1"); err != nil {
		t.Fatal(err)
	}
	marker := "keep the old inode"
	if err := os.WriteFile(filepath.Join(release, "marker"), []byte(marker), 0o600); err != nil {
		t.Fatal(err)
	}
	configured := configuredFile(t, "0.153.4")
	binary, err := ensure(context.Background(), configured, root, fakeOperations("0.159.3"))
	if err != nil || !strings.HasPrefix(binary, release+string(os.PathSeparator)) {
		t.Fatalf("binary=%s err=%v", binary, err)
	}
	quarantines, err := filepath.Glob(filepath.Join(root, ".quarantine-0.159.3-*"))
	if err != nil || len(quarantines) != 1 {
		t.Fatalf("quarantines=%v err=%v", quarantines, err)
	}
	if raw, err := os.ReadFile(filepath.Join(quarantines[0], "marker")); err != nil || string(raw) != marker {
		t.Fatalf("quarantine lost content: %q %v", raw, err)
	}
	// Even a newer-looking binary in quarantine must never become a candidate.
	if err := writeFakeInstall(quarantines[0], "99.0.0"); err != nil {
		t.Fatal(err)
	}
	selected, err := ensure(context.Background(), configured, root, fakeOperations("0.159.3"))
	if err != nil || selected != binary {
		t.Fatalf("quarantine selected: %s %v", selected, err)
	}
}

func TestEnsureInvalidConfiguredVersionDoesNotCheckOrInstall(t *testing.T) {
	ops := fakeOperations("0.159.3")
	ops.lookup = func(context.Context) (string, error) {
		t.Fatal("looked up unsupported configured binary")
		return "", nil
	}
	ops.install = func(context.Context, string, string) error {
		t.Fatal("installed over unsupported configured binary")
		return nil
	}
	if _, err := ensure(context.Background(), configuredFile(t, "custom-version"), t.TempDir(), ops); err == nil {
		t.Fatal("accepted unrecognized configured version")
	}
}

func TestEnsureSubprocessWorker(t *testing.T) {
	root := os.Getenv("CODEX_MANAGED_TEST_ROOT")
	if root == "" {
		return
	}
	ops := fakeOperations("0.159.3")
	ops.install = func(_ context.Context, prefix, target string) error {
		file, err := os.OpenFile(filepath.Join(root, "installs"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		_, writeErr := fmt.Fprintln(file, target)
		if err := file.Close(); err != nil {
			return err
		}
		if writeErr != nil {
			return writeErr
		}
		time.Sleep(200 * time.Millisecond)
		return writeFakeInstall(prefix, target)
	}
	binary, err := ensure(context.Background(), filepath.Join(root, "configured"), root, ops)
	if err != nil || !strings.Contains(binary, filepath.Join("releases", "0.159.3")) {
		t.Fatalf("binary=%s err=%v", binary, err)
	}
}

func TestEnsureSerializesAcrossProcesses(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "configured"), []byte("0.153.4"), 0o700); err != nil {
		t.Fatal(err)
	}
	type running struct{ wait func() error }
	var children []running
	for i := 0; i < 3; i++ {
		cmd := execlaunch.Command(os.Args[0], "-test.run=^TestEnsureSubprocessWorker$", "-test.timeout=10s")
		cmd.Env = append(os.Environ(), "CODEX_MANAGED_TEST_ROOT="+root)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		children = append(children, running{wait: cmd.Wait})
	}
	for _, child := range children {
		if err := child.wait(); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(filepath.Join(root, "installs"))
	if err != nil || string(raw) != "0.159.3\n" {
		t.Fatalf("duplicate installation: %q %v", raw, err)
	}
}

func TestInstallPrivateUsesOfficialRegistryAndPrivatePrefix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell npm is Unix-only")
	}
	tools := t.TempDir()
	argsPath := filepath.Join(tools, "args")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$CODEX_MANAGED_TEST_ARGS\"\n"
	if err := os.WriteFile(filepath.Join(tools, "npm"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CODEX_MANAGED_TEST_ARGS", argsPath)
	staging := t.TempDir()
	if err := installPrivate(context.Background(), staging, "0.159.3"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	expected := "install\n--prefix\n" + staging + "\n--no-audit\n--no-fund\n--registry=https://registry.npmjs.org\n@openai/codex@0.159.3\n"
	if string(raw) != expected {
		t.Fatalf("wrong npm install args: %q", raw)
	}
}
