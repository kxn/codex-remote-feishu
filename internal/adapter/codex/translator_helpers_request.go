package codex

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

const nativeRequestIDPrefix = "__native_request_id_json__:"

func extractRequestPayload(message map[string]any) map[string]any {
	request := lookupMap(message, "params", "request")
	if len(request) > 0 {
		return request
	}
	request = lookupMap(message, "params", "serverRequest")
	if len(request) > 0 {
		return request
	}
	return map[string]any{}
}

func extractRequestID(message map[string]any, request map[string]any) string {
	for _, candidate := range []any{
		request["id"],
		message["id"],
		lookupAny(message, "params", "requestId"),
		lookupAny(message, "params", "id"),
	} {
		if requestID := canonicalRequestID(candidate); requestID != "" {
			return requestID
		}
	}
	return ""
}

func canonicalRequestID(value any) string {
	switch current := value.(type) {
	case nil:
		return ""
	case string:
		current = strings.TrimSpace(current)
		if current == "" {
			return ""
		}
		if strings.HasPrefix(current, nativeRequestIDPrefix) {
			return encodeNativeRequestIDJSON(current)
		}
		return current
	default:
		return encodeNativeRequestIDJSON(current)
	}
}

func encodeNativeRequestIDJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil || string(raw) == "null" {
		return ""
	}
	return nativeRequestIDPrefix + base64.RawURLEncoding.EncodeToString(raw)
}

func decodeNativeRequestID(requestID string) any {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return ""
	}
	if !strings.HasPrefix(requestID, nativeRequestIDPrefix) {
		return requestID
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(requestID, nativeRequestIDPrefix))
	if err != nil {
		return requestID
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return requestID
	}
	return value
}

func extractRequestThreadID(message map[string]any, request map[string]any) string {
	return xutil.FirstNonEmpty(
		lookupString(message, "params", "thread", "id"),
		lookupString(message, "params", "threadId"),
		lookupString(request, "thread", "id"),
		xutil.LookupStringFromAny(request["threadId"]),
	)
}

func extractRequestTurnID(message map[string]any, request map[string]any) string {
	return xutil.FirstNonEmpty(
		lookupString(message, "params", "turn", "id"),
		lookupString(message, "params", "turnId"),
		lookupString(request, "turn", "id"),
		xutil.LookupStringFromAny(request["turnId"]),
	)
}

func extractRequestType(method string, request, params map[string]any) string {
	return string(canonicalRequestType(method, effectiveRawRequestType(method, request, params)))
}

func canonicalRequestType(method, rawType string) agentproto.RequestType {
	switch strings.TrimSpace(method) {
	case "tool/requestUserInput", "item/tool/requestUserInput":
		return agentproto.RequestTypeRequestUserInput
	case "item/tool/call":
		return agentproto.RequestTypeToolCallback
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval":
		return agentproto.RequestTypeApproval
	case "item/permissions/requestApproval":
		return agentproto.RequestTypePermissionsRequestApproval
	case "mcpServer/elicitation/request":
		return agentproto.RequestTypeMCPServerElicitation
	case "account/chatgptAuthTokens/refresh", "attestation/generate", "currentTime/read":
		return agentproto.RequestTypeUnsupportedServerRequest
	case "applyPatchApproval", "execCommandApproval":
		return agentproto.RequestTypeApproval
	}
	raw := strings.ToLower(strings.TrimSpace(rawType))
	switch {
	case raw == "", raw == "approval", raw == "confirm", raw == "confirmation":
		return agentproto.RequestTypeApproval
	case strings.HasPrefix(raw, "approval"):
		return agentproto.RequestTypeApproval
	case strings.HasPrefix(raw, "confirm"):
		return agentproto.RequestTypeApproval
	case raw == "request_user_input", raw == "requestuserinput":
		return agentproto.RequestTypeRequestUserInput
	case raw == "permissions_request_approval", raw == "permissionsrequestapproval":
		return agentproto.RequestTypePermissionsRequestApproval
	case raw == "mcp_server_elicitation", raw == "mcpserverelicitation":
		return agentproto.RequestTypeMCPServerElicitation
	case raw == "tool_callback", raw == "toolcallback":
		return agentproto.RequestTypeToolCallback
	default:
		return agentproto.RequestType(raw)
	}
}

