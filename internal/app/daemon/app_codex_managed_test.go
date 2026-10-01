package daemon

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func TestManagedCodexWarmupBindsStartupProbesToPrivateBinary(t *testing.T) {
	root := t.TempDir()
	env := []string{"HTTPS_PROXY=http://localhost:1234", "CODEX_HOME=/original"}
	app := &App{headlessRuntime: HeadlessRuntimeConfig{CodexRealBinary: "/original/codex", BaseEnv: env, Paths: relayruntime.Paths{StateDir: root}}}
	app.warmManagedCodex(context.Background(), true, func(ctx context.Context, configured, privateRoot string, childEnv []string) (string, error) {
		if configured != "/original/codex" || privateRoot != filepath.Join(root, "codex-runtime") || !reflect.DeepEqual(childEnv, env) {
			t.Fatalf("wrong update material: %s %s %v", configured, privateRoot, childEnv)
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("startup update has no watchdog")
		}
		return "/private/codex", nil
	})
	if app.headlessRuntime.CodexRealBinary != "/private/codex" {
		t.Fatal("startup probes still use the old binary")
	}
}

func TestManagedCodexPeriodicWarmupDoesNotReplaceRunningRuntime(t *testing.T) {
	app := &App{headlessRuntime: HeadlessRuntimeConfig{CodexRealBinary: "/original/codex", Paths: relayruntime.Paths{StateDir: t.TempDir()}}}
	app.warmManagedCodex(context.Background(), false, func(context.Context, string, string, []string) (string, error) {
		return "/private/new-codex", nil
	})
	if app.headlessRuntime.CodexRealBinary != "/original/codex" {
		t.Fatal("periodic warmup changed active runtime")
	}
	app.warmManagedCodex(context.Background(), true, func(context.Context, string, string, []string) (string, error) {
		return "", errors.New("install failed")
	})
	if app.headlessRuntime.CodexRealBinary != "/original/codex" {
		t.Fatal("failed update changed runtime")
	}
}

func TestManagedCodexStartupWarmupPreservesConcurrentConfigChange(t *testing.T) {
	app := &App{headlessRuntime: HeadlessRuntimeConfig{CodexRealBinary: "/original/codex", Paths: relayruntime.Paths{StateDir: t.TempDir()}}}
	app.warmManagedCodex(context.Background(), true, func(context.Context, string, string, []string) (string, error) {
		app.mu.Lock()
		app.headlessRuntime.CodexRealBinary = "/user/new-choice"
		app.mu.Unlock()
		return "/private/codex", nil
	})
	if app.headlessRuntime.CodexRealBinary != "/user/new-choice" {
		t.Fatal("warmup overwrote the user's changed configuration")
	}
}
