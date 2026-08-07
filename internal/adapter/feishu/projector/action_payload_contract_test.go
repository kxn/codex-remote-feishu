package projector

import (
	"testing"

	frontstagecontract "github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
)

func TestActionPayloadAttachInstanceUsesCanonicalShape(t *testing.T) {
	payload := actionPayloadAttachInstance("inst-1")
	if payload[frontstagecontract.CardActionPayloadKeyKind] != cardActionKindAttachInstance {
		t.Fatalf("unexpected attach instance kind: %#v", payload)
	}
	if payload["instance_id"] != "inst-1" {
		t.Fatalf("unexpected attach instance payload: %#v", payload)
	}
}

func TestActionPayloadUseThreadFieldDefaultsSelectionFieldName(t *testing.T) {
	payload := actionPayloadUseThreadField("", true)
	if payload[frontstagecontract.CardActionPayloadKeyKind] != cardActionKindUseThread {
		t.Fatalf("unexpected use-thread kind: %#v", payload)
	}
	if payload[frontstagecontract.CardActionPayloadKeyFieldName] != cardSelectionThreadFieldName {
		t.Fatalf("expected default selection field name, got %#v", payload)
	}
	if payload[frontstagecontract.CardActionPayloadKeyAllowCrossWorkspace] != true {
		t.Fatalf("expected allow_cross_workspace to stay true, got %#v", payload)
	}
}

func TestActionPayloadThreadSelectionCursorUsesCanonicalShape(t *testing.T) {
	payload := actionPayloadThreadSelectionCursor("vscode_all", 0)
	if payload[frontstagecontract.CardActionPayloadKeyKind] != cardActionKindThreadSelectionPage {
		t.Fatalf("unexpected thread-selection page kind: %#v", payload)
	}
	if payload[frontstagecontract.CardActionPayloadKeyViewMode] != "vscode_all" {
		t.Fatalf("expected view mode to be serialized, got %#v", payload)
	}
	if _, ok := payload[frontstagecontract.CardActionPayloadKeyCursor]; ok {
		t.Fatalf("did not expect zero cursor to be serialized, got %#v", payload)
	}

	next := actionPayloadThreadSelectionCursor("vscode_scoped_all", 7)
	if next[frontstagecontract.CardActionPayloadKeyCursor] != 7 {
		t.Fatalf("expected positive cursor to be serialized, got %#v", next)
	}
}

func TestActionPayloadRequestControlOmitsEmptyOptionalFields(t *testing.T) {
	payload := actionPayloadRequestControl("req-1", "request_user_input", "cancel_turn", "", 0)
	if payload[frontstagecontract.CardActionPayloadKeyKind] != cardActionKindRequestControl {
		t.Fatalf("unexpected request control kind: %#v", payload)
	}
	if _, ok := payload[frontstagecontract.CardActionPayloadKeyQuestionID]; ok {
		t.Fatalf("did not expect empty question id to be serialized, got %#v", payload)
	}
	if _, ok := payload[frontstagecontract.CardActionPayloadKeyRequestRevision]; ok {
		t.Fatalf("did not expect zero request revision to be serialized, got %#v", payload)
	}
}

func TestActionPayloadTargetPickerValueOmitsEmptyTargetValue(t *testing.T) {
	payload := actionPayloadTargetPickerValue(cardActionKindTargetPickerOpenPathPicker, "picker-1", "")
	if payload[frontstagecontract.CardActionPayloadKeyKind] != cardActionKindTargetPickerOpenPathPicker || payload[frontstagecontract.CardActionPayloadKeyPickerID] != "picker-1" {
		t.Fatalf("unexpected target picker payload: %#v", payload)
	}
	if _, ok := payload[frontstagecontract.CardActionPayloadKeyTargetValue]; ok {
		t.Fatalf("did not expect empty target value to be serialized, got %#v", payload)
	}
}

func TestActionPayloadTargetPickerCursorUsesCanonicalShape(t *testing.T) {
	payload := actionPayloadTargetPickerCursor("picker-1", cardTargetPickerSessionFieldName, 0)
	if payload[frontstagecontract.CardActionPayloadKeyKind] != cardActionKindTargetPickerPage || payload[frontstagecontract.CardActionPayloadKeyPickerID] != "picker-1" {
		t.Fatalf("unexpected target picker page payload: %#v", payload)
	}
	if payload[frontstagecontract.CardActionPayloadKeyFieldName] != cardTargetPickerSessionFieldName {
		t.Fatalf("expected target picker field name, got %#v", payload)
	}
	if _, ok := payload[frontstagecontract.CardActionPayloadKeyCursor]; ok {
		t.Fatalf("did not expect zero cursor to be serialized, got %#v", payload)
	}

	next := actionPayloadTargetPickerCursor("picker-1", cardTargetPickerWorkspaceFieldName, 7)
	if next[frontstagecontract.CardActionPayloadKeyCursor] != 7 {
		t.Fatalf("expected positive cursor to be serialized, got %#v", next)
	}
}

func TestActionPayloadPathPickerCursorUsesCanonicalShape(t *testing.T) {
	payload := actionPayloadPathPickerCursor("picker-1", cardPathPickerFileSelectFieldName, 0)
	if payload[frontstagecontract.CardActionPayloadKeyKind] != cardActionKindPathPickerPage || payload[frontstagecontract.CardActionPayloadKeyPickerID] != "picker-1" {
		t.Fatalf("unexpected path picker page payload: %#v", payload)
	}
	if payload[frontstagecontract.CardActionPayloadKeyFieldName] != cardPathPickerFileSelectFieldName {
		t.Fatalf("expected path picker field name, got %#v", payload)
	}
	if _, ok := payload[frontstagecontract.CardActionPayloadKeyCursor]; ok {
		t.Fatalf("did not expect zero cursor to be serialized, got %#v", payload)
	}

	next := actionPayloadPathPickerCursor("picker-1", cardPathPickerDirectorySelectFieldName, 5)
	if next[frontstagecontract.CardActionPayloadKeyCursor] != 5 {
		t.Fatalf("expected positive cursor to be serialized, got %#v", next)
	}
}

func TestActionPayloadWithLifecycleAddsLifecycleID(t *testing.T) {
	payload := actionPayloadNavigation(cardActionKindShowAllWorkspaces)
	stamped := actionPayloadWithLifecycle(payload, "life-1")
	if stamped[frontstagecontract.CardActionPayloadKeyDaemonLifecycleID] != "life-1" {
		t.Fatalf("expected lifecycle stamp, got %#v", stamped)
	}
}
