package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) reviewCommandSupportForSurface(surface *state.SurfaceConsoleRecord, instanceID string) (control.FeishuCommandSupport, bool) {
	if s == nil {
		return control.FeishuCommandSupport{}, false
	}
	instanceID = strings.TrimSpace(instanceID)
	var inst *state.InstanceRecord
	switch {
	case instanceID != "":
		inst = s.root.Instances[instanceID]
	case surface != nil:
		inst = s.root.Instances[strings.TrimSpace(surface.AttachedInstanceID)]
	}
	return control.ResolveFeishuCommandSupport(s.buildCatalogContextWithInstance(surface, inst), control.FeishuCommandReview)
}

func (s *Service) reviewCommandAllowedForSurface(surface *state.SurfaceConsoleRecord, instanceID string) bool {
	support, ok := s.reviewCommandSupportForSurface(surface, instanceID)
	return ok && support.DispatchAllowed
}
