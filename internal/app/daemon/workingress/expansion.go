package workingress

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"strings"
)

type DefaultExpander struct{}

func (DefaultExpander) Expand(_ context.Context, envelope work.RawIngressEnvelope) (ExpandedMessage, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(envelope.RawContent), &raw); err != nil {
		return ExpandedMessage{}, err
	}
	result := ExpandedMessage{Complete: true}
	if envelope.MessageType == "text" {
		if err := json.Unmarshal(raw["text"], &result.Text); err != nil {
			return result, err
		}
		for _, mention := range envelope.Mentions {
			if mention.Key != "" {
				name := mention.Name
				if name == "" {
					name = "提及对象"
				}
				result.Text = strings.ReplaceAll(result.Text, mention.Key, "@"+name)
			}
		}
	} else {
		return fallbackExpansion(envelope, "附件或富内容尚未展开；保留原始平台引用，请勿把未读取内容当作已知事实。"), nil
	}
	if envelope.ParentID != "" {
		result.Complete = false
		result.Issues = append(result.Issues, "引用消息尚未展开；本消息正文已保存。")
	}
	return result, nil
}
func fallbackExpansion(envelope work.RawIngressEnvelope, issue string) ExpandedMessage {
	text := envelope.RawContent
	var raw struct {
		Text string `json:"text"`
	}
	if json.Unmarshal([]byte(envelope.RawContent), &raw) == nil && raw.Text != "" {
		text = raw.Text
	}
	return ExpandedMessage{Text: fmt.Sprintf("%s\n\n[部分内容未获取] %s\n原始平台消息：%s；类型：%s", text, issue, envelope.MessageID, envelope.MessageType), Complete: false, Issues: []string{issue}}
}
