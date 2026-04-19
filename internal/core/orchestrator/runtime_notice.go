package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func globalRuntimeNotice(family control.NoticeDeliveryFamily, code, title, text string) control.Notice {
	return control.Notice{
		Code:             strings.TrimSpace(code),
		Title:            strings.TrimSpace(title),
		Text:             strings.TrimSpace(text),
		DeliveryClass:    control.NoticeDeliveryClassGlobalRuntime,
		DeliveryFamily:   family,
		DeliveryDedupKey: strings.TrimSpace(code),
	}
}

func GlobalRuntimeShutdownNotice(text string) control.Notice {
	return globalRuntimeNotice(control.NoticeDeliveryFamilyDaemonShutdown, "daemon_shutting_down", "", text)
}

func GlobalRuntimeGatewayApplyFailureNotice(problem agentproto.ErrorInfo) control.Notice {
	return globalRuntimeNoticeFromProblem(control.NoticeDeliveryFamilyGatewayApplyFailure, problem)
}

func globalRuntimeNoticeFromProblem(family control.NoticeDeliveryFamily, problem agentproto.ErrorInfo) control.Notice {
	notice := NoticeForProblem(problem)
	notice.DeliveryClass = control.NoticeDeliveryClassGlobalRuntime
	notice.DeliveryFamily = family
	if notice.DeliveryDedupKey == "" {
		notice.DeliveryDedupKey = strings.TrimSpace(problem.Code)
	}
	return notice
}
