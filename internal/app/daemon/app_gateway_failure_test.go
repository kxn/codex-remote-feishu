package daemon

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

func TestDaemonFlushesGatewayFailureNoticeInSameBatch(t *testing.T) {
	gateway := &flakyGateway{failures: 1}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "feishu:app-1:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	if len(gateway.operations) != 1 || !strings.Contains(gateway.operations[0].CardTitle, "链路错误") {
		t.Fatalf("expected one failure notice before another action is needed, got %#v", gateway.operations)
	}

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "feishu:app-1:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	if len(gateway.operations) != 2 {
		t.Fatalf("expected one failure notice and current card without repeating either, got %#v", gateway.operations)
	}
	if !strings.Contains(gateway.operations[0].CardTitle, "链路错误") || gateway.operations[0].CardBody != "" || !strings.Contains(fmt.Sprint(gateway.operations[0].CardElements), "位置：gateway_apply") {
		t.Fatalf("expected queued gateway failure notice first, got %#v", gateway.operations[0])
	}
	if gateway.operations[0].AttentionText != "需要你回来处理：飞书投递失败。" || gateway.operations[0].AttentionUserID != "user-1" {
		t.Fatalf("expected queued gateway failure notice to carry attention, got %#v", gateway.operations[0])
	}
	if gateway.operations[1].CardTitle != "切换工作区与会话" {
		t.Fatalf("expected recovered response to be target picker card, got %#v", gateway.operations[1])
	}
	if !strings.Contains(fmt.Sprint(gateway.operations[1].CardElements), "当前还没有可切换的工作区") {
		t.Fatalf("expected recovered target picker to explain missing workspaces, got %#v", gateway.operations[1])
	}
	sawConfirmSwitch := false
	sawDisabledSwitch := false
	for _, button := range operationCardButtons(gateway.operations[1]) {
		textNode, _ := button["text"].(map[string]any)
		label, _ := textNode["content"].(string)
		switch label {
		case "切换":
			sawConfirmSwitch = true
			sawDisabledSwitch = button["disabled"] == true
		}
	}
	if !sawConfirmSwitch || !sawDisabledSwitch {
		t.Fatalf("expected recovered target picker to expose disabled switch action, got %#v", gateway.operations[1])
	}
}

type failingNoticeGateway struct {
	batches [][]feishu.Operation
}

func (g *failingNoticeGateway) Start(context.Context, feishu.ActionHandler) error { return nil }

func (g *failingNoticeGateway) Apply(_ context.Context, operations []feishu.Operation) error {
	g.batches = append(g.batches, append([]feishu.Operation(nil), operations...))
	return errors.New("lark temporarily unavailable")
}

func TestDaemonGatewayFailureNoticeAttemptsAreBounded(t *testing.T) {
	gateway := &failingNoticeGateway{}
	app := New(":0", ":0", gateway, serverIdentityForTest())
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	if len(gateway.batches) != 2 {
		t.Fatalf("expected original response and one notice attempt only, got %d gateway calls", len(gateway.batches))
	}
	if len(gateway.batches[0]) != 1 || gateway.batches[0][0].CardTitle != "切换工作区与会话" ||
		len(gateway.batches[1]) != 1 || !strings.Contains(gateway.batches[1][0].CardTitle, "链路错误") {
		t.Fatalf("expected notice attempt without replaying the failed response, got %#v", gateway.batches)
	}
	if len(app.pendingGlobalRuntimeNotices["surface-1"]) != 1 {
		t.Fatalf("expected one retained notice after continued outage, got %#v", app.pendingGlobalRuntimeNotices)
	}
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	if len(gateway.batches) != 4 || len(app.pendingGlobalRuntimeNotices["surface-1"]) != 1 {
		t.Fatalf("expected next action to attempt pending notice once without growing the queue, got %d calls and %#v", len(gateway.batches), app.pendingGlobalRuntimeNotices)
	}
}

type runtimeNoticeApplyGateway func([]feishu.Operation) error

func (runtimeNoticeApplyGateway) Start(context.Context, feishu.ActionHandler) error { return nil }

func (g runtimeNoticeApplyGateway) Apply(_ context.Context, operations []feishu.Operation) error {
	return g(operations)
}

