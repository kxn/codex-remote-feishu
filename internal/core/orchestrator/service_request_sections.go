package orchestrator

import (
	"encoding/json"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func buildApprovalRequestSections(body string) []state.RequestPromptTextSectionRecord {
	body = strings.TrimSpace(body)
	if body == "" {
		body = "本地 Codex 正在等待你的确认。"
	}
	return appendRequestPromptSection(nil, "", body)
}

func buildRequestUserInputSections(body string) []state.RequestPromptTextSectionRecord {
	body = strings.TrimSpace(body)
	if body == "" {
		body = "本地 Codex 正在等待你补充参数或说明。"
	}
	return appendRequestPromptSection(nil, "", body)
}

func buildGenericRequestSections(body string) []state.RequestPromptTextSectionRecord {
	body = strings.TrimSpace(body)
	if body == "" {
		body = "本地 Codex 正在等待处理新的交互请求。"
	}
	return appendRequestPromptSection(nil, "", body)
}

func buildToolCallbackRequestSections(prompt *agentproto.RequestPrompt, metadata map[string]any) []state.RequestPromptTextSectionRecord {
	sections := appendRequestPromptSection(nil, "",
		"当前工具请求客户端执行一段 dynamic tool callback。",
		"此 relay/headless 客户端暂不支持直接执行，系统已自动回报 unsupported 结果。",
	)

	infoLines := make([]string, 0, 2)
	if toolName := requestToolCallbackToolName(prompt, metadata); toolName != "" {
		infoLines = append(infoLines, "工具："+toolName)
	}
	if callID := requestToolCallbackCallID(prompt, metadata); callID != "" {
		infoLines = append(infoLines, "Call ID："+callID)
	}
	sections = appendRequestPromptSection(sections, "回调信息", infoLines...)

	if arguments := requestToolCallbackArgumentsPreview(prompt, metadata); arguments != "" {
		sections = appendRequestPromptSection(sections, "回调参数", arguments)
	}
	return sections
}

func appendRequestPromptSection(sections []state.RequestPromptTextSectionRecord, label string, lines ...string) []state.RequestPromptTextSectionRecord {
	section := state.RequestPromptTextSectionRecord{
		Label: strings.TrimSpace(label),
		Lines: append([]string(nil), lines...),
	}.Normalized()
	if section.Label == "" && len(section.Lines) == 0 {
		return sections
	}
	return append(sections, section)
}

func requestPromptSectionsToControl(sections []state.RequestPromptTextSectionRecord) []control.FeishuCardTextSection {
	if len(sections) == 0 {
		return nil
	}
	out := make([]control.FeishuCardTextSection, 0, len(sections))
	for _, section := range sections {
		normalized := section.Normalized()
		if normalized.Label == "" && len(normalized.Lines) == 0 {
			continue
		}
		out = append(out, control.FeishuCardTextSection{
			Label: normalized.Label,
			Lines: append([]string(nil), normalized.Lines...),
		})
	}
	return out
}

func requestToolCallbackToolName(prompt *agentproto.RequestPrompt, metadata map[string]any) string {
	if prompt != nil && prompt.ToolCallback != nil {
		if value := strings.TrimSpace(prompt.ToolCallback.ToolName); value != "" {
			return value
		}
	}
	return strings.TrimSpace(metadataString(metadata, "tool"))
}

func requestToolCallbackCallID(prompt *agentproto.RequestPrompt, metadata map[string]any) string {
	if prompt != nil && prompt.ToolCallback != nil {
		if value := strings.TrimSpace(prompt.ToolCallback.CallID); value != "" {
			return value
		}
	}
	return strings.TrimSpace(metadataString(metadata, "callId"))
}

func requestToolCallbackArgumentsPreview(prompt *agentproto.RequestPrompt, metadata map[string]any) string {
	var raw any
	if prompt != nil && prompt.ToolCallback != nil {
		raw = prompt.ToolCallback.Arguments
	}
	if raw == nil && len(metadata) != 0 {
		raw = metadata["arguments"]
	}
	if raw == nil {
		return ""
	}
	bytes, err := json.Marshal(raw)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(bytes))
}