func effectiveRawRequestType(method string, request, params map[string]any) string {
	if raw := extractRawRequestType(request, params); raw != "" {
		return raw
	}
	return defaultRequestRawType(method, params)
}

func defaultRequestRawType(method string, params map[string]any) string {
	switch strings.TrimSpace(method) {
	case "tool/requestUserInput", "item/tool/requestUserInput":
		return "request_user_input"
	case "item/tool/call":
		return "tool_callback"
	case "item/permissions/requestApproval":
		return "permissions_request_approval"
	case "mcpServer/elicitation/request":
		return "mcp_server_elicitation"
	case "account/chatgptAuthTokens/refresh":
		return "account_chatgpt_auth_tokens_refresh"
	case "attestation/generate":
		return "attestation_generate"
	case "currentTime/read":
		return "current_time_read"
	case "applyPatchApproval":
		return "apply_patch_approval"
	case "execCommandApproval":
		return "exec_command_approval"
	case "item/fileChange/requestApproval":
		return "approval_file_change"
	case "item/commandExecution/requestApproval":
		if len(lookupMap(params, "networkApprovalContext")) != 0 {
			return "approval_network"
		}
		return "approval_command"
	default:
		return ""
	}
}

func extractRawRequestType(request, params map[string]any) string {
	return strings.TrimSpace(xutil.FirstNonEmpty(
		xutil.LookupStringFromAny(request["type"]),
		xutil.LookupStringFromAny(request["requestType"]),
		xutil.LookupStringFromAny(request["kind"]),
		xutil.LookupStringFromAny(params["type"]),
		xutil.LookupStringFromAny(params["requestType"]),
		xutil.LookupStringFromAny(params["kind"]),
	))
}

func extractRequestPrompt(method string, message map[string]any) *agentproto.RequestPrompt {
	switch strings.TrimSpace(method) {
	case "tool/requestUserInput", "item/tool/requestUserInput":
		return extractRequestUserInputPrompt(message)
	case "item/tool/call":
		return extractToolCallbackPrompt(message)
	case "item/commandExecution/requestApproval":
		return extractCommandExecutionRequestApprovalPrompt(message)
	case "item/fileChange/requestApproval":
		return extractFileChangeRequestApprovalPrompt(message)
	case "item/permissions/requestApproval":
		return extractPermissionsRequestPrompt(message)
	case "mcpServer/elicitation/request":
		return extractMCPElicitationPrompt(message)
	case "account/chatgptAuthTokens/refresh", "attestation/generate", "currentTime/read":
		return extractUnsupportedServerRequestPrompt(method, message)
	default:
		return extractGenericRequestPrompt(method, message)
	}
}

func extractUnsupportedServerRequestPrompt(method string, message map[string]any) *agentproto.RequestPrompt {
	request := extractRequestPayload(message)
	params := lookupMap(message, "params")
	rawType := effectiveRawRequestType(method, request, params)
	prompt := &agentproto.RequestPrompt{
		Type:    agentproto.RequestTypeUnsupportedServerRequest,
		RawType: normalizeRawRequestType(rawType),
		Title:   "不支持的 Codex 请求",
		Body:    unsupportedServerRequestBody(method),
	}
	if prompt.RawType == "" {
		prompt.RawType = normalizeRawRequestType(method)
	}
	return prompt
}

func unsupportedServerRequestBody(method string) string {
	method = strings.TrimSpace(method)
	if method == "" {
		return "当前 Feishu Remote/headless 客户端不支持这类 Codex server request，已按 fail-closed 策略拒绝。"
	}
	return fmt.Sprintf("当前 Feishu Remote/headless 客户端不支持 Codex server request %q，已按 fail-closed 策略拒绝。", method)
}

