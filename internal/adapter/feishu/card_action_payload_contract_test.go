package feishu

import (
	"testing"

	frontstagecontract "github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
)

func TestRootActionPayloadPageSubmitDefaultsFieldName(t *testing.T) {
	payload := actionPayloadPageSubmit("model_command", "", "")
	if payload[frontstagecontract.CardActionPayloadKeyKind] != cardActionKindPageSubmit {
		t.Fatalf("unexpected payload kind: %#v", payload)
	}
	if payload[frontstagecontract.CardActionPayloadKeyFieldName] != cardActionPayloadDefaultCommandFieldName {
		t.Fatalf("expected default command field, got %#v", payload)
	}
	if _, ok := payload[frontstagecontract.CardActionPayloadKeyActionArgPrefix]; ok {
		t.Fatalf("did not expect empty action arg prefix, got %#v", payload)
	}
}

func TestActionPayloadUpgradeOwnerFlowUsesPickerAndOptionIDs(t *testing.T) {
	payload := actionPayloadUpgradeOwnerFlow("flow-1", "accept")
	if payload[frontstagecontract.CardActionPayloadKeyKind] != cardActionKindUpgradeOwnerFlow {
		t.Fatalf("unexpected payload kind: %#v", payload)
	}
	if payload[frontstagecontract.CardActionPayloadKeyPickerID] != "flow-1" || payload[frontstagecontract.CardActionPayloadKeyOptionID] != "accept" {
		t.Fatalf("unexpected flow payload: %#v", payload)
	}
}

func TestActionPayloadSubmitRequestFormIncludesRequestFields(t *testing.T) {
	payload := actionPayloadSubmitRequestForm("req-1", "request_user_input")
	if payload[frontstagecontract.CardActionPayloadKeyKind] != cardActionKindSubmitRequestForm {
		t.Fatalf("unexpected payload kind: %#v", payload)
	}
	if payload[frontstagecontract.CardActionPayloadKeyRequestID] != "req-1" || payload[frontstagecontract.CardActionPayloadKeyRequestType] != "request_user_input" {
		t.Fatalf("unexpected submit request form payload: %#v", payload)
	}
}

func TestActionPayloadWithLifecycleAddsLifecycleID(t *testing.T) {
	payload := actionPayloadNavigation(cardActionKindShowAllWorkspaces)
	stamped := actionPayloadWithLifecycle(payload, "life-1")
	if stamped[frontstagecontract.CardActionPayloadKeyDaemonLifecycleID] != "life-1" {
		t.Fatalf("expected lifecycle stamp, got %#v", stamped)
	}
}
