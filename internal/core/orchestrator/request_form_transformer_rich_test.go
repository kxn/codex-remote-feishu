package orchestrator

import (
	"encoding/json"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestPureRequestFormClaudePlanKeepsStructuredScopedSelection(t *testing.T) {
	form, err := NewRequestForm(agentproto.BackendClaude, nil, map[string]any{"requestType": "approval", "requestMethod": "tool/ExitPlanMode", "body": "Create internal/new.go and modify docs/guide.md"}, "/workspace")
	if err != nil {
		t.Fatal(err)
	}
	opened, err := TransformRequestForm(form, HumanRequestFormAction{OptionID: "acceptForSession"})
	if err != nil || opened.Complete || opened.Response != nil || opened.NextForm.StructuredForm == nil {
		t.Fatalf("scope panel %#v %v", opened, err)
	}
	fields := opened.NextForm.StructuredForm.Fields
	if len(fields) != 3 {
		t.Fatal("plan authorization fields lost")
	}
	answers := map[string][]string{}
	for _, field := range fields {
		answers[field.Name] = field.DefaultValues
	}
	answers["directories"] = []string{"/not-offered"}
	if _, err = TransformRequestForm(opened.NextForm, HumanRequestFormAction{Answers: answers}); err == nil {
		t.Fatal("forged authorization directory accepted")
	}
	answers["directories"] = fields[1].DefaultValues
	done, err := TransformRequestForm(opened.NextForm, HumanRequestFormAction{Answers: answers})
	if err != nil || !done.Complete {
		t.Fatalf("scoped plan %#v %v", done, err)
	}
	selection := done.Response["permissionSelection"].(map[string]any)
	if selection["scope"] != "session" || selection["grant_level"] != "scoped_rules" || len(selection["directories"].([]string)) == 0 {
		t.Fatalf("scoped selection %#v", selection)
	}
	if form.StructuredForm != nil || opened.NextForm.StructuredForm == nil {
		t.Fatal("pure transition mutated its earlier state")
	}
}
func TestPureRequestFormMCPRetainsSchemaTypesAndPersistRestrictions(t *testing.T) {
	schema := map[string]any{"type": "object", "properties": map[string]any{"count": map[string]any{"type": "integer"}, "enabled": map[string]any{"type": "boolean"}, "mode": map[string]any{"type": "string", "enum": []any{"safe", "fast"}}}, "required": []any{"count", "enabled", "mode"}}
	prompt := &agentproto.RequestPrompt{Type: agentproto.RequestTypeMCPServerElicitation, MCPElicitation: &agentproto.MCPElicitationPrompt{Mode: "form", RequestedSchema: schema, Meta: map[string]any{"source": "mcp-server"}}}
	form, err := NewRequestForm(agentproto.BackendCodex, prompt, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = TransformRequestForm(form, HumanRequestFormAction{Answers: map[string][]string{"count": {"not-a-number"}, "enabled": {"true"}, "mode": {"safe"}}}); err == nil {
		t.Fatal("invalid MCP integer accepted")
	}
	done, err := TransformRequestForm(form, HumanRequestFormAction{Answers: map[string][]string{"count": {"3"}, "enabled": {"true"}, "mode": {"safe"}}})
	if err != nil || !done.Complete {
		t.Fatalf("MCP typed response %#v %v", done, err)
	}
	raw, _ := json.Marshal(done.Response["content"])
	if string(raw) != `{"count":3,"enabled":true,"mode":"safe"}` {
		t.Fatalf("MCP payload types lost: %s", raw)
	}
	for _, persist := range []string{"session", "always"} {
		approval := &agentproto.RequestPrompt{Type: agentproto.RequestTypeMCPServerElicitation, MCPElicitation: &agentproto.MCPElicitationPrompt{Meta: map[string]any{"codex_approval_kind": "mcp_tool_call", "persist": []any{persist}}}}
		grant, err := NewRequestForm(agentproto.BackendCodex, approval, nil, "")
		if err != nil {
			t.Fatal(err)
		}
		result, err := TransformRequestForm(grant, HumanRequestFormAction{OptionID: "acceptForSession"})
		if persist == "always" {
			if err == nil {
				t.Fatal("unsupported session/persistent grant manufactured")
			}
		} else if err != nil || !result.Complete {
			t.Fatalf("advertised session grant %#v %v", result, err)
		}
	}
}
func TestPureRequestFormFeedbackPreservesClaudeReviseAndCodexFollowup(t *testing.T) {
	cases := []struct {
		backend                agentproto.Backend
		kind, option, decision string
		followup               bool
	}{
		{agentproto.BackendClaude, control.RequestSemanticPlanConfirmation, "revise", "revise", false},
		{agentproto.BackendClaude, control.RequestSemanticApprovalCanUseTool, "captureFeedback", "decline", false},
		{agentproto.BackendCodex, control.RequestSemanticApprovalCommand, "captureFeedback", "decline", true},
	}
	for _, test := range cases {
		form, err := NewRequestForm(test.backend, nil, map[string]any{"requestType": "approval", "requestKind": test.kind}, "/workspace")
		if err != nil {
			t.Fatal(err)
		}
		capture, err := TransformRequestForm(form, HumanRequestFormAction{OptionID: test.option})
		if err != nil || capture.Complete || capture.Capture == nil {
			t.Fatalf("feedback capture %#v %v", capture, err)
		}
		done, err := TransformRequestForm(capture.NextForm, HumanRequestFormAction{Feedback: "Please change the proposed operation."})
		if err != nil || !done.Complete || done.Response["decision"] != test.decision {
			t.Fatalf("feedback response %#v %v", done, err)
		}
		if test.followup {
			if done.FollowupHumanInput == "" || done.Response["message"] != nil {
				t.Fatal("Codex followup input collapsed into unsupported same-request feedback")
			}
		} else if done.Response["message"] == nil || done.FollowupHumanInput != "" {
			t.Fatal("Claude same-request feedback was split into another native execution")
		}
	}
}
