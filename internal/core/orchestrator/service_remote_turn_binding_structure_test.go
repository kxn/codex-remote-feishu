package orchestrator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoteTurnBindingMapsStayBehindFacade(t *testing.T) {
	allowed := map[string]bool{
		"service_runtime_clusters.go": true,
		"service_queue_binding.go":    true,
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob orchestrator sources: %v", err)
	}
	var offenders []string
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") || allowed[file] {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		source := string(data)
		if strings.Contains(source, "s.turns.pendingRemote") || strings.Contains(source, "s.turns.activeRemote") {
			offenders = append(offenders, file)
		}
	}
	if len(offenders) > 0 {
		t.Fatalf("remote turn binding maps should be accessed through facade helpers, offenders: %s", strings.Join(offenders, ", "))
	}
}
