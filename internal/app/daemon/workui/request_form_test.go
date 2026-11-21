package workui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func requestCardNodes(t *testing.T, raw json.RawMessage) []map[string]any {
	t.Helper()
	var card map[string]any
	if err := json.Unmarshal(raw, &card); err != nil {
		t.Fatal(err)
	}
	if card["schema"] != "2.0" {
		t.Fatal("not JSON 2.0")
	}
	nodes := []map[string]any{}
	var walk func(any)
	walk = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			nodes = append(nodes, v)
			for _, child := range v {
				walk(child)
			}
		case []any:
			for _, child := range v {
				walk(child)
			}
		}
	}
	walk(card)
	return nodes
}
func assertNativeCallback(t *testing.T, node map[string]any, viewID string) {
	t.Helper()
	if _, ok := node["value"]; ok {
		t.Fatal("legacy top-level button.value emitted")
	}
	behaviors, ok := node["behaviors"].([]any)
	if !ok || len(behaviors) != 1 {
		t.Fatalf("missing JSON2 behaviors %#v", node)
	}
	behavior := behaviors[0].(map[string]any)
	value := behavior["value"].(map[string]any)
	if behavior["type"] != "callback" || value["team_view"] != viewID || value["action"] != "native.respond" {
		t.Fatalf("callback contract %#v", behavior)
	}
}
func TestRequestFormCardRendersFullPlanMultiSelectAndJSON2Actions(t *testing.T) {
	form, err := orchestrator.NewRequestForm(agentproto.BackendClaude, nil, map[string]any{"requestType": "approval", "requestMethod": "tool/ExitPlanMode", "body": "Create internal/new.go and update docs/guide.md"}, "/workspace")
	if err != nil {
		t.Fatal(err)
	}
	opened, err := orchestrator.TransformRequestForm(form, orchestrator.HumanRequestFormAction{OptionID: "acceptForSession"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := RequestFormCard("view-plan", opened.NextForm)
	if err != nil {
		t.Fatal(err)
	}
	fields := map[string]string{}
	buttons := 0
	for _, node := range requestCardNodes(t, raw) {
		tag, _ := node["tag"].(string)
		if tag == "select_static" || tag == "multi_select_static" {
			fields[node["name"].(string)] = tag
		}
		if tag == "button" {
			assertNativeCallback(t, node, "view-plan")
			buttons++
		}
	}
	if fields["grant_level"] != "select_static" || fields["directories"] != "multi_select_static" || fields["rule_classes"] != "multi_select_static" || buttons < 3 {
		t.Fatalf("plan features lost: %#v buttons=%d", fields, buttons)
	}
	if !strings.Contains(string(raw), "selected_values") || !strings.Contains(string(raw), "/workspace") {
		t.Fatal("exact offered permissions/default selection lost")
	}
}
func TestRequestFormCardSecretDoesNotEchoDraftOrDefault(t *testing.T) {
	form, err := orchestrator.NewRequestForm(agentproto.BackendCodex, &agentproto.RequestPrompt{Type: agentproto.RequestTypeRequestUserInput, Questions: []agentproto.RequestQuestion{{ID: "secret-field", Question: "Token", Secret: true, DefaultValue: "original-secret"}}}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	form.DraftAnswers["secret-field"] = "saved-secret"
	raw, err := RequestFormCard("view-secret", form)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "original-secret") || strings.Contains(string(raw), "saved-secret") {
		t.Fatal("secret escaped into shared card JSON")
	}
	found := false
	for _, node := range requestCardNodes(t, raw) {
		if node["tag"] == "input" {
			found = node["name"] == "secret-field" && node["input_type"] == "password"
			if _, ok := node["default_value"]; ok {
				t.Fatal("secret was prefilled into card")
			}
		}
	}
	if !found {
		t.Fatal("secret input semantic lost")
	}
}
func TestRequestFormCardFeedbackSkipAndSafeDynamicText(t *testing.T) {
	form := orchestrator.RequestFormState{Revision: 1, Title: "**human title**", Sections: []state.RequestPromptTextSectionRecord{{Label: "<unsafe-label>", Lines: []string{"[external](https://example.com)"}}}, Questions: []state.RequestPromptQuestionRecord{{ID: "q", Question: "Optional", Optional: true, AllowOther: true, Options: []state.RequestPromptQuestionOptionRecord{{Label: "Choice with no description"}}}}}
	raw, err := RequestFormCard("view-question", form)
	if err != nil {
		t.Fatal(err)
	}
	skip := false
	for _, node := range requestCardNodes(t, raw) {
		if node["tag"] == "markdown" {
			t.Fatal("dynamic native request text interpreted as markdown")
		}
		if node["tag"] == "button" {
			assertNativeCallback(t, node, "view-question")
			value := node["behaviors"].([]any)[0].(map[string]any)["value"].(map[string]any)
			skip = skip || value["skip_question_id"] == "q"
		}
	}
	if !skip || !strings.Contains(string(raw), "Choice with no description") {
		t.Fatal("optional/other-choice semantics lost")
	}
	form.FeedbackMode = "decline_with_feedback"
	raw, err = RequestFormCard("view-feedback", form)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, node := range requestCardNodes(t, raw) {
		if node["tag"] == "input" && node["name"] == "__feedback" {
			found = true
		}
	}
	if !found {
		t.Fatal("feedback form field contract missing")
	}
}
func TestRequestFormCardRejectsLimitsWithoutDroppingFields(t *testing.T) {
	form := orchestrator.RequestFormState{Revision: 1, Title: "Question", Questions: []state.RequestPromptQuestionRecord{{ID: "q", Question: "Choose"}}}
	for range 101 {
		form.Questions[0].Options = append(form.Questions[0].Options, state.RequestPromptQuestionOptionRecord{Label: "choice"})
	}
	if _, err := RequestFormCard("view", form); err == nil {
		t.Fatal("oversized selector silently cropped")
	}
	form.Questions[0].Options = nil
	form.Sections = []state.RequestPromptTextSectionRecord{{Lines: []string{strings.Repeat("Large source ", 3000)}}}
	if _, err := RequestFormCard("view", form); err == nil {
		t.Fatal("oversized body silently cropped")
	}
	form.Sections = nil
	form.StructuredForm = &state.RequestPromptStructuredFormRecord{Fields: []state.RequestPromptFormFieldRecord{{Name: "same", Kind: state.RequestPromptFormFieldSelectStatic}, {Name: "same", Kind: state.RequestPromptFormFieldSelectStatic}}}
	if _, err := RequestFormCard("view", form); err == nil {
		t.Fatal("duplicate form field identity accepted")
	}
}
