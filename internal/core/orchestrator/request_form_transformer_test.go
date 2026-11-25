package orchestrator

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
)

func TestPureRequestFormQuestionDraftsValidationAndPersistence(t *testing.T) {
	prompt := &agentproto.RequestPrompt{Type: agentproto.RequestTypeRequestUserInput, Questions: []agentproto.RequestQuestion{{ID: "choice", Question: "Choose", Options: []agentproto.RequestQuestionOption{{Label: "A"}, {Label: "B"}}}, {ID: "secret", Question: "Provide details", AllowOther: true, Secret: true}}}
	form, err := NewRequestForm(agentproto.BackendCodex, prompt, nil, "/workspace")
	if err != nil {
		t.Fatal(err)
	}
	if !form.Questions[1].Secret || !form.Questions[1].AllowOther {
		t.Fatal("typed question semantic flags lost")
	}
	before, _ := json.Marshal(form)
	if _, err = TransformRequestForm(form, HumanRequestFormAction{Answers: map[string][]string{"choice": {"invalid"}}}); err == nil {
		t.Fatal("invalid enum accepted")
	}
	after, _ := json.Marshal(form)
	if string(before) != string(after) {
		t.Fatal("invalid action mutated source snapshot")
	}
	draft, err := TransformRequestForm(form, HumanRequestFormAction{Answers: map[string][]string{"choice": {"A"}}})
	if err != nil || draft.Complete || !draft.NeedsRefresh || draft.NextForm.CurrentQuestionIndex != 1 {
		t.Fatalf("draft %#v %v", draft, err)
	}
	var persisted RequestFormState
	raw, _ := json.Marshal(draft.NextForm)
	if err = json.Unmarshal(raw, &persisted); err != nil {
		t.Fatal(err)
	}
	done, err := TransformRequestForm(persisted, HumanRequestFormAction{Answers: map[string][]string{"secret": {"free text"}}})
	if err != nil || !done.Complete || done.Response["answers"] == nil {
		t.Fatalf("done %#v %v", done, err)
	}
	if _, err = TransformRequestForm(done.NextForm, HumanRequestFormAction{Answers: map[string][]string{"secret": {"late"}}}); err == nil {
		t.Fatal("submitted form accepted another action")
	}
	if prompt.Questions[0].Options[0].Label != "A" {
		t.Fatal("input prompt mutated")
	}
}
func TestPureRequestFormOptionalQuestionSkipAndNavigation(t *testing.T) {
	metadata := map[string]any{"requestType": "request_user_input", "questions": []any{map[string]any{"id": "q1", "question": "Required"}, map[string]any{"id": "q2", "question": "Optional", "optional": true}}}
	form, err := NewRequestForm(agentproto.BackendCodex, nil, metadata, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = TransformRequestForm(form, HumanRequestFormAction{SkipQuestionID: "q1"}); err == nil {
		t.Fatal("required question skipped")
	}
	draft, err := TransformRequestForm(form, HumanRequestFormAction{Answers: map[string][]string{"q1": {"ready"}}})
	if err != nil {
		t.Fatal(err)
	}
	previous, err := TransformRequestForm(draft.NextForm, HumanRequestFormAction{OptionID: frontstagecontract.RequestPromptOptionStepPrevious})
	if err != nil || previous.NextForm.CurrentQuestionIndex != 0 {
		t.Fatalf("previous %#v %v", previous, err)
	}
	next, err := TransformRequestForm(previous.NextForm, HumanRequestFormAction{OptionID: frontstagecontract.RequestPromptOptionStepNext})
	if err != nil {
		t.Fatal(err)
	}
	skipped, err := TransformRequestForm(next.NextForm, HumanRequestFormAction{SkipQuestionID: "q2"})
	if err != nil || !skipped.Complete {
		t.Fatalf("skip %#v %v", skipped, err)
	}
}
func TestPureRequestFormPermissionsPreserveExactGrantedPayload(t *testing.T) {
	permissions := map[string]any{"network": map[string]any{"enabled": true}}
	prompt := &agentproto.RequestPrompt{Type: agentproto.RequestTypePermissionsRequestApproval, Permissions: &agentproto.PermissionsRequestPrompt{ProfilePresent: true, Profile: permissions}}
	form, err := NewRequestForm(agentproto.BackendCodex, prompt, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	done, err := TransformRequestForm(form, HumanRequestFormAction{OptionID: "acceptForSession"})
	if err != nil || !done.Complete || done.Response["scope"] != "session" {
		t.Fatalf("grant %#v %v", done, err)
	}
	a, _ := json.Marshal(done.Response["permissions"])
	b, _ := json.Marshal(permissions)
	if string(a) != string(b) {
		t.Fatalf("permissions changed: %s %s", a, b)
	}
	denied, err := TransformRequestForm(form, HumanRequestFormAction{OptionID: "decline"})
	if err != nil || !reflect.DeepEqual(denied.Response["permissions"], map[string]any{}) {
		t.Fatalf("decline %#v %v", denied, err)
	}
}
