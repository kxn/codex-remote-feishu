package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type requestPromptPresentationDefinition struct {
	RequestType  string
	SemanticKind string
	Title        string
	Sections     []state.RequestPromptTextSectionRecord
	Options      []state.RequestPromptOptionRecord
	Questions    []state.RequestPromptQuestionRecord
	HintText     string
}

func buildRequestPromptPresentationDefinition(prompt *agentproto.RequestPrompt, metadata map[string]any) (requestPromptPresentationDefinition, string) {
	requestType := normalizeRequestType(firstNonEmpty(promptRequestType(prompt), metadataString(metadata, "requestType")))
	if requestType == "" {
		requestType = "approval"
	}
	semanticKind := deriveRequestPromptSemanticKind(requestType, prompt, metadata)
	definition := requestPromptPresentationDefinition{
		RequestType:  requestType,
		SemanticKind: semanticKind,
	}
	switch semanticKind {
	case control.RequestSemanticApprovalCommand:
		definition.Title = requestPromptTitle(firstNonEmpty(metadataString(metadata, "title"), promptTitle(prompt)), "需要确认执行命令", "需要处理请求", "需要确认")
		definition.Sections = buildApprovalCommandRequestSections(prompt, metadata)
		definition.Options = buildApprovalRequestOptions(semanticKind, metadata)
		definition.HintText = approvalRequestHintText(semanticKind, definition.Options)
	case control.RequestSemanticApprovalFileChange:
		definition.Title = requestPromptTitle(firstNonEmpty(metadataString(metadata, "title"), promptTitle(prompt)), "需要确认修改文件", "需要处理请求", "需要确认")
		definition.Sections = buildApprovalFileChangeRequestSections(prompt, metadata)
		definition.Options = buildApprovalRequestOptions(semanticKind, metadata)
		definition.HintText = approvalRequestHintText(semanticKind, definition.Options)
	case control.RequestSemanticApprovalNetwork:
		definition.Title = requestPromptTitle(firstNonEmpty(metadataString(metadata, "title"), promptTitle(prompt)), "需要确认网络访问", "需要处理请求", "需要确认")
		definition.Sections = buildApprovalNetworkRequestSections(prompt, metadata)
		definition.Options = buildApprovalRequestOptions(semanticKind, metadata)
		definition.HintText = approvalRequestHintText(semanticKind, definition.Options)
	case control.RequestSemanticRequestUserInput:
		definition.Title = requestPromptTitle(firstNonEmpty(metadataString(metadata, "title"), promptTitle(prompt)), "需要补充输入", "需要处理请求")
		definition.Sections = buildRequestUserInputSections(promptBodyOrMetadata(prompt, metadata), "本地 Codex 正在等待你补充参数或说明。")
		definition.Questions = metadataRequestQuestions(metadata)
		if len(definition.Questions) == 0 {
			return definition, "收到缺少问题定义的 request_user_input 请求，当前无法在飞书端处理。"
		}
	case control.RequestSemanticPermissionsRequestApproval:
		definition.Title = requestPromptTitle(firstNonEmpty(metadataString(metadata, "title"), promptTitle(prompt)), "需要授予权限", "需要处理请求")
		definition.Sections = buildPermissionsRequestSections(prompt, metadata)
		definition.Options = buildPermissionsRequestOptions()
		definition.HintText = "你可以选择仅授权当前这一次，或在当前会话内持续授权。"
	case control.RequestSemanticMCPServerElicitationForm:
		definition.Title = requestPromptTitle(firstNonEmpty(metadataString(metadata, "title"), promptTitle(prompt)), "需要处理 MCP 请求", "需要处理请求")
		definition.Sections = buildMCPElicitationSections(prompt, metadata)
		definition.Questions = buildMCPElicitationQuestions(prompt, metadata)
		definition.Options = buildMCPElicitationOptions(prompt, metadata, definition.Questions)
	case control.RequestSemanticMCPServerElicitationURL:
		definition.Title = requestPromptTitle(firstNonEmpty(metadataString(metadata, "title"), promptTitle(prompt)), "需要处理 MCP 请求", "需要处理请求")
		definition.Sections = buildMCPElicitationSections(prompt, metadata)
		definition.Options = buildMCPElicitationOptions(prompt, metadata, nil)
		definition.HintText = "如果需要先完成外部页面操作，请完成后再点击“继续”；如果不打算继续，可直接拒绝或取消。"
	case control.RequestSemanticToolCallback:
		definition.Title = requestPromptTitle(firstNonEmpty(metadataString(metadata, "title"), promptTitle(prompt)), "工具回调暂不支持", "收到工具回调", "需要处理请求")
		definition.Sections = buildToolCallbackRequestSections(prompt, metadata)
	default:
		definition.Title = requestPromptTitle(firstNonEmpty(metadataString(metadata, "title"), promptTitle(prompt)), "需要确认", "需要处理请求")
		definition.Sections = buildApprovalRequestSections(promptBodyOrMetadata(prompt, metadata), "本地 Codex 正在等待你的确认。")
		definition.Options = buildApprovalRequestOptions(semanticKind, metadata)
		definition.HintText = approvalRequestHintText(semanticKind, definition.Options)
	}
	return definition, ""
}

