package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// releaseManagedHeadlessAfterDetach releases the daemon-owned app-server that
// was carrying a detached surface. Keeping that process alive after /detach
// leaves the Codex thread writer locked and prevents a local `codex resume`
// from taking over. Do not stop an instance still attached to another surface.
func (s *Service) releaseManagedHeadlessAfterDetach(surface *state.SurfaceConsoleRecord, instanceID string) []eventcontract.Event {
	if surface == nil {
		return nil
	}
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return nil
	}
	inst := s.root.Instances[instanceID]
	if !state.IsManagedHeadlessInstance(inst) || len(s.findAttachedSurfaces(instanceID)) != 0 {
		return nil
	}
	command := &control.DaemonCommand{
		Kind:             control.DaemonCommandKillHeadless,
		SurfaceSessionID: surface.SurfaceSessionID,
		InstanceID:       instanceID,
		ThreadID:         surface.SelectedThreadID,
		ThreadCWD:        inst.WorkspaceRoot,
		WorkspaceKey:     inst.WorkspaceKey,
	}
	return []eventcontract.Event{{
		Kind:             eventcontract.KindDaemonCommand,
		SurfaceSessionID: surface.SurfaceSessionID,
		DaemonCommand:    command,
	}}
}
