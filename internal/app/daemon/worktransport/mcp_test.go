package worktransport

import (
	"context"
	"errors"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestMCPReadRequiresCanonicalMappingAndCommittedExactReceipt(t *testing.T) {
	f := newTransportFixture(t)
	f.relay.send = func(instance string, command agentproto.Command) error {
		f.tx(func(tx *workstore.Tx) error {
			native, err := tx.GetNativeCommand(command.CommandID)
			if err != nil {
				return err
			}
			if native.Kind != agentproto.CommandRuntimeMCPRead || native.Binding.AttemptID != "" {
				t.Fatal("MCP read invented attempt or lacked pre-send canonical mapping")
			}
			if _, err = tx.NativeMCPCommandResult(command.CommandID); !errors.Is(err, workstore.ErrNotFound) {
				t.Fatal("uncommitted MCP result was observable")
			}
			return nil
		})
		event := f.nativeEvent(agentproto.Event{Kind: agentproto.EventRuntimeMCPResult, CommandID: command.CommandID, RequestID: "exact-mcp-rpc", Execution: agentproto.CloneExecutionBinding(command.Execution), RuntimeMCP: &agentproto.RuntimeMCPResult{Version: 1, Mode: "status", Supported: true, Servers: []agentproto.RuntimeMCPServer{{Name: "native-docs", Status: "connected"}}, NextCursor: "page-2"}})
		if err := f.coordinator.HandleEvent(context.Background(), "wrong-transport", event); err == nil {
			t.Fatal("MCP receipt accepted another transport")
		}
		if err := f.coordinator.HandleEvent(context.Background(), instance, event); err != nil {
			return err
		}
		return f.coordinator.HandleEvent(context.Background(), instance, event) // lost ACK replay
	}
	result, err := f.driver.ReadMCP(context.Background(), "worker", agentproto.RuntimeMCPQuery{Version: 1, Mode: "status", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Supported || len(result.Servers) != 1 || result.Servers[0].Name != "native-docs" || result.NextCursor != "page-2" || f.enqueued != 0 {
		t.Fatal("MCP result lost exact native provenance or triggered reconciliation")
	}
}

func TestMCPOAuthURLIsPendingUntilExactNativeCompletion(t *testing.T) {
	f := newTransportFixture(t)
	owner := f.request.Carrier.Owner
	command := agentproto.Command{CommandID: "native-login", Kind: agentproto.CommandMCPOAuthLogin, Execution: &owner, MCP: agentproto.MCPCommand{OAuthLogin: &agentproto.MCPOAuthLoginCommand{ServerName: "docs"}}}
	if err := f.driver.persistInspection(context.Background(), command); err != nil {
		t.Fatal(err)
	}
	f.relay.send = func(instance string, received agentproto.Command) error {
		pending := f.nativeEvent(agentproto.Event{Kind: agentproto.EventMCPOAuthLoginURLReady, CommandID: command.CommandID, RequestID: "login-rpc", Execution: &owner, Status: "pending", MCPOAuthLogin: &agentproto.MCPOAuthLoginEvent{ServerName: "docs", AuthorizationURL: "https://auth.example/authorize?state=exact-flow"}})
		if err := f.coordinator.HandleEvent(context.Background(), instance, pending); err != nil {
			return err
		}
		f.tx(func(tx *workstore.Tx) error {
			op, err := tx.GetOperation(command.CommandID)
			if op.State != work.OperationSending {
				t.Fatal("authorization URL falsely confirmed login")
			}
			event, e := tx.NativeMCPCommandResult(command.CommandID)
			if e != nil {
				return e
			}
			if event.Kind != agentproto.EventMCPOAuthLoginURLReady {
				t.Fatal("real authorization URL was lost")
			}
			return err
		})
		completed := f.nativeEvent(agentproto.Event{Kind: agentproto.EventMCPOAuthLoginCompleted, CommandID: command.CommandID, RequestID: "login-rpc", Execution: &owner, Status: "completed", MCPOAuthLogin: &agentproto.MCPOAuthLoginEvent{ServerName: "docs", Success: true}})
		return f.coordinator.HandleEvent(context.Background(), instance, completed)
	}
	if err := f.driver.SendCommand(context.Background(), "instance", command); err != nil {
		t.Fatal(err)
	}
	f.tx(func(tx *workstore.Tx) error {
		op, err := tx.GetOperation(command.CommandID)
		if op.State != work.OperationConfirmed {
			t.Fatal("actual OAuth completion was never confirmed")
		}
		return err
	})
}
