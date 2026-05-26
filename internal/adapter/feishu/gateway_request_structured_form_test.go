package feishu

import (
	"testing"

	larkcallback "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func TestParseCardActionTriggerEventPreservesRequestFormMultiSelectValues(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	gateway.recordSurfaceMessage("om-card-structured-1", "feishu:app-1:user:user-1")
	userID := "user-1"
	event := &larkcallback.CardActionTriggerEvent{
		Event: &larkcallback.CardActionTriggerRequest{
			Operator: &larkcallback.Operator{UserID: &userID},
			Action: &larkcallback.CallBackAction{
				Value: map[string]interface{}{
					"kind":             "submit_request_form",
					"request_id":       "req-1",
					"request_type":     "approval",
					"request_revision": 5,
				},
				FormValue: map[string]interface{}{
					"directories": []interface{}{
						"/data/dl/droid/internal/core/orchestrator",
						"/data/dl/droid/internal/adapter/feishu",
					},
					"grant_level": "scoped_rules",
					"rule_classes": []interface{}{
						"edit_existing_files",
						"create_new_files",
					},
				},
			},
			Context: &larkcallback.Context{
				OpenChatID:    "oc_1",
				OpenMessageID: "om-card-structured-1",
			},
		},
	}

	action, ok := gateway.parseCardActionTriggerEvent(event)
	if !ok {
		t.Fatal("expected structured request form callback to be parsed")
	}
	if action.Request == nil {
		t.Fatalf("expected request payload, got %#v", action)
	}
	if got := action.Request.Answers["directories"]; len(got) != 2 {
		t.Fatalf("expected multi-select values preserved, got %#v", action.Request.Answers)
	}
	if got := action.Request.Answers["rule_classes"]; len(got) != 2 {
		t.Fatalf("expected multi-select rules preserved, got %#v", action.Request.Answers)
	}
}
