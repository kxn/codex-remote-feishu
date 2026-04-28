package control

import "testing"

func TestResolveRequestBridgeContract(t *testing.T) {
	tests := []struct {
		name          string
		semanticKind  string
		requestType   string
		wantKind      RequestBridgeKind
		wantInterrupt bool
	}{
		{name: "default approval", semanticKind: RequestSemanticApprovalCommand, requestType: "approval", wantKind: RequestBridgeApproval},
		{name: "claude can use tool", semanticKind: RequestSemanticApprovalCanUseTool, requestType: "approval", wantKind: RequestBridgeCanUseTool},
		{name: "elicitation", semanticKind: RequestSemanticMCPServerElicitationForm, requestType: "mcp_server_elicitation", wantKind: RequestBridgeElicitation},
		{name: "plan confirmation", semanticKind: RequestSemanticPlanConfirmation, requestType: "approval", wantKind: RequestBridgePlanConfirmation, wantInterrupt: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			contract := ResolveRequestBridgeContract(tt.semanticKind, tt.requestType)
			if contract.Kind != tt.wantKind || contract.InterruptOnDecline != tt.wantInterrupt {
				t.Fatalf("ResolveRequestBridgeContract(%q, %q) = %#v", tt.semanticKind, tt.requestType, contract)
			}
		})
	}
}

func TestRequestBridgeShouldInterruptOnDecline(t *testing.T) {
	contract := ResolveRequestBridgeContract(RequestSemanticPlanConfirmation, "approval")
	if !RequestBridgeShouldInterruptOnDecline(contract, map[string]any{"decision": "decline"}) {
		t.Fatal("expected plan decline to request interrupt")
	}
	if RequestBridgeShouldInterruptOnDecline(contract, map[string]any{"decision": "accept"}) {
		t.Fatal("did not expect accept to request interrupt")
	}
}
