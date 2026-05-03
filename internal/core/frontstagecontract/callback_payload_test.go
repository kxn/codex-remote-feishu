package frontstagecontract

import "testing"

func TestActionPayloadKindTrimsCanonicalKind(t *testing.T) {
	value := map[string]any{
		CardActionPayloadKeyKind: "  " + CardActionKindShowAllThreads + "  ",
	}
	if got := ActionPayloadKind(value); got != CardActionKindShowAllThreads {
		t.Fatalf("ActionPayloadKind() = %q, want %q", got, CardActionKindShowAllThreads)
	}
}

func TestActionPayloadAttachInstanceUsesCanonicalKeys(t *testing.T) {
	payload := ActionPayloadAttachInstance("inst-1")
	if payload[CardActionPayloadKeyKind] != CardActionKindAttachInstance {
		t.Fatalf("unexpected attach instance kind: %#v", payload)
	}
	if payload[CardActionPayloadKeyInstanceID] != "inst-1" {
		t.Fatalf("unexpected attach instance payload: %#v", payload)
	}
}

func TestActionPayloadPageSubmitDefaultsFieldName(t *testing.T) {
	payload := ActionPayloadPageSubmit("model_command", "", "")
	if payload[CardActionPayloadKeyKind] != CardActionKindPageSubmit {
		t.Fatalf("unexpected payload kind: %#v", payload)
	}
	if payload[CardActionPayloadKeyFieldName] != CardActionPayloadDefaultCommandFieldName {
		t.Fatalf("expected default command field, got %#v", payload)
	}
	if _, ok := payload[CardActionPayloadKeyActionArgPrefix]; ok {
		t.Fatalf("did not expect empty action arg prefix, got %#v", payload)
	}
}

func TestActionPayloadPageLocalSubmitDefaultsFieldName(t *testing.T) {
	payload := ActionPayloadPageLocalSubmit("show_menu", "", "")
	if payload[CardActionPayloadKeyKind] != CardActionKindPageLocalSubmit {
		t.Fatalf("unexpected payload kind: %#v", payload)
	}
	if payload[CardActionPayloadKeyFieldName] != CardActionPayloadDefaultCommandFieldName {
		t.Fatalf("expected default command field, got %#v", payload)
	}
	if _, ok := payload[CardActionPayloadKeyActionArgPrefix]; ok {
		t.Fatalf("did not expect empty action arg prefix, got %#v", payload)
	}
}

func TestActionPayloadPageLocalActionUsesCanonicalKind(t *testing.T) {
	payload := ActionPayloadPageLocalAction("surface.command.menu", "send_settings")
	if payload[CardActionPayloadKeyKind] != CardActionKindPageLocalAction {
		t.Fatalf("unexpected payload kind: %#v", payload)
	}
	if payload[CardActionPayloadKeyActionKind] != "surface.command.menu" {
		t.Fatalf("unexpected action kind: %#v", payload)
	}
	if payload[CardActionPayloadKeyActionArg] != "send_settings" {
		t.Fatalf("unexpected action arg: %#v", payload)
	}
}

func TestActionPayloadRequestControlOmitsEmptyOptionalFields(t *testing.T) {
	payload := ActionPayloadRequestControl("req-1", "request_user_input", RequestControlCancelTurn, "", 0)
	if payload[CardActionPayloadKeyKind] != CardActionKindRequestControl {
		t.Fatalf("unexpected request control kind: %#v", payload)
	}
	if _, ok := payload[CardActionPayloadKeyQuestionID]; ok {
		t.Fatalf("did not expect empty question id to be serialized, got %#v", payload)
	}
	if _, ok := payload[CardActionPayloadKeyRequestRevision]; ok {
		t.Fatalf("did not expect zero request revision to be serialized, got %#v", payload)
	}
}

func TestActionPayloadWithCatalogAddsStructuredProvenance(t *testing.T) {
	payload := ActionPayloadWithCatalog(ActionPayloadPageAction("surface.command.model", ""), "model", "model.default", "claude")
	if payload[CardActionPayloadKeyCatalogFamilyID] != "model" {
		t.Fatalf("unexpected family payload: %#v", payload)
	}
	if payload[CardActionPayloadKeyCatalogVariantID] != "model.default" {
		t.Fatalf("unexpected variant payload: %#v", payload)
	}
	if payload[CardActionPayloadKeyCatalogBackend] != "claude" {
		t.Fatalf("unexpected backend payload: %#v", payload)
	}
}

func TestActionPayloadWithLifecycleAddsLifecycleID(t *testing.T) {
	payload := ActionPayloadNavigation(CardActionKindShowAllWorkspaces)
	stamped := ActionPayloadWithLifecycle(payload, "life-1")
	if stamped[CardActionPayloadKeyDaemonLifecycleID] != "life-1" {
		t.Fatalf("expected lifecycle stamp, got %#v", stamped)
	}
}
