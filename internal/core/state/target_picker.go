package state

import (
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

type ActiveTargetPickerRecord struct {
	PickerID             string
	OwnerUserID          string
	Source               control.TargetPickerRequestSource
	SelectedMode         control.FeishuTargetPickerMode
	SelectedSource       control.FeishuTargetPickerSourceKind
	SelectedWorkspaceKey string
	SelectedSessionValue string
	CreatedAt            time.Time
	ExpiresAt            time.Time
}
