package feishu

import (
	"fmt"
	"strings"
)

type EventSubscriptionTestCardRequest struct {
	GatewayID       string
	ReceiveID       string
	ReceiveIDType   string
	AttentionUserID string
	EventConsoleURL string
	Events          []string
	Phrase          string
}

type CallbackTestCardRequest struct {
	GatewayID       string
	ReceiveID       string
	ReceiveIDType   string
	AttentionUserID string
	CallbackURL     string
	Callbacks       []string
	CallbackValue   map[string]any
}

func EventSubscriptionTestCardOperation(req EventSubscriptionTestCardRequest) Operation {
	return Operation{
		Kind:            OperationSendCard,
		GatewayID:       strings.TrimSpace(req.GatewayID),
		ReceiveID:       strings.TrimSpace(req.ReceiveID),
		ReceiveIDType:   strings.TrimSpace(req.ReceiveIDType),
		AttentionUserID: strings.TrimSpace(req.AttentionUserID),
		CardTitle:       "事件订阅测试",
		CardThemeKey:    "info",
		CardElements: []map[string]any{
			{
				"tag":     "markdown",
				"content": fmt.Sprintf("我们开始测试飞书事件订阅，请确保机器人在 [飞书后台](%s) 配置订阅方式为长连接，并添加以下事件：", strings.TrimSpace(req.EventConsoleURL)),
			},
			cardPlainTextBlockElement(strings.Join(trimCardLines(req.Events), "\n")),
			cardPlainTextBlockElement(fmt.Sprintf(
				"请在这里回复“%s”，保证我能收到消息。\n如果我没有回应，请去飞书后台确认增加事件配置以后是否发布了新版本。需要发布以后才会生效。",
				strings.TrimSpace(req.Phrase),
			)),
		},
	}
}

func CallbackTestCardOperation(req CallbackTestCardRequest) Operation {
	return Operation{
		Kind:            OperationSendCard,
		GatewayID:       strings.TrimSpace(req.GatewayID),
		ReceiveID:       strings.TrimSpace(req.ReceiveID),
		ReceiveIDType:   strings.TrimSpace(req.ReceiveIDType),
		AttentionUserID: strings.TrimSpace(req.AttentionUserID),
		CardTitle:       "回调测试",
		CardThemeKey:    "info",
		CardElements: []map[string]any{
			{
				"tag":     "markdown",
				"content": fmt.Sprintf("我们开始测试飞书回调配置，请确保机器人在 [飞书后台](%s) 配置回调订阅方式为长连接，并添加以下回调：", strings.TrimSpace(req.CallbackURL)),
			},
			cardPlainTextBlockElement(strings.Join(trimCardLines(req.Callbacks), "\n")),
			cardPlainTextBlockElement("请点击下方按钮完成验证。\n如果没有响应，请去飞书后台确认增加回调配置以后是否发布了新版本。需要发布以后才会生效。"),
			{
				"tag":  "button",
				"type": "primary",
				"text": cardPlainText("点此测试回调"),
				"behaviors": []map[string]any{{
					"type":  "callback",
					"value": req.CallbackValue,
				}},
			},
		},
	}
}

func trimCardLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}
