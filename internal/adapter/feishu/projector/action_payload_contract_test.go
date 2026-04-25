package projector

import "testing"

func TestActionPayloadAttachInstanceUsesCanonicalShape(t *testing.T) {
	payload := actionPayloadAttachInstance("inst-1")
	if payload[cardActionPayloadKeyKind] != cardActionKindAttachInstance {
		t.Fatalf("unexpected attach instance kind: %#v", payload)
	}
	if payload["instance_id"] != "inst-1" {
		t.Fatalf("unexpected attach instance payload: %#v", payload)
	}
}

func TestActionPayloadUseThreadFieldDefaultsSelectionFieldName(t *testing.T) {
	payload := actionPayloadUseThreadField("", true)
	if payload[cardActionPayloadKeyKind] != cardActionKindUseThread {
		t.Fatalf("unexpected use-thread kind: %#v", payload)
	}
	if payload[cardActionPayloadKeyFieldName] != cardSelectionThreadFieldName {
		t.Fatalf("expected default selection field name, got %#v", payload)
	}
	if payload[cardActionPayloadKeyAllowCrossWorkspace] != true {
		t.Fatalf("expected allow_cross_workspace to stay true, got %#v", payload)
	}
}

func TestActionPayloadRequestControlOmitsEmptyOptionalFields(t *testing.T) {
	payload := actionPayloadRequestControl("req-1", "request_user_input", "cancel_turn", "", 0)
	if payload[cardActionPayloadKeyKind] != cardActionKindRequestControl {
		t.Fatalf("unexpected request control kind: %#v", payload)
	}
	if _, ok := payload[cardActionPayloadKeyQuestionID]; ok {
		t.Fatalf("did not expect empty question id to be serialized, got %#v", payload)
	}
	if _, ok := payload[cardActionPayloadKeyRequestRevision]; ok {
		t.Fatalf("did not expect zero request revision to be serialized, got %#v", payload)
	}
}

func TestActionPayloadTargetPickerValueOmitsEmptyTargetValue(t *testing.T) {
	payload := actionPayloadTargetPickerValue(cardActionKindTargetPickerSelectSource, "picker-1", "")
	if payload[cardActionPayloadKeyKind] != cardActionKindTargetPickerSelectSource || payload[cardActionPayloadKeyPickerID] != "picker-1" {
		t.Fatalf("unexpected target picker payload: %#v", payload)
	}
	if _, ok := payload[cardActionPayloadKeyTargetValue]; ok {
		t.Fatalf("did not expect empty target value to be serialized, got %#v", payload)
	}
}

func TestActionPayloadTargetPickerCursorUsesCanonicalShape(t *testing.T) {
	payload := actionPayloadTargetPickerCursor("picker-1", cardTargetPickerSessionFieldName, 0)
	if payload[cardActionPayloadKeyKind] != cardActionKindTargetPickerPage || payload[cardActionPayloadKeyPickerID] != "picker-1" {
		t.Fatalf("unexpected target picker page payload: %#v", payload)
	}
	if payload[cardActionPayloadKeyFieldName] != cardTargetPickerSessionFieldName {
		t.Fatalf("expected target picker field name, got %#v", payload)
	}
	if _, ok := payload[cardActionPayloadKeyCursor]; ok {
		t.Fatalf("did not expect zero cursor to be serialized, got %#v", payload)
	}

	next := actionPayloadTargetPickerCursor("picker-1", cardTargetPickerWorkspaceFieldName, 7)
	if next[cardActionPayloadKeyCursor] != 7 {
		t.Fatalf("expected positive cursor to be serialized, got %#v", next)
	}
}

func TestActionPayloadPathPickerCursorUsesCanonicalShape(t *testing.T) {
	payload := actionPayloadPathPickerCursor("picker-1", cardPathPickerFileSelectFieldName, 0)
	if payload[cardActionPayloadKeyKind] != cardActionKindPathPickerPage || payload[cardActionPayloadKeyPickerID] != "picker-1" {
		t.Fatalf("unexpected path picker page payload: %#v", payload)
	}
	if payload[cardActionPayloadKeyFieldName] != cardPathPickerFileSelectFieldName {
		t.Fatalf("expected path picker field name, got %#v", payload)
	}
	if _, ok := payload[cardActionPayloadKeyCursor]; ok {
		t.Fatalf("did not expect zero cursor to be serialized, got %#v", payload)
	}

	next := actionPayloadPathPickerCursor("picker-1", cardPathPickerDirectorySelectFieldName, 5)
	if next[cardActionPayloadKeyCursor] != 5 {
		t.Fatalf("expected positive cursor to be serialized, got %#v", next)
	}
}

func TestActionPayloadWithLifecycleAddsLifecycleID(t *testing.T) {
	payload := actionPayloadNavigation(cardActionKindShowAllWorkspaces)
	stamped := actionPayloadWithLifecycle(payload, "life-1")
	if stamped[cardActionPayloadKeyDaemonLifecycleID] != "life-1" {
		t.Fatalf("expected lifecycle stamp, got %#v", stamped)
	}
}
