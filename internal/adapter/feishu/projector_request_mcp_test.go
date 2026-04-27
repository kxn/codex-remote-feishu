package feishu

import (
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
)

func TestProjectPermissionsRequestPromptAsCard(t *testing.T) {
	projector := NewProjector()
	ops := projector.ProjectEvent("chat-1", requestPromptEvent(control.FeishuRequestView{
		RequestID:   "req-perm-1",
		RequestType: "permissions_request_approval",
		Title:       "需要授予权限",
		Sections: []control.FeishuCardTextSection{
			{Lines: []string{"本地 Codex 正在等待授予附加权限。"}},
			{Label: "申请权限", Lines: []string{"- Read docs (`docs.read`)"}},
		},
		Options: []control.RequestPromptOption{
			{OptionID: "accept", Label: "允许本次", Style: "primary"},
			{OptionID: "acceptForSession", Label: "本会话允许", Style: "default"},
			{OptionID: "decline", Label: "拒绝", Style: "default"},
		},
	}))

	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if ops[0].CardBody != "" {
		t.Fatalf("expected permissions prompt body to stay empty, got %#v", ops[0])
	}
	if got := plainTextContent(ops[0].CardElements[0]); !strings.Contains(got, "本地 Codex 正在等待授予附加权限。") {
		t.Fatalf("unexpected permissions intro section: %#v", ops[0].CardElements[0])
	}
	if got := plainTextContent(ops[0].CardElements[2]); !strings.Contains(got, "Read docs (`docs.read`)") {
		t.Fatalf("expected permission names to stay in plain_text, got %#v", ops[0].CardElements[2])
	}
	row := cardElementButtons(t, ops[0].CardElements[3])
	if len(row) != 3 {
		t.Fatalf("expected three permission action buttons, got %#v", ops[0].CardElements[3])
	}
	acceptValue := cardButtonPayload(t, row[0])
	sessionValue := cardButtonPayload(t, row[1])
	declineValue := cardButtonPayload(t, row[2])
	if acceptValue["request_option_id"] != "accept" || sessionValue["request_option_id"] != "acceptForSession" || declineValue["request_option_id"] != "decline" {
		t.Fatalf("unexpected permission request payloads: %#v %#v %#v", acceptValue, sessionValue, declineValue)
	}
	if got := markdownContent(ops[0].CardElements[4]); !strings.Contains(got, "当前会话内持续授权") {
		t.Fatalf("unexpected permission hint: %#v", ops[0].CardElements[4])
	}
}

func TestProjectMCPElicitationFormPromptAsCard(t *testing.T) {
	projector := NewProjector()
	ops := projector.ProjectEvent("chat-1", requestPromptEvent(control.FeishuRequestView{
		RequestID:       "req-mcp-form-1",
		RequestType:     "mcp_server_elicitation",
		RequestRevision: 5,
		Title:           "需要处理 MCP 请求",
		Sections: []control.FeishuCardTextSection{{
			Lines: []string{"请补充返回内容", "MCP 服务：docs", "授权页面：https://example.com/approve?next=`token`"},
		}},
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
	}))

	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if ops[0].CardBody != "" {
		t.Fatalf("expected mcp prompt body to stay empty, got %#v", ops[0])
	}
	if got := plainTextContent(ops[0].CardElements[0]); !containsAll(got, "请补充返回内容", "MCP 服务：docs", "https://example.com/approve?next=`token`") {
		t.Fatalf("expected mcp intro section to stay plain_text, got %#v", ops[0].CardElements[0])
	}
	if got := markdownContent(ops[0].CardElements[1]); !strings.Contains(got, "填写进度") || !strings.Contains(got, "0/2") {
		t.Fatalf("expected mcp form progress markdown, got %#v", ops[0].CardElements[1])
	}
	if got := markdownContent(ops[0].CardElements[2]); !strings.Contains(got, "问题 1") {
		t.Fatalf("expected first mcp question heading, got %#v", ops[0].CardElements[2])
	}
	if got := plainTextContent(ops[0].CardElements[3]); !containsAll(got, "标题：模式", "说明：", "选择执行模式（必填）", "可选项：", "- auto") {
		t.Fatalf("expected first mcp question body to stay plain_text, got %#v", ops[0].CardElements[3])
	}
	optionValue := cardButtonPayload(t, ops[0].CardElements[4])
	requestAnswers, _ := optionValue["request_answers"].(map[string]any)
	modeAnswers, _ := requestAnswers["mode"].([]any)
	if optionValue["kind"] != "request_respond" || len(modeAnswers) != 1 || modeAnswers[0] != "auto" {
		t.Fatalf("unexpected direct response payload: %#v", optionValue)
	}
	if optionValue["request_revision"] != 5 {
		t.Fatalf("expected direct response payload to carry request revision, got %#v", optionValue)
	}
	secondOptionValue := cardButtonPayload(t, ops[0].CardElements[5])
	secondRequestAnswers, _ := secondOptionValue["request_answers"].(map[string]any)
	secondModeAnswers, _ := secondRequestAnswers["mode"].([]any)
	if secondOptionValue["kind"] != "request_respond" || len(secondModeAnswers) != 1 || secondModeAnswers[0] != "manual" {
		t.Fatalf("unexpected second direct response payload: %#v", secondOptionValue)
	}
	if ops[0].CardElements[6]["tag"] != "hr" {
		t.Fatalf("expected cancel footer divider, got %#v", ops[0].CardElements[6])
	}
	cancelValue := cardButtonPayload(t, ops[0].CardElements[7])
	if cancelValue["kind"] != "request_control" || cancelValue["request_control"] != "cancel_request" || cancelValue["request_revision"] != 5 {
		t.Fatalf("unexpected mcp cancel payload: %#v", cancelValue)
	}
	for _, action := range cardActionsFromElements(ops[0].CardElements) {
		value := cardValueMap(action)
		if value["request_option_id"] == "submit" || value["request_option_id"] == "decline" {
			t.Fatalf("did not expect explicit submit/decline payloads in auto-advance mcp prompt, got %#v", ops[0].CardElements)
		}
	}
}

