package wrapper

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

var (
	mockClaudeBinaryOnce sync.Once
	mockClaudeBinaryPath string
	mockClaudeBinaryErr  error
)

func installMockClaudeHelper(t *testing.T, scenario string) string {
	t.Helper()
	t.Setenv("MOCKCLAUDE_SCENARIO", scenario)

	repoRoot := wrapperTestRepoRoot(t)
	path, err := buildMockClaudeBinary(repoRoot)
	if err != nil {
		t.Fatalf("build mockclaude helper: %v", err)
	}
	return path
}

func buildMockClaudeBinary(repoRoot string) (string, error) {
	mockClaudeBinaryOnce.Do(func() {
		outputDir, err := os.MkdirTemp("", "mockclaude-bin-*")
		if err != nil {
			mockClaudeBinaryErr = err
			return
		}

		binaryName := "mockclaude"
		if runtime.GOOS == "windows" {
			binaryName += ".exe"
		}
		outputPath := filepath.Join(outputDir, binaryName)

		cmd := exec.Command("go", "build", "-o", outputPath, "./testkit/mockclaude/cmd/mockclaude")
		cmd.Dir = repoRoot
		output, err := cmd.CombinedOutput()
		if err != nil {
			mockClaudeBinaryErr = fmt.Errorf("go build ./testkit/mockclaude/cmd/mockclaude: %w\n%s", err, string(output))
			return
		}
		mockClaudeBinaryPath = outputPath
	})
	return mockClaudeBinaryPath, mockClaudeBinaryErr
}
