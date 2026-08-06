package feishu

import "testing"

func TestRootActionPayloadPageSubmitDefaultsFieldName(t *testing.T) {
	payload := actionPayloadPageSubmit("model_command", "", "")
	if payload[cardActionPayloadKeyKind] != cardActionKindPageSubmit {
		t.Fatalf("unexpected payload kind: %#v", payload)
	}
	if payload[cardActionPayloadKeyFieldName] != cardActionPayloadDefaultCommandFieldName {
		t.Fatalf("expected default command field, got %#v", payload)
	}
	if _, ok := payload[cardActionPayloadKeyActionArgPrefix]; ok {
		t.Fatalf("did not expect empty action arg prefix, got %#v", payload)
	}
}

func TestActionPayloadUpgradeOwnerFlowUsesPickerAndOptionIDs(t *testing.T) {
	payload := actionPayloadUpgradeOwnerFlow("flow-1", "accept")
	if payload[cardActionPayloadKeyKind] != cardActionKindUpgradeOwnerFlow {
		t.Fatalf("unexpected payload kind: %#v", payload)
	}
	if payload[cardActionPayloadKeyPickerID] != "flow-1" || payload[cardActionPayloadKeyOptionID] != "accept" {
		t.Fatalf("unexpected flow payload: %#v", payload)
	}
}

func TestActionPayloadSubmitRequestFormIncludesRequestFields(t *testing.T) {
	payload := actionPayloadSubmitRequestForm("req-1", "request_user_input")
	if payload[cardActionPayloadKeyKind] != cardActionKindSubmitRequestForm {
		t.Fatalf("unexpected payload kind: %#v", payload)
	}
	if payload[cardActionPayloadKeyRequestID] != "req-1" || payload[cardActionPayloadKeyRequestType] != "request_user_input" {
		t.Fatalf("unexpected submit request form payload: %#v", payload)
	}
}

func TestActionPayloadWithLifecycleAddsLifecycleID(t *testing.T) {
	payload := actionPayloadNavigation(cardActionKindShowAllWorkspaces)
	stamped := actionPayloadWithLifecycle(payload, "life-1")
	if stamped[cardActionPayloadKeyDaemonLifecycleID] != "life-1" {
		t.Fatalf("expected lifecycle stamp, got %#v", stamped)
	}
}
