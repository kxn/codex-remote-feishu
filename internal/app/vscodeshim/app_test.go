package vscodeshim

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/managedshim"
)

func TestResolveLaunchPlanUsesStateBinding(t *testing.T) {
	dir := t.TempDir()
	entrypoint := filepath.Join(dir, "bundle", "codex")
	realPath := managedshim.RealBinaryPath(entrypoint)
	sidecarPath := managedshim.SidecarPath(entrypoint)
	statePath := filepath.Join(dir, "install-state.json")
	configPath := filepath.Join(dir, "config.json")
	targetBinary := filepath.Join(dir, "bin", "codex-remote")

	writeExecutable(t, realPath, "real-codex")
	writeExecutable(t, targetBinary, "codex-remote")
	writeConfigFile(t, configPath)
	writeInstallStateFile(t, statePath, `{
  "configPath": "`+configPath+`",
  "currentBinaryPath": "`+targetBinary+`"
}`)
	if err := managedshim.WriteSidecar(sidecarPath, managedshim.Sidecar{
		InstallStatePath: statePath,
		ConfigPath:       configPath,
		InstanceID:       "stable",
	}); err != nil {
		t.Fatalf("WriteSidecar: %v", err)
	}

	plan, err := resolveLaunchPlan(entrypoint, []string{"PATH=/bin"})
	if err != nil {
		t.Fatalf("resolveLaunchPlan: %v", err)
	}
	if plan.Fallback {
		t.Fatal("did not expect fallback plan")
	}
	if plan.BinaryPath != targetBinary {
		t.Fatalf("BinaryPath = %q, want %q", plan.BinaryPath, targetBinary)
	}
	if envValue(plan.Env, "CODEX_REMOTE_CONFIG") != configPath {
		t.Fatalf("CODEX_REMOTE_CONFIG = %q", envValue(plan.Env, "CODEX_REMOTE_CONFIG"))
	}
	if envValue(plan.Env, "CODEX_REAL_BINARY") != realPath {
		t.Fatalf("CODEX_REAL_BINARY = %q", envValue(plan.Env, "CODEX_REAL_BINARY"))
	}
}

func TestResolveLaunchPlanFallsBackWhenConfigIsMissing(t *testing.T) {
	dir := t.TempDir()
	entrypoint := filepath.Join(dir, "bundle", "codex")
	realPath := managedshim.RealBinaryPath(entrypoint)
	sidecarPath := managedshim.SidecarPath(entrypoint)
	statePath := filepath.Join(dir, "install-state.json")
	targetBinary := filepath.Join(dir, "bin", "codex-remote")

	writeExecutable(t, realPath, "real-codex")
	writeExecutable(t, targetBinary, "codex-remote")
	writeInstallStateFile(t, statePath, `{
  "configPath": "`+filepath.Join(dir, "missing-config.json")+`",
  "currentBinaryPath": "`+targetBinary+`"
}`)
	if err := managedshim.WriteSidecar(sidecarPath, managedshim.Sidecar{
		InstallStatePath: statePath,
		ConfigPath:       filepath.Join(dir, "missing-config.json"),
	}); err != nil {
		t.Fatalf("WriteSidecar: %v", err)
	}

	plan, err := resolveLaunchPlan(entrypoint, []string{"PATH=/bin"})
	if err != nil {
		t.Fatalf("resolveLaunchPlan: %v", err)
	}
	if !plan.Fallback {
		t.Fatal("expected fallback plan")
	}
	if plan.BinaryPath != realPath {
		t.Fatalf("BinaryPath = %q, want %q", plan.BinaryPath, realPath)
	}
}

func TestResolveLaunchPlanRejectsRecursiveTargetAndFallsBack(t *testing.T) {
	dir := t.TempDir()
	entrypoint := filepath.Join(dir, "bundle", "codex")
	realPath := managedshim.RealBinaryPath(entrypoint)
	sidecarPath := managedshim.SidecarPath(entrypoint)
	statePath := filepath.Join(dir, "install-state.json")
	configPath := filepath.Join(dir, "config.json")

	writeExecutable(t, realPath, "real-codex")
	writeConfigFile(t, configPath)
	writeInstallStateFile(t, statePath, `{
  "configPath": "`+configPath+`",
  "currentBinaryPath": "`+entrypoint+`"
}`)
	if err := managedshim.WriteSidecar(sidecarPath, managedshim.Sidecar{
		InstallStatePath: statePath,
		ConfigPath:       configPath,
	}); err != nil {
		t.Fatalf("WriteSidecar: %v", err)
	}

	plan, err := resolveLaunchPlan(entrypoint, []string{"PATH=/bin"})
	if err != nil {
		t.Fatalf("resolveLaunchPlan: %v", err)
	}
	if !plan.Fallback {
		t.Fatal("expected recursive target to fall back")
	}
	if plan.BinaryPath != realPath {
		t.Fatalf("BinaryPath = %q, want %q", plan.BinaryPath, realPath)
	}
}

