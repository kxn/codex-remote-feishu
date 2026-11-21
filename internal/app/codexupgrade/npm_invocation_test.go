package codexupgrade

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsNPMUsesInstalledNodeWithoutShellOrWorkingDirectoryGuess(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(root, "node_modules", "npm", "bin", "npm-cli.js")
	if err := os.MkdirAll(filepath.Dir(script), 0700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{script, filepath.Join(root, "node.exe")} {
		if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	executable, args, err := windowsNPMInvocation(filepath.Join(root, "npm.cmd"), []string{"install", "-g", "@openai/codex@0.2.0"})
	if err != nil {
		t.Fatal(err)
	}
	if executable != filepath.Join(root, "node.exe") || len(args) != 4 || args[0] != script || args[3] != "@openai/codex@0.2.0" {
		t.Fatalf("wrong native npm invocation: %s %v", executable, args)
	}
	if _, _, err = windowsNPMInvocation(filepath.Join(root, "other.cmd"), nil); err == nil {
		t.Fatal("arbitrary shell launcher accepted")
	}
	if err = os.Remove(script); err != nil {
		t.Fatal(err)
	}
	if _, _, err = windowsNPMInvocation(filepath.Join(root, "npm.cmd"), nil); err == nil {
		t.Fatal("missing npm native source accepted")
	}
}
