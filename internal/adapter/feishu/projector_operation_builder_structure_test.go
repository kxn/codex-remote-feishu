package feishu

import (
	"os"
	"strings"
	"testing"
)

func TestProjectEventBaseUsesCardOperationBuilder(t *testing.T) {
	source := readProjectorSource(t, "projector.go")
	body := sourceBetween(t, source, "func (p *Projector) projectEventBase(", "func (p *Projector) projectBlock(")
	for _, forbidden := range []string{
		"cardEnvelope:     cardEnvelopeV2",
		"card:             rawCardDocument(",
		"card:             rawCardDocumentWithHeader(",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("projectEventBase should build card operations through the shared builder, found %q", forbidden)
		}
	}
	if count := strings.Count(body, "newEventCardOperation("); count < 8 {
		t.Fatalf("expected common card payloads to use newEventCardOperation, got %d", count)
	}
}

func TestProjectorSideCardPathsUseCardOperationBuilder(t *testing.T) {
	for _, file := range []string{
		"projector_exec_command_progress.go",
		"projector_thread_history.go",
	} {
		source := readProjectorSource(t, file)
		if strings.Contains(source, "cardEnvelope:") || strings.Contains(source, "rawCardDocument(") {
			t.Fatalf("%s should use shared card operation builder for send/update projector paths", file)
		}
		if !strings.Contains(source, "newEventCardOperation(") {
			t.Fatalf("%s should call newEventCardOperation", file)
		}
	}
}

func readProjectorSource(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(data)
}

func sourceBetween(t *testing.T, source, start, end string) string {
	t.Helper()
	startIdx := strings.Index(source, start)
	if startIdx < 0 {
		t.Fatalf("missing start marker %q", start)
	}
	rest := source[startIdx:]
	endIdx := strings.Index(rest, end)
	if endIdx < 0 {
		t.Fatalf("missing end marker %q", end)
	}
	return rest[:endIdx]
}
