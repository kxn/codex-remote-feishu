package nativeinspection

import (
	"encoding/json"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func ValidBootstrap(backend agentproto.Backend, probe agentproto.RPCProbe) bool {
	if backend == agentproto.BackendCodex || backend == agentproto.BackendOpenCode {
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		var response struct {
			ID     json.RawMessage `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		return json.Unmarshal(probe.Request, &request) == nil && json.Unmarshal(probe.Response, &response) == nil && request.Method == "initialize" && len(request.ID) > 0 && string(request.ID) == string(response.ID) && len(response.Result) > 0 && (len(response.Error) == 0 || string(response.Error) == "null")
	}
	var request struct {
		Type    string `json:"type"`
		ID      string `json:"request_id"`
		Request struct {
			Subtype string `json:"subtype"`
		} `json:"request"`
	}
	var response struct {
		Type     string `json:"type"`
		Response struct {
			ID      string `json:"request_id"`
			Subtype string `json:"subtype"`
		} `json:"response"`
	}
	return json.Unmarshal(probe.Request, &request) == nil && json.Unmarshal(probe.Response, &response) == nil && request.Type == "control_request" && request.ID != "" && request.Request.Subtype == "initialize" && response.Type == "control_response" && response.Response.ID == request.ID && response.Response.Subtype == "success"
}

func MatchRPC(probe agentproto.RPCProbe, method, sessionID string) (json.RawMessage, error) {
	var request struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params struct {
			ThreadID string `json:"threadId"`
		} `json:"params"`
	}
	var response struct {
		ID     json.RawMessage `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if json.Unmarshal(probe.Request, &request) != nil || json.Unmarshal(probe.Response, &response) != nil || len(request.ID) == 0 || string(request.ID) != string(response.ID) || request.Method != method || request.Params.ThreadID != sessionID || (len(response.Error) > 0 && string(response.Error) != "null") || len(response.Result) == 0 {
		return nil, fmt.Errorf("native probe request/response correlation failed")
	}
	return response.Result, nil
}

// ValidEphemeralReviewGoalProbe accepts only the exact Codex protocol
// unsupported response for the same authenticated ephemeral session.
func ValidEphemeralReviewGoalProbe(probe agentproto.RPCProbe, sessionID string) bool {
	var req struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params struct {
			ThreadID string `json:"threadId"`
		} `json:"params"`
	}
	var res struct {
		ID    json.RawMessage `json:"id"`
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Result json.RawMessage `json:"result"`
	}
	return json.Unmarshal(probe.Request, &req) == nil && json.Unmarshal(probe.Response, &res) == nil && len(req.ID) > 0 && string(req.ID) == string(res.ID) && req.Method == "thread/goal/get" && req.Params.ThreadID == sessionID && res.Error.Code == -32600 && res.Error.Message == "ephemeral thread does not support goals: "+sessionID && len(res.Result) == 0
}
