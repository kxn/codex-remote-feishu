package daemon

import (
	"context"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"testing"
	"time"
)

func TestTeamHumanActionsBoundConcurrencyWithoutBlockingOtherChats(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	r := &teamRuntime{runContext: ctx}
	defer func() { cancel(); r.workers.Wait() }()
	started := make(chan string, 4)
	handler := func(ctx context.Context, v workstore.HumanInteraction) error {
		started <- v.ID
		<-ctx.Done()
		return nil
	}
	action := func(id, chat string) workstore.HumanInteraction {
		return workstore.HumanInteraction{ID: id, Envelope: work.HumanInteractionEnvelope{Tenant: "tenant", ChatID: chat}}
	}
	if !r.queueHumanAction(action("first", "busy-chat"), handler) {
		t.Fatal("first action rejected")
	}
	if r.queueHumanAction(action("same-chat", "busy-chat"), handler) {
		t.Fatal("same chat executed concurrently")
	}
	for _, id := range []string{"second", "third", "fourth"} {
		if !r.queueHumanAction(action(id, id), handler) {
			t.Fatal("independent chat blocked")
		}
	}
	if r.queueHumanAction(action("overflow", "fifth-chat"), handler) {
		t.Fatal("unbounded network workers")
	}
	for range 4 {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("one slow chat blocked another")
		}
	}
	cancel()
	r.workers.Wait()
	if r.queueHumanAction(action("late", "late-chat"), handler) {
		t.Fatal("new action started during shutdown")
	}
}
