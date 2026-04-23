package feishu

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type driveAPIError struct {
	API       string
	Code      int
	Msg       string
	RequestID string
	LogID     string
}

func (e *driveAPIError) Error() string {
	if e == nil {
		return ""
	}
	if strings.TrimSpace(e.Msg) == "" {
		return fmt.Sprintf("feishu drive api error %d", e.Code)
	}
	return fmt.Sprintf("feishu drive api error %d: %s", e.Code, strings.TrimSpace(e.Msg))
}

func isPreviewDriveAccessDeniedError(err error) bool {
	var apiErr *driveAPIError
	if !errors.As(err, &apiErr) {
		return false
	}
	switch apiErr.Code {
	case 99991672:
		return true
	default:
		return false
	}
}

func parsePreviewRemoteTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	if value, err := strconv.ParseInt(raw, 10, 64); err == nil {
		switch {
		case value > 1_000_000_000_000:
			return time.UnixMilli(value).UTC()
		case value > 0:
			return time.Unix(value, 0).UTC()
		default:
			return time.Time{}
		}
	}
	if value, err := time.Parse(time.RFC3339, raw); err == nil {
		return value.UTC()
	}
	return time.Time{}
}