func TestDaemonSerializesPendingRuntimeNoticeDelivery(t *testing.T) {
	for _, test := range []struct {
		name          string
		err           error
		retireGateway bool
		want          []string
	}{
		{name: "success", want: []string{"first notice", "blocked notice", "appended notice"}},
		{name: "retry", err: errors.New("lark temporarily unavailable"), want: []string{"first notice", "blocked notice", "blocked notice", "appended notice"}},
		{name: "gateway retired", retireGateway: true, want: []string{"first notice", "blocked notice", "appended notice"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			const surfaceID = "feishu:app-1:chat:1"
			attempts := make(chan string, 16)
			blocked := make(chan struct{})
			release := make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			var blockedOnce atomic.Bool
			gateway := runtimeNoticeApplyGateway(func(operations []feishu.Operation) error {
				for _, op := range operations {
					if op.CardTitle == "切换工作区与会话" {
						continue
					}
					attempts <- op.CardTitle
					if op.CardTitle == "blocked notice" && blockedOnce.CompareAndSwap(false, true) {
						close(blocked)
						<-release
						return test.err
					}
				}
				return nil
			})
			app := New(":0", ":0", gateway, serverIdentityForTest())
			act := func() {
				app.HandleAction(context.Background(), control.Action{
					Kind: control.ActionListInstances, SurfaceSessionID: surfaceID, ChatID: "chat-1", ActorUserID: "user-1",
				})
			}
			queueNotice := func(title string) {
				app.mu.Lock()
				defer app.mu.Unlock()
				app.queueGlobalRuntimeNoticeLocked(eventcontract.Event{
					Kind: eventcontract.KindNotice, SurfaceSessionID: surfaceID,
					Notice: &control.Notice{
						Code: "gateway_apply_failed", Title: title, Text: "飞书回显投递失败。",
						DeliveryClass:    control.NoticeDeliveryClassGlobalRuntime,
						DeliveryFamily:   control.NoticeDeliveryFamilyGatewayApplyFailure,
						DeliveryDedupKey: title,
					},
				})
			}
			queueNotice("first notice")
			queueNotice("blocked notice")
			firstDone := make(chan struct{})
			go func() {
				act()
				close(firstDone)
			}()
			defer func() {
				unblock()
				<-firstDone
			}()
			select {
			case <-blocked:
			case <-time.After(3 * time.Second):
				t.Fatal("initial flush never reached the second notice")
			}

			concurrentDone := make(chan struct{})
			go func() {
				if test.retireGateway {
					app.mu.Lock()
					app.purgeFeishuGatewayRuntimeStateLocked("app-1", nil)
					app.mu.Unlock()
				}
				queueNotice("appended notice")
				act()
				close(concurrentDone)
			}()
			defer func() {
				unblock()
				<-concurrentDone
			}()
			select {
			case <-concurrentDone:
			case <-time.After(3 * time.Second):
				t.Fatal("concurrent action waited for the in-flight notice")
			}
			wantWhileBlocked := 2
			if test.retireGateway {
				// The replacement gateway can deliver without waiting for the retired gateway.
				wantWhileBlocked++
			}
			if got := len(attempts); got != wantWhileBlocked {
				t.Errorf("notice attempts while old delivery was blocked = %d, want %d", got, wantWhileBlocked)
			}
			unblock()
			<-firstDone
			if test.err != nil {
				act()
			}

			close(attempts)
			var got []string
			for title := range attempts {
				got = append(got, title)
			}
			if !slices.Equal(got, test.want) {
				t.Errorf("notice attempts = %v, want %v", got, test.want)
			}
		})
	}
}

func TestQueueGatewayFailureNoticeRetryabilityAndSafeGuidance(t *testing.T) {
	for _, test := range []struct {
		name          string
		err           error
		wantRetryable bool
	}{
		{
			name: "invalid card patch",
			err: fmt.Errorf("apply: %w", &feishu.APIError{
				API: "im.v1.message.patch", Code: 230001, Msg: "This message is NOT a card",
			}),
		},
		{
			name: "non-card patch from Feishu extended reason",
			err: &feishu.APIError{
				API: "im.v1.message.patch", Code: 230001,
				Msg: "Your request contains an invalid request parameter, ext=This message is NOT a card.",
			},
		},
		{
			name: "unclassified parameter error",
			err: &feishu.APIError{
				API: "im.v1.message.patch", Code: 230001, Msg: "unknown parameter error",
			},
			wantRetryable: true,
		},
		{name: "network error", err: errors.New("connection reset"), wantRetryable: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			app := New(":0", ":0", &recordingGateway{}, serverIdentityForTest())
			app.queueGatewayFailureNotice(eventcontract.Event{
				Kind: eventcontract.KindNotice, SurfaceSessionID: "surface-1",
			}, test.err)
			pending := app.pendingGlobalRuntimeNotices["surface-1"]
			if len(pending) != 1 || pending[0].Notice == nil {
				t.Fatalf("expected one queued failure notice, got %#v", pending)
			}
			text := pending[0].Notice.Text
			if got := strings.Contains(text, "可重试：是"); got != test.wantRetryable {
				t.Fatalf("retryable=%t, want %t: %s", got, test.wantRetryable, text)
			}
			if !strings.Contains(text, "先查询当前状态") || !strings.Contains(text, "勿重复提交原操作") {
				t.Fatalf("expected guidance to confirm state before any repeated operation: %s", text)
			}
		})
	}
}