func TestResolveLaunchPlanKeepsPerEntrypointBindingsSeparate(t *testing.T) {
	dir := t.TempDir()

	entrypointA := filepath.Join(dir, "bundle-a", "codex")
	entrypointB := filepath.Join(dir, "bundle-b", "codex")
	realA := managedshim.RealBinaryPath(entrypointA)
	realB := managedshim.RealBinaryPath(entrypointB)
	stateA := filepath.Join(dir, "instance-a", "install-state.json")
	stateB := filepath.Join(dir, "instance-b", "install-state.json")
	configA := filepath.Join(dir, "instance-a", "config.json")
	configB := filepath.Join(dir, "instance-b", "config.json")
	targetA := filepath.Join(dir, "instance-a", "bin", "codex-remote")
	targetB := filepath.Join(dir, "instance-b", "bin", "codex-remote")

	writeExecutable(t, realA, "real-a")
	writeExecutable(t, realB, "real-b")
	writeExecutable(t, targetA, "codex-remote-a")
	writeExecutable(t, targetB, "codex-remote-b")
	writeConfigFile(t, configA)
	writeConfigFile(t, configB)
	writeInstallStateFile(t, stateA, `{
  "configPath": "`+configA+`",
  "currentBinaryPath": "`+targetA+`"
}`)
	writeInstallStateFile(t, stateB, `{
  "configPath": "`+configB+`",
  "currentBinaryPath": "`+targetB+`"
}`)
	if err := managedshim.WriteSidecar(managedshim.SidecarPath(entrypointA), managedshim.Sidecar{
		InstallStatePath: stateA,
		ConfigPath:       configA,
		InstanceID:       "instance-a",
	}); err != nil {
		t.Fatalf("WriteSidecar(a): %v", err)
	}
	if err := managedshim.WriteSidecar(managedshim.SidecarPath(entrypointB), managedshim.Sidecar{
		InstallStatePath: stateB,
		ConfigPath:       configB,
		InstanceID:       "instance-b",
	}); err != nil {
		t.Fatalf("WriteSidecar(b): %v", err)
	}

	planA, err := resolveLaunchPlan(entrypointA, nil)
	if err != nil {
		t.Fatalf("resolveLaunchPlan(a): %v", err)
	}
	planB, err := resolveLaunchPlan(entrypointB, nil)
	if err != nil {
		t.Fatalf("resolveLaunchPlan(b): %v", err)
	}

	if planA.BinaryPath != targetA || planB.BinaryPath != targetB {
		t.Fatalf("unexpected target binaries: a=%q b=%q", planA.BinaryPath, planB.BinaryPath)
	}
	if envValue(planA.Env, "CODEX_REMOTE_CONFIG") != configA || envValue(planB.Env, "CODEX_REMOTE_CONFIG") != configB {
		t.Fatalf("unexpected config bindings: a=%q b=%q", envValue(planA.Env, "CODEX_REMOTE_CONFIG"), envValue(planB.Env, "CODEX_REMOTE_CONFIG"))
	}
	if envValue(planA.Env, "CODEX_REAL_BINARY") != realA || envValue(planB.Env, "CODEX_REAL_BINARY") != realB {
		t.Fatalf("unexpected real binary bindings: a=%q b=%q", envValue(planA.Env, "CODEX_REAL_BINARY"), envValue(planB.Env, "CODEX_REAL_BINARY"))
	}
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			return strings.TrimPrefix(item, prefix)
		}
	}
	return ""
}

func writeExecutable(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func writeConfigFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("{\"version\":1}\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func writeInstallStateFile(t *testing.T, path, raw string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}
}
