package control

import (
	"fmt"
	"strings"
)

type ReviewCommandMode string

const (
	ReviewCommandModeCommitPicker ReviewCommandMode = "commit_picker"
	ReviewCommandModeCommitSHA    ReviewCommandMode = "commit_sha"
	ReviewCommandModeUncommitted  ReviewCommandMode = "uncommitted"
	ReviewCommandModeCancel       ReviewCommandMode = "cancel"
)

type ParsedReviewCommand struct {
	Mode      ReviewCommandMode
	CommitSHA string
}

func ParseFeishuReviewCommandText(text string) (ParsedReviewCommand, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ParsedReviewCommand{}, reviewCommandUsageError()
	}
	fields := strings.Fields(trimmed)
	if len(fields) == 0 || strings.ToLower(fields[0]) != "/review" {
		return ParsedReviewCommand{}, reviewCommandUsageError()
	}
	if len(fields) == 1 {
		return ParsedReviewCommand{Mode: ReviewCommandModeCommitPicker}, nil
	}

	switch strings.ToLower(fields[1]) {
	case "uncommitted":
		if len(fields) != 2 {
			return ParsedReviewCommand{}, reviewCommandUsageError()
		}
		return ParsedReviewCommand{Mode: ReviewCommandModeUncommitted}, nil
	case "commit":
		switch len(fields) {
		case 2:
			return ParsedReviewCommand{Mode: ReviewCommandModeCommitPicker}, nil
		case 3:
			sha := strings.TrimSpace(strings.ToLower(fields[2]))
			if !isReviewCommitPrefix(sha) {
				return ParsedReviewCommand{}, fmt.Errorf("commit SHA 需要是 7 到 40 位十六进制字符。")
			}
			return ParsedReviewCommand{
				Mode:      ReviewCommandModeCommitSHA,
				CommitSHA: sha,
			}, nil
		default:
			return ParsedReviewCommand{}, reviewCommandUsageError()
		}
	case "cancel":
		if len(fields) != 2 {
			return ParsedReviewCommand{}, reviewCommandUsageError()
		}
		return ParsedReviewCommand{Mode: ReviewCommandModeCancel}, nil
	default:
		return ParsedReviewCommand{}, reviewCommandUsageError()
	}
}

func reviewCommandUsageError() error {
	return fmt.Errorf("支持的用法：`/review`、`/review uncommitted`、`/review commit`、`/review commit <sha>`。")
}

func isReviewCommitPrefix(value string) bool {
	if len(value) < 7 || len(value) > 40 {
		return false
	}
	for _, r := range value {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		case r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}
