package feishu

import (
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestProjectCommandViewRendersModelCard(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventFeishuCommandView,
		FeishuCommandView: &control.FeishuCommandView{
			Config: &control.FeishuCommandConfigView{
				CommandID:          control.FeishuCommandModel,
				EffectiveValue:     "gpt-5.4",
				OverrideValue:      "gpt-5.4-mini",
				OverrideExtraValue: "high",
			},
		},
		FeishuCommandContext: &control.FeishuUICommandContext{
			DTOOwner:  control.FeishuUIDTOwnerCommand,
			ViewKind:  "config",
			CommandID: control.FeishuCommandModel,
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if ops[0].CardTitle != "模型" {
		t.Fatalf("unexpected card title: %#v", ops[0])
	}
	if len(ops[0].CardElements) < 6 {
		t.Fatalf("expected structured model card elements, got %#v", ops[0].CardElements)
	}
	if ops[0].CardBody != "" {
		t.Fatalf("expected command view model card to avoid markdown body summary, got %#v", ops[0])
	}
	if ops[0].CardElements[0]["content"] != "菜单首页 / 发送设置 / 模型" {
		t.Fatalf("unexpected breadcrumb element: %#v", ops[0].CardElements[0])
	}
	if !containsMarkdownExact(ops[0].CardElements, "**当前模型**") || !containsCardTextExact(ops[0].CardElements, "gpt-5.4") {
		t.Fatalf("expected model card to render current model as structured text, got %#v", ops[0].CardElements)
	}
	if !containsMarkdownExact(ops[0].CardElements, "**飞书覆盖**") || !containsCardTextExact(ops[0].CardElements, "gpt-5.4-mini") {
		t.Fatalf("expected model card to render override model as structured text, got %#v", ops[0].CardElements)
	}
	if !containsMarkdownExact(ops[0].CardElements, "**附带推理覆盖**") || !containsCardTextExact(ops[0].CardElements, "high") {
		t.Fatalf("expected model card to render override reasoning as structured text, got %#v", ops[0].CardElements)
	}
	if !containsMarkdownExact(ops[0].CardElements, "**常见模型**") {
		t.Fatalf("expected model card to keep section headings, got %#v", ops[0].CardElements)
	}
	renderedElements := renderedV2BodyElements(t, ops[0])
	assertNoDuplicateNamedCardElements(t, renderedElements)
	selectFormFound := false
	textFormFound := false
	relatedFound := false
	clearCount := 0
	for _, element := range renderedElements {
		tag, _ := element["tag"].(string)
		switch tag {
		case "form":
			formElements, _ := element["elements"].([]map[string]any)
			if len(formElements) == 0 {
				continue
			}
			switch formElements[0]["tag"] {
			case "select_static":
				selectFormFound = true
			case "input":
				textFormFound = true
			}
		case "button":
			value := renderedButtonCallbackValue(t, element)
			if value["kind"] == "run_command" && value["command_text"] == "/model clear" {
				clearCount++
			}
			if value["kind"] == "run_command" && value["command_text"] == "/menu send_settings" {
				relatedFound = true
			}
		}
	}
	if !selectFormFound || !textFormFound || !relatedFound || clearCount != 1 {
		t.Fatalf("expected rendered V2 model form and related action, got %#v", renderedElements)
	}
}

func TestProjectCommandViewKeepsMarkdownLikeModelNamesOutOfMarkdownSummary(t *testing.T) {
	projector := NewProjector()
	override := "gpt-5.4-mini `x` [demo](https://example.com)"
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventFeishuCommandView,
		FeishuCommandView: &control.FeishuCommandView{
			Config: &control.FeishuCommandConfigView{
				CommandID:      control.FeishuCommandModel,
				EffectiveValue: "gpt-5.4",
				OverrideValue:  override,
			},
		},
		FeishuCommandContext: &control.FeishuUICommandContext{
			DTOOwner:  control.FeishuUIDTOwnerCommand,
			ViewKind:  "config",
			CommandID: control.FeishuCommandModel,
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if ops[0].CardBody != "" {
		t.Fatalf("expected structured summary to clear markdown body, got %#v", ops[0])
	}
	if !containsCardTextExact(ops[0].CardElements, override) {
		t.Fatalf("expected override model to stay in plain text section, got %#v", ops[0].CardElements)
	}
	for _, element := range ops[0].CardElements {
		if strings.Contains(markdownContent(element), override) {
			t.Fatalf("expected markdown elements to avoid dynamic override text, got %#v", ops[0].CardElements)
		}
	}
}
