package orchestrator

import (
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func transformMCPForm(form RequestFormState, record *state.RequestPromptRecord, option string, answers map[string][]string) (map[string]any, bool, string) {
	meta := promptMCPElicitationMeta(record.Prompt, form.Metadata)
	if record.SemanticKind == control.RequestSemanticMCPServerElicitationApproval {
		if !requestHasOption(record, option) {
			return nil, false, "MCP 工具授权选项无效或当前不可用。"
		}
		switch option {
		case "accept":
			return buildMCPElicitationPayload("accept", nil, mcpElicitationApprovalResponseMeta(meta, "")), true, ""
		case "acceptForSession":
			if !mcpElicitationAdvertisesPersist(meta, mcpToolApprovalPersistSession) {
				return nil, false, "上游未声明支持本会话授权。"
			}
			return buildMCPElicitationPayload("accept", nil, mcpElicitationApprovalResponseMeta(meta, mcpToolApprovalPersistSession)), true, ""
		case "decline", "cancel":
			return buildMCPElicitationPayload(option, nil, mcpElicitationApprovalResponseMeta(meta, "")), true, ""
		default:
			return nil, false, "MCP 工具授权选项无效。"
		}
	}
	if option == "decline" || option == "cancel" {
		return buildMCPElicitationPayload(option, nil, meta), true, ""
	}
	if option != "" && option != "accept" {
		return nil, false, "MCP 表单选项无效。"
	}
	if len(record.Questions) == 0 {
		return buildMCPElicitationPayload("accept", nil, meta), true, ""
	}
	content, complete, _, invalid := buildMCPElicitationContent(record, answers)
	if invalid != "" {
		return nil, false, invalid
	}
	if !complete {
		return nil, false, ""
	}
	return buildMCPElicitationPayload("accept", content, meta), true, ""
}
