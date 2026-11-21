package wrapper

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func mcpResultEvent(command agentproto.Command) agentproto.Event {
	return agentproto.Event{Kind: agentproto.EventRuntimeMCPResult, CommandID: command.CommandID, ThreadID: command.Target.ThreadID, RuntimeMCP: &agentproto.RuntimeMCPResult{Version: agentproto.RuntimeMCPProtocolVersion, Mode: command.RuntimeMCP.Mode, Servers: []agentproto.RuntimeMCPServer{}}}
}

func (r *workExecutionRuntime) translateMCPRead(command agentproto.Command) (runtimeCommandResult, error) {
	q := command.RuntimeMCP
	if q == nil || q.Version != agentproto.RuntimeMCPProtocolVersion || (q.Mode != "status" && q.Mode != "configuration") || q.Limit < 0 || q.Limit > 100 {
		return runtimeCommandResult{}, fmt.Errorf("invalid fixed MCP query")
	}
	if r.Backend() == agentproto.BackendOpenCode {
		event := mcpResultEvent(command)
		event.RuntimeMCP.Problem = &agentproto.ErrorInfo{Code: "mcp_inventory_unavailable", Message: "The ACP backend exposes no native MCP inventory RPC."}
		r.work.annotateEvents([]agentproto.Event{event})
		event.Execution = agentproto.CloneExecutionBinding(command.Execution)
		return runtimeCommandResult{Events: []agentproto.Event{event}}, nil
	}
	requestID := command.CommandID + ":mcp:" + q.Mode
	var request any
	if r.Backend() == agentproto.BackendClaude {
		if q.Cursor != "" {
			return runtimeCommandResult{}, fmt.Errorf("Claude MCP inventory is not paginated")
		}
		request = map[string]any{"type": "control_request", "request_id": requestID, "request": map[string]any{"subtype": "mcp_status"}}
	} else {
		params := map[string]any{}
		method := "config/read"
		if q.Mode == "status" {
			method = "mcpServerStatus/list"
			params["cursor"] = q.Cursor
			params["limit"] = 100
			if q.Limit > 0 {
				params["limit"] = q.Limit
			}
			if command.Target.ThreadID != "" {
				params["threadId"] = command.Target.ThreadID
			}
		} else {
			params["includeLayers"] = false
		}
		request = map[string]any{"id": requestID, "method": method, "params": params}
	}
	frame, err := json.Marshal(request)
	if err != nil {
		return runtimeCommandResult{}, err
	}
	frame = append(frame, '\n')
	r.work.mu.Lock()
	if r.work.mcpRequests == nil {
		r.work.mcpRequests = map[string]agentproto.Command{}
	}
	r.work.mcpRequests[requestID] = command
	r.work.mu.Unlock()
	if err = r.work.registerFrames(command.CommandID, [][]byte{frame}); err != nil {
		return runtimeCommandResult{}, err
	}
	return runtimeCommandResult{Phases: []runtimeCommandPhase{{OutboundToChild: [][]byte{frame}}}}, nil
}

func (r *workExecutionRuntime) observeMCPReply(raw []byte) (agentproto.Event, bool) {
	id := nativeResponseRequestID(raw)
	if id == "" {
		return agentproto.Event{}, false
	}
	r.work.mu.Lock()
	command, ok := r.work.mcpRequests[id]
	if ok {
		delete(r.work.mcpRequests, id)
		delete(r.work.nativeRequests, id)
	}
	r.work.mu.Unlock()
	if !ok {
		return agentproto.Event{}, false
	}
	event := mcpResultEvent(command)
	event.RequestID = id
	var envelope map[string]any
	if json.Unmarshal(raw, &envelope) != nil {
		return event, true
	}
	body, _ := envelope["result"].(map[string]any)
	failure := envelope["error"] != nil
	if r.Backend() == agentproto.BackendClaude {
		response, _ := envelope["response"].(map[string]any)
		body, _ = response["response"].(map[string]any)
		failure = response["subtype"] != "success"
	}
	if failure || body == nil {
		event.RuntimeMCP.Problem = &agentproto.ErrorInfo{Code: "mcp_native_query_failed", Message: "Native MCP query failed; raw credential-bearing diagnostics were not published."}
		return event, true
	}
	event.RuntimeMCP.Supported = true
	if command.RuntimeMCP.Mode == "configuration" && r.Backend() == agentproto.BackendCodex {
		config, _ := body["config"].(map[string]any)
		servers, _ := config["mcp_servers"].(map[string]any)
		for name, value := range servers {
			entry := agentproto.RuntimeMCPServer{Name: name}
			settings, _ := value.(map[string]any)
			if enabled, ok := settings["enabled"].(bool); ok {
				entry.Enabled = &enabled
			}
			if endpoint, _ := settings["url"].(string); endpoint != "" {
				entry.Origin = mcpSafeOrigin(endpoint)
				entry.Transport = "http"
			} else if settings["command"] != nil {
				entry.Transport = "stdio"
			}
			event.RuntimeMCP.Servers = append(event.RuntimeMCP.Servers, entry)
		}
	} else {
		entries, _ := body["data"].([]any)
		if r.Backend() == agentproto.BackendClaude {
			entries, _ = body["mcpServers"].([]any)
		}
		for _, value := range entries {
			settings, _ := value.(map[string]any)
			name, _ := settings["name"].(string)
			if name == "" {
				continue
			}
			entry := agentproto.RuntimeMCPServer{Name: name}
			entry.Status, _ = settings["runtimeStatus"].(string)
			if entry.Status == "" {
				entry.Status, _ = settings["status"].(string)
			}
			entry.Authentication, _ = settings["authStatus"].(string)
			origin, _ := settings["httpOrigin"].(string)
			entry.Origin = mcpSafeOrigin(origin)
			if entry.Origin != "" {
				entry.Transport = "http"
			}
			tools, _ := settings["tools"].(map[string]any)
			entry.ToolCount = len(tools)
			event.RuntimeMCP.Servers = append(event.RuntimeMCP.Servers, entry)
		}
		event.RuntimeMCP.NextCursor, _ = body["nextCursor"].(string)
	}
	if len(event.RuntimeMCP.Servers) > 100 {
		event.RuntimeMCP.Supported = false
		event.RuntimeMCP.Servers = nil
		event.RuntimeMCP.Problem = &agentproto.ErrorInfo{Code: "mcp_inventory_limit", Message: "Native MCP inventory exceeded the bounded summary; use native status pagination."}
	}
	sort.Slice(event.RuntimeMCP.Servers, func(i, j int) bool { return event.RuntimeMCP.Servers[i].Name < event.RuntimeMCP.Servers[j].Name })
	return event, true
}

func mcpSafeOrigin(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	return (&url.URL{Scheme: u.Scheme, Host: u.Host}).String()
}