func extractGenericRequestPrompt(method string, message map[string]any) *agentproto.RequestPrompt {
	request := extractRequestPayload(message)
	params := lookupMap(message, "params")
	rawType := effectiveRawRequestType(method, request, params)
	requestType := canonicalRequestType(method, rawType)
	if requestType == agentproto.RequestTypeToolCallback {
		return extractToolCallbackPromptFromPayload(request, params)
	}
	prompt := &agentproto.RequestPrompt{
		Type:           requestType,
		RawType:        normalizeRawRequestType(rawType),
		ItemID:         extractRequestItemID(request, params),
		Title:          xutil.FirstNonEmpty(xutil.LookupStringFromAny(request["title"]), xutil.LookupStringFromAny(request["name"]), xutil.LookupStringFromAny(params["title"])),
		Body:           extractRequestBody(request, params),
		AcceptLabel:    extractRequestAcceptLabel(request, params),
		DeclineLabel:   extractRequestDeclineLabel(request, params),
		Options:        requestOptionsFromMaps(extractRequestOptions(request, params)),
		Questions:      requestQuestionsFromMaps(extractRequestUserInputQuestions(request, params)),
		Permissions:    nil,
		MCPElicitation: nil,
	}
	if prompt.Title == "" {
		prompt.Title = defaultRequestTitle(prompt.Type)
	}
	return prompt
}

func extractToolCallbackPrompt(message map[string]any) *agentproto.RequestPrompt {
	return extractToolCallbackPromptFromPayload(extractRequestPayload(message), lookupMap(message, "params"))
}

func extractToolCallbackPromptFromPayload(request, params map[string]any) *agentproto.RequestPrompt {
	rawPayload := xutil.CloneMap(params)
	if len(rawPayload) == 0 {
		rawPayload = xutil.CloneMap(request)
	}
	prompt := &agentproto.RequestPrompt{
		Type:    agentproto.RequestTypeToolCallback,
		RawType: "tool_callback",
		ItemID:  extractRequestItemID(request, params),
		Title: xutil.FirstNonEmpty(
			xutil.LookupStringFromAny(request["title"]),
			xutil.LookupStringFromAny(params["title"]),
		),
		ToolCallback: &agentproto.ToolCallbackPrompt{
			CallID: xutil.FirstNonEmpty(
				xutil.LookupStringFromAny(params["callId"]),
				xutil.LookupStringFromAny(request["callId"]),
			),
			ToolName: xutil.FirstNonEmpty(
				xutil.LookupStringFromAny(params["tool"]),
				xutil.LookupStringFromAny(request["tool"]),
			),
			Arguments: xutil.CloneJSONValue(firstNonNil(
				params["arguments"],
				request["arguments"],
			)),
			RawPayload: xutil.CloneMap(rawPayload),
		},
	}
	if prompt.Title == "" {
		prompt.Title = defaultRequestTitle(prompt.Type)
	}
	return prompt
}

