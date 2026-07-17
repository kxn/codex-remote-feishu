package orchestrator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestItemBufferMapStaysBehindFacade(t *testing.T) {
	allowed := map[string]bool{
		"service.go":               true,
		"service_helpers_items.go": true,
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
		if strings.Contains(source, ".itemBuffers") {
			offenders = append(offenders, file)
		}
	}
	if len(offenders) > 0 {
		t.Fatalf("item buffer map should be accessed through facade helpers, offenders: %s", strings.Join(offenders, ", "))
	}
}
