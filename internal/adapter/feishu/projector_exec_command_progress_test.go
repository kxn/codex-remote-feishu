package feishu

import (
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestProjectExecCommandProgressCreatesReplyCard(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind:             control.UIEventExecCommandProgress,
		SurfaceSessionID: "surface-1",
		SourceMessageID:  "om-source-1",
		ExecCommandProgress: &control.ExecCommandProgress{
			ThreadID: "thread-1",
			TurnID:   "turn-1",
			ItemID:   "cmd-1",
			Commands: []string{
				`/bin/bash -lc "npm test"`,
				`bash -lc 'go test ./...'`,
			},
		},
	})
	if len(ops) != 1 {
		t.Fatalf("expected one operation, got %#v", ops)
	}
	op := ops[0]
	if op.Kind != OperationSendCard || op.ReplyToMessageID != "om-source-1" || !op.CardUpdateMulti {
		t.Fatalf("expected initial exec progress card reply, got %#v", op)
	}
	if !strings.Contains(op.CardBody, "npm test") || !strings.Contains(op.CardBody, "go test ./...") {
		t.Fatalf("expected command list body to include normalized commands, got %#v", op)
	}
	if strings.Contains(op.CardBody, "bash -lc") {
		t.Fatalf("expected command list body to strip shell wrapper, got %#v", op)
	}
	if strings.Contains(op.CardBody, "状态") || strings.Contains(op.CardBody, "目录") {
		t.Fatalf("expected command list body only, got %#v", op)
	}
}

func TestProjectExecCommandProgressUpdatesExistingCard(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind:             control.UIEventExecCommandProgress,
		SurfaceSessionID: "surface-1",
		SourceMessageID:  "om-source-1",
		ExecCommandProgress: &control.ExecCommandProgress{
			ThreadID:  "thread-1",
			TurnID:    "turn-1",
			ItemID:    "cmd-1",
			MessageID: "om-progress-1",
			Command:   "npm test",
			Status:    "completed",
			Final:     true,
		},
	})
	if len(ops) != 1 {
		t.Fatalf("expected one operation, got %#v", ops)
	}
	op := ops[0]
	if op.Kind != OperationUpdateCard || op.MessageID != "om-progress-1" || op.ReplyToMessageID != "" {
		t.Fatalf("expected update operation for existing exec progress card, got %#v", op)
	}
	if op.CardThemeKey != cardThemeInfo {
		t.Fatalf("expected exec progress to use info theme, got %#v", op)
	}
}