func extractCommandExecutionRequestApprovalPrompt(message map[string]any) *agentproto.RequestPrompt {
	prompt := extractGenericRequestPrompt("item/commandExecution/requestApproval", message)
	if prompt == nil {
		return nil
	}
	params := lookupMap(message, "params")
	bodyLines := make([]string, 0, 8)
	if prompt.Body != "" {
		bodyLines = append(bodyLines, prompt.Body)
	}
	network := lookupMap(params, "networkApprovalContext")
	if len(network) != 0 {
		if prompt.Title == "" || prompt.Title == "需要确认" {
			prompt.Title = "需要确认网络访问"
		}
		host := xutil.FirstNonEmpty(
			xutil.LookupStringFromAny(network["host"]),
			xutil.LookupStringFromAny(network["hostname"]),
		)
		protocol := xutil.LookupStringFromAny(network["protocol"])
		port := xutil.FirstNonEmpty(
			xutil.LookupStringFromAny(network["port"]),
			xutil.LookupStringFromAny(network["destinationPort"]),
		)
		if len(bodyLines) == 0 {
			bodyLines = append(bodyLines, "本地 Codex 正在等待你确认一次受管网络访问。")
		}
		if host != "" {
			bodyLines = append(bodyLines, "目标主机："+host)
		}
		if protocol != "" {
			bodyLines = append(bodyLines, "协议："+protocol)
		}
		if port != "" {
			bodyLines = append(bodyLines, "端口："+port)
		}
		prompt.Body = strings.TrimSpace(strings.Join(bodyLines, "\n"))
		return prompt
	}
	if prompt.Title == "" || prompt.Title == "需要确认" {
		prompt.Title = "需要确认执行命令"
	}
	if cwd := strings.TrimSpace(xutil.LookupStringFromAny(params["cwd"])); cwd != "" && !strings.Contains(prompt.Body, cwd) {
		if len(bodyLines) > 0 {
			bodyLines = append(bodyLines, "")
		}
		bodyLines = append(bodyLines, "工作目录："+cwd)
	}
	prompt.Body = strings.TrimSpace(strings.Join(bodyLines, "\n"))
	return prompt
}

func extractFileChangeRequestApprovalPrompt(message map[string]any) *agentproto.RequestPrompt {
	prompt := extractGenericRequestPrompt("item/fileChange/requestApproval", message)
	if prompt == nil {
		return nil
	}
	params := lookupMap(message, "params")
	if prompt.Title == "" || prompt.Title == "需要确认" {
		prompt.Title = "需要确认修改文件"
	}
	grantRoot := strings.TrimSpace(xutil.FirstNonEmpty(
		xutil.LookupStringFromAny(params["grantRoot"]),
		lookupString(params, "request", "grantRoot"),
	))
	if grantRoot == "" {
		return prompt
	}
	bodyLines := make([]string, 0, 4)
	if prompt.Body != "" {
		bodyLines = append(bodyLines, prompt.Body, "")
	}
	bodyLines = append(bodyLines, "授权根目录："+grantRoot)
	prompt.Body = strings.TrimSpace(strings.Join(bodyLines, "\n"))
	return prompt
}

func extractRequestUserInputPrompt(message map[string]any) *agentproto.RequestPrompt {
	params := lookupMap(message, "params")
	prompt := &agentproto.RequestPrompt{
		Type:      agentproto.RequestTypeRequestUserInput,
		Title:     xutil.FirstNonEmpty(xutil.LookupStringFromAny(params["title"]), xutil.LookupStringFromAny(params["header"])),
		RawType:   "request_user_input",
		Body:      xutil.FirstNonEmpty(xutil.LookupStringFromAny(params["message"]), xutil.LookupStringFromAny(params["body"]), xutil.LookupStringFromAny(params["description"])),
		ItemID:    extractRequestItemID(nil, params),
		Questions: requestQuestionsFromMaps(extractRequestUserInputQuestions(nil, params)),
	}
	if prompt.Title == "" {
		prompt.Title = defaultRequestTitle(prompt.Type)
	}
	return prompt
}

func extractPermissionsRequestPrompt(message map[string]any) *agentproto.RequestPrompt {
	params := lookupMap(message, "params")
	reason := xutil.FirstNonEmpty(
		xutil.LookupStringFromAny(params["reason"]),
		lookupString(params, "request", "reason"),
	)
	body := xutil.FirstNonEmpty(
		xutil.LookupStringFromAny(params["message"]),
		xutil.LookupStringFromAny(params["body"]),
		reason,
	)
	prompt := &agentproto.RequestPrompt{
		Type:    agentproto.RequestTypePermissionsRequestApproval,
		RawType: "permissions_request_approval",
		Title:   xutil.FirstNonEmpty(xutil.LookupStringFromAny(params["title"]), "需要授予权限"),
		Body:    body,
		ItemID: xutil.FirstNonEmpty(
			xutil.LookupStringFromAny(params["itemId"]),
			lookupString(params, "request", "itemId"),
		),
		Permissions: &agentproto.PermissionsRequestPrompt{
			Reason:      reason,
			Permissions: extractRequestMapList(firstNonNil(params["permissions"], lookupAny(params, "request", "permissions"))),
		},
	}
	if prompt.Body == "" {
		prompt.Body = "本地 Codex 正在等待授予附加权限。"
	}
	return prompt
}

