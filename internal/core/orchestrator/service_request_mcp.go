package orchestrator

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const mcpElicitationJSONFieldID = "__mcp_elicitation_json"

func buildPermissionsRequestBody(prompt *agentproto.RequestPrompt, metadata map[string]any) string {
	body := strings.TrimSpace(firstNonEmpty(
		promptBody(prompt),
		metadataString(metadata, "body"),
		metadataString(metadata, "reason"),
		"本地 Codex 正在等待授予附加权限。",
	))
	lines := []string{body}
	if reason := strings.TrimSpace(firstNonEmpty(metadataString(metadata, "reason"), promptPermissionsReason(prompt))); reason != "" && !strings.Contains(body, reason) {
		lines = append(lines, "", "原因："+reason)
	}
	permissions := promptPermissionsList(prompt, metadata)
	if len(permissions) != 0 {
		lines = append(lines, "", "申请权限：")
		for _, permission := range permissions {
			name := firstNonEmpty(
				lookupStringFromAny(permission["title"]),
				lookupStringFromAny(permission["name"]),
				lookupStringFromAny(permission["permission"]),
				lookupStringFromAny(permission["scope"]),
			)
			if code := firstNonEmpty(lookupStringFromAny(permission["name"]), lookupStringFromAny(permission["permission"])); code != "" && code != name {
				name += " (`" + code + "`)"
			}
			if name == "" {
				name = "`(unknown)`"
			}
			lines = append(lines, "- "+name)
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func buildPermissionsRequestOptions() []state.RequestPromptOptionRecord {
	return []state.RequestPromptOptionRecord{
		{OptionID: "accept", Label: "允许本次", Style: "primary"},
		{OptionID: "acceptForSession", Label: "本会话允许", Style: "default"},
		{OptionID: "decline", Label: "拒绝", Style: "default"},
	}
}

func buildPermissionsRequestResponse(request *state.RequestPromptRecord, action control.Action) (map[string]any, bool, []control.UIEvent) {
	optionID := control.NormalizeRequestOptionID(firstNonEmpty(action.RequestOptionID, requestOptionIDFromApproved(action.Approved)))
	switch optionID {
	case "accept":
		return map[string]any{
			"permissions": cloneJSONValue(promptPermissionsList(request.Prompt, nil)),
			"scope":       "turn",
		}, true, nil
	case "acceptForSession":
		return map[string]any{
			"permissions": cloneJSONValue(promptPermissionsList(request.Prompt, nil)),
			"scope":       "session",
		}, true, nil
	case "decline":
		return map[string]any{
			"permissions": []any{},
			"scope":       "turn",
		}, true, nil
	default:
		return nil, false, nil
	}
}

func buildMCPElicitationBody(prompt *agentproto.RequestPrompt, metadata map[string]any) string {
	mode := mcpElicitationMode(prompt, metadata)
	lines := []string{}
	if body := strings.TrimSpace(firstNonEmpty(promptBody(prompt), metadataString(metadata, "body"))); body != "" {
		lines = append(lines, body)
	}
	if serverName := strings.TrimSpace(firstNonEmpty(promptMCPElicitationServerName(prompt), metadataString(metadata, "serverName"))); serverName != "" {
		lines = append(lines, "", "MCP 服务：`"+serverName+"`")
	}
	switch mode {
	case "url":
		url := strings.TrimSpace(firstNonEmpty(promptMCPElicitationURL(prompt), metadataString(metadata, "url")))
		if url != "" {
			lines = append(lines, "", "授权页面："+url)
			lines = append(lines, "完成外部授权后，再点击“继续”。")
		}
	case "form":
		lines = append(lines, "", "请填写需要返回给 MCP server 的内容，然后提交继续。")
	}
	if len(lines) == 0 {
		return "本地 Codex 正在等待 MCP server 返回更多信息。"
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func buildMCPElicitationQuestions(prompt *agentproto.RequestPrompt, metadata map[string]any) []state.RequestPromptQuestionRecord {
	if mcpElicitationMode(prompt, metadata) != "form" {
		return nil
	}
	schema := promptMCPElicitationSchema(prompt, metadata)
	if len(schema) == 0 {
		return []state.RequestPromptQuestionRecord{{
			ID:          mcpElicitationJSONFieldID,
			Header:      "返回内容",
			Question:    "请填写要返回给 MCP server 的 JSON 内容",
			AllowOther:  true,
			Placeholder: `例如 {"token":"..."}`,
		}}
	}
	if questions := buildMCPElicitationFlatQuestions(schema); len(questions) != 0 {
		return questions
	}
	return []state.RequestPromptQuestionRecord{{
		ID:          mcpElicitationJSONFieldID,
		Header:      "返回内容",
		Question:    "当前 schema 较复杂，请直接填写 JSON 内容",
		AllowOther:  true,
		Placeholder: `例如 {"token":"...","granted":true}`,
	}}
}

func buildMCPElicitationOptions(prompt *agentproto.RequestPrompt, metadata map[string]any, questions []state.RequestPromptQuestionRecord) []state.RequestPromptOptionRecord {
	if mcpElicitationMode(prompt, metadata) == "form" || len(questions) != 0 {
		return []state.RequestPromptOptionRecord{
			{OptionID: "decline", Label: "拒绝", Style: "default"},
			{OptionID: "cancel", Label: "取消", Style: "default"},
		}
	}
	continueLabel := "继续"
	if strings.TrimSpace(firstNonEmpty(promptMCPElicitationURL(prompt), metadataString(metadata, "url"))) != "" {
		continueLabel = "我已完成，继续"
	}
	return []state.RequestPromptOptionRecord{
		{OptionID: "accept", Label: continueLabel, Style: "primary"},
		{OptionID: "decline", Label: "拒绝", Style: "default"},
		{OptionID: "cancel", Label: "取消", Style: "default"},
	}
}

func (s *Service) buildMCPElicitationResponse(surface *state.SurfaceConsoleRecord, request *state.RequestPromptRecord, action control.Action) (map[string]any, bool, []control.UIEvent) {
	optionID := control.NormalizeRequestOptionID(strings.TrimSpace(action.RequestOptionID))
	switch optionID {
	case "decline", "cancel":
		return buildMCPElicitationPayload(optionID, nil, promptMCPElicitationMeta(request.Prompt, nil)), true, nil
	}
	if len(request.Questions) == 0 && optionID == "" {
		optionID = "accept"
	}
	if len(request.Questions) == 0 && optionID == "accept" {
		return buildMCPElicitationPayload("accept", nil, promptMCPElicitationMeta(request.Prompt, nil)), true, nil
	}
	submitIntent := optionID == "accept" || optionID == requestUserInputNormalizedOptionID(requestUserInputSubmitOptionID)
	content, complete, missingLabels, errText := buildMCPElicitationContent(request, action.RequestAnswers)
	if errText != "" {
		return nil, false, notice(surface, "request_invalid", errText)
	}
	if !submitIntent {
		if len(action.RequestAnswers) == 0 {
			return nil, false, notice(surface, "request_invalid", "请先填写或选择返回内容，再提交给 MCP server。")
		}
		bumpRequestCardRevision(request)
		if len(missingLabels) == 0 {
			return nil, false, s.requestPromptRefreshWithNotice(surface, request, "request_saved", "已记录当前输入，确认无误后点击“提交并继续”。")
		}
		if len(missingLabels) == 1 {
			return nil, false, s.requestPromptRefreshWithNotice(surface, request, "request_saved", fmt.Sprintf("已记录当前输入。还差 1 项：%s。", missingLabels[0]))
		}
		return nil, false, s.requestPromptRefreshWithNotice(surface, request, "request_saved", fmt.Sprintf("已记录当前输入。还差 %d 项待填写。", len(missingLabels)))
	}
	if !complete {
		if len(missingLabels) == 1 {
			return nil, false, notice(surface, "request_invalid", fmt.Sprintf("字段“%s”还没有填写。", missingLabels[0]))
		}
		if len(missingLabels) != 0 {
			return nil, false, notice(surface, "request_invalid", fmt.Sprintf("仍有 %d 个字段未填写，请补全后再提交。", len(missingLabels)))
		}
		return nil, false, notice(surface, "request_invalid", "当前没有可提交的 MCP 返回内容。")
	}
	return buildMCPElicitationPayload("accept", content, promptMCPElicitationMeta(request.Prompt, nil)), true, nil
}

func buildMCPElicitationPayload(action string, content any, meta map[string]any) map[string]any {
	payload := map[string]any{
		"action":  strings.TrimSpace(action),
		"content": cloneJSONValue(content),
	}
	payload["_meta"] = cloneJSONValue(meta)
	if payload["_meta"] == nil {
		payload["_meta"] = map[string]any{}
	}
	return payload
}

func buildMCPElicitationContent(request *state.RequestPromptRecord, rawAnswers map[string][]string) (any, bool, []string, string) {
	if request == nil {
		return nil, false, nil, "这个 MCP 请求缺少有效定义，当前无法提交。"
	}
	if request.DraftAnswers == nil {
		request.DraftAnswers = map[string]string{}
	}
	for key, values := range rawAnswers {
		answer := firstTrimmedAnswer(values)
		if strings.TrimSpace(key) == "" || answer == "" {
			continue
		}
		request.DraftAnswers[strings.TrimSpace(key)] = answer
	}
	if len(request.Questions) == 1 && request.Questions[0].ID == mcpElicitationJSONFieldID {
		answer := strings.TrimSpace(request.DraftAnswers[mcpElicitationJSONFieldID])
		if answer == "" {
			return nil, false, []string{"返回内容"}, ""
		}
		var content any
		if err := json.Unmarshal([]byte(answer), &content); err != nil {
			return nil, false, nil, "JSON 内容格式不正确，请检查后重试。"
		}
		return content, true, nil, ""
	}
	schema := promptMCPElicitationSchema(request.Prompt, nil)
	properties, required, ok := mcpElicitationFlatProperties(schema)
	if !ok {
		return nil, false, nil, "当前 MCP form schema 暂不支持直接转换，请改为填写 JSON 返回内容。"
	}
	requiredSet := map[string]bool{}
	for _, name := range required {
		requiredSet[name] = true
	}
	content := map[string]any{}
	missing := []string{}
	keys := make([]string, 0, len(properties))
	for name := range properties {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	for _, name := range keys {
		property := properties[name]
		answer := strings.TrimSpace(request.DraftAnswers[name])
		if answer == "" {
			if requiredSet[name] {
				missing = append(missing, firstNonEmpty(
					lookupStringFromAny(property["title"]),
					lookupStringFromAny(property["description"]),
					name,
				))
			}
			continue
		}
		value, err := coerceMCPElicitationAnswer(property, answer)
		if err != nil {
			return nil, false, nil, fmt.Sprintf("字段“%s”无效：%v", firstNonEmpty(lookupStringFromAny(property["title"]), name), err)
		}
		content[name] = value
	}
	return content, len(missing) == 0 && len(content) != 0, missing, ""
}

func buildMCPElicitationFlatQuestions(schema map[string]any) []state.RequestPromptQuestionRecord {
	properties, required, ok := mcpElicitationFlatProperties(schema)
	if !ok {
		return nil
	}
	requiredSet := map[string]bool{}
	for _, name := range required {
		requiredSet[name] = true
	}
	keys := make([]string, 0, len(properties))
	for name := range properties {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	questions := make([]state.RequestPromptQuestionRecord, 0, len(keys))
	for _, name := range keys {
		property := properties[name]
		options := mcpElicitationQuestionOptions(property)
		placeholder := strings.TrimSpace(mcpElicitationPlaceholder(property))
		questionText := strings.TrimSpace(lookupStringFromAny(property["description"]))
		if questionText == "" {
			questionText = "请填写该字段"
		}
		if requiredSet[name] {
			questionText = questionText + "（必填）"
		}
		directResponse := len(options) != 0 && len(options) <= 4
		questions = append(questions, state.RequestPromptQuestionRecord{
			ID:             name,
			Header:         firstNonEmpty(lookupStringFromAny(property["title"]), name),
			Question:       questionText,
			AllowOther:     !directResponse,
			Secret:         lookupBoolFromAny(property["secret"]) || strings.EqualFold(strings.TrimSpace(lookupStringFromAny(property["format"])), "password"),
			Options:        options,
			Placeholder:    placeholder,
			DirectResponse: directResponse,
		})
	}
	return questions
}

func mcpElicitationFlatProperties(schema map[string]any) (map[string]map[string]any, []string, bool) {
	if len(schema) == 0 {
		return nil, nil, false
	}
	schemaType := strings.TrimSpace(lookupStringFromAny(schema["type"]))
	if schemaType != "" && schemaType != "object" {
		return nil, nil, false
	}
	rawProperties, ok := schema["properties"].(map[string]any)
	if !ok || len(rawProperties) == 0 {
		return nil, nil, false
	}
	properties := map[string]map[string]any{}
	for name, raw := range rawProperties {
		property, ok := raw.(map[string]any)
		if !ok {
			return nil, nil, false
		}
		if propertyType := strings.TrimSpace(lookupStringFromAny(property["type"])); propertyType == "object" {
			return nil, nil, false
		}
		properties[strings.TrimSpace(name)] = property
	}
	var required []string
	switch typed := schema["required"].(type) {
	case []any:
		for _, item := range typed {
			if name := strings.TrimSpace(lookupStringFromAny(item)); name != "" {
				required = append(required, name)
			}
		}
	case []string:
		for _, item := range typed {
			if name := strings.TrimSpace(item); name != "" {
				required = append(required, name)
			}
		}
	}
	return properties, required, len(properties) != 0
}

func mcpElicitationQuestionOptions(property map[string]any) []state.RequestPromptQuestionOptionRecord {
	rawOptions, ok := property["enum"].([]any)
	if !ok || len(rawOptions) == 0 {
		return nil
	}
	options := make([]state.RequestPromptQuestionOptionRecord, 0, len(rawOptions))
	for _, raw := range rawOptions {
		label := strings.TrimSpace(lookupStringFromAny(raw))
		if label == "" {
			continue
		}
		options = append(options, state.RequestPromptQuestionOptionRecord{Label: label})
	}
	return options
}

func mcpElicitationPlaceholder(property map[string]any) string {
	if placeholder := strings.TrimSpace(lookupStringFromAny(property["placeholder"])); placeholder != "" {
		return placeholder
	}
	switch strings.TrimSpace(lookupStringFromAny(property["type"])) {
	case "boolean":
		return "填写 true / false"
	case "integer":
		return "填写整数"
	case "number":
		return "填写数字"
	case "array":
		return "可填写 JSON 数组，或使用逗号分隔多个值"
	default:
		return ""
	}
}

func coerceMCPElicitationAnswer(property map[string]any, answer string) (any, error) {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return "", nil
	}
	if options := mcpElicitationQuestionOptions(property); len(options) != 0 {
		valid := false
		for _, option := range options {
			if strings.EqualFold(option.Label, answer) {
				answer = option.Label
				valid = true
				break
			}
		}
		if !valid {
			return nil, fmt.Errorf("不在可选项中")
		}
	}
	switch strings.TrimSpace(lookupStringFromAny(property["type"])) {
	case "boolean":
		switch strings.ToLower(answer) {
		case "true", "yes", "y", "1":
			return true, nil
		case "false", "no", "n", "0":
			return false, nil
		default:
			return nil, fmt.Errorf("请填写 true 或 false")
		}
	case "integer":
		value, err := strconv.Atoi(answer)
		if err != nil {
			return nil, fmt.Errorf("请填写整数")
		}
		return value, nil
	case "number":
		value, err := strconv.ParseFloat(answer, 64)
		if err != nil {
			return nil, fmt.Errorf("请填写数字")
		}
		return value, nil
	case "array":
		if strings.HasPrefix(answer, "[") {
			var value any
			if err := json.Unmarshal([]byte(answer), &value); err != nil {
				return nil, fmt.Errorf("JSON 数组格式不正确")
			}
			return value, nil
		}
		parts := strings.Split(answer, ",")
		values := make([]string, 0, len(parts))
		for _, part := range parts {
			if trimmed := strings.TrimSpace(part); trimmed != "" {
				values = append(values, trimmed)
			}
		}
		return values, nil
	case "object":
		var value map[string]any
		if err := json.Unmarshal([]byte(answer), &value); err != nil {
			return nil, fmt.Errorf("JSON 对象格式不正确")
		}
		return value, nil
	default:
		return answer, nil
	}
}

func mcpElicitationMode(prompt *agentproto.RequestPrompt, metadata map[string]any) string {
	mode := strings.TrimSpace(firstNonEmpty(
		promptMCPElicitationMode(prompt),
		metadataString(metadata, "elicitationMode"),
	))
	switch mode {
	case "url":
		return "url"
	default:
		return "form"
	}
}

func promptBody(prompt *agentproto.RequestPrompt) string {
	if prompt == nil {
		return ""
	}
	return strings.TrimSpace(prompt.Body)
}

func promptPermissionsReason(prompt *agentproto.RequestPrompt) string {
	if prompt == nil || prompt.Permissions == nil {
		return ""
	}
	return strings.TrimSpace(prompt.Permissions.Reason)
}

func promptPermissionsList(prompt *agentproto.RequestPrompt, metadata map[string]any) []map[string]any {
	if prompt != nil && prompt.Permissions != nil && len(prompt.Permissions.Permissions) != 0 {
		cloned, _ := cloneJSONValue(prompt.Permissions.Permissions).([]map[string]any)
		if cloned != nil {
			return cloned
		}
		items := make([]map[string]any, 0, len(prompt.Permissions.Permissions))
		for _, item := range prompt.Permissions.Permissions {
			record, _ := cloneJSONValue(item).(map[string]any)
			if record != nil {
				items = append(items, record)
			}
		}
		return items
	}
	if len(metadata) == 0 {
		return nil
	}
	rawItems, _ := metadata["permissions"].([]map[string]any)
	if rawItems != nil {
		return rawItems
	}
	rawAny, _ := metadata["permissions"].([]any)
	items := make([]map[string]any, 0, len(rawAny))
	for _, raw := range rawAny {
		record, _ := raw.(map[string]any)
		if record != nil {
			items = append(items, record)
		}
	}
	return items
}

func promptMCPElicitationMode(prompt *agentproto.RequestPrompt) string {
	if prompt == nil || prompt.MCPElicitation == nil {
		return ""
	}
	return strings.TrimSpace(prompt.MCPElicitation.Mode)
}

func promptMCPElicitationURL(prompt *agentproto.RequestPrompt) string {
	if prompt == nil || prompt.MCPElicitation == nil {
		return ""
	}
	return strings.TrimSpace(prompt.MCPElicitation.URL)
}

func promptMCPElicitationServerName(prompt *agentproto.RequestPrompt) string {
	if prompt == nil || prompt.MCPElicitation == nil {
		return ""
	}
	return strings.TrimSpace(prompt.MCPElicitation.ServerName)
}

func promptMCPElicitationSchema(prompt *agentproto.RequestPrompt, metadata map[string]any) map[string]any {
	if prompt != nil && prompt.MCPElicitation != nil && len(prompt.MCPElicitation.RequestedSchema) != 0 {
		cloned, _ := cloneJSONValue(prompt.MCPElicitation.RequestedSchema).(map[string]any)
		return cloned
	}
	if len(metadata) == 0 {
		return nil
	}
	record, _ := metadata["requestedSchema"].(map[string]any)
	return record
}

func promptMCPElicitationMeta(prompt *agentproto.RequestPrompt, metadata map[string]any) map[string]any {
	if prompt != nil && prompt.MCPElicitation != nil && len(prompt.MCPElicitation.Meta) != 0 {
		cloned, _ := cloneJSONValue(prompt.MCPElicitation.Meta).(map[string]any)
		return cloned
	}
	if len(metadata) == 0 {
		return nil
	}
	record, _ := metadata["meta"].(map[string]any)
	return record
}

func cloneJSONValue(value any) any {
	switch typed := value.(type) {
	case nil:
		return nil
	case map[string]any:
		cloned := make(map[string]any, len(typed))
		for key, item := range typed {
			cloned[key] = cloneJSONValue(item)
		}
		return cloned
	case []any:
		cloned := make([]any, 0, len(typed))
		for _, item := range typed {
			cloned = append(cloned, cloneJSONValue(item))
		}
		return cloned
	case []map[string]any:
		cloned := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			record, _ := cloneJSONValue(item).(map[string]any)
			cloned = append(cloned, record)
		}
		return cloned
	default:
		return typed
	}
}