func TestProjectMCPElicitationFormPromptRendersCurrentFormFieldAsSingleStepForm(t *testing.T) {
	projector := NewProjector()
	ops := projector.ProjectEvent("chat-1", requestPromptEvent(control.FeishuRequestView{
		RequestID:            "req-mcp-form-2",
		RequestType:          "mcp_server_elicitation",
		RequestRevision:      6,
		CurrentQuestionIndex: 1,
		Questions: []control.RequestPromptQuestion{
			{
				ID:             "mode",
				Header:         "模式",
				Question:       "选择执行模式（必填）",
				Answered:       true,
				DefaultValue:   "auto",
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
	}))
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if got := markdownContent(ops[0].CardElements[0]); !strings.Contains(got, "填写进度") || !strings.Contains(got, "1/2") || !strings.Contains(got, "当前第 2 题") {
		t.Fatalf("expected step-aware mcp progress, got %#v", ops[0].CardElements[0])
	}
	var form map[string]any
	for _, element := range ops[0].CardElements {
		if cardStringValue(element["tag"]) == "form" {
			form = element
			break
		}
	}
	if form["tag"] != "form" {
		t.Fatalf("expected current-step mcp form, got %#v", form)
	}
	formElements, _ := form["elements"].([]map[string]any)
	if len(formElements) != 1 || formElements[0]["tag"] != "column_set" {
		t.Fatalf("expected compact single-row mcp form, got %#v", form)
	}
	columns, _ := formElements[0]["columns"].([]map[string]any)
	if len(columns) != 2 {
		t.Fatalf("expected compact row to keep input+submit columns, got %#v", formElements[0])
	}
	rowRightElements, _ := columns[1]["elements"].([]map[string]any)
	if len(rowRightElements) != 1 || rowRightElements[0]["tag"] != "button" || cardButtonLabel(t, rowRightElements[0]) != "提交" {
		t.Fatalf("unexpected compact mcp submit row: %#v", formElements[0])
	}
	if rowRightElements[0]["disabled"] == true {
		t.Fatalf("expected mcp form submit button to stay clickable for submit-time validation, got %#v", rowRightElements[0])
	}
	saveValue := renderedButtonCallbackValue(t, rowRightElements[0])
	if saveValue["kind"] != "submit_request_form" || saveValue["request_option_id"] != nil || saveValue["request_revision"] != 6 {
		t.Fatalf("unexpected compact mcp submit payload: %#v", saveValue)
	}
}

func TestProjectPermissionsRequestPromptSealedStateDropsActions(t *testing.T) {
	projector := NewProjector()
	ops := projector.ProjectEvent("chat-1", requestPromptEvent(control.FeishuRequestView{
		RequestID:   "req-perm-2",
		RequestType: "permissions_request_approval",
		Title:       "需要授予权限",
		Phase:       frontstagecontract.PhaseWaitingDispatch,
		StatusText:  "已提交当前请求，等待 Codex 继续。",
		Sections: []control.FeishuCardTextSection{
			{Lines: []string{"本地 Codex 正在等待授予附加权限。"}},
		},
		Options: []control.RequestPromptOption{
			{OptionID: "accept", Label: "允许本次", Style: "primary"},
			{OptionID: "acceptForSession", Label: "本会话允许", Style: "default"},
			{OptionID: "decline", Label: "拒绝", Style: "default"},
		},
	}))
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if len(cardActionsFromElements(ops[0].CardElements)) != 0 {
		t.Fatalf("expected sealed permissions prompt to drop all actions, got %#v", ops[0].CardElements)
	}
	if got := markdownContent(ops[0].CardElements[len(ops[0].CardElements)-1]); !strings.Contains(got, "已提交当前请求") {
		t.Fatalf("expected sealed permissions prompt to render waiting status, got %#v", ops[0].CardElements)
	}
}

func TestProjectMCPElicitationURLPromptSealedStateDropsActions(t *testing.T) {
	projector := NewProjector()
	ops := projector.ProjectEvent("chat-1", requestPromptEvent(control.FeishuRequestView{
		RequestID:   "req-mcp-url-2",
		RequestType: "mcp_server_elicitation",
		Title:       "需要处理 MCP 请求",
		Phase:       frontstagecontract.PhaseWaitingDispatch,
		StatusText:  "已提交当前请求，等待 Codex 继续。",
		Sections: []control.FeishuCardTextSection{{
			Lines: []string{"请完成外部授权", "授权页面：https://example.com/approve"},
		}},
		Options: []control.RequestPromptOption{
			{OptionID: "accept", Label: "继续", Style: "primary"},
			{OptionID: "decline", Label: "拒绝", Style: "default"},
			{OptionID: "cancel", Label: "取消", Style: "default"},
		},
	}))
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if len(cardActionsFromElements(ops[0].CardElements)) != 0 {
		t.Fatalf("expected sealed url elicitation prompt to drop all actions, got %#v", ops[0].CardElements)
	}
	if got := markdownContent(ops[0].CardElements[len(ops[0].CardElements)-1]); !strings.Contains(got, "已提交当前请求") {
		t.Fatalf("expected sealed url elicitation prompt to render waiting status, got %#v", ops[0].CardElements)
	}
}

func TestProjectToolCallbackPromptStaysReadOnly(t *testing.T) {
	projector := NewProjector()
	ops := projector.ProjectEvent("chat-1", requestPromptEvent(control.FeishuRequestView{
		RequestID:   "req-tool-1",
		RequestType: "tool_callback",
		Title:       "工具回调暂不支持",
		Phase:       frontstagecontract.PhaseWaitingDispatch,
		StatusText:  "当前客户端不支持执行该工具回调，已自动上报 unsupported 结果，等待 Codex 继续。",
		Sections: []control.FeishuCardTextSection{
			{Lines: []string{"当前工具请求客户端执行一段 dynamic tool callback。", "此 relay/headless 客户端暂不支持直接执行，系统已自动回报 unsupported 结果。"}},
			{Label: "回调信息", Lines: []string{"工具：lookup_ticket", "Call ID：call-1"}},
			{Label: "回调参数", Lines: []string{`{"ticket":"ABC-123"}`}},
		},
	}))
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if len(cardActionsFromElements(ops[0].CardElements)) != 0 {
		t.Fatalf("expected tool callback prompt to remain read-only, got %#v", ops[0].CardElements)
	}
	if got := plainTextContent(ops[0].CardElements[0]); !containsAll(got, "当前工具请求客户端执行一段 dynamic tool callback。", "系统已自动回报 unsupported 结果。") {
		t.Fatalf("unexpected tool callback intro section: %#v", ops[0].CardElements[0])
	}
	if got := markdownContent(ops[0].CardElements[len(ops[0].CardElements)-1]); !strings.Contains(got, "已自动上报 unsupported") {
		t.Fatalf("expected tool callback status markdown, got %#v", ops[0].CardElements)
	}
}
