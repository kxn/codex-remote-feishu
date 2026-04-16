package feishu

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	larkcallback "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func TestParseCardActionTriggerEventBuildsTargetPickerSelectActions(t *testing.T) {
	tests := []struct {
		name      string
		payload   map[string]any
		option    string
		formValue map[string]interface{}
		wantKind  control.ActionKind
		wantValue string
	}{
		{
			name: "mode from payload",
			payload: map[string]any{
				"kind":         cardActionKindTargetPickerSelectMode,
				"picker_id":    "picker-1",
				"target_value": string(control.FeishuTargetPickerModeAddWorkspace),
			},
			wantKind:  control.ActionTargetPickerSelectMode,
			wantValue: string(control.FeishuTargetPickerModeAddWorkspace),
		},
		{
			name: "source from option fallback",
			payload: map[string]any{
				"kind":       cardActionKindTargetPickerSelectSource,
				"picker_id":  "picker-1",
				"field_name": cardTargetPickerSourceFieldName,
			},
			option:    string(control.FeishuTargetPickerSourceGitURL),
			wantKind:  control.ActionTargetPickerSelectSource,
			wantValue: string(control.FeishuTargetPickerSourceGitURL),
		},
		{
			name: "workspace from form value",
			payload: map[string]any{
				"kind":      cardActionKindTargetPickerSelectWorkspace,
				"picker_id": "picker-1",
			},
			formValue: map[string]interface{}{
				cardTargetPickerWorkspaceFieldName: []interface{}{"/data/dl/web"},
			},
			wantKind:  control.ActionTargetPickerSelectWorkspace,
			wantValue: "/data/dl/web",
		},
		{
			name: "session from option fallback",
			payload: map[string]any{
				"kind":       cardActionKindTargetPickerSelectSession,
				"picker_id":  "picker-1",
				"field_name": cardTargetPickerSessionFieldName,
			},
			option:    "thread:thread-2",
			wantKind:  control.ActionTargetPickerSelectSession,
			wantValue: "thread:thread-2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
			gateway.recordSurfaceMessage("om-card-target-picker", "feishu:app-1:user:user-1")
			userID := "user-1"
			event := &larkcallback.CardActionTriggerEvent{
				Event: &larkcallback.CardActionTriggerRequest{
					Operator: &larkcallback.Operator{UserID: &userID},
					Action: &larkcallback.CallBackAction{
						Value:     tt.payload,
						Option:    tt.option,
						FormValue: tt.formValue,
					},
					Context: &larkcallback.Context{
						OpenChatID:    "oc_1",
						OpenMessageID: "om-card-target-picker",
					},
				},
			}

			action, ok := gateway.parseCardActionTriggerEvent(event)
			if !ok {
				t.Fatal("expected target picker action to parse")
			}
			if action.Kind != tt.wantKind || action.PickerID != "picker-1" {
				t.Fatalf("unexpected target picker action: %#v", action)
			}
			switch tt.wantKind {
			case control.ActionTargetPickerSelectMode, control.ActionTargetPickerSelectSource, control.ActionTargetPickerSelectSession:
				if action.TargetPickerValue != tt.wantValue {
					t.Fatalf("target picker value = %q, want %q", action.TargetPickerValue, tt.wantValue)
				}
			case control.ActionTargetPickerSelectWorkspace:
				if action.WorkspaceKey != tt.wantValue {
					t.Fatalf("workspace key = %q, want %q", action.WorkspaceKey, tt.wantValue)
				}
			}
		})
	}
}

func TestParseCardActionTriggerEventBuildsTargetPickerConfirmAction(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	gateway.recordSurfaceMessage("om-card-target-picker-confirm", "feishu:app-1:user:user-1")
	userID := "user-1"
	event := &larkcallback.CardActionTriggerEvent{
		Event: &larkcallback.CardActionTriggerRequest{
			Operator: &larkcallback.Operator{UserID: &userID},
			Action: &larkcallback.CallBackAction{
				Value: map[string]any{
					"kind":      cardActionKindTargetPickerConfirm,
					"picker_id": "picker-1",
				},
				FormValue: map[string]interface{}{
					cardTargetPickerWorkspaceFieldName: []interface{}{"/data/dl/web"},
					cardTargetPickerSessionFieldName:   []interface{}{"new_thread"},
				},
			},
			Context: &larkcallback.Context{
				OpenChatID:    "oc_1",
				OpenMessageID: "om-card-target-picker-confirm",
			},
		},
	}

	action, ok := gateway.parseCardActionTriggerEvent(event)
	if !ok {
		t.Fatal("expected target picker confirm action to parse")
	}
	if action.Kind != control.ActionTargetPickerConfirm || action.PickerID != "picker-1" {
		t.Fatalf("unexpected target picker confirm: %#v", action)
	}
	if action.WorkspaceKey != "/data/dl/web" || action.TargetPickerValue != "new_thread" {
		t.Fatalf("unexpected target picker confirm payload: %#v", action)
	}
}
