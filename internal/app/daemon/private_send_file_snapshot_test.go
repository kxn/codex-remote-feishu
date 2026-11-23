package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrivateSendFileSnapshotSurvivesSourceReplacement(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(source, []byte("selected bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	snapshot, size, _, err := snapshotPrivateSendFile(t.Context(), root, "effect", source)
	if err != nil {
		t.Fatal(err)
	}
	replacement := source + ".replacement"
	if err = os.WriteFile(replacement, []byte("other content"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(replacement, source); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(snapshot)
	if err != nil || string(content) != "selected bytes" || size != int64(len(content)) || filepath.Base(snapshot) != "report.txt" {
		t.Fatalf("frozen file=%q size=%d err=%v", content, size, err)
	}
	info, err := os.Stat(snapshot)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("snapshot permissions: %v %v", info, err)
	}
}
