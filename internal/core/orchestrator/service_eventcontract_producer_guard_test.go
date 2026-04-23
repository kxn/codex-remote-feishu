package orchestrator

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCanonicalUIProducerGuard(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
	dirs := []string{
		filepath.Join(root, "internal", "core", "orchestrator"),
		filepath.Join(root, "internal", "app", "daemon"),
		filepath.Join(root, "internal", "app", "cronruntime"),
	}
	bannedTokens := []string{
		"legacyUIEventFromContract",
		"PageView:",
		"SelectionView:",
		"RequestView:",
		"PathPickerView:",
		"TargetPickerView:",
		"ThreadHistoryView:",
	}

	for _, dir := range dirs {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return nil
			}
			if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			text := string(content)
			for _, token := range bannedTokens {
				if strings.Contains(text, token) {
					t.Errorf("%s contains banned token %q; UI events must go through the canonical producer", path, token)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
}
