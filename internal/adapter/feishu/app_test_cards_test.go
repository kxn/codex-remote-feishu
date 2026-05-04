package feishu

import (
	"reflect"
	"testing"
)

func TestEventSubscriptionTestCardOperation(t *testing.T) {
	op := EventSubscriptionTestCardOperation(EventSubscriptionTestCardRequest{
		GatewayID:       " app-1 ",
		ReceiveID:       " open-id ",
		ReceiveIDType:   "open_id",
		AttentionUserID: " ou-user ",
		EventConsoleURL: "https://open.feishu.cn/app/cli_xxx/event?tab=event",
		Events:          []string{" im.message.receive_v1 ", " application.bot.menu_v6 "},
		Phrase:          "测试",
	})

	if op.Kind != OperationSendCard || op.GatewayID != "app-1" || op.ReceiveID != "open-id" || op.AttentionUserID != "ou-user" {
		t.Fatalf("unexpected operation envelope: %#v", op)
	}
	if op.CardTitle != "事件订阅测试" || op.CardThemeKey != "info" {
		t.Fatalf("unexpected card header: %#v", op)
	}
	if len(op.CardElements) != 3 {
		t.Fatalf("expected three card elements, got %#v", op.CardElements)
	}
	if got := markdownElementContent(op.CardElements[0]); got != "我们开始测试飞书事件订阅，请确保机器人在 [飞书后台](https://open.feishu.cn/app/cli_xxx/event?tab=event) 配置订阅方式为长连接，并添加以下事件：" {
		t.Fatalf("unexpected intro markdown: %q", got)
	}
	if got := plainTextElementContent(op.CardElements[1]); got != "im.message.receive_v1\napplication.bot.menu_v6" {
		t.Fatalf("unexpected events plain text: %q", got)
	}
	if got := plainTextElementContent(op.CardElements[2]); got != "请在这里回复“测试”，保证我能收到消息。\n如果我没有回应，请去飞书后台确认增加事件配置以后是否发布了新版本。需要发布以后才会生效。" {
		t.Fatalf("unexpected instruction plain text: %q", got)
	}
}

func TestCallbackTestCardOperation(t *testing.T) {
	callbackValue := map[string]any{"action": "feishu_app_test_callback"}
	op := CallbackTestCardOperation(CallbackTestCardRequest{
		GatewayID:       "app-1",
		ReceiveID:       "open-id",
		ReceiveIDType:   "open_id",
		AttentionUserID: "ou-user",
		CallbackURL:     "https://open.feishu.cn/app/cli_xxx/event?tab=callback",
		Callbacks:       []string{"card.action.trigger"},
		CallbackValue:   callbackValue,
	})

	if op.Kind != OperationSendCard || op.CardTitle != "回调测试" || op.CardThemeKey != "info" {
		t.Fatalf("unexpected card operation: %#v", op)
	}
	if len(op.CardElements) != 4 {
		t.Fatalf("expected four card elements, got %#v", op.CardElements)
	}
	if got := markdownElementContent(op.CardElements[0]); got != "我们开始测试飞书回调配置，请确保机器人在 [飞书后台](https://open.feishu.cn/app/cli_xxx/event?tab=callback) 配置回调订阅方式为长连接，并添加以下回调：" {
		t.Fatalf("unexpected intro markdown: %q", got)
	}
	if got := plainTextElementContent(op.CardElements[1]); got != "card.action.trigger" {
		t.Fatalf("unexpected callbacks plain text: %q", got)
	}
	button := op.CardElements[3]
	if button["tag"] != "button" || button["type"] != "primary" {
		t.Fatalf("unexpected callback button: %#v", button)
	}
	behaviors, _ := button["behaviors"].([]map[string]any)
	if len(behaviors) != 1 || !reflect.DeepEqual(behaviors[0]["value"], callbackValue) {
		t.Fatalf("unexpected callback button behavior: %#v", button)
	}
}

func markdownElementContent(element map[string]any) string {
	value, _ := element["content"].(string)
	return value
}

func plainTextElementContent(element map[string]any) string {
	text, _ := element["text"].(map[string]any)
	value, _ := text["content"].(string)
	return value
}