func extractMCPElicitationPrompt(message map[string]any) *agentproto.RequestPrompt {
	params := lookupMap(message, "params")
	request := lookupMap(message, "params", "request")
	mode := strings.TrimSpace(xutil.FirstNonEmpty(
		xutil.LookupStringFromAny(request["mode"]),
		xutil.LookupStringFromAny(params["mode"]),
	))
	body := xutil.FirstNonEmpty(
		xutil.LookupStringFromAny(request["message"]),
		xutil.LookupStringFromAny(params["message"]),
	)
	url := xutil.FirstNonEmpty(
		xutil.LookupStringFromAny(request["url"]),
		xutil.LookupStringFromAny(params["url"]),
	)
	if mode == "url" && url != "" && !strings.Contains(body, url) {
		if body != "" {
			body += "\n\n"
		}
		body += url
	}
	prompt := &agentproto.RequestPrompt{
		Type:    agentproto.RequestTypeMCPServerElicitation,
		RawType: "mcp_server_elicitation",
		Title:   xutil.FirstNonEmpty(xutil.LookupStringFromAny(params["title"]), "需要处理 MCP 请求"),
		Body:    body,
		MCPElicitation: &agentproto.MCPElicitationPrompt{
			ServerName: xutil.FirstNonEmpty(
				xutil.LookupStringFromAny(params["serverName"]),
				xutil.LookupStringFromAny(request["serverName"]),
			),
			Mode:          mode,
			Message:       xutil.FirstNonEmpty(xutil.LookupStringFromAny(request["message"]), xutil.LookupStringFromAny(params["message"])),
			URL:           url,
			ElicitationID: xutil.FirstNonEmpty(xutil.LookupStringFromAny(request["elicitationId"]), xutil.LookupStringFromAny(params["elicitationId"])),
			RequestedSchema: xutil.CloneMap(lookupMapFromAny(firstNonNil(
				request["requestedSchema"],
				params["requestedSchema"],
			))),
			Meta: xutil.CloneMap(lookupMapFromAny(firstNonNil(
				request["_meta"],
				params["_meta"],
			))),
		},
	}
	if prompt.Body == "" {
		prompt.Body = "本地 Codex 正在等待 MCP server 返回更多信息。"
	}
	return prompt
}

