package feishu

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	larkcallback "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func TestParseCardActionTriggerEventBuildsHistoryActions(t *testing.T) {
	tests := []struct {
		name      string
		payload   map[string]any
		option    string
		formValue map[string]interface{}
		wantKind  control.ActionKind
		wantPage  int
		wantTurn  string
	}{
		{
			name: "page button",
			payload: map[string]any{
				"kind":      cardActionKindHistoryPage,
				"picker_id": "history-1",
				"page":      2,
			},
			wantKind: control.ActionHistoryPage,
			wantPage: 2,
		},
		{
			name: "detail from form value",
			payload: map[string]any{
				"kind":      cardActionKindHistoryDetail,
				"picker_id": "history-1",
			},
			formValue: map[string]interface{}{
				cardThreadHistoryTurnFieldName: []interface{}{"turn-2"},
			},
			wantKind: control.ActionHistoryDetail,
			wantTurn: "turn-2",
		},
		{
			name: "detail from option fallback",
			payload: map[string]any{
				"kind":       cardActionKindHistoryDetail,
				"picker_id":  "history-1",
				"field_name": cardThreadHistoryTurnFieldName,
			},
			option:   "turn-3",
			wantKind: control.ActionHistoryDetail,
			wantTurn: "turn-3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
			gateway.recordSurfaceMessage("om-card-history", "feishu:app-1:user:user-1")
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
						OpenMessageID: "om-card-history",
					},
				},
			}

			action, ok := gateway.parseCardActionTriggerEvent(event)
			if !ok {
				t.Fatal("expected history action to parse")
			}
			if action.Kind != tt.wantKind || action.PickerID != "history-1" {
				t.Fatalf("unexpected history action: %#v", action)
			}
			if action.Page != tt.wantPage {
				t.Fatalf("page = %d, want %d", action.Page, tt.wantPage)
			}
			if action.TurnID != tt.wantTurn {
				t.Fatalf("turn id = %q, want %q", action.TurnID, tt.wantTurn)
			}
		})
	}
}
