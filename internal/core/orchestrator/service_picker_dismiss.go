package orchestrator

import (
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// dismissActivePickersForTextMessage clears any active target or path picker
// in editing stage when the user sends a text message. This prevents the
// picker from blocking the message and avoids stale picker cards lingering
// after the user bypasses the picker by typing directly.
//
// Pickers in the Processing stage (async git import, worktree creation) are
// NOT dismissed — they are legitimately blocking and the user should wait.
func (s *Service) dismissActivePickersForTextMessage(surface *state.SurfaceConsoleRecord) {
	if surface == nil {
		return
	}
	if picker := s.activeTargetPicker(surface); picker != nil {
		if picker.Stage != control.FeishuTargetPickerStageProcessing {
			s.clearTargetPickerRuntime(surface)
		}
	}
	if s.activePathPicker(surface) != nil {
		s.clearSurfacePathPicker(surface)
	}
}
