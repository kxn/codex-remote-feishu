package testutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSamePathResolvesExistingSymlinkPrefixWithMissingSuffix(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatalf("mkdir real: %v", err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	left := filepath.Join(link, "missing", "child")
	right := filepath.Join(real, "missing", "child")
	if !SamePath(left, right) {
		t.Fatalf("SamePath(%q, %q) = false, want true", left, right)
	}
}
