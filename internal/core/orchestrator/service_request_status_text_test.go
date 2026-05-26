package orchestrator

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestPendingRequestNoticeTextForCanUseTool(t *testing.T) {
	request := &state.RequestPromptRecord{
		RequestType:     string(agentproto.RequestTypeApproval),
		SemanticKind:    control.RequestSemanticApprovalCanUseTool,
		Backend:         agentproto.BackendClaude,
		VisibilityState: requestVisibilityVisible,
	}

	got := pendingRequestNoticeText(request)
	want := "当前有待确认工具调用请求。请先处理这张确认卡片后再继续。"
	if got != want {
		t.Fatalf("pendingRequestNoticeText() = %q, want %q", got, want)
	}
}

func TestRequestPromptPendingDispatchStatusTextForCanUseTool(t *testing.T) {
	request := &state.RequestPromptRecord{
		RequestType:  string(agentproto.RequestTypeApproval),
		SemanticKind: control.RequestSemanticApprovalCanUseTool,
		Backend:      agentproto.BackendClaude,
	}

	request.LifecycleState = requestLifecycleSubmitting
	if got, want := requestPromptPendingDispatchStatusText(request), "正在提交当前工具调用确认，等待本地 Claude 接收。"; got != want {
		t.Fatalf("submitting status = %q, want %q", got, want)
	}

	request.LifecycleState = requestLifecycleAwaitingBackendConsume
	if got, want := requestPromptPendingDispatchStatusText(request), "已提交当前工具调用确认，等待 Claude 继续。"; got != want {
		t.Fatalf("awaiting status = %q, want %q", got, want)
	}
}
