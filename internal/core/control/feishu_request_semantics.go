package control

import "strings"

const (
	RequestSemanticApproval                   = "approval"
	RequestSemanticApprovalCommand            = "approval_command"
	RequestSemanticApprovalFileChange         = "approval_file_change"
	RequestSemanticApprovalNetwork            = "approval_network"
	RequestSemanticRequestUserInput           = "request_user_input"
	RequestSemanticPermissionsRequestApproval = "permissions_request_approval"
	RequestSemanticMCPServerElicitation       = "mcp_server_elicitation"
	RequestSemanticMCPServerElicitationURL    = "mcp_server_elicitation_url"
	RequestSemanticMCPServerElicitationForm   = "mcp_server_elicitation_form"
	RequestSemanticToolCallback               = "tool_callback"
)

func NormalizeRequestSemanticKind(value, requestType string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case RequestSemanticApproval:
		return RequestSemanticApproval
	case RequestSemanticApprovalCommand:
		return RequestSemanticApprovalCommand
	case RequestSemanticApprovalFileChange:
		return RequestSemanticApprovalFileChange
	case RequestSemanticApprovalNetwork:
		return RequestSemanticApprovalNetwork
	case RequestSemanticRequestUserInput, "requestuserinput":
		return RequestSemanticRequestUserInput
	case RequestSemanticPermissionsRequestApproval, "permissionsrequestapproval":
		return RequestSemanticPermissionsRequestApproval
	case RequestSemanticMCPServerElicitation, "mcpserverelicitation":
		return RequestSemanticMCPServerElicitation
	case RequestSemanticMCPServerElicitationURL, "mcpserverelicitationurl":
		return RequestSemanticMCPServerElicitationURL
	case RequestSemanticMCPServerElicitationForm, "mcpserverelicitationform":
		return RequestSemanticMCPServerElicitationForm
	case RequestSemanticToolCallback, "toolcallback":
		return RequestSemanticToolCallback
	}
	switch strings.ToLower(strings.TrimSpace(requestType)) {
	case "", "approval":
		return RequestSemanticApproval
	case RequestSemanticRequestUserInput, "requestuserinput":
		return RequestSemanticRequestUserInput
	case RequestSemanticPermissionsRequestApproval, "permissionsrequestapproval":
		return RequestSemanticPermissionsRequestApproval
	case RequestSemanticMCPServerElicitation, "mcpserverelicitation":
		return RequestSemanticMCPServerElicitation
	case RequestSemanticToolCallback, "toolcallback":
		return RequestSemanticToolCallback
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}
