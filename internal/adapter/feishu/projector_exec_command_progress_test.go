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
			Command:  "npm test",
			CWD:      "/data/dl/droid",
			Status:   "running",
		},
	})
	if len(ops) != 1 {
		t.Fatalf("expected one operation, got %#v", ops)
	}
	op := ops[0]
	if op.Kind != OperationSendCard || op.ReplyToMessageID != "om-source-1" || !op.CardUpdateMulti {
		t.Fatalf("expected initial exec progress card reply, got %#v", op)
	}
	if !strings.Contains(op.CardBody, "`npm test`") || !strings.Contains(op.CardBody, "`/data/dl/droid`") {
		t.Fatalf("expected command progress body to include command and cwd, got %#v", op)
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
	if op.CardThemeKey != cardThemeSuccess {
		t.Fatalf("expected completed exec progress to use success theme, got %#v", op)
	}
}
