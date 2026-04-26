package codex

import (
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
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
	return firstNonEmptyString(
		lookupString(message, "params", "thread", "id"),
		lookupString(message, "params", "threadId"),
		lookupString(request, "thread", "id"),
		lookupStringFromAny(request["threadId"]),
	)
}

func extractRequestTurnID(message map[string]any, request map[string]any) string {
	return firstNonEmptyString(
		lookupString(message, "params", "turn", "id"),
		lookupString(message, "params", "turnId"),
		lookupString(request, "turn", "id"),
		lookupStringFromAny(request["turnId"]),
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
	return strings.TrimSpace(firstNonEmptyString(
		lookupStringFromAny(request["type"]),
		lookupStringFromAny(request["requestType"]),
		lookupStringFromAny(request["kind"]),
		lookupStringFromAny(params["type"]),
		lookupStringFromAny(params["requestType"]),
		lookupStringFromAny(params["kind"]),
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
	default:
		return extractGenericRequestPrompt(method, message)
	}
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
		Title:          firstNonEmptyString(lookupStringFromAny(request["title"]), lookupStringFromAny(request["name"]), lookupStringFromAny(params["title"])),
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
	rawPayload := cloneMap(params)
	if len(rawPayload) == 0 {
		rawPayload = cloneMap(request)
	}
	prompt := &agentproto.RequestPrompt{
		Type:    agentproto.RequestTypeToolCallback,
		RawType: "tool_callback",
		ItemID:  extractRequestItemID(request, params),
		Title: firstNonEmptyString(
			lookupStringFromAny(request["title"]),
			lookupStringFromAny(params["title"]),
		),
		ToolCallback: &agentproto.ToolCallbackPrompt{
			CallID: firstNonEmptyString(
				lookupStringFromAny(params["callId"]),
				lookupStringFromAny(request["callId"]),
			),
			ToolName: firstNonEmptyString(
				lookupStringFromAny(params["tool"]),
				lookupStringFromAny(request["tool"]),
			),
			Arguments: cloneJSONValue(firstNonNil(
				params["arguments"],
				request["arguments"],
			)),
			RawPayload: cloneMap(rawPayload),
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
		host := firstNonEmptyString(
			lookupStringFromAny(network["host"]),
			lookupStringFromAny(network["hostname"]),
		)
		protocol := lookupStringFromAny(network["protocol"])
		port := firstNonEmptyString(
			lookupStringFromAny(network["port"]),
			lookupStringFromAny(network["destinationPort"]),
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
	if cwd := strings.TrimSpace(lookupStringFromAny(params["cwd"])); cwd != "" && !strings.Contains(prompt.Body, cwd) {
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
	grantRoot := strings.TrimSpace(firstNonEmptyString(
		lookupStringFromAny(params["grantRoot"]),
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
		Title:     firstNonEmptyString(lookupStringFromAny(params["title"]), lookupStringFromAny(params["header"])),
		RawType:   "request_user_input",
		Body:      firstNonEmptyString(lookupStringFromAny(params["message"]), lookupStringFromAny(params["body"]), lookupStringFromAny(params["description"])),
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
	reason := firstNonEmptyString(
		lookupStringFromAny(params["reason"]),
		lookupString(params, "request", "reason"),
	)
	body := firstNonEmptyString(
		lookupStringFromAny(params["message"]),
		lookupStringFromAny(params["body"]),
		reason,
	)
	prompt := &agentproto.RequestPrompt{
		Type:    agentproto.RequestTypePermissionsRequestApproval,
		RawType: "permissions_request_approval",
		Title:   firstNonEmptyString(lookupStringFromAny(params["title"]), "需要授予权限"),
		Body:    body,
		ItemID: firstNonEmptyString(
			lookupStringFromAny(params["itemId"]),
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
	mode := strings.TrimSpace(firstNonEmptyString(
		lookupStringFromAny(request["mode"]),
		lookupStringFromAny(params["mode"]),
	))
	body := firstNonEmptyString(
		lookupStringFromAny(request["message"]),
		lookupStringFromAny(params["message"]),
	)
	url := firstNonEmptyString(
		lookupStringFromAny(request["url"]),
		lookupStringFromAny(params["url"]),
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
		Title:   firstNonEmptyString(lookupStringFromAny(params["title"]), "需要处理 MCP 请求"),
		Body:    body,
		MCPElicitation: &agentproto.MCPElicitationPrompt{
			ServerName: firstNonEmptyString(
				lookupStringFromAny(params["serverName"]),
				lookupStringFromAny(request["serverName"]),
			),
			Mode:          mode,
			Message:       firstNonEmptyString(lookupStringFromAny(request["message"]), lookupStringFromAny(params["message"])),
			URL:           url,
			ElicitationID: firstNonEmptyString(lookupStringFromAny(request["elicitationId"]), lookupStringFromAny(params["elicitationId"])),
			RequestedSchema: cloneMap(lookupMapFromAny(firstNonNil(
				request["requestedSchema"],
				params["requestedSchema"],
			))),
			Meta: cloneMap(lookupMapFromAny(firstNonNil(
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
			metadata["permissions"] = cloneJSONValue(prompt.Permissions.Permissions)
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
			metadata["requestedSchema"] = cloneMap(prompt.MCPElicitation.RequestedSchema)
		}
		if len(prompt.MCPElicitation.Meta) != 0 {
			metadata["meta"] = cloneMap(prompt.MCPElicitation.Meta)
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
			metadata["arguments"] = cloneJSONValue(prompt.ToolCallback.Arguments)
		}
		if len(prompt.ToolCallback.RawPayload) != 0 {
			metadata["toolCallbackPayload"] = cloneMap(prompt.ToolCallback.RawPayload)
		}
	}
	params := lookupMap(message, "params")
	if value := strings.TrimSpace(lookupStringFromAny(params["cwd"])); value != "" {
		metadata["cwd"] = value
	}
	if value := strings.TrimSpace(firstNonEmptyString(lookupStringFromAny(params["grantRoot"]), lookupString(params, "request", "grantRoot"))); value != "" {
		metadata["grantRoot"] = value
	}
	if actions := extractRequestMapList(params["commandActions"]); len(actions) != 0 {
		metadata["commandActions"] = cloneJSONValue(actions)
	}
	if network := cloneMap(lookupMap(params, "networkApprovalContext")); len(network) != 0 {
		metadata["networkApprovalContext"] = network
	}
	if amendment := cloneMap(lookupMap(params, "proposedExecpolicyAmendment")); len(amendment) != 0 {
		metadata["proposedExecpolicyAmendment"] = amendment
	}
	if permissions := extractRequestMapList(params["additionalPermissions"]); len(permissions) != 0 {
		metadata["additionalPermissions"] = cloneJSONValue(permissions)
	}
	if decisions := cloneJSONValue(firstNonNil(params["availableDecisions"], lookupAny(message, "params", "request", "availableDecisions"))); decisions != nil {
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
	decision := firstNonEmptyString(
		lookupStringFromAny(result["decision"]),
		lookupString(params, "result", "decision"),
		lookupString(params, "response", "decision"),
		lookupStringFromAny(params["decision"]),
		lookupString(request, "result", "decision"),
		lookupString(request, "response", "decision"),
		lookupStringFromAny(request["decision"]),
	)
	if decision != "" {
		metadata["decision"] = decision
	}
	action := firstNonEmptyString(
		lookupStringFromAny(result["action"]),
		lookupStringFromAny(params["action"]),
		lookupStringFromAny(request["action"]),
	)
	if action != "" {
		metadata["action"] = action
	}
	scope := firstNonEmptyString(
		lookupStringFromAny(result["scope"]),
		lookupStringFromAny(params["scope"]),
		lookupStringFromAny(request["scope"]),
	)
	if scope != "" {
		metadata["scope"] = scope
	}
	if permissions := extractRequestMapList(firstNonNil(result["permissions"], params["permissions"], request["permissions"])); len(permissions) != 0 {
		metadata["permissions"] = permissions
	}
	if content := cloneJSONValue(result["content"]); content != nil {
		metadata["content"] = content
	}
	if meta := lookupMap(result, "_meta"); len(meta) != 0 {
		metadata["meta"] = meta
	}
	return metadata
}

func extractRequestCommand(request, params map[string]any) string {
	command := firstNonEmptyString(
		lookupStringFromAny(request["command"]),
		lookupString(request, "command", "command"),
		lookupString(request, "command", "text"),
		lookupStringFromAny(params["command"]),
		lookupString(params, "command", "command"),
		lookupString(params, "command", "text"),
	)
	return strings.TrimSpace(command)
}

func extractRequestBody(request, params map[string]any) string {
	body := firstNonEmptyString(
		lookupStringFromAny(request["message"]),
		lookupStringFromAny(request["description"]),
		lookupStringFromAny(request["body"]),
		lookupStringFromAny(request["prompt"]),
		lookupStringFromAny(request["reason"]),
		lookupStringFromAny(params["message"]),
		lookupStringFromAny(params["description"]),
		lookupStringFromAny(params["body"]),
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
	return firstNonEmptyString(
		lookupStringFromAny(request["acceptLabel"]),
		lookupStringFromAny(request["approveLabel"]),
		lookupStringFromAny(request["allowLabel"]),
		lookupStringFromAny(request["confirmLabel"]),
		lookupStringFromAny(params["acceptLabel"]),
	)
}

func extractRequestDeclineLabel(request, params map[string]any) string {
	return firstNonEmptyString(
		lookupStringFromAny(request["declineLabel"]),
		lookupStringFromAny(request["denyLabel"]),
		lookupStringFromAny(request["rejectLabel"]),
		lookupStringFromAny(params["declineLabel"]),
	)
}

func extractRequestItemID(request, params map[string]any) string {
	return firstNonEmptyString(
		lookupStringFromAny(request["itemId"]),
		lookupString(request, "item", "id"),
		lookupStringFromAny(params["itemId"]),
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
