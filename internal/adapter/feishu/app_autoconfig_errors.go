package feishu

import (
	"errors"
	"net/http"
)

func overridePlanFromAPIError(plan AutoConfigPlan, err error, phase autoConfigFailurePhase) AutoConfigPlan {
	updated := plan
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr == nil {
		updated.Status = AutoConfigStatusBlocked
		updated.Summary, updated.BlockingReason = autoConfigGenericFailureSummary(phase)
		return updated
	}
	switch apiErr.Code {
	case 210040, 210020, 210302:
		updated.Status = AutoConfigStatusAwaitingReview
		updated.Summary = "飞书应用正在审核中，暂时无法继续修改或发布。"
		updated.BlockingReason = autoConfigBlockingUnderReview
	case 210043, 210035, 210021, 210015, 210001, 210034, 210014:
		updated.Status = AutoConfigStatusUnsupported
		updated.Summary = "当前飞书应用不能从这里自动修改，请在飞书后台手动维护配置。"
		updated.BlockingReason = autoConfigBlockingUnsupported
	case 210303, 210304:
		updated.Status = AutoConfigStatusBlocked
		updated.Summary = "飞书发布请求参数无效，当前发布未被接受。"
		updated.BlockingReason = autoConfigBlockingInvalidPublish
	default:
		updated.Status = AutoConfigStatusBlocked
		updated.Summary, updated.BlockingReason = autoConfigGenericAPIErrorSummary(apiErr, phase)
	}
	return updated
}

func autoConfigGenericAPIErrorSummary(apiErr *APIError, phase autoConfigFailurePhase) (string, string) {
	if apiErr == nil {
		return autoConfigGenericFailureSummary(phase)
	}
	if apiErr.StatusCode == http.StatusUnauthorized {
		return "当前飞书应用凭证已经失效，请重新连接飞书机器人。", autoConfigBlockingCredentialIssue
	}
	if apiErr.StatusCode == http.StatusForbidden || len(apiErr.PermissionViolations) > 0 {
		return "当前凭证没有修改飞书应用配置的权限，请使用有权限的管理员账号处理。", autoConfigBlockingPermissionIssue
	}
	if _, ok := ExtractPermissionGap(apiErr); ok {
		return "当前凭证缺少修改飞书应用配置所需的权限，请在飞书后台处理。", autoConfigBlockingPermissionIssue
	}
	if phase == autoConfigFailurePublish && apiErr.Code == 99992402 {
		return "飞书发布请求参数无效，当前发布未被接受。", autoConfigBlockingInvalidPublish
	}
	return autoConfigGenericFailureSummary(phase)
}

func autoConfigGenericFailureSummary(phase autoConfigFailurePhase) (string, string) {
	switch phase {
	case autoConfigFailureRead:
		return "暂时无法读取飞书应用配置，请稍后重新检查。", autoConfigBlockingReadFailed
	case autoConfigFailurePublish:
		return "飞书没有接受发布提交，请稍后重试或到飞书后台发布。", autoConfigBlockingPublishFailed
	default:
		return "飞书没有接受自动配置写入，请稍后重试或到飞书后台处理。", autoConfigBlockingWriteFailed
	}
}
