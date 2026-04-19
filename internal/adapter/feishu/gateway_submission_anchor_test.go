package feishu

import (
	"context"
	"testing"
	"time"

	larkcallback "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestHandleCardActionTriggerWaitsForCommandResultReplacementAction(t *testing.T) {
	action := control.Action{
		Kind: control.ActionStatus,
		Inbound: &control.ActionInboundMeta{
			CardDaemonLifecycleID: "life-1",
		},
	}
	started := make(chan struct{})
	release := make(chan struct{})
	resultCh := make(chan *larkcallback.CardActionTriggerResponse, 1)
	errCh := make(chan error, 1)
	handler := func(context.Context, control.Action) *ActionResult {
		close(started)
		<-release
		return &ActionResult{
			ReplaceCurrentCard: &Operation{
				Kind:         OperationSendCard,
				CardTitle:    "当前状态",
				CardBody:     "当前模式：normal",
				CardThemeKey: cardThemeInfo,
			},
		}
	}

	go func() {
		resp, err := handleCardActionTrigger(context.Background(), action, handler)
		if err != nil {
			errCh <- err
			return
		}
		resultCh <- resp
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("expected handler to start synchronously")
	}
	select {
	case <-resultCh:
		t.Fatal("expected callback to wait for handler result")
	case err := <-errCh:
		t.Fatalf("handleCardActionTrigger returned error: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(release)

	select {
	case err := <-errCh:
		t.Fatalf("handleCardActionTrigger returned error: %v", err)
	case resp := <-resultCh:
		if resp == nil || resp.Card == nil {
			t.Fatalf("expected replacement callback response, got %#v", resp)
		}
	case <-time.After(time.Second):
		t.Fatal("expected callback to return after handler finished")
	}
}

func TestHandleCardActionTriggerWaitsForBareUpgradeContinuationAction(t *testing.T) {
	action := control.Action{
		Kind: control.ActionUpgradeCommand,
		Text: "/upgrade",
		Inbound: &control.ActionInboundMeta{
			CardDaemonLifecycleID: "life-1",
		},
	}
	started := make(chan struct{})
	release := make(chan struct{})
	resultCh := make(chan *larkcallback.CardActionTriggerResponse, 1)
	errCh := make(chan error, 1)
	handler := func(context.Context, control.Action) *ActionResult {
		close(started)
		<-release
		return &ActionResult{
			ReplaceCurrentCard: &Operation{
				Kind:         OperationSendCard,
				CardTitle:    "Upgrade",
				CardBody:     "升级状态",
				CardThemeKey: cardThemeInfo,
			},
		}
	}

	go func() {
		resp, err := handleCardActionTrigger(context.Background(), action, handler)
		if err != nil {
			errCh <- err
			return
		}
		resultCh <- resp
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("expected handler to start synchronously")
	}
	select {
	case <-resultCh:
		t.Fatal("expected callback to wait for handler result")
	case err := <-errCh:
		t.Fatalf("handleCardActionTrigger returned error: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(release)

	select {
	case err := <-errCh:
		t.Fatalf("handleCardActionTrigger returned error: %v", err)
	case resp := <-resultCh:
		if resp == nil || resp.Card == nil {
			t.Fatalf("expected replacement callback response, got %#v", resp)
		}
	case <-time.After(time.Second):
		t.Fatal("expected callback to return after handler finished")
	}
}

func TestHandleCardActionTriggerWaitsForBareCronContinuationAction(t *testing.T) {
	action := control.Action{
		Kind: control.ActionCronCommand,
		Text: "/cron",
		Inbound: &control.ActionInboundMeta{
			CardDaemonLifecycleID: "life-1",
		},
	}
	started := make(chan struct{})
	release := make(chan struct{})
	resultCh := make(chan *larkcallback.CardActionTriggerResponse, 1)
	errCh := make(chan error, 1)
	handler := func(context.Context, control.Action) *ActionResult {
		close(started)
		<-release
		return &ActionResult{
			ReplaceCurrentCard: &Operation{
				Kind:         OperationSendCard,
				CardTitle:    "Cron",
				CardBody:     "Cron 状态",
				CardThemeKey: cardThemeInfo,
			},
		}
	}

	go func() {
		resp, err := handleCardActionTrigger(context.Background(), action, handler)
		if err != nil {
			errCh <- err
			return
		}
		resultCh <- resp
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("expected handler to start synchronously")
	}
	select {
	case <-resultCh:
		t.Fatal("expected callback to wait for handler result")
	case err := <-errCh:
		t.Fatalf("handleCardActionTrigger returned error: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(release)

	select {
	case err := <-errCh:
		t.Fatalf("handleCardActionTrigger returned error: %v", err)
	case resp := <-resultCh:
		if resp == nil || resp.Card == nil {
			t.Fatalf("expected replacement callback response, got %#v", resp)
		}
	case <-time.After(time.Second):
		t.Fatal("expected callback to return after handler finished")
	}
}