func deriveRequestPromptSemanticKind(requestType string, prompt *agentproto.RequestPrompt, metadata map[string]any) string {
	rawKind := firstNonEmpty(strings.TrimSpace(metadataString(metadata, "requestKind")), promptRawType(prompt))
	switch control.NormalizeRequestSemanticKind(rawKind, requestType) {
	case control.RequestSemanticApprovalCommand:
		return control.RequestSemanticApprovalCommand
	case control.RequestSemanticApprovalFileChange:
		return control.RequestSemanticApprovalFileChange
	case control.RequestSemanticApprovalNetwork:
		return control.RequestSemanticApprovalNetwork
	case control.RequestSemanticRequestUserInput:
		return control.RequestSemanticRequestUserInput
	case control.RequestSemanticPermissionsRequestApproval:
		return control.RequestSemanticPermissionsRequestApproval
	case control.RequestSemanticMCPServerElicitationURL:
		return control.RequestSemanticMCPServerElicitationURL
	case control.RequestSemanticMCPServerElicitationForm:
		return control.RequestSemanticMCPServerElicitationForm
	case control.RequestSemanticToolCallback:
		return control.RequestSemanticToolCallback
	}
	switch requestType {
	case "approval":
		switch strings.ToLower(strings.TrimSpace(firstNonEmpty(
			rawKind,
			metadataString(metadata, "requestMethod"),
		))) {
		case control.RequestSemanticApprovalCommand, "item/commandexecution/requestapproval":
			if len(requestMetadataMap(metadata["networkApprovalContext"])) != 0 {
				return control.RequestSemanticApprovalNetwork
			}
			return control.RequestSemanticApprovalCommand
		case control.RequestSemanticApprovalFileChange, "item/filechange/requestapproval":
			return control.RequestSemanticApprovalFileChange
		case control.RequestSemanticApprovalNetwork:
			return control.RequestSemanticApprovalNetwork
		}
		if len(requestMetadataMap(metadata["networkApprovalContext"])) != 0 {
			return control.RequestSemanticApprovalNetwork
		}
		if strings.TrimSpace(metadataString(metadata, "grantRoot")) != "" {
			return control.RequestSemanticApprovalFileChange
		}
		return control.RequestSemanticApproval
	case "request_user_input":
		return control.RequestSemanticRequestUserInput
	case "permissions_request_approval":
		return control.RequestSemanticPermissionsRequestApproval
	case "mcp_server_elicitation":
		switch strings.ToLower(strings.TrimSpace(firstNonEmpty(promptMCPElicitationMode(prompt), metadataString(metadata, "elicitationMode")))) {
		case "form":
			return control.RequestSemanticMCPServerElicitationForm
		case "url":
			return control.RequestSemanticMCPServerElicitationURL
		default:
			if len(buildMCPElicitationQuestions(prompt, metadata)) != 0 {
				return control.RequestSemanticMCPServerElicitationForm
			}
			return control.RequestSemanticMCPServerElicitationURL
		}
	case "tool_callback":
		return control.RequestSemanticToolCallback
	default:
		return control.NormalizeRequestSemanticKind("", requestType)
	}
}

func buildApprovalCommandRequestSections(prompt *agentproto.RequestPrompt, metadata map[string]any) []state.RequestPromptTextSectionRecord {
	sections := buildApprovalRequestSections(promptBodyOrMetadata(prompt, metadata), "本地 Codex 正在等待你确认执行命令。")
	if cwd := strings.TrimSpace(metadataString(metadata, "cwd")); cwd != "" {
		sections = appendRequestPromptSection(sections, "工作目录", cwd)
	}
	if permissions := requestPermissionLines(metadataRequestMapList(metadata["additionalPermissions"])); len(permissions) != 0 {
		sections = appendRequestPromptSection(sections, "附加权限", permissions...)
	}
	return sections
}

func buildApprovalFileChangeRequestSections(prompt *agentproto.RequestPrompt, metadata map[string]any) []state.RequestPromptTextSectionRecord {
	sections := buildApprovalRequestSections(promptBodyOrMetadata(prompt, metadata), "本地 Codex 正在等待你确认文件修改。")
	if grantRoot := strings.TrimSpace(metadataString(metadata, "grantRoot")); grantRoot != "" {
		sections = appendRequestPromptSection(sections, "写入范围", grantRoot)
	}
	return sections
}

