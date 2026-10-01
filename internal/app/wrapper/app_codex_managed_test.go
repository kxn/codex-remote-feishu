package wrapper

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func TestManagedCodexSelectionIsLimitedToOwnedHeadlessChildren(t *testing.T) {
	for _, cfg := range []Config{
		{Source: "vscode", Managed: true, Lifetime: "host-bound"},
		{Source: "headless", Lifetime: "standalone"},
		{Source: "headless", Managed: true, Lifetime: "host-bound"},
	} {
		cfg.CodexRealBinary = "/original/codex"
		app := New(cfg)
		got, err := app.selectCodexChildBinary(context.Background(), nil, func(context.Context, string, string, []string) (string, error) {
			t.Fatal("external client triggered a private update")
			return "", nil
		})
		if err != nil || got != cfg.CodexRealBinary {
			t.Fatalf("got %q, %v", got, err)
		}
	}
}

func TestManagedCodexSelectionUsesPrivateRootAndRestoredEnvironment(t *testing.T) {
	root := t.TempDir()
	app := New(Config{Source: "headless", Managed: true, Lifetime: "daemon-owned", CodexRealBinary: "/original/codex", RuntimePaths: relayruntime.Paths{StateDir: root}})
	env := []string{"HTTPS_PROXY=http://localhost:1234", "CODEX_HOME=/original"}
	got, err := app.selectCodexChildBinary(context.Background(), env, func(ctx context.Context, configured, privateRoot string, childEnv []string) (string, error) {
		if configured != "/original/codex" || privateRoot != filepath.Join(root, "codex-runtime") || !reflect.DeepEqual(childEnv, env) {
			t.Fatalf("wrong update material: %s %s %v", configured, privateRoot, childEnv)
		}
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 5*time.Second {
			t.Fatal("update exceeds child startup watchdog budget")
		}
		return "/private/codex", nil
	})
	if err != nil || got != "/private/codex" {
		t.Fatalf("got %q, %v", got, err)
	}
	_, err = app.selectCodexChildBinary(context.Background(), env, func(context.Context, string, string, []string) (string, error) {
		return "", errors.New("install failed")
	})
	if err == nil {
		t.Fatal("known failed update fell back to old binary")
	}
}

func TestManagedCodexRestartPreparationFailureKeepsCurrentChild(t *testing.T) {
	app := New(Config{Source: "headless", Managed: true, Lifetime: "daemon-owned", Args: []string{"app-server"}})
	stopped := false
	current := &childSession{cancel: func() { stopped = true }}
	next, err := app.restartChildSession(context.Background(), restartRequest{}, current, nil, nil, nil, nil, nil, nil, nil, 0, nil, nil, nil)
	if err == nil || stopped || next != current {
		t.Fatalf("failed update stopped current child: next=%p current=%p stopped=%v err=%v", next, current, stopped, err)
	}
}
