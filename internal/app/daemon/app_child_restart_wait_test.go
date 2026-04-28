package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestRestartRelayChildCodexAndWaitSucceedsAfterAckAndOutcome(t *testing.T) {
	app := New(":0", ":0", nil, agentproto.ServerIdentity{})
	app.sendAgentCommand = func(instanceID string, command agentproto.Command) error {
		if instanceID != "inst-1" {
			t.Fatalf("unexpected instance id: %s", instanceID)
		}
		app.onCommandAck(context.Background(), instanceID, agentproto.CommandAck{
			CommandID: command.CommandID,
			Accepted:  true,
		})
		app.onEvents(context.Background(), instanceID, []agentproto.Event{
			agentproto.NewChildRestartUpdatedEvent(command.CommandID, "thread-1", agentproto.ChildRestartStatusSucceeded, nil),
		})
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := app.restartRelayChildCodexAndWait(ctx, "inst-1"); err != nil {
		t.Fatalf("restartRelayChildCodexAndWait: %v", err)
	}
}

func TestRestartRelayChildCodexAndWaitFailsOnRejectedAck(t *testing.T) {
	app := New(":0", ":0", nil, agentproto.ServerIdentity{})
	app.sendAgentCommand = func(instanceID string, command agentproto.Command) error {
		app.onCommandAck(context.Background(), instanceID, agentproto.CommandAck{
			CommandID: command.CommandID,
			Accepted:  false,
			Error:     "restart rejected",
		})
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := app.restartRelayChildCodexAndWait(ctx, "inst-1")
	if err == nil {
		t.Fatal("expected rejected ack to fail")
	}
	problem := agentproto.ErrorInfoFromError(err, agentproto.ErrorInfo{})
	if problem.Code != "command_rejected" {
		t.Fatalf("problem code = %q, want command_rejected (%#v)", problem.Code, problem)
	}
}

func TestRestartRelayChildCodexAndWaitFailsOnRestoreOutcome(t *testing.T) {
	app := New(":0", ":0", nil, agentproto.ServerIdentity{})
	app.sendAgentCommand = func(instanceID string, command agentproto.Command) error {
		app.onCommandAck(context.Background(), instanceID, agentproto.CommandAck{
			CommandID: command.CommandID,
			Accepted:  true,
		})
		app.onEvents(context.Background(), instanceID, []agentproto.Event{
			agentproto.NewChildRestartUpdatedEvent(command.CommandID, "thread-1", agentproto.ChildRestartStatusFailed, &agentproto.ErrorInfo{
				Code:    "restore_failed",
				Message: "restore failed",
			}),
		})
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := app.restartRelayChildCodexAndWait(ctx, "inst-1")
	if err == nil {
		t.Fatal("expected failed restore outcome to fail")
	}
	problem := agentproto.ErrorInfoFromError(err, agentproto.ErrorInfo{})
	if problem.Code != "restore_failed" {
		t.Fatalf("problem code = %q, want restore_failed (%#v)", problem.Code, problem)
	}
}

func TestRestartRelayChildCodexAndWaitTimesOutWaitingForAck(t *testing.T) {
	app := New(":0", ":0", nil, agentproto.ServerIdentity{})
	app.sendAgentCommand = func(string, agentproto.Command) error { return nil }

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := app.restartRelayChildCodexAndWait(ctx, "inst-1")
	if err == nil {
		t.Fatal("expected missing ack to time out")
	}
	problem := agentproto.ErrorInfoFromError(err, agentproto.ErrorInfo{})
	if problem.Code != "child_restart_ack_timeout" {
		t.Fatalf("problem code = %q, want child_restart_ack_timeout (%#v)", problem.Code, problem)
	}
}

func TestRestartRelayChildCodexAndWaitTimesOutWaitingForRestoreOutcome(t *testing.T) {
	app := New(":0", ":0", nil, agentproto.ServerIdentity{})
	app.sendAgentCommand = func(instanceID string, command agentproto.Command) error {
		app.onCommandAck(context.Background(), instanceID, agentproto.CommandAck{
			CommandID: command.CommandID,
			Accepted:  true,
		})
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := app.restartRelayChildCodexAndWait(ctx, "inst-1")
	if err == nil {
		t.Fatal("expected missing restore outcome to time out")
	}
	problem := agentproto.ErrorInfoFromError(err, agentproto.ErrorInfo{})
	if problem.Code != "child_restart_restore_timeout" {
		t.Fatalf("problem code = %q, want child_restart_restore_timeout (%#v)", problem.Code, problem)
	}
}
