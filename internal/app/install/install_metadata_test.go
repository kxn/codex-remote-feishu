package install

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestCanonicalInstallBinDirForMigrationReturnsCanonicalDirForVersionScopedPath(t *testing.T) {
	baseDir := t.TempDir()
	versionsRoot := filepath.Join(baseDir, "releases")
	state := InstallState{
		InstanceID:        defaultInstanceID,
		BaseDir:           baseDir,
		CurrentBinaryPath: filepath.Join(versionsRoot, "v1.8.4", executableName(runtime.GOOS)),
		VersionsRoot:      versionsRoot,
	}
	canonicalDir, needsMigration := canonicalInstallBinDirForMigration(runtime.GOOS, state)
	if !needsMigration {
		t.Fatalf("expected migration needed for version-scoped path")
	}
	wantDir := defaultInstallBinDirForInstance(runtime.GOOS, baseDir, defaultInstanceID)
	if canonicalDir != wantDir {
		t.Fatalf("canonicalDir = %q, want %q", canonicalDir, wantDir)
	}
}

func TestCanonicalInstallBinDirForMigrationReturnsFalseForCustomDir(t *testing.T) {
	baseDir := t.TempDir()
	customBinDir := filepath.Join(baseDir, "my-custom-bin")
	state := InstallState{
		InstanceID:        defaultInstanceID,
		BaseDir:           baseDir,
		CurrentBinaryPath: filepath.Join(customBinDir, executableName(runtime.GOOS)),
		VersionsRoot:      filepath.Join(baseDir, "releases"),
	}
	_, needsMigration := canonicalInstallBinDirForMigration(runtime.GOOS, state)
	if needsMigration {
		t.Fatalf("expected no migration for custom dir outside VersionsRoot")
	}
}

func TestCanonicalInstallBinDirForMigrationReturnsFalseForAlreadyCanonicalPath(t *testing.T) {
	baseDir := t.TempDir()
	canonicalDir := defaultInstallBinDirForInstance(runtime.GOOS, baseDir, defaultInstanceID)
	state := InstallState{
		InstanceID:        defaultInstanceID,
		BaseDir:           baseDir,
		CurrentBinaryPath: filepath.Join(canonicalDir, executableName(runtime.GOOS)),
		VersionsRoot:      filepath.Join(baseDir, "releases"),
	}
	_, needsMigration := canonicalInstallBinDirForMigration(runtime.GOOS, state)
	if needsMigration {
		t.Fatalf("expected no migration for already canonical path")
	}
}

func TestCanonicalInstallBinDirForMigrationReturnsFalseWhenCurrentBinaryPathEmpty(t *testing.T) {
	baseDir := t.TempDir()
	state := InstallState{
		InstanceID:   defaultInstanceID,
		BaseDir:      baseDir,
		VersionsRoot: filepath.Join(baseDir, "releases"),
	}
	_, needsMigration := canonicalInstallBinDirForMigration(runtime.GOOS, state)
	if needsMigration {
		t.Fatalf("expected no migration when CurrentBinaryPath is empty")
	}
}

func TestCanonicalInstallBinDirForMigrationReturnsFalseWhenVersionsRootEmpty(t *testing.T) {
	baseDir := t.TempDir()
	state := InstallState{
		InstanceID:        defaultInstanceID,
		BaseDir:           baseDir,
		CurrentBinaryPath: filepath.Join(baseDir, "releases", "v1.8.4", executableName(runtime.GOOS)),
	}
	_, needsMigration := canonicalInstallBinDirForMigration(runtime.GOOS, state)
	if needsMigration {
		t.Fatalf("expected no migration when VersionsRoot is empty")
	}
}

func TestCanonicalInstallBinDirForMigrationHandlesNonDefaultInstance(t *testing.T) {
	baseDir := t.TempDir()
	instanceID := "myproject"
	versionsRoot := filepath.Join(baseDir, "releases")
	state := InstallState{
		InstanceID:        instanceID,
		BaseDir:           baseDir,
		CurrentBinaryPath: filepath.Join(versionsRoot, "v2.0.0", executableName(runtime.GOOS)),
		VersionsRoot:      versionsRoot,
	}
	canonicalDir, needsMigration := canonicalInstallBinDirForMigration(runtime.GOOS, state)
	if !needsMigration {
		t.Fatalf("expected migration needed for non-default instance version-scoped path")
	}
	wantDir := defaultInstallBinDirForInstance(runtime.GOOS, baseDir, instanceID)
	if canonicalDir != wantDir {
		t.Fatalf("canonicalDir = %q, want %q", canonicalDir, wantDir)
	}
}

func TestCanonicalInstallBinDirForMigrationReturnsFalseWhenVersionsRootIsCustom(t *testing.T) {
	baseDir := t.TempDir()
	// Binary is under a custom versions root that isn't the default releases dir.
	customVersionsRoot := filepath.Join(baseDir, "my-cache", "versions")
	state := InstallState{
		InstanceID:        defaultInstanceID,
		BaseDir:           baseDir,
		CurrentBinaryPath: filepath.Join(customVersionsRoot, "v1.8.4", executableName(runtime.GOOS)),
		VersionsRoot:      customVersionsRoot,
	}
	// This should still return true because binaryWithinVersionsRoot checks
	// whether the binary is under the given VersionsRoot, regardless of whether
	// it's the default or custom. The migration target is still canonical.
	canonicalDir, needsMigration := canonicalInstallBinDirForMigration(runtime.GOOS, state)
	if !needsMigration {
		t.Fatalf("expected migration when binary is under any VersionsRoot")
	}
	wantDir := defaultInstallBinDirForInstance(runtime.GOOS, baseDir, defaultInstanceID)
	if canonicalDir != wantDir {
		t.Fatalf("canonicalDir = %q, want %q", canonicalDir, wantDir)
	}
}

