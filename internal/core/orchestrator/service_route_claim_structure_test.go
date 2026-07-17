package orchestrator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRouteClaimMapsStayBehindFacade(t *testing.T) {
	allowed := map[string]bool{
		"service.go":                true,
		"service_routing_claims.go": true,
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
		if strings.Contains(source, ".workspaceClaims") ||
			strings.Contains(source, ".instanceClaims") ||
			strings.Contains(source, ".threadClaims") {
			offenders = append(offenders, file)
		}
	}
	if len(offenders) > 0 {
		t.Fatalf("route claim maps should be accessed through claim facade helpers, offenders: %s", strings.Join(offenders, ", "))
	}
}
