package worktransport

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

// ReadMCP runs outside ingress. It returns only the exact committed response to
// this canonical owner-only command, never another worker's configuration.
func (d *Driver) ReadMCP(ctx context.Context, workerID string, query agentproto.RuntimeMCPQuery) (agentproto.RuntimeMCPResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var value agentproto.RuntimeMCPResult
	var command agentproto.Command
	var instance string
	random := make([]byte, 20)
	if _, err := rand.Read(random); err != nil {
		return value, err
	}
	err := d.store.Within(ctx, func(tx *workstore.Tx) error {
		worker, e := tx.GetWorker(workerID)
		if e != nil {
			return e
		}
		carrier, e := tx.GetCarrier(worker.CarrierIncarnation)
		if e != nil {
			return e
		}
		if !carrier.Matches(worker) {
			return fmt.Errorf("MCP query requires the exact current carrier")
		}
		binding := carrier.Owner
		command = agentproto.Command{CommandID: "mcp_" + hex.EncodeToString(random), IssuedAt: d.now(), Kind: agentproto.CommandRuntimeMCPRead, Execution: &binding, Target: agentproto.Target{ThreadID: worker.NativeSessionID}, RuntimeMCP: &query}
		instance = worker.InstanceID
		return nil
	})
	if err != nil {
		return value, err
	}
	if err = d.persistInspection(ctx, command); err != nil {
		return value, err
	}
	if err = d.SendCommand(ctx, instance, command); err != nil {
		_ = d.finishInspection(context.WithoutCancel(ctx), command.CommandID, "", err)
		return value, err
	}
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		err = d.store.Within(ctx, func(tx *workstore.Tx) error {
			event, e := tx.NativeMCPCommandResult(command.CommandID)
			if e != nil {
				return e
			}
			if event.RuntimeMCP == nil {
				return fmt.Errorf("native MCP result is missing its typed body")
			}
			value = *event.RuntimeMCP
			return nil
		})
		if err == nil {
			return value, nil
		}
		if !errors.Is(err, workstore.ErrNotFound) {
			return value, err
		}
		select {
		case <-ctx.Done():
			_ = d.finishInspection(context.WithoutCancel(ctx), command.CommandID, "", ctx.Err())
			return value, ctx.Err()
		case <-tick.C:
		}
	}
}