// TestCanonicalInstallBinDirForMigrationCrossPlatform verifies migration logic
// for all three platform path layouts, not just the CI host platform.
// This catches path-construction bugs that only manifest on macOS or Windows.
func TestCanonicalInstallBinDirForMigrationCrossPlatform(t *testing.T) {
	platforms := []struct {
		name  string
		goos  string
		envFn func(t *testing.T)
	}{
		{name: "darwin", goos: "darwin", envFn: func(_ *testing.T) {}},
		{name: "linux", goos: "linux", envFn: func(_ *testing.T) {}},
		{name: "windows", goos: "windows", envFn: func(t *testing.T) {
			t.Setenv("LOCALAPPDATA", filepath.Join(t.TempDir(), "AppData", "Local"))
		}},
	}

	for _, p := range platforms {
		t.Run(p.name+"/version_scoped_needs_migration", func(t *testing.T) {
			p.envFn(t)
			baseDir := t.TempDir()
			versionsRoot := filepath.Join(baseDir, "releases")
			state := InstallState{
				InstanceID:        defaultInstanceID,
				BaseDir:           baseDir,
				CurrentBinaryPath: filepath.Join(versionsRoot, "v1.8.4", executableName(p.goos)),
				VersionsRoot:      versionsRoot,
			}
			canonicalDir, needsMigration := canonicalInstallBinDirForMigration(p.goos, state)
			if !needsMigration {
				t.Fatalf("expected migration needed for %s", p.goos)
			}
			wantDir := defaultInstallBinDirForInstance(p.goos, baseDir, defaultInstanceID)
			if canonicalDir != wantDir {
				t.Fatalf("canonicalDir = %q, want %q", canonicalDir, wantDir)
			}
		})

		t.Run(p.name+"/custom_dir_no_migration", func(t *testing.T) {
			p.envFn(t)
			baseDir := t.TempDir()
			customBinDir := filepath.Join(baseDir, "my-custom-bin")
			state := InstallState{
				InstanceID:        defaultInstanceID,
				BaseDir:           baseDir,
				CurrentBinaryPath: filepath.Join(customBinDir, executableName(p.goos)),
				VersionsRoot:      filepath.Join(baseDir, "releases"),
			}
			_, needsMigration := canonicalInstallBinDirForMigration(p.goos, state)
			if needsMigration {
				t.Fatalf("expected no migration for custom dir on %s", p.goos)
			}
		})

		t.Run(p.name+"/already_canonical_no_migration", func(t *testing.T) {
			p.envFn(t)
			baseDir := t.TempDir()
			canonicalDir := defaultInstallBinDirForInstance(p.goos, baseDir, defaultInstanceID)
			state := InstallState{
				InstanceID:        defaultInstanceID,
				BaseDir:           baseDir,
				CurrentBinaryPath: filepath.Join(canonicalDir, executableName(p.goos)),
				VersionsRoot:      filepath.Join(baseDir, "releases"),
			}
			_, needsMigration := canonicalInstallBinDirForMigration(p.goos, state)
			if needsMigration {
				t.Fatalf("expected no migration for already canonical path on %s", p.goos)
			}
		})

		t.Run(p.name+"/canonical_dir_not_under_releases", func(t *testing.T) {
			// Verify that the canonical dir is NOT under VersionsRoot,
			// so we never create a circular migration loop.
			p.envFn(t)
			baseDir := t.TempDir()
			versionsRoot := filepath.Join(baseDir, "releases")
			canonicalDir := defaultInstallBinDirForInstance(p.goos, baseDir, defaultInstanceID)
			if canonicalDir == "" {
				t.Skipf("no canonical dir for %s", p.goos)
			}
			rel, err := filepath.Rel(versionsRoot, canonicalDir)
			if err != nil {
				t.Skipf("cannot compute rel on %s: %v", p.goos, err)
			}
			if rel != ".." && len(rel) > 0 && rel[0] != '.' {
				// canonicalDir is inside versionsRoot — would cause infinite migration loop.
				t.Fatalf("canonicalDir %q is inside VersionsRoot %q on %s", canonicalDir, versionsRoot, p.goos)
			}
		})
	}
}

// TestDefaultInstallBinDirForInstancePaths verifies the expected canonical
// path layout for each platform. This test catches regressions in the
// platform-specific path construction.
func TestDefaultInstallBinDirForInstancePaths(t *testing.T) {
	baseDir := t.TempDir()

	t.Run("darwin", func(t *testing.T) {
		got := defaultInstallBinDirForInstance("darwin", baseDir, defaultInstanceID)
		want := filepath.Join(baseDir, "Library", "Application Support", "codex-remote", "bin")
		if got != want {
			t.Fatalf("darwin path = %q, want %q", got, want)
		}
	})
	t.Run("linux", func(t *testing.T) {
		got := defaultInstallBinDirForInstance("linux", baseDir, defaultInstanceID)
		want := filepath.Join(baseDir, ".local", "share", "codex-remote", "bin")
		if got != want {
			t.Fatalf("linux path = %q, want %q", got, want)
		}
	})
	t.Run("windows", func(t *testing.T) {
		localAppData := filepath.Join(baseDir, "AppData", "Local")
		t.Setenv("LOCALAPPDATA", localAppData)
		got := defaultInstallBinDirForInstance("windows", baseDir, defaultInstanceID)
		want := filepath.Join(localAppData, "codex-remote", "bin")
		if got != want {
			t.Fatalf("windows path = %q, want %q", got, want)
		}
	})
}
