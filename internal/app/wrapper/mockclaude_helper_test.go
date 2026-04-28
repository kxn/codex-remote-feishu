package wrapper

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/testkit/mockclaude"
)

func TestHelperProcessMockClaude(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "mockclaude" {
		return
	}
	if err := mockclaude.RunIO(mockclaude.NewFromEnv(), os.Stdin, os.Stdout); err != nil {
		var exitErr mockclaude.ExitCodeError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.Code)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func installMockClaudeHelper(t *testing.T, scenario string) string {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	script := filepath.Join(t.TempDir(), "mockclaude-helper.sh")
	content := strings.Join([]string{
		"#!/usr/bin/env bash",
		"set -euo pipefail",
		"export GO_WANT_HELPER_PROCESS=mockclaude",
		"export MOCKCLAUDE_SCENARIO=" + shellSingleQuote(scenario),
		"exec " + shellSingleQuote(executable) + " -test.run '^TestHelperProcessMockClaude$' -- \"$@\"",
		"",
	}, "\n")
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("WriteFile(%s): %v", script, err)
	}
	return script
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}
