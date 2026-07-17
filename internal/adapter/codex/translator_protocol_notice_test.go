package codex

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestObserveServerProtocolNoticesExtractTypedCarrier(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		method   string
		kind     string
		threadID string
		summary  string
		details  string
		path     string
		rangeVal string
	}{
		{
			name:     "warning",
			raw:      `{"method":"warning","params":{"threadId":"thread-1","message":"Context is almost full."}}`,
			method:   "warning",
			kind:     "warning",
			threadID: "thread-1",
			summary:  "Context is almost full.",
		},
		{
			name:     "guardian warning",
			raw:      `{"method":"guardianWarning","params":{"threadId":"thread-1","message":"Risky command blocked."}}`,
			method:   "guardianWarning",
			kind:     "guardian",
			threadID: "thread-1",
			summary:  "Risky command blocked.",
		},
		{
			name:    "deprecation notice",
			raw:     `{"method":"deprecationNotice","params":{"summary":"Old setting is deprecated.","details":"Use new setting."}}`,
			method:  "deprecationNotice",
			kind:    "deprecation",
			summary: "Old setting is deprecated.",
			details: "Use new setting.",
		},
		{
			name:     "config warning",
			raw:      `{"method":"configWarning","params":{"threadId":"thread-2","summary":"Invalid config.","details":"Unknown key.","path":"/tmp/config.toml","range":{"startLine":3,"startColumn":5,"endLine":3,"endColumn":12}}}`,
			method:   "configWarning",
			kind:     "config",
			threadID: "thread-2",
			summary:  "Invalid config.",
			details:  "Unknown key.",
			path:     "/tmp/config.toml",
			rangeVal: "3:5-3:12",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := NewTranslator("inst-1")
			result, err := tr.ObserveServer([]byte(tt.raw))
			if err != nil {
				t.Fatalf("observe protocol notice: %v", err)
			}
			if len(result.Events) != 1 {
				t.Fatalf("expected one protocol notice event, got %#v", result.Events)
			}
			event := result.Events[0]
			if event.Kind != agentproto.EventProtocolNotice || event.ProtocolNotice == nil {
				t.Fatalf("expected protocol notice event, got %#v", event)
			}
			notice := event.ProtocolNotice
			if notice.Method != tt.method || notice.Kind != tt.kind || notice.ThreadID != tt.threadID || notice.Summary != tt.summary || notice.Details != tt.details || notice.Path != tt.path || notice.Range != tt.rangeVal {
				t.Fatalf("unexpected notice carrier: %#v", notice)
			}
			if event.Metadata["method"] != tt.method || event.Metadata["kind"] != tt.kind {
				t.Fatalf("unexpected notice metadata: %#v", event.Metadata)
			}
		})
	}
}

func TestObserveServerProtocolNoticeDropsUnusablePayload(t *testing.T) {
	tr := NewTranslator("inst-1")
	result, err := tr.ObserveServer([]byte(`{"method":"warning","params":{"threadId":"thread-1"}}`))
	if err != nil {
		t.Fatalf("observe empty warning: %v", err)
	}
	if len(result.Events) != 0 {
		t.Fatalf("expected empty warning to be ignored, got %#v", result.Events)
	}
}
