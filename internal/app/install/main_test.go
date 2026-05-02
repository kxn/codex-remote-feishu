package install

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) {
	tempRoot, err := os.MkdirTemp("", "codex-remote-install-test-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "install TestMain temp dir: %v\n", err)
		os.Exit(1)
	}
	homeDir := filepath.Join(tempRoot, "home")
	configHome := filepath.Join(tempRoot, "xdg-config")
	dataHome := filepath.Join(tempRoot, "xdg-data")
	stateHome := filepath.Join(tempRoot, "xdg-state")
	repoRoot := filepath.Join(tempRoot, "repo")
	for _, dir := range []string{homeDir, configHome, dataHome, stateHome, repoRoot} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "install TestMain mkdir %s: %v\n", dir, err)
			os.Exit(1)
		}
	}

	setenvOrExit := func(key, value string) {
		if err := os.Setenv(key, value); err != nil {
			fmt.Fprintf(os.Stderr, "install TestMain setenv %s: %v\n", key, err)
			os.Exit(1)
		}
	}
	setenvOrExit("HOME", homeDir)
	setenvOrExit("XDG_CONFIG_HOME", configHome)
	setenvOrExit("XDG_DATA_HOME", dataHome)
	setenvOrExit("XDG_STATE_HOME", stateHome)
	setenvOrExit(repoRootEnvVar, repoRoot)

	originalHome := serviceUserHomeDir
	originalSystemctl := systemctlUserRunner
	originalLaunchctl := launchctlUserRunner
	serviceUserHomeDir = func() (string, error) { return homeDir, nil }
	systemctlUserRunner = func(_ context.Context, args ...string) (string, error) {
		return "", fmt.Errorf("unexpected live systemctl access in install tests: %v", args)
	}
	launchctlUserRunner = func(_ context.Context, args ...string) (string, error) {
		return "", fmt.Errorf("unexpected live launchctl access in install tests: %v", args)
	}
	defer func() {
		serviceUserHomeDir = originalHome
		systemctlUserRunner = originalSystemctl
		launchctlUserRunner = originalLaunchctl
	}()

	code := m.Run()
	if err := os.RemoveAll(tempRoot); err != nil {
		fmt.Fprintf(os.Stderr, "install TestMain cleanup %s: %v\n", tempRoot, err)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}
