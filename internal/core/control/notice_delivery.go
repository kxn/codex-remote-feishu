package control

import "strings"

type NoticeDeliveryClass string

const (
	NoticeDeliveryClassDefault       NoticeDeliveryClass = ""
	NoticeDeliveryClassGlobalRuntime NoticeDeliveryClass = "global_runtime"
)

type NoticeDeliveryFamily string

const (
	NoticeDeliveryFamilySurfaceResume       NoticeDeliveryFamily = "surface_resume"
	NoticeDeliveryFamilyVSCodeResume        NoticeDeliveryFamily = "vscode_resume"
	NoticeDeliveryFamilyVSCodeOpenPrompt    NoticeDeliveryFamily = "vscode_open_prompt"
	NoticeDeliveryFamilyTransportDegraded   NoticeDeliveryFamily = "transport_degraded"
	NoticeDeliveryFamilyDaemonShutdown      NoticeDeliveryFamily = "daemon_shutdown"
	NoticeDeliveryFamilyGatewayApplyFailure NoticeDeliveryFamily = "gateway_apply_failure"
)

func (n Notice) IsGlobalRuntime() bool {
	return n.DeliveryClass == NoticeDeliveryClassGlobalRuntime
}

func (n Notice) DeliveryDedupIdentity() string {
	if key := strings.TrimSpace(n.DeliveryDedupKey); key != "" {
		return key
	}
	if code := strings.TrimSpace(n.Code); code != "" {
		return code
	}
	return strings.TrimSpace(n.Title) + "\n" + strings.TrimSpace(n.Text)
}
