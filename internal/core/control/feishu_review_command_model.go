package control

import (
	"fmt"
	"strings"
)

type ReviewCommandTarget string

const (
	ReviewCommandTargetUncommitted ReviewCommandTarget = "uncommitted"
)

type ParsedReviewCommand struct {
	Target ReviewCommandTarget
}

func ParseFeishuReviewCommandText(text string) (ParsedReviewCommand, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ParsedReviewCommand{}, fmt.Errorf("当前仅支持 `/review uncommitted`。")
	}
	fields := strings.Fields(strings.ToLower(trimmed))
	if len(fields) < 2 || fields[0] != "/review" {
		return ParsedReviewCommand{}, fmt.Errorf("当前仅支持 `/review uncommitted`。")
	}
	if len(fields) != 2 {
		return ParsedReviewCommand{}, fmt.Errorf("当前仅支持 `/review uncommitted`。")
	}
	switch fields[1] {
	case string(ReviewCommandTargetUncommitted):
		return ParsedReviewCommand{Target: ReviewCommandTargetUncommitted}, nil
	default:
		return ParsedReviewCommand{}, fmt.Errorf("当前仅支持 `/review uncommitted`。")
	}
}
