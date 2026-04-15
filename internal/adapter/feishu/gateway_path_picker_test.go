package feishu

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	larkcallback "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func TestParseCardActionTriggerEventBuildsPathPickerEntryActions(t *testing.T) {
	tests := []struct {
		name       string
		payload    map[string]any
		option     string
		options    []string
		formValue  map[string]interface{}
		wantKind   control.ActionKind
		wantPicker string
		wantEntry  string
	}{
		{
			name: "enter",
			payload: map[string]any{
				"kind":       cardActionKindPathPickerEnter,
				"picker_id":  "picker-1",
				"entry_name": "subdir",
			},
			wantKind:   control.ActionPathPickerEnter,
			wantPicker: "picker-1",
			wantEntry:  "subdir",
		},
		{
			name: "select",
			payload: map[string]any{
				"kind":       cardActionKindPathPickerSelect,
				"picker_id":  "picker-1",
				"entry_name": "a.txt",
			},
			wantKind:   control.ActionPathPickerSelect,
			wantPicker: "picker-1",
			wantEntry:  "a.txt",
		},
		{
			name: "enter from select option",
			payload: map[string]any{
				"kind":       cardActionKindPathPickerEnter,
				"picker_id":  "picker-1",
				"field_name": cardPathPickerDirectorySelectFieldName,
			},
			option:     "subdir",
			wantKind:   control.ActionPathPickerEnter,
			wantPicker: "picker-1",
			wantEntry:  "subdir",
		},
		{
			name: "enter parent from select option",
			payload: map[string]any{
				"kind":       cardActionKindPathPickerEnter,
				"picker_id":  "picker-1",
				"field_name": cardPathPickerDirectorySelectFieldName,
			},
			option:     "..",
			wantKind:   control.ActionPathPickerEnter,
			wantPicker: "picker-1",
			wantEntry:  "..",
		},
		{
			name: "select from options array",
			payload: map[string]any{
				"kind":       cardActionKindPathPickerSelect,
				"picker_id":  "picker-1",
				"field_name": cardPathPickerFileSelectFieldName,
			},
			options:    []string{"b.txt"},
			wantKind:   control.ActionPathPickerSelect,
			wantPicker: "picker-1",
			wantEntry:  "b.txt",
		},
		{
			name: "select from form value fallback",
			payload: map[string]any{
				"kind":       cardActionKindPathPickerSelect,
				"picker_id":  "picker-1",
				"field_name": cardPathPickerFileSelectFieldName,
			},
			formValue: map[string]interface{}{
				cardPathPickerFileSelectFieldName: []interface{}{"report.txt"},
			},
			wantKind:   control.ActionPathPickerSelect,
			wantPicker: "picker-1",
			wantEntry:  "report.txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
			gateway.recordSurfaceMessage("om-card-picker", "feishu:app-1:user:user-1")
			userID := "user-1"
			event := &larkcallback.CardActionTriggerEvent{
				Event: &larkcallback.CardActionTriggerRequest{
					Operator: &larkcallback.Operator{UserID: &userID},
					Action: &larkcallback.CallBackAction{
						Value:     tt.payload,
						Option:    tt.option,
						Options:   tt.options,
						FormValue: tt.formValue,
					},
					Context: &larkcallback.Context{
						OpenChatID:    "oc_1",
						OpenMessageID: "om-card-picker",
					},
				},
			}

			action, ok := gateway.parseCardActionTriggerEvent(event)
			if !ok {
				t.Fatal("expected path picker action to parse")
			}
			if action.Kind != tt.wantKind || action.PickerID != tt.wantPicker || action.PickerEntry != tt.wantEntry {
				t.Fatalf("unexpected path picker action: %#v", action)
			}
		})
	}
}

func TestParseCardActionTriggerEventBuildsPathPickerNavigationActions(t *testing.T) {
	tests := []struct {
		name     string
		kind     string
		wantKind control.ActionKind
	}{
		{name: "up", kind: cardActionKindPathPickerUp, wantKind: control.ActionPathPickerUp},
		{name: "confirm", kind: cardActionKindPathPickerConfirm, wantKind: control.ActionPathPickerConfirm},
		{name: "cancel", kind: cardActionKindPathPickerCancel, wantKind: control.ActionPathPickerCancel},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
			gateway.recordSurfaceMessage("om-card-picker-nav", "feishu:app-1:user:user-1")
			userID := "user-1"
			event := &larkcallback.CardActionTriggerEvent{
				Event: &larkcallback.CardActionTriggerRequest{
					Operator: &larkcallback.Operator{UserID: &userID},
					Action: &larkcallback.CallBackAction{Value: map[string]any{
						"kind":      tt.kind,
						"picker_id": "picker-1",
					}},
					Context: &larkcallback.Context{
						OpenChatID:    "oc_1",
						OpenMessageID: "om-card-picker-nav",
					},
				},
			}

			action, ok := gateway.parseCardActionTriggerEvent(event)
			if !ok {
				t.Fatal("expected path picker navigation to parse")
			}
			if action.Kind != tt.wantKind || action.PickerID != "picker-1" {
				t.Fatalf("unexpected path picker navigation action: %#v", action)
			}
		})
	}
}
