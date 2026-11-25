package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// targetPickerOperationReady requires a result produced by this operation as
// well as an actually usable route. A snapshot left by a prior operation is
// never proof that the current request succeeded.
func (s *Service) targetPickerOperationReady(surface *state.SurfaceConsoleRecord, workspace, thread string, events []eventcontract.Event) bool {
	if surface == nil || surface.PendingHeadless != nil {
		return false
	}
	inst := s.root.Instances[strings.TrimSpace(surface.AttachedInstanceID)]
	if inst == nil || !inst.Online || !s.surfaceInstanceCompatibleForAttach(surface, inst) {
		return false
	}
	if thread != "" {
		if !targetPickerThreadReady(surface, thread) {
			return false
		}
	} else if !targetPickerNewThreadReady(surface, workspace) {
		return false
	}
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		if e.Notice != nil {
			switch e.Notice.Code {
			case "new_thread_ready", "already_new_thread_ready", "new_thread_ready_reset", "selection_unchanged", "attached", "headless_restore_attached", "surface_resume_attached":
				return true
			case "thread_selection_changed":
				if e.ThreadSelection != nil {
					return true
				}
			default:
				return false
			}
		}
		if e.ThreadSelection != nil {
			return true
		}
	}
	return false
}

func targetPickerStartedPending(surface *state.SurfaceConsoleRecord, events []eventcontract.Event) bool {
	if surface == nil || surface.PendingHeadless == nil {
		return false
	}
	for _, event := range events {
		cmd := event.DaemonCommand
		if cmd != nil && cmd.Kind == control.DaemonCommandStartHeadless && cmd.InstanceID != "" && cmd.InstanceID == surface.PendingHeadless.InstanceID {
			return true
		}
	}
	return false
}

func targetPickerOperationFailure(events []eventcontract.Event) string {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].ThreadSelection != nil {
			return ""
		}
		n := events[i].Notice
		if n == nil {
			continue
		}
		switch n.Code {
		case "new_thread_ready", "already_new_thread_ready", "new_thread_ready_reset", "selection_unchanged", "thread_selection_changed", "attached", "headless_restore_attached", "surface_resume_attached":
			return ""
		case "headless_starting":
			continue
		default:
			return strings.TrimSpace(n.Text)
		}
	}
	return ""
}
