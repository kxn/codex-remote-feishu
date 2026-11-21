package wrapper

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestManagedMCPReadUsesExactNativeRPCAndOnlyPublishesSafeSummary(t *testing.T) {
	for _, backend := range []agentproto.Backend{agentproto.BackendCodex, agentproto.BackendClaude} {
		for _, mode := range []string{"status", "configuration"} {
			t.Run(string(backend)+"-"+mode, func(t *testing.T) {
				app := newTestJournalWorkApp(t, backend)
				owner := app.work.snapshot.Owner
				command := agentproto.Command{CommandID: "mcp-query", Kind: agentproto.CommandRuntimeMCPRead, Execution: &owner, Target: agentproto.Target{ThreadID: "session"}, RuntimeMCP: &agentproto.RuntimeMCPQuery{Version: 1, Mode: mode, Limit: 10}}
				if _, err := app.prepareJournalCommand(command); err != nil {
					t.Fatal(err)
				}
				result, err := app.runtime.TranslateCommand(command)
				if err != nil {
					t.Fatal(err)
				}
				frames := flattenRuntimeCommandPhaseFrames(result.Phases)
				if len(frames) != 1 {
					t.Fatalf("frames=%d", len(frames))
				}
				if err = writeJournaledChildFrame(io.Discard, frames[0], app.runtime); err != nil {
					t.Fatal(err)
				}
				var request map[string]any
				if err = json.Unmarshal(frames[0], &request); err != nil {
					t.Fatal(err)
				}
				var response any
				if backend == agentproto.BackendClaude {
					if request["request"].(map[string]any)["subtype"] != "mcp_status" {
						t.Fatal("arbitrary Claude RPC escaped fixed query")
					}
					response = map[string]any{"type": "control_response", "response": map[string]any{"request_id": request["request_id"], "subtype": "success", "response": map[string]any{"mcpServers": []any{map[string]any{"name": "docs", "status": "connected", "config": map[string]any{"headers": map[string]any{"Authorization": "secret-header"}}}}}}}
				} else if mode == "status" {
					if request["method"] != "mcpServerStatus/list" {
						t.Fatal("wrong status RPC")
					}
					response = map[string]any{"id": request["id"], "result": map[string]any{"data": []any{map[string]any{"name": "docs", "runtimeStatus": "connected", "authStatus": "oAuth", "httpOrigin": "https://user:secret-password@docs.example/secret-path?token=secret-query", "tools": map[string]any{"one": map[string]any{"description": "secret-tool-body"}}}}}}
				} else {
					if request["method"] != "config/read" {
						t.Fatal("wrong configuration RPC")
					}
					response = map[string]any{"id": request["id"], "result": map[string]any{"config": map[string]any{"mcp_servers": map[string]any{"docs": map[string]any{"enabled": true, "url": "https://user:secret-password@docs.example/secret-path?token=secret-query", "http_headers": map[string]any{"Authorization": "secret-header"}}}}}}
				}
				raw, _ := json.Marshal(response)
				observed, err := app.runtime.ObserveServer(raw)
				if err != nil {
					t.Fatal(err)
				}
				if len(observed.Events) != 1 || observed.Events[0].RuntimeMCP == nil || !observed.Events[0].RuntimeMCP.Supported || observed.Events[0].CommandID != command.CommandID || observed.Events[0].Execution == nil || *observed.Events[0].Execution != owner {
					t.Fatalf("MCP reply lost command association: %#v", observed.Events)
				}
				encoded, _ := json.Marshal(observed.Events[0])
				for _, secret := range []string{"secret-header", "secret-password", "secret-query", "secret-path", "secret-tool-body"} {
					if strings.Contains(string(encoded), secret) {
						t.Fatalf("MCP safe summary leaked %s", secret)
					}
				}
				if len(observed.Events[0].RuntimeMCP.Servers) != 1 {
					t.Fatal("actual native server inventory disappeared")
				}
			})
		}
	}
}

func TestManagedOpenCodeMCPInventoryReportsActualCapabilityBoundary(t *testing.T) {
	app := newTestJournalWorkApp(t, agentproto.BackendOpenCode)
	owner := app.work.snapshot.Owner
	command := agentproto.Command{CommandID: "acp-mcp", Kind: agentproto.CommandRuntimeMCPRead, Execution: &owner, RuntimeMCP: &agentproto.RuntimeMCPQuery{Version: 1, Mode: "status"}}
	result, err := app.runtime.TranslateCommand(command)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Phases) != 0 || len(result.Events) != 1 || result.Events[0].RuntimeMCP.Supported || result.Events[0].RuntimeMCP.Problem == nil {
		t.Fatal("ACP pretended to expose a native MCP inventory")
	}
}
