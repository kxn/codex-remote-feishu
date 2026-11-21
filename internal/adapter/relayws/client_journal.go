package relayws

import (
	"context"
	"fmt"

	"github.com/gorilla/websocket"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

// EventJournal owns durable storage. The ordinary client outbox is still only
// a transport queue; journal entries remain pending until a durable receipt.
type EventJournal interface {
	PersistEvents([]agentproto.Event) ([]agentproto.Event, error)
	Replay(after int64, limit int) ([]agentproto.Event, int64, error)
	Acknowledge(agentproto.EventReceipt) error
}

func (c *Client) SetEventJournal(journal EventJournal) { c.journal = journal }

func (c *Client) replayJournal(conn relayConn) error {
	if c.journal == nil {
		return nil
	}
	var cursor int64
	for {
		events, next, err := c.journal.Replay(cursor, 128)
		if err != nil {
			return err
		}
		if len(events) == 0 {
			return nil
		}
		if next <= cursor {
			return fmt.Errorf("journal replay cursor did not advance")
		}
		cursor = next
		envelope := agentproto.Envelope{Type: agentproto.EnvelopeEventBatch, EventBatch: &agentproto.EventBatch{InstanceID: c.hello.Instance.InstanceID, Events: events}}
		raw, err := agentproto.MarshalEnvelope(envelope)
		if err != nil {
			return err
		}
		c.logRaw("out", raw, envelope.Type, c.hello.Instance.InstanceID, "")
		if err := conn.WriteMessage(websocket.TextMessage, raw); err != nil {
			return err
		}
	}
}

func (c *Client) handleEventReceipt(ctx context.Context, receipt agentproto.EventReceipt) error {
	if receipt.InstanceID != "" && receipt.InstanceID != c.hello.Instance.InstanceID {
		return fmt.Errorf("runtime event receipt instance mismatch")
	}
	if c.journal != nil {
		if err := c.journal.Acknowledge(receipt); err != nil {
			return err
		}
	}
	if c.callbacks.OnEventReceipt != nil {
		return c.callbacks.OnEventReceipt(ctx, receipt)
	}
	return nil
}

func (c *Client) handleCommand(ctx context.Context, command agentproto.Command) {
	var commandErr error
	if c.callbacks.OnCommand != nil {
		commandErr = c.callbacks.OnCommand(ctx, command)
	}
	ack := agentproto.CommandAck{CommandID: command.CommandID, Accepted: commandErr == nil}
	if c.callbacks.RuntimeCommandReceipt != nil {
		receipt, err := c.callbacks.RuntimeCommandReceipt(command.CommandID)
		if err != nil && commandErr == nil {
			commandErr = err
			ack.Accepted = false
		}
		ack.RuntimeReceipt = receipt
	}
	if commandErr != nil {
		problem := agentproto.ErrorInfoFromError(commandErr, agentproto.ErrorInfo{
			Code: "command_rejected", Layer: "wrapper", Stage: "handle_command", Operation: string(command.Kind),
			Message: "wrapper 拒绝执行 relay 命令。", SurfaceSessionID: command.Origin.Surface,
			CommandID: command.CommandID, ThreadID: command.Target.ThreadID, TurnID: command.Target.TurnID,
		})
		ack.Error, ack.Problem = problem.Message, &problem
	}
	_ = c.SendCommandAck(ack)
}
