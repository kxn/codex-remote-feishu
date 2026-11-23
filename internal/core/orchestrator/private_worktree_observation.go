package orchestrator

import (
	"errors"
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/gitmeta"
)

var ErrPrivateWorktreeStale = errors.New("worktree observation no longer matches picker")

type PrivateWorktreeObservation struct {
	Request          gitmeta.WorktreeCreateRequest
	Result           gitmeta.WorktreePreviewResult
	ErrorCode        gitmeta.WorktreeCreateErrorCode
	ErrorDestination string
}

// PrivateWorktreeRequest freezes only the read-only git inputs under the
// processor lock. No git process or filesystem mutation runs here.
func (s *Service) PrivateWorktreeRequest(action control.Action) (gitmeta.WorktreeCreateRequest, error) {
	var request gitmeta.WorktreeCreateRequest
	if !s.config.PrivateApplication {
		return request, fmt.Errorf("private service required")
	}
	surface := s.root.Surfaces[action.SurfaceSessionID]
	if surface == nil {
		return request, fmt.Errorf("private surface missing")
	}
	if action.Kind == control.ActionWorkspaceNewWorktree {
		request.BaseWorkspacePath = normalizeTargetPickerWorkspaceSelection(s.surfaceCurrentWorkspaceKeyRaw(surface))
		return request, nil
	}
	record := s.activeTargetPicker(surface)
	if record == nil || record.PickerID != action.PickerID || targetPickerEffectivePage(record) != control.FeishuTargetPickerPageWorktree {
		return request, fmt.Errorf("worktree picker changed")
	}
	copy := *record
	s.applyTargetPickerDraftAnswers(&copy, action.RequestAnswers)
	if (action.Kind == control.ActionTargetPickerSelectWorkspace || action.Kind == control.ActionTargetPickerConfirm) && action.WorkspaceKey != "" {
		copy.SelectedWorkspaceKey = action.WorkspaceKey
	}
	request.BaseWorkspacePath = normalizeTargetPickerWorkspaceSelection(copy.SelectedWorkspaceKey)
	request.BranchName = strings.TrimSpace(copy.WorktreeBranchName)
	request.DirectoryName = strings.TrimSpace(copy.WorktreeDirectoryName)
	return request, nil
}

func (s *Service) ApplyPrivateWorktreeObservation(action control.Action, observation PrivateWorktreeObservation) ([]eventcontract.Event, error) {
	request, err := s.PrivateWorktreeRequest(action)
	if err != nil || request != observation.Request {
		return nil, ErrPrivateWorktreeStale
	}
	s.privateWorktreeObservation = &observation
	s.privateWorktreeObservationMissing = false
	events := s.ApplySurfaceAction(action)
	missing := s.privateWorktreeObservationMissing
	s.privateWorktreeObservation = nil
	s.privateWorktreeObservationMissing = false
	if missing {
		return nil, fmt.Errorf("worktree preview requested an unobserved state")
	}
	return events, nil
}