func extractRequestMetadata(method string, message map[string]any, prompt *agentproto.RequestPrompt) map[string]any {
	metadata := map[string]any{}
	if prompt == nil {
		return metadata
	}
	if prompt.Type != "" {
		metadata["requestType"] = string(prompt.Type)
	}
	if prompt.RawType != "" {
		metadata["requestKind"] = prompt.RawType
	}
	if prompt.ItemID != "" {
		metadata["itemId"] = prompt.ItemID
	}
	if prompt.Title != "" {
		metadata["title"] = prompt.Title
	}
	if prompt.Body != "" {
		metadata["body"] = prompt.Body
	}
	if prompt.AcceptLabel != "" {
		metadata["acceptLabel"] = prompt.AcceptLabel
	}
	if prompt.DeclineLabel != "" {
		metadata["declineLabel"] = prompt.DeclineLabel
	}
	if len(prompt.Options) != 0 {
		metadata["options"] = requestOptionsToMaps(prompt.Options)
	}
	if len(prompt.Questions) != 0 {
		metadata["questions"] = requestQuestionsToMaps(prompt.Questions)
	}
	if prompt.Permissions != nil {
		if prompt.Permissions.Reason != "" {
			metadata["reason"] = prompt.Permissions.Reason
		}
		if len(prompt.Permissions.Permissions) != 0 {
			metadata["permissions"] = xutil.CloneJSONValue(prompt.Permissions.Permissions)
		}
	}
	if prompt.MCPElicitation != nil {
		if prompt.MCPElicitation.ServerName != "" {
			metadata["serverName"] = prompt.MCPElicitation.ServerName
		}
		if prompt.MCPElicitation.Mode != "" {
			metadata["elicitationMode"] = prompt.MCPElicitation.Mode
		}
		if prompt.MCPElicitation.Message != "" {
			metadata["elicitationMessage"] = prompt.MCPElicitation.Message
		}
		if prompt.MCPElicitation.URL != "" {
			metadata["url"] = prompt.MCPElicitation.URL
		}
		if prompt.MCPElicitation.ElicitationID != "" {
			metadata["elicitationId"] = prompt.MCPElicitation.ElicitationID
		}
		if len(prompt.MCPElicitation.RequestedSchema) != 0 {
			metadata["requestedSchema"] = xutil.CloneMap(prompt.MCPElicitation.RequestedSchema)
		}
		if len(prompt.MCPElicitation.Meta) != 0 {
			metadata["meta"] = xutil.CloneMap(prompt.MCPElicitation.Meta)
		}
	}
	if prompt.ToolCallback != nil {
		if prompt.ToolCallback.CallID != "" {
			metadata["callId"] = prompt.ToolCallback.CallID
		}
		if prompt.ToolCallback.ToolName != "" {
			metadata["tool"] = prompt.ToolCallback.ToolName
		}
		if prompt.ToolCallback.Arguments != nil {
			metadata["arguments"] = xutil.CloneJSONValue(prompt.ToolCallback.Arguments)
		}
		if len(prompt.ToolCallback.RawPayload) != 0 {
			metadata["toolCallbackPayload"] = xutil.CloneMap(prompt.ToolCallback.RawPayload)
		}
	}
	params := lookupMap(message, "params")
	if value := strings.TrimSpace(xutil.LookupStringFromAny(params["cwd"])); value != "" {
		metadata["cwd"] = value
	}
	if value := strings.TrimSpace(xutil.FirstNonEmpty(xutil.LookupStringFromAny(params["grantRoot"]), lookupString(params, "request", "grantRoot"))); value != "" {
		metadata["grantRoot"] = value
	}
	if actions := extractRequestMapList(params["commandActions"]); len(actions) != 0 {
		metadata["commandActions"] = xutil.CloneJSONValue(actions)
	}
	if network := xutil.CloneMap(lookupMap(params, "networkApprovalContext")); len(network) != 0 {
		metadata["networkApprovalContext"] = network
	}
	if amendment := xutil.CloneMap(lookupMap(params, "proposedExecpolicyAmendment")); len(amendment) != 0 {
		metadata["proposedExecpolicyAmendment"] = amendment
	}
	if permissions := extractRequestMapList(params["additionalPermissions"]); len(permissions) != 0 {
		metadata["additionalPermissions"] = xutil.CloneJSONValue(permissions)
	}
	if decisions := xutil.CloneJSONValue(firstNonNil(params["availableDecisions"], lookupAny(message, "params", "request", "availableDecisions"))); decisions != nil {
		metadata["availableDecisions"] = decisions
	}
	if requestMethod := strings.TrimSpace(method); requestMethod != "" {
		metadata["requestMethod"] = requestMethod
	}
	return metadata
}

