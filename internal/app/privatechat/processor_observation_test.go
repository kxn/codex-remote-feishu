package privatechat

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

func TestRecallAndReactionRequireOwnedSourceWithoutInventedActor(t *testing.T) {
	ctx := context.Background()
	o := Owner{"g", "s", "c", "u", "tenant", "app"}
	j, err := OpenJournal(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	p, err := NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	input := privateAction(o, control.ActionTextMessage)
	input.Text = "hello"
	input.MessageID = "source"
	seq := admittedAction(t, j, o, "source", "input", input)
	if _, err = p.ApplyIngress(ctx, o, 1, seq); err != nil {
		t.Fatal(err)
	}
	recall := control.Action{Kind: control.ActionMessageRecalled, GatewayID: o.GatewayID, SurfaceSessionID: o.SurfaceID, ChatID: o.ChatID, TargetMessageID: "source"}
	seq = admittedAction(t, j, o, "recall", "control", recall)
	if _, err = p.ApplyIngress(ctx, o, 1, seq); err != nil {
		t.Fatalf("SDK recall: %v", err)
	}
	reaction := control.Action{Kind: control.ActionReactionCreated, GatewayID: o.GatewayID, SurfaceSessionID: o.SurfaceID, ActorUserID: o.UserID, TargetMessageID: "source", ReactionType: "THUMBSUP"}
	seq = admittedAction(t, j, o, "reaction", "control", reaction)
	if _, err = p.ApplyIngress(ctx, o, 1, seq); err != nil {
		t.Fatalf("SDK reaction: %v", err)
	}
	reaction.ActorUserID = "foreign"
	seq = admittedAction(t, j, o, "foreign", "control", reaction)
	if _, err = p.ApplyIngress(ctx, o, 1, seq); !errors.Is(err, ErrInvalidOwner) {
		t.Fatalf("foreign reaction: %v", err)
	}
	recall.TargetMessageID = "other-message"
	seq = admittedAction(t, j, o, "unowned", "control", recall)
	if _, err = p.ApplyIngress(ctx, o, 1, seq); !errors.Is(err, ErrInvalidOwner) {
		t.Fatalf("unowned recall: %v", err)
	}
}
