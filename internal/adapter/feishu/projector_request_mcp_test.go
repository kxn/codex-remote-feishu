package feishu

import (
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestProjectPermissionsRequestPromptAsCard(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventFeishuDirectRequestPrompt,
		FeishuDirectRequestPrompt: &control.FeishuDirectRequestPrompt{
			RequestID:   "req-perm-1",
			RequestType: "permissions_request_approval",
			Title:       "需要授予权限",
			Body:        "申请权限：\n- Read docs (`docs.read`)",
			Options: []control.RequestPromptOption{
				{OptionID: "accept", Label: "允许本次", Style: "primary"},
				{OptionID: "acceptForSession", Label: "本会话允许", Style: "default"},
				{OptionID: "decline", Label: "拒绝", Style: "default"},
			},
		},
	})

	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	row := cardElementButtons(t, ops[0].CardElements[0])
	if len(row) != 3 {
		t.Fatalf("expected three permission action buttons, got %#v", ops[0].CardElements[0])
	}
	acceptValue := cardButtonPayload(t, row[0])
	sessionValue := cardButtonPayload(t, row[1])
	declineValue := cardButtonPayload(t, row[2])
	if acceptValue["request_option_id"] != "accept" || sessionValue["request_option_id"] != "acceptForSession" || declineValue["request_option_id"] != "decline" {
		t.Fatalf("unexpected permission request payloads: %#v %#v %#v", acceptValue, sessionValue, declineValue)
	}
	if got := markdownContent(ops[0].CardElements[1]); !strings.Contains(got, "当前会话内持续授权") {
		t.Fatalf("unexpected permission hint: %#v", ops[0].CardElements[1])
	}
}

func TestProjectMCPElicitationFormPromptAsCard(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventFeishuDirectRequestPrompt,
		FeishuDirectRequestPrompt: &control.FeishuDirectRequestPrompt{
			RequestID:       "req-mcp-form-1",
			RequestType:     "mcp_server_elicitation",
			RequestRevision: 5,
			Title:           "需要处理 MCP 请求",
			Questions: []control.RequestPromptQuestion{
				{
					ID:             "mode",
					Header:         "模式",
					Question:       "选择执行模式（必填）",
					DirectResponse: true,
					Options: []control.RequestPromptQuestionOption{
						{Label: "auto"},
						{Label: "manual"},
					},
				},
				{
					ID:          "token",
					Header:      "Token",
					Question:    "填写 OAuth token（必填）",
					AllowOther:  true,
					Placeholder: "请填写 token",
				},
			},
			Options: []control.RequestPromptOption{
				{OptionID: "decline", Label: "拒绝", Style: "default"},
				{OptionID: "cancel", Label: "取消", Style: "default"},
			},
		},
	})

	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if got := markdownContent(ops[0].CardElements[0]); !strings.Contains(got, "填写进度") || !strings.Contains(got, "0/2") {
		t.Fatalf("expected mcp form progress markdown, got %#v", ops[0].CardElements[0])
	}
	optionRow := cardElementButtons(t, ops[0].CardElements[2])
	if len(optionRow) != 2 {
		t.Fatalf("expected direct response buttons for first field, got %#v", ops[0].CardElements[2])
	}
	optionValue := cardButtonPayload(t, optionRow[0])
	requestAnswers, _ := optionValue["request_answers"].(map[string]any)
	modeAnswers, _ := requestAnswers["mode"].([]any)
	if optionValue["kind"] != "request_respond" || len(modeAnswers) != 1 || modeAnswers[0] != "auto" {
		t.Fatalf("unexpected direct response payload: %#v", optionValue)
	}
	form, _ := ops[0].CardElements[4]["elements"].([]map[string]any)
	if len(form) != 2 {
		t.Fatalf("expected one mcp form input and one submit button, got %#v", ops[0].CardElements[4])
	}
	if label := cardButtonLabel(t, form[1]); label != "提交并继续" {
		t.Fatalf("unexpected mcp form submit label: %#v", form[1])
	}
	submitValue := cardButtonPayload(t, form[1])
	if submitValue["request_option_id"] != "submit" || submitValue["request_revision"] != 5 {
		t.Fatalf("unexpected mcp form submit payload: %#v", submitValue)
	}
	terminalRow := cardElementButtons(t, ops[0].CardElements[5])
	if len(terminalRow) != 2 {
		t.Fatalf("expected decline/cancel row, got %#v", ops[0].CardElements[5])
	}
	if got := cardButtonLabel(t, terminalRow[0]); got != "拒绝" {
		t.Fatalf("unexpected terminal action row: %#v", ops[0].CardElements[5])
	}
}
