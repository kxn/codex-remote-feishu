package workruntime

import (
	"encoding/json"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (c *Coordinator) applyMCPReceipt(tx *workstore.Tx, carrier workstore.CarrierRegistration, event agentproto.Event) error {
	command, err := tx.ResolveNativeCommand(carrier.Incarnation, event.CommandID)
	if err != nil {
		return err
	}
	if event.Execution == nil || command.Binding != *event.Execution || !carrier.Owner.SameOwner(*event.Execution) || event.ThreadID != command.SessionID {
		return fmt.Errorf("MCP receipt lacks exact native command ownership")
	}
	op, err := tx.GetOperation(command.CommandID)
	if err != nil {
		return err
	}
	canonical, err := tx.NativeCommandPayload(command.CommandID, carrier.Incarnation)
	if err != nil {
		return err
	}
	if event.Kind == agentproto.EventRuntimeMCPResult && (canonical.RuntimeMCP == nil || event.RuntimeMCP == nil || canonical.RuntimeMCP.Mode != event.RuntimeMCP.Mode) {
		return fmt.Errorf("MCP query result differs from its canonical mode")
	}
	if event.Kind != agentproto.EventRuntimeMCPResult && (canonical.MCP.OAuthLogin == nil || event.MCPOAuthLogin == nil || canonical.MCP.OAuthLogin.ServerName != event.MCPOAuthLogin.ServerName || canonical.MCP.OAuthLogin.ThreadID != event.MCPOAuthLogin.ThreadID) {
		return fmt.Errorf("MCP login result differs from its canonical server and session")
	}
	if event.Kind == agentproto.EventRuntimeMCPResult {
		if command.Kind != agentproto.CommandRuntimeMCPRead || event.RuntimeMCP == nil || event.RuntimeMCP.Version != agentproto.RuntimeMCPProtocolVersion {
			return fmt.Errorf("invalid native MCP inventory receipt")
		}
		if event.RuntimeMCP.Supported && event.RequestID == "" {
			return fmt.Errorf("native MCP inventory has no actual RPC identity")
		}
	} else if command.Kind != agentproto.CommandMCPOAuthLogin || event.RequestID == "" || event.MCPOAuthLogin == nil {
		return fmt.Errorf("MCP login receipt lacks its exact native RPC")
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return err
	}
	ref := fmt.Sprintf("native-mcp:%s:%d", carrier.Incarnation, event.Journal.Sequence)
	if err = tx.PutContent(work.NewContent(ref, command.Binding.ScopeID, "application/json", raw, c.options.Clock())); err != nil {
		return err
	}
	if op.State == work.OperationConfirmed || op.State == work.OperationTerminal {
		return nil
	}
	before := op.Revision
	op.Revision++
	op.ExternalReceipt = ref
	op.UpdatedAt = c.options.Clock()
	if event.Kind == agentproto.EventRuntimeMCPResult || event.Kind == agentproto.EventMCPOAuthLoginCompleted {
		op.State = work.OperationConfirmed
		if event.Kind == agentproto.EventMCPOAuthLoginCompleted && !event.MCPOAuthLogin.Success {
			op.State = work.OperationTerminal
			op.LastError = "native MCP authentication failed"
		}
	}
	_, _, err = tx.PutOperation(op, before)
	return err
}
