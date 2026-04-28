package control

import "strings"

type RequestBridgeKind string

const (
	RequestBridgeApproval         RequestBridgeKind = "approval"
	RequestBridgeCanUseTool       RequestBridgeKind = "can_use_tool"
	RequestBridgeRequestUserInput RequestBridgeKind = "request_user_input"
	RequestBridgePermissions      RequestBridgeKind = "permissions_request_approval"
	RequestBridgeElicitation      RequestBridgeKind = "elicitation"
	RequestBridgePlanConfirmation RequestBridgeKind = "plan_confirmation"
	RequestBridgeToolCallback     RequestBridgeKind = "tool_callback"
)

type RequestBridgeContract struct {
	Kind               RequestBridgeKind
	InterruptOnDecline bool
}

func ResolveRequestBridgeContract(semanticKind, requestType string) RequestBridgeContract {
	switch NormalizeRequestSemanticKind(semanticKind, requestType) {
	case RequestSemanticApprovalCanUseTool:
		return RequestBridgeContract{Kind: RequestBridgeCanUseTool}
	case RequestSemanticPlanConfirmation:
		return RequestBridgeContract{
			Kind:               RequestBridgePlanConfirmation,
			InterruptOnDecline: true,
		}
	case RequestSemanticRequestUserInput:
		return RequestBridgeContract{Kind: RequestBridgeRequestUserInput}
	case RequestSemanticPermissionsRequestApproval:
		return RequestBridgeContract{Kind: RequestBridgePermissions}
	case RequestSemanticMCPServerElicitation,
		RequestSemanticMCPServerElicitationForm,
		RequestSemanticMCPServerElicitationURL:
		return RequestBridgeContract{Kind: RequestBridgeElicitation}
	case RequestSemanticToolCallback:
		return RequestBridgeContract{Kind: RequestBridgeToolCallback}
	default:
		return RequestBridgeContract{Kind: RequestBridgeApproval}
	}
}

func RequestBridgeShouldInterruptOnDecline(contract RequestBridgeContract, response map[string]any) bool {
	if !contract.InterruptOnDecline || len(response) == 0 {
		return false
	}
	decision := NormalizeRequestOptionID(strings.TrimSpace(lookupRequestBridgeDecision(response)))
	switch decision {
	case "decline", "cancel":
		return true
	default:
		return false
	}
}

func lookupRequestBridgeDecision(response map[string]any) string {
	if len(response) == 0 {
		return ""
	}
	if decision, _ := response["decision"].(string); strings.TrimSpace(decision) != "" {
		return decision
	}
	return ""
}
