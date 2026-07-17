package daemon

import (
	"os"
	"strings"
	"testing"
)

func TestDaemonIngressUsesCentralSurfaceRecoveryPipeline(t *testing.T) {
	data, err := os.ReadFile("app_ingress.go")
	if err != nil {
		t.Fatalf("read app_ingress.go: %v", err)
	}
	source := string(data)
	entrypoints := []string{"handleAction", "onHello", "onEvents", "onDisconnect", "onTick"}
	for _, entrypoint := range entrypoints {
		body := functionBodyForStructureTest(t, source, entrypoint)
		if !strings.Contains(body, "runSurfaceRecoveryPipelineLocked") {
			t.Fatalf("%s does not call runSurfaceRecoveryPipelineLocked", entrypoint)
		}
		for _, primitive := range []string{
			"consumeVSCodeCompatibilityFollowupLocked",
			"maybePromptVSCodeCompatibilityAtLocked",
			"maybeRecoverVSCodeSurfacesLocked",
			"maybePromptDetachedVSCodeSurfacesLocked",
			"maybeRecoverHeadlessSurfacesLocked",
		} {
			if strings.Contains(body, primitive) {
				t.Fatalf("%s still calls recovery primitive %s directly", entrypoint, primitive)
			}
		}
	}
}

func TestVSCodeCompatibilityFollowupConsumerDoesNotRunRecoveryPrimitives(t *testing.T) {
	data, err := os.ReadFile("app_vscode_migration.go")
	if err != nil {
		t.Fatalf("read app_vscode_migration.go: %v", err)
	}
	body := functionBodyForStructureTest(t, string(data), "consumeVSCodeCompatibilityFollowupLocked")
	for _, primitive := range []string{
		"maybePromptVSCodeCompatibilityAtLocked",
		"promptVSCodeCompatibilityAtLocked",
		"maybeRecoverVSCodeSurfacesLocked",
		"maybePromptDetachedVSCodeSurfacesLocked",
	} {
		if strings.Contains(body, primitive) {
			t.Fatalf("consumeVSCodeCompatibilityFollowupLocked still runs recovery primitive %s directly", primitive)
		}
	}
}

func functionBodyForStructureTest(t *testing.T, source, name string) string {
	t.Helper()
	needle := "func (a *App) " + name + "("
	start := strings.Index(source, needle)
	if start < 0 {
		t.Fatalf("function %s not found", name)
	}
	open := strings.Index(source[start:], "{")
	if open < 0 {
		t.Fatalf("function %s has no body", name)
	}
	pos := start + open
	depth := 0
	for i := pos; i < len(source); i++ {
		switch source[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return source[pos : i+1]
			}
		}
	}
	t.Fatalf("function %s body is not balanced", name)
	return ""
}
