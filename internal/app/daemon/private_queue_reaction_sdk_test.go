package daemon

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
)

func waitQueueReaction(t *testing.T, f *continuityFixture, from int, message, emoji string) {
	t.Helper()
	for {
		for _, op := range f.gateway.snapshotOperations()[from:] {
			if string(op.Kind) == "add_reaction" && op.MessageID == message && op.EmojiType == emoji {
				return
			}
		}
		select {
		case <-f.ctx.Done():
			t.Fatalf("reaction %s on %s not visible", emoji, message)
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func waitQueueCount(t *testing.T, f *continuityFixture, prefix string, count int) {
	t.Helper()
	for i := 0; i < 30; i++ {
		before := len(f.gateway.snapshotOperations())
		f.user.send(f.app, fmt.Sprintf("%s-%d", prefix, i), "/status")
		f.user.waitVisible(f.app, before, "当前 1 条执行中")
		for _, op := range f.gateway.snapshotOperations()[before:] {
			body, _ := feishu.FreezePrivateOperation(op, f.app.daemonLifecycleID)
			if strings.Contains(string(body), fmt.Sprintf("%d 条排队", count)) {
				return
			}
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("%d queued inputs not visible", count)
}

func TestPrivateSDKColdQueueReactionStopJourney(t *testing.T) {
	stager := newStructuredUATStager(t)
	f := newContinuityFixtureBare(t, func(a *App) { a.applications.ingress.stager = stager })
	n := len(f.gateway.snapshotOperations())
	f.user.send(f.app, "cold-queue-hi", "hi")
	f.user.waitVisible(f.app, n, "/list")
	f.user.chooseNew(f.app, "queue-initial")
	n = len(f.gateway.snapshotOperations())
	f.user.send(f.app, "first-complete", "start session")
	f.user.waitFinal(f.app, n, "first-complete", "continuity-final")
	n = len(f.gateway.snapshotOperations())
	f.user.send(f.app, "busy", "busy-turn")
	for {
		visible := false
		for _, op := range f.gateway.snapshotOperations()[n:] {
			if string(op.Kind) == "add_reaction" && op.MessageID == "busy" && op.EmojiType == "THINKING" {
				visible = true
			}
		}
		if visible {
			break
		}
		select {
		case <-f.ctx.Done():
			t.Fatal("busy reaction not visible")
		case <-time.After(20 * time.Millisecond):
		}
	}
	f.user.send(f.app, "first-queue", "first queued text")
	raw := platformingress.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", SenderTenant: "tenant", EventID: "queue-image", MessageID: "queue-image", ChatID: "private-chat", ChatType: "p2p", SenderType: "user", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, MessageType: "image", RawContent: `{"image_key":"one"}`, ReceivedAt: time.Now()}
	n = len(f.gateway.snapshotOperations())
	if err := f.app.applications.hook.Receive(f.ctx, raw); err != nil {
		t.Fatal(err)
	}
	waitQueueReaction(t, f, n, "queue-image", "OneSecond")
	f.user.send(f.app, "image-description", "describe queued image")
	f.user.send(f.app, "shell-queue", "echo shell")
	// The status card is the user's evidence that all inputs have reached the queue.
	waitQueueCount(t, f, "queue-status", 3)
	reaction := platformingress.PlatformIngressEvent{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", EventID: "queue-like", Kind: "reaction.created", MessageID: "image-description", OperatorType: "user", OperatorIDs: platformingress.SenderIDs{OpenID: "user"}, Emoji: "ThumbsUp", ReceivedAt: time.Now()}
	n = len(f.gateway.snapshotOperations())
	if err := f.app.applications.hook.ReceivePlatformEvent(f.ctx, reaction); err != nil {
		t.Fatal(err)
	}
	if err := f.app.applications.hook.ReceivePlatformEvent(f.ctx, reaction); err != nil {
		t.Fatal(err)
	}
	waitQueueReaction(t, f, n, "image-description", "THUMBSUP")
	applause := reaction
	applause.EventID = "queue-applause"
	applause.MessageID = "shell-queue"
	applause.Emoji = "APPLAUSE"
	n = len(f.gateway.snapshotOperations())
	if err := f.app.applications.hook.ReceivePlatformEvent(f.ctx, applause); err != nil {
		t.Fatal(err)
	}
	waitQueueReaction(t, f, n, "shell-queue", "APPLAUSE")
	f.user.send(f.app, "recall-queue", "remove this queued input")
	f.user.send(f.app, "all-queue", "include this queued input")
	waitQueueCount(t, f, "before-recall", 3)
	recalled := platformingress.PlatformIngressEvent{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", EventID: "recall-event", Kind: "message.recalled", ChatID: "private-chat", MessageID: "recall-queue", ReceivedAt: time.Now()}
	if err := f.app.applications.hook.ReceivePlatformEvent(f.ctx, recalled); err != nil {
		t.Fatal(err)
	}
	waitQueueCount(t, f, "after-recall", 2)
	n = len(f.gateway.snapshotOperations())
	f.user.send(f.app, "queue-steer-all", "/steerall")
	f.user.waitVisible(f.app, n, "并入当前执行")
	waitQueueReaction(t, f, n, "all-queue", "THUMBSUP")
	n = len(f.gateway.snapshotOperations())
	f.user.send(f.app, "queue-stop", "/stop")
	f.user.waitVisible(f.app, n, "停止")
	for {
		stopped := false
		for _, op := range f.gateway.snapshotOperations()[n:] {
			if string(op.Kind) == "remove_reaction" && op.MessageID == "busy" && op.EmojiType == "THINKING" {
				stopped = true
			}
		}
		if stopped {
			break
		}
		select {
		case <-f.ctx.Done():
			t.Fatal("native stop not visible")
		case <-time.After(20 * time.Millisecond):
		}
	}
	n = len(f.gateway.snapshotOperations())
	f.user.send(f.app, "queue-next", "after queue stop")
	f.user.waitFinal(f.app, n, "queue-next", "continuity-final")
	data, err := os.ReadFile(f.trace)
	if err != nil {
		t.Fatal(err)
	}
	trace := string(data)
	if strings.Count(trace, `"method":"turn/steer"`) != 2 || !strings.Contains(trace, `"type":"localImage"`) || !strings.Contains(trace, "describe queued image") || !strings.Contains(trace, `"method":"thread/shellCommand"`) || strings.Contains(trace, "remove this queued input") {
		t.Fatalf("queued reaction effects differ: %s", trace)
	}
}
