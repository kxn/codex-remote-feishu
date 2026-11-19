package orchestrator

import (
	"strings"
	"unicode/utf8"
)

// A question message outside the bounded interactive shape uses ordinary text,
// preserving every title and option instead of sending an unusable large card.
func asyncUserInputFitsCard(questions []asyncUserInputQuestion) bool {
	if len(questions) > 16 {
		return false
	}
	textRunes := 0
	for _, question := range questions {
		if len(question.Options) > 16 {
			return false
		}
		textRunes += utf8.RuneCountInString(question.Title)
		for _, option := range question.Options {
			textRunes += utf8.RuneCountInString(option)
		}
	}
	return textRunes <= 1000
}

func asyncUserInputTextFallback(context string, questions []asyncUserInputQuestion) string {
	var text strings.Builder
	text.WriteString(context)
	for _, question := range questions {
		text.WriteString("\n\n问题：")
		text.WriteString(question.Title)
		for _, option := range question.Options {
			text.WriteString("\n- ")
			text.WriteString(option)
		}
	}
	text.WriteString("\n\n问题内容较长，请在当前会话直接回复。")
	return text.String()
}