func extractResolvedRequestMetadata(requestType string, request, params map[string]any) map[string]any {
	metadata := map[string]any{}
	if requestType != "" {
		metadata["requestType"] = requestType
	}
	result := lookupMapFromAny(firstNonNil(
		params["result"],
		params["response"],
		request["result"],
		request["response"],
	))
	decision := xutil.FirstNonEmpty(
		xutil.LookupStringFromAny(result["decision"]),
		lookupString(params, "result", "decision"),
		lookupString(params, "response", "decision"),
		xutil.LookupStringFromAny(params["decision"]),
		lookupString(request, "result", "decision"),
		lookupString(request, "response", "decision"),
		xutil.LookupStringFromAny(request["decision"]),
	)
	if decision != "" {
		metadata["decision"] = decision
	}
	action := xutil.FirstNonEmpty(
		xutil.LookupStringFromAny(result["action"]),
		xutil.LookupStringFromAny(params["action"]),
		xutil.LookupStringFromAny(request["action"]),
	)
	if action != "" {
		metadata["action"] = action
	}
	scope := xutil.FirstNonEmpty(
		xutil.LookupStringFromAny(result["scope"]),
		xutil.LookupStringFromAny(params["scope"]),
		xutil.LookupStringFromAny(request["scope"]),
	)
	if scope != "" {
		metadata["scope"] = scope
	}
	if permissions := extractRequestMapList(firstNonNil(result["permissions"], params["permissions"], request["permissions"])); len(permissions) != 0 {
		metadata["permissions"] = permissions
	}
	if content := xutil.CloneJSONValue(result["content"]); content != nil {
		metadata["content"] = content
	}
	if meta := lookupMap(result, "_meta"); len(meta) != 0 {
		metadata["meta"] = meta
	}
	return metadata
}

func extractRequestCommand(request, params map[string]any) string {
	command := xutil.FirstNonEmpty(
		xutil.LookupStringFromAny(request["command"]),
		lookupString(request, "command", "command"),
		lookupString(request, "command", "text"),
		xutil.LookupStringFromAny(params["command"]),
		lookupString(params, "command", "command"),
		lookupString(params, "command", "text"),
	)
	return strings.TrimSpace(command)
}

func extractRequestBody(request, params map[string]any) string {
	body := xutil.FirstNonEmpty(
		xutil.LookupStringFromAny(request["message"]),
		xutil.LookupStringFromAny(request["description"]),
		xutil.LookupStringFromAny(request["body"]),
		xutil.LookupStringFromAny(request["prompt"]),
		xutil.LookupStringFromAny(request["reason"]),
		xutil.LookupStringFromAny(params["message"]),
		xutil.LookupStringFromAny(params["description"]),
		xutil.LookupStringFromAny(params["body"]),
	)
	command := extractRequestCommand(request, params)
	if command != "" {
		if body != "" {
			body += "\n\n"
		}
		body += "```text\n" + command + "\n```"
	}
	return body
}

func extractRequestAcceptLabel(request, params map[string]any) string {
	return xutil.FirstNonEmpty(
		xutil.LookupStringFromAny(request["acceptLabel"]),
		xutil.LookupStringFromAny(request["approveLabel"]),
		xutil.LookupStringFromAny(request["allowLabel"]),
		xutil.LookupStringFromAny(request["confirmLabel"]),
		xutil.LookupStringFromAny(params["acceptLabel"]),
	)
}

func extractRequestDeclineLabel(request, params map[string]any) string {
	return xutil.FirstNonEmpty(
		xutil.LookupStringFromAny(request["declineLabel"]),
		xutil.LookupStringFromAny(request["denyLabel"]),
		xutil.LookupStringFromAny(request["rejectLabel"]),
		xutil.LookupStringFromAny(params["declineLabel"]),
	)
}

func extractRequestItemID(request, params map[string]any) string {
	return xutil.FirstNonEmpty(
		xutil.LookupStringFromAny(request["itemId"]),
		lookupString(request, "item", "id"),
		xutil.LookupStringFromAny(params["itemId"]),
		lookupString(params, "item", "id"),
	)
}

func defaultRequestTitle(requestType agentproto.RequestType) string {
	switch requestType {
	case agentproto.RequestTypeApproval:
		return "需要确认"
	case agentproto.RequestTypeRequestUserInput:
		return "需要补充输入"
	case agentproto.RequestTypePermissionsRequestApproval:
		return "需要授予权限"
	case agentproto.RequestTypeToolCallback:
		return "收到工具回调"
	default:
		return "需要处理请求"
	}
}

func normalizeRawRequestType(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
