package orchestrator

import (
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func requestPromptPendingDispatchStatusText(request *state.RequestPromptRecord) string {
	if request == nil {
		return "已提交当前请求，等待本地后端继续处理。"
	}
	if normalizeRequestLifecycleState(request.LifecycleState) == requestLifecycleSubmitting {
		return requestPromptSubmittingStatusText(request)
	}
	return requestPromptAwaitingBackendConsumeStatusText(request)
}

func requestPromptSubmittingStatusText(request *state.RequestPromptRecord) string {
	backendName := control.RequestLocalBackendDisplayName(requestPromptBackend(request))
	switch requestPromptSemanticKind(request) {
	case control.RequestSemanticApprovalCommand, control.RequestSemanticApprovalFileChange, control.RequestSemanticApprovalNetwork, control.RequestSemanticApproval:
		return "正在提交当前确认，等待" + backendName + " 接收。"
	case control.RequestSemanticPermissionsRequestApproval:
		return "正在提交授权决定，等待" + backendName + " 接收。"
	case control.RequestSemanticMCPServerElicitationForm:
		return "正在提交当前表单，等待" + backendName + " 接收。"
	case control.RequestSemanticToolCallback:
		return "当前客户端不支持执行该工具回调，正在自动上报 unsupported 结果，等待" + backendName + " 接收。"
	case control.RequestSemanticMCPServerElicitationURL, control.RequestSemanticMCPServerElicitation:
		return "正在提交当前请求，等待" + backendName + " 接收。"
	default:
		return "正在提交当前答案，等待" + backendName + " 接收。"
	}
}

func requestPromptAwaitingBackendConsumeStatusText(request *state.RequestPromptRecord) string {
	waitingText := requestWaitingContinueText(requestPromptBackend(request))
	switch requestPromptSemanticKind(request) {
	case control.RequestSemanticApprovalCommand, control.RequestSemanticApprovalFileChange, control.RequestSemanticApprovalNetwork, control.RequestSemanticApproval:
		return "已提交当前确认，" + waitingText + "。"
	case control.RequestSemanticPermissionsRequestApproval:
		return "已提交授权决定，" + waitingText + "。"
	case control.RequestSemanticMCPServerElicitationForm:
		return "已提交当前表单，" + waitingText + "。"
	case control.RequestSemanticToolCallback:
		return "当前客户端不支持执行该工具回调，已自动上报 unsupported 结果，" + waitingText + "。"
	case control.RequestSemanticMCPServerElicitationURL, control.RequestSemanticMCPServerElicitation:
		return "已提交当前请求，" + waitingText + "。"
	default:
		return "已提交当前答案，" + waitingText + "。"
	}
}
