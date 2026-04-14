package state

import (
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

type ActiveThreadHistoryRecord struct {
	PickerID    string
	OwnerUserID string
	ThreadID    string
	MessageID   string
	ViewMode    control.FeishuThreadHistoryViewMode
	Page        int
	TurnID      string
	CreatedAt   time.Time
	ExpiresAt   time.Time
}
