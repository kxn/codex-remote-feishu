package surfaceresume

import (
	"strings"
	"time"
)

type HeadlessRestoreHint struct {
	SurfaceSessionID string    `json:"surfaceSessionID"`
	GatewayID        string    `json:"gatewayID,omitempty"`
	ChatID           string    `json:"chatID,omitempty"`
	ActorUserID      string    `json:"actorUserID,omitempty"`
	ThreadID         string    `json:"threadID"`
	ThreadTitle      string    `json:"threadTitle,omitempty"`
	ThreadCWD        string    `json:"threadCWD,omitempty"`
	UpdatedAt        time.Time `json:"updatedAt,omitempty"`
}

func NormalizeHeadlessRestoreHint(hint HeadlessRestoreHint) (HeadlessRestoreHint, bool) {
	hint.SurfaceSessionID = strings.TrimSpace(hint.SurfaceSessionID)
	hint.GatewayID = strings.TrimSpace(hint.GatewayID)
	hint.ChatID = strings.TrimSpace(hint.ChatID)
	hint.ActorUserID = strings.TrimSpace(hint.ActorUserID)
	hint.ThreadID = strings.TrimSpace(hint.ThreadID)
	hint.ThreadCWD = strings.TrimSpace(hint.ThreadCWD)
	hint.ThreadTitle = NormalizeThreadTitle(hint.ThreadTitle, hint.ThreadID, hint.ThreadCWD, "")
	if hint.SurfaceSessionID == "" || hint.ThreadID == "" {
		return HeadlessRestoreHint{}, false
	}
	if !hint.UpdatedAt.IsZero() {
		hint.UpdatedAt = hint.UpdatedAt.UTC()
	}
	return hint, true
}
