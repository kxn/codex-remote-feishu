package feishu

import (
	"reflect"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestProjectInteractiveCommandCatalogRendersBreadcrumbsAndCommandForm(t *testing.T) {
	projector := NewProjector()
	ops := projector.ProjectEvent("chat-1", commandCatalogEvent(control.FeishuPageView{
		CatalogBackend:  agentproto.BackendClaude,
		Title:           "使用模型",
		SummarySections: summarySections("直接在卡片里输入模型名。"),
		Interactive:     true,
		DisplayStyle:    control.CommandCatalogDisplayCompactButtons,
		Breadcrumbs:     []control.CommandCatalogBreadcrumb{{Label: "菜单首页"}, {Label: "参数设置"}, {Label: "使用模型"}},
		Sections: []control.CommandCatalogSection{{
			Title: "手动输入",
			Entries: []control.CommandCatalogEntry{{
				Form: &control.CommandCatalogForm{
					CommandID:        control.FeishuCommandModel,
					CommandText:      "/model",
					CatalogFamilyID:  control.FeishuCommandModel,
					CatalogVariantID: "model.default",
					SubmitLabel:      "应用",
					Field: control.CommandCatalogFormField{
						Name:        "command_args",
						Kind:        control.CommandCatalogFormFieldText,
						Label:       "输入模型名，或输入“模型名 推理强度”。",
						Placeholder: "gpt-5.4 high",
					},
				},
			}},
		}},
		RelatedButtons: []control.CommandCatalogButton{{
			Label:       "返回上一层",
			CommandText: "/menu send_settings",
		}},
	}))
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if len(ops[0].CardElements) != 6 {
		t.Fatalf("expected breadcrumb + summary + section + form + divider + related action, got %#v", ops[0].CardElements)
	}
	if ops[0].CardElements[0]["content"] != "菜单首页 / 参数设置 / 使用模型" {
		t.Fatalf("unexpected breadcrumb element: %#v", ops[0].CardElements[0])
	}
	if !containsCardTextExact(ops[0].CardElements, "直接在卡片里输入模型名。") {
		t.Fatalf("expected summary plain text block, got %#v", ops[0].CardElements)
	}
	formContainer := ops[0].CardElements[3]
	if formContainer["tag"] != "form" {
		t.Fatalf("expected form container, got %#v", formContainer)
	}
	formElements, _ := formContainer["elements"].([]map[string]any)
	if len(formElements) != 2 {
		t.Fatalf("expected input + submit button, got %#v", formContainer)
	}
	input, _ := formElements[0]["name"].(string)
	if input != "command_args" {
		t.Fatalf("unexpected form field name: %#v", formElements[0])
	}
	if formElements[1]["action_type"] != nil || formElements[1]["form_action_type"] != "submit" {
		t.Fatalf("expected V2 form submit button, got %#v", formElements[1])
	}
	if formElements[1]["disabled"] == true {
		t.Fatalf("expected command form submit button to stay clickable for submit-time validation, got %#v", formElements[1])
	}
	value := cardButtonPayload(t, formElements[1])
	assertPageSubmitPayload(t, value, control.ActionModelCommand, "", "command_args")
	assertCatalogProvenancePayloadMatchesCommand(t, value, agentproto.BackendClaude, "/model")
	if ops[0].CardElements[4]["tag"] != "hr" {
		t.Fatalf("expected divider before related action row, got %#v", ops[0].CardElements)
	}
	relatedRow := cardElementButtons(t, ops[0].CardElements[5])
	relatedValue := cardButtonPayload(t, relatedRow[0])
	assertPageLocalActionPayloadMatchesCommand(t, relatedValue, "/menu send_settings")
	if ops[0].cardEnvelope != cardEnvelopeV2 || ops[0].card == nil {
		t.Fatalf("expected command catalog with form to use V2 in #120, got %#v", ops[0])
	}
	assertNoLegacyCardModelMarkers(t, ops[0].CardElements)
	renderedElements := renderedV2BodyElements(t, ops[0])
	if renderedElements[3]["tag"] != "form" {
		t.Fatalf("expected rendered V2 form element, got %#v", renderedElements)
	}
	if renderedElements[4]["tag"] != "hr" {
		t.Fatalf("expected rendered V2 divider before related button, got %#v", renderedElements)
	}
	renderedFormElements, _ := renderedElements[3]["elements"].([]map[string]any)
	if len(renderedFormElements) != 2 {
		t.Fatalf("expected rendered V2 form to keep input and submit button, got %#v", renderedElements[3])
	}
	if renderedFormElements[1]["action_type"] != nil || renderedFormElements[1]["form_action_type"] != "submit" {
		t.Fatalf("expected command form submit button to use V2 form_action_type, got %#v", renderedFormElements[1])
	}
	if renderedFormElements[1]["disabled"] == true {
		t.Fatalf("expected rendered command form submit button to stay clickable for submit-time validation, got %#v", renderedFormElements[1])
	}
	renderedSubmitValue := renderedButtonCallbackValue(t, renderedFormElements[1])
	assertPageSubmitPayload(t, renderedSubmitValue, control.ActionModelCommand, "", "command_args")
	assertCatalogProvenancePayloadMatchesCommand(t, renderedSubmitValue, agentproto.BackendClaude, "/model")
	renderedRelatedValue := renderedButtonCallbackValue(t, renderedElements[5])
	assertPageLocalActionPayloadMatchesCommand(t, renderedRelatedValue, "/menu send_settings")
}

func TestProjectInteractiveCommandCatalogRendersSelectStaticCommandForm(t *testing.T) {
	projector := NewProjector()
	ops := projector.ProjectEvent("chat-1", commandCatalogEvent(control.FeishuPageView{
		Title:       "使用模型",
		Interactive: true,
		Sections: []control.CommandCatalogSection{{
			Title: "常见模型",
			Entries: []control.CommandCatalogEntry{{
				Form: &control.CommandCatalogForm{
					CommandID:   control.FeishuCommandModel,
					CommandText: "/model",
					SubmitLabel: "应用",
					Field: control.CommandCatalogFormField{
						Name:         "command_args",
						Kind:         control.CommandCatalogFormFieldSelectStatic,
						Label:        "从下拉里选择常见模型。",
						Placeholder:  "选择模型",
						DefaultValue: "gpt-5.4-mini",
						Options: []control.CommandCatalogFormFieldOption{
							{Label: "gpt-5.4", Value: "gpt-5.4"},
							{Label: "gpt-5.4-mini", Value: "gpt-5.4-mini"},
						},
					},
				},
			}},
		}},
	}))
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	formContainer := ops[0].CardElements[1]
	if formContainer["tag"] != "form" {
		t.Fatalf("expected form container, got %#v", formContainer)
	}
	formElements, _ := formContainer["elements"].([]map[string]any)
	if len(formElements) != 2 {
		t.Fatalf("expected select + submit button, got %#v", formContainer)
	}
	if formElements[0]["tag"] != "select_static" {
		t.Fatalf("expected select_static field, got %#v", formElements[0])
	}
	if _, ok := formElements[0]["label"]; ok {
		t.Fatalf("expected select_static to omit unsupported label property, got %#v", formElements[0])
	}
	if _, ok := formElements[0]["label_position"]; ok {
		t.Fatalf("expected select_static to omit unsupported label_position property, got %#v", formElements[0])
	}
	if formElements[0]["initial_option"] != "gpt-5.4-mini" {
		t.Fatalf("expected initial option, got %#v", formElements[0])
	}
	options, _ := formElements[0]["options"].([]map[string]any)
	if len(options) != 2 || options[0]["value"] != "gpt-5.4" || options[1]["value"] != "gpt-5.4-mini" {
		t.Fatalf("unexpected select_static options: %#v", formElements[0])
	}
	if formElements[1]["form_action_type"] != "submit" {
		t.Fatalf("expected V2 submit button, got %#v", formElements[1])
	}
}

func TestProjectMenuHomeRendersRootBreadcrumbAndNamedGroupButtons(t *testing.T) {
	projector := NewProjector()
	projector.SetMenuHomeVersion("v9.9.9")
	view := control.BuildFeishuCommandMenuHomePageViewForContext(control.CatalogContext{})
	ops := projector.ProjectEvent("chat-1", commandCatalogEvent(view))
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	groups := control.FeishuCommandGroups()
	if len(ops[0].CardElements) != 1+len(groups) {
		t.Fatalf("expected root breadcrumb plus one button row per group, got %#v", ops[0].CardElements)
	}
	wantHeader := "Codex Remote Feishu · v9.9.9\nGitHub: [kxn/codex-remote-feishu](https://github.com/kxn/codex-remote-feishu)\n使用说明：[查看文档](https://my.feishu.cn/docx/PTncdNBf1oS9N5xBikBcGi2enzc)"
	if ops[0].CardElements[0]["content"] != wantHeader {
		t.Fatalf("expected menu home header, got %#v", ops[0].CardElements[0])
	}
	labels := make([]string, 0, len(groups))
	for _, element := range ops[0].CardElements[1:] {
		row := cardElementButtons(t, element)
		if len(row) != 1 {
			t.Fatalf("expected one compact button per row, got %#v", element)
		}
		labels = append(labels, cardButtonLabel(t, row[0]))
	}
	want := make([]string, 0, len(groups))
	for _, group := range groups {
		want = append(want, group.Title)
	}
	if !reflect.DeepEqual(labels, want) {
		t.Fatalf("menu home button labels = %#v, want %#v", labels, want)
	}
}

func TestProjectCommandCatalogRendersNoticeAreaBetweenBusinessAndFooter(t *testing.T) {
	projector := NewProjector()
	ops := projector.ProjectEvent("chat-1", commandCatalogEvent(control.FeishuPageView{
		Title: "上下文已压缩",
		BodySections: []control.FeishuCardTextSection{{
			Label: "当前会话",
			Lines: []string{"修复登录流程 (thread-1)"},
		}},
		NoticeSections: []control.FeishuCardTextSection{{
			Label: "结果",
			Lines: []string{"当前会话的上下文已压缩完成。"},
		}},
		RelatedButtons: []control.CommandCatalogButton{{
			Label:       "返回菜单",
			CommandText: "/menu",
		}},
	}))
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if len(ops[0].CardElements) != 7 {
		t.Fatalf("expected body + divider + notice + divider + footer, got %#v", ops[0].CardElements)
	}
	if ops[0].CardElements[0]["content"] != "**当前会话**" {
		t.Fatalf("expected business section label first, got %#v", ops[0].CardElements[0])
	}
	if ops[0].CardElements[2]["tag"] != "hr" {
		t.Fatalf("expected divider before notice area, got %#v", ops[0].CardElements)
	}
	if ops[0].CardElements[3]["content"] != "**结果**" {
		t.Fatalf("expected notice label after divider, got %#v", ops[0].CardElements)
	}
	if ops[0].CardElements[6]["tag"] != "button" {
		t.Fatalf("expected footer button after second divider, got %#v", ops[0].CardElements)
	}
	renderedElements := renderedV2BodyElements(t, ops[0])
	if renderedElements[2]["tag"] != "hr" || renderedElements[5]["tag"] != "hr" {
		t.Fatalf("expected both dividers to survive V2 render, got %#v", renderedElements)
	}
}

func TestProjectCommandCatalogKeepsManualLocalCallbackFreeOfCatalogProvenance(t *testing.T) {
	projector := NewProjector()
	ops := projector.ProjectEvent("chat-1", commandCatalogEvent(control.FeishuPageView{
		Title:       "调试",
		Interactive: true,
		Sections: []control.CommandCatalogSection{{
			Entries: []control.CommandCatalogEntry{{
				Buttons: []control.CommandCatalogButton{{
					Label:         "管理页外链",
					Kind:          control.CommandCatalogButtonCallbackAction,
					CommandID:     control.FeishuCommandAdminSubcommand,
					CallbackValue: actionPayloadPageLocalAction(string(control.ActionAdminCommand), "web"),
				}},
			}},
		}},
	}))
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	row := cardElementButtons(t, ops[0].CardElements[0])
	value := cardButtonPayload(t, row[0])
	assertPageLocalActionPayloadMatchesCommand(t, value, "/admin web")
}
