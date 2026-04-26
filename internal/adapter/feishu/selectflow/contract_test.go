package selectflow

import (
	"testing"

	larkcallback "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func TestRecoverCallbackValuePrefersExplicitPayloadValue(t *testing.T) {
	action := &larkcallback.CallBackAction{
		Option: "thread-from-option",
		FormValue: map[string]interface{}{
			"selection_thread": []interface{}{"thread-from-form"},
		},
	}
	got := RecoverCallbackValue(map[string]any{
		"thread_id": "thread-from-payload",
	}, action, "selection_thread", "thread_id")
	if got != "thread-from-payload" {
		t.Fatalf("RecoverCallbackValue() = %q, want payload value", got)
	}
}

func TestRecoverCallbackValueUsesSelectedOptionBeforeFormValue(t *testing.T) {
	action := &larkcallback.CallBackAction{
		Option: "thread-from-option",
		FormValue: map[string]interface{}{
			"selection_thread": []interface{}{"thread-from-form"},
		},
	}
	got := RecoverCallbackValue(nil, action, "selection_thread", "")
	if got != "thread-from-option" {
		t.Fatalf("RecoverCallbackValue() = %q, want option value", got)
	}
}

func TestPaginatedSelectFlowDefinitionUsesPayloadFieldOverride(t *testing.T) {
	def := PaginatedSelectFlowDefinition{FieldName: "selection_thread"}
	action := &larkcallback.CallBackAction{
		FormValue: map[string]interface{}{
			"path_picker_file": []interface{}{"report.txt"},
		},
	}
	got := def.RecoverSelectedValue(map[string]any{
		"field_name": "path_picker_file",
	}, action)
	if got != "report.txt" {
		t.Fatalf("RecoverSelectedValue() = %q, want field override value", got)
	}
}

func TestFlowDefinitionsUseSharedPaginationHint(t *testing.T) {
	defs := []PaginatedSelectFlowDefinition{
		PathPickerDirectoryFlow,
		PathPickerFileFlow,
		TargetPickerWorkspaceFlow,
		TargetPickerSessionFlow,
		ThreadSelectionFlow,
	}
	for _, def := range defs {
		if def.PaginationHint != DefaultPaginationHint {
			t.Fatalf("expected shared pagination hint, got %#v", def)
		}
	}
}
