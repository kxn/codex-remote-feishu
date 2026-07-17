package orchestrator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPendingSteerBindingMapStaysBehindFacade(t *testing.T) {
	allowed := map[string]bool{
		"service_pending_steer_binding.go": true,
		"service_runtime_clusters.go":      true,
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
		if strings.Contains(source, ".turns.pendingSteers") {
			offenders = append(offenders, file)
		}
	}
	if len(offenders) > 0 {
		t.Fatalf("pending steer binding map should be accessed through facade helpers, offenders: %s", strings.Join(offenders, ", "))
	}
}
