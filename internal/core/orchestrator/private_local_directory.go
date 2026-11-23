package orchestrator

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

// CompletePrivateLocalDirectory consumes a verified durable host effect result.
// It validates the exact frozen picker/path and never performs filesystem I/O.
func (s *Service) CompletePrivateLocalDirectory(surfaceID, pickerID, path string, succeeded bool) ([]eventcontract.Event, error) {
	if !s.config.PrivateApplication {
		return nil, fmt.Errorf("private service required")
	}
	surface := s.root.Surfaces[surfaceID]
	if surface == nil {
		return nil, fmt.Errorf("private surface missing")
	}
	flow, record := s.activeOwnerCardFlow(surface), s.activeTargetPicker(surface)
	if flow == nil || record == nil || record.PickerID != pickerID || record.PendingKind != targetPickerPendingPrivateMkdir || record.PendingWorkspaceKey != normalizeWorkspaceClaimKey(path) || strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("private directory result does not match pending picker")
	}
	if !succeeded {
		return s.finishTargetPickerWithStage(surface, flow, record, control.FeishuTargetPickerStageFailed, "创建失败", "创建目标目录失败，请检查权限后重试。", false, nil), nil
	}
	return s.finishPrivateLocalDirectory(surface, flow, record, path), nil
}