func buildApprovalNetworkRequestSections(prompt *agentproto.RequestPrompt, metadata map[string]any) []state.RequestPromptTextSectionRecord {
	sections := buildApprovalRequestSections(promptBodyOrMetadata(prompt, metadata), "本地 Codex 正在等待你确认网络访问。")
	network := requestMetadataMap(metadata["networkApprovalContext"])
	lines := make([]string, 0, 3)
	if host := strings.TrimSpace(firstNonEmpty(lookupStringFromAny(network["host"]), lookupStringFromAny(network["hostname"]))); host != "" {
		lines = append(lines, "目标主机："+host)
	}
	if protocol := strings.TrimSpace(lookupStringFromAny(network["protocol"])); protocol != "" {
		lines = append(lines, "协议："+protocol)
	}
	if port := strings.TrimSpace(firstNonEmpty(lookupStringFromAny(network["port"]), lookupStringFromAny(network["destinationPort"]))); port != "" {
		lines = append(lines, "端口："+port)
	}
	if len(lines) != 0 {
		sections = appendRequestPromptSection(sections, "网络目标", lines...)
	}
	return sections
}

func requestPromptSemanticKind(record *state.RequestPromptRecord) string {
	if record == nil {
		return control.RequestSemanticApproval
	}
	return control.NormalizeRequestSemanticKind(strings.TrimSpace(record.SemanticKind), strings.TrimSpace(record.RequestType))
}

func promptRequestType(prompt *agentproto.RequestPrompt) string {
	if prompt == nil {
		return ""
	}
	return string(prompt.Type)
}

func promptRawType(prompt *agentproto.RequestPrompt) string {
	if prompt == nil {
		return ""
	}
	return strings.TrimSpace(prompt.RawType)
}

func promptTitle(prompt *agentproto.RequestPrompt) string {
	if prompt == nil {
		return ""
	}
	return strings.TrimSpace(prompt.Title)
}

func promptBodyOrMetadata(prompt *agentproto.RequestPrompt, metadata map[string]any) string {
	return strings.TrimSpace(firstNonEmpty(promptBody(prompt), metadataString(metadata, "body")))
}

func requestPromptTitle(current, fallback string, genericTitles ...string) string {
	current = strings.TrimSpace(current)
	if current == "" {
		return fallback
	}
	for _, generic := range genericTitles {
		if current == strings.TrimSpace(generic) {
			return fallback
		}
	}
	return current
}

func approvalRequestHintText(semanticKind string, options []state.RequestPromptOptionRecord) string {
	captureFeedback := false
	for _, option := range options {
		if control.NormalizeRequestOptionID(option.OptionID) == "captureFeedback" {
			captureFeedback = true
			break
		}
	}
	switch semanticKind {
	case control.RequestSemanticApprovalCommand:
		if captureFeedback {
			return "如果命令或参数不符合预期，请点击“告诉 Codex 怎么改”；如果只是当前不想执行，可以直接拒绝或取消。"
		}
		return "请确认这条命令是否可以继续执行。"
	case control.RequestSemanticApprovalFileChange:
		if captureFeedback {
			return "如果写入范围或改动方向不符合预期，请点击“告诉 Codex 怎么改”；如果不允许写入，直接拒绝即可。"
		}
		return "请确认这次文件修改是否可以继续。"
	case control.RequestSemanticApprovalNetwork:
		if captureFeedback {
			return "如果联网目标或访问方式不符合预期，请点击“告诉 Codex 怎么改”；如果不允许联网，直接拒绝即可。"
		}
		return "请确认这次网络访问是否可以继续。"
	default:
		if captureFeedback {
			return "如果想拒绝并补充处理意见，请点击“告诉 Codex 怎么改”后再发送下一条文字。"
		}
		return "这个确认只影响当前这一次请求。"
	}
}

func requestPermissionLines(permissions []map[string]any) []string {
	if len(permissions) == 0 {
		return nil
	}
	lines := make([]string, 0, len(permissions))
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
			name = "(unknown)"
		}
		lines = append(lines, "- "+name)
	}
	return lines
}

func metadataRequestMapList(source any) []map[string]any {
	if source == nil {
		return nil
	}
	var rawItems []any
	switch typed := source.(type) {
	case []any:
		rawItems = typed
	case []map[string]any:
		rawItems = make([]any, 0, len(typed))
		for _, item := range typed {
			rawItems = append(rawItems, item)
		}
	default:
		return nil
	}
	items := make([]map[string]any, 0, len(rawItems))
	for _, raw := range rawItems {
		record, _ := raw.(map[string]any)
		if record != nil {
			items = append(items, record)
		}
	}
	return items
}

func requestMetadataMap(source any) map[string]any {
	record, _ := source.(map[string]any)
	if record == nil {
		return nil
	}
	return record
}
