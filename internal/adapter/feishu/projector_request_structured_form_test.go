package feishu

import (
	"testing"

	projectorpkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/projector"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
)

func TestRequestPromptStructuredFormRendersMultiSelectStatic(t *testing.T) {
	elements := projectorpkg.RequestPromptElements(control.FeishuRequestView{
		RequestID:       "req-1",
		RequestType:     "approval",
		SemanticKind:    control.RequestSemanticPlanConfirmation,
		RequestRevision: 3,
		StructuredForm: &control.RequestPromptStructuredForm{
			SubmitLabel: "按以上授权继续",
			Fields: []control.RequestPromptFormField{
				{
					Name:  "grant_level",
					Kind:  control.RequestPromptFormFieldSelectStatic,
					Label: "授权级别",
					Options: []control.RequestPromptFormFieldOption{
						{Label: "仅按选中范围自动允许", Value: "scoped_rules"},
					},
					DefaultValues: []string{"scoped_rules"},
				},
				{
					Name:  "directories",
					Kind:  control.RequestPromptFormFieldMultiSelectStatic,
					Label: "目录范围",
					Options: []control.RequestPromptFormFieldOption{
						{Label: "internal/core/orchestrator", Value: "/data/dl/droid/internal/core/orchestrator"},
						{Label: "internal/adapter/feishu", Value: "/data/dl/droid/internal/adapter/feishu"},
					},
					DefaultValues: []string{"/data/dl/droid/internal/core/orchestrator"},
				},
			},
		},
		Options: []control.RequestPromptOption{
			{OptionID: frontstagecontract.RequestPromptOptionStepPrevious, Label: "返回", Style: "default"},
			{OptionID: "decline", Label: "拒绝", Style: "default"},
		},
	}, "daemon-1")

	form := findCardFormByName(t, elements, "request_form_req-1_structured")
	fields := cardMapArray(form["elements"])
	if len(fields) < 3 {
		t.Fatalf("expected structured request form fields plus submit button, got %#v", form)
	}
	if got := cardStringValue(fields[0]["tag"]); got != "select_static" {
		t.Fatalf("expected first field to be select_static, got %#v", fields[0])
	}
	if got := cardStringValue(fields[1]["tag"]); got != "multi_select_static" {
		t.Fatalf("expected second field to be multi_select_static, got %#v", fields[1])
	}
	if got := cardStringValue(fields[2]["tag"]); got != "button" || cardButtonLabel(t, fields[2]) != "按以上授权继续" {
		t.Fatalf("expected structured submit button, got %#v", fields[2])
	}
	value := cardButtonPayload(t, fields[2])
	if value["kind"] != "submit_request_form" || value["request_revision"] != 3 {
		t.Fatalf("unexpected submit payload: %#v", value)
	}
}

func findCardFormByName(t *testing.T, elements []map[string]any, name string) map[string]any {
	t.Helper()
	for _, element := range elements {
		if cardStringValue(element["tag"]) == "form" && cardStringValue(element["name"]) == name {
			return element
		}
	}
	t.Fatalf("form %q not found in %#v", name, elements)
	return nil
}
