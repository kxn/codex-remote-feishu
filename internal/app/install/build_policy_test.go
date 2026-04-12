package install

import (
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/buildinfo"
)

func withBuildFlavorForInstallTest(t *testing.T, flavor buildinfo.Flavor) {
	t.Helper()
	previous := buildinfo.FlavorValue
	buildinfo.FlavorValue = string(flavor)
	t.Cleanup(func() {
		buildinfo.FlavorValue = previous
	})
}

func TestInferReleaseTrackUsesBuildFlavorFallback(t *testing.T) {
	withBuildFlavorForInstallTest(t, buildinfo.FlavorShipping)
	if got := inferReleaseTrack("", string(InstallSourceRepo)); got != ReleaseTrackProduction {
		t.Fatalf("shipping repo inferReleaseTrack = %q, want production", got)
	}

	withBuildFlavorForInstallTest(t, buildinfo.FlavorDev)
	if got := inferReleaseTrack("", string(InstallSourceRepo)); got != ReleaseTrackAlpha {
		t.Fatalf("dev repo inferReleaseTrack = %q, want alpha", got)
	}
}

func TestBootstrapShippingFlavorKeepsPprofDisabledAndDefaultsTrackToProduction(t *testing.T) {
	withBuildFlavorForInstallTest(t, buildinfo.FlavorShipping)

	baseDir := t.TempDir()
	binaryPath := seedBinary(t, filepath.Join(baseDir, "source-bin", "codex-remote"), "binary-bin")
	service := NewService()
	state, err := service.Bootstrap(Options{
		BaseDir:    baseDir,
		BinaryPath: binaryPath,
	})
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if state.CurrentTrack != ReleaseTrackProduction {
		t.Fatalf("CurrentTrack = %q, want production", state.CurrentTrack)
	}

	cfg := loadAppConfigForTest(t, state.ConfigPath)
	if cfg.Debug.Pprof == nil {
		t.Fatal("expected pprof settings to be provisioned for instance ports")
	}
	if cfg.Debug.Pprof.Enabled {
		t.Fatalf("expected shipping bootstrap to keep pprof disabled, got %#v", cfg.Debug.Pprof)
	}
}

func TestBootstrapDevFlavorEnablesPprofByDefault(t *testing.T) {
	withBuildFlavorForInstallTest(t, buildinfo.FlavorDev)

	baseDir := t.TempDir()
	binaryPath := seedBinary(t, filepath.Join(baseDir, "source-bin", "codex-remote"), "binary-bin")
	service := NewService()
	state, err := service.Bootstrap(Options{
		BaseDir:    baseDir,
		BinaryPath: binaryPath,
	})
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	cfg := loadAppConfigForTest(t, state.ConfigPath)
	if cfg.Debug.Pprof == nil || !cfg.Debug.Pprof.Enabled {
		t.Fatalf("expected dev bootstrap to enable pprof by default, got %#v", cfg.Debug.Pprof)
	}
}
