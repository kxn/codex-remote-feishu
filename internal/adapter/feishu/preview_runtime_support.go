package feishu

import (
	"strconv"
	"strings"
	"time"
)

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
