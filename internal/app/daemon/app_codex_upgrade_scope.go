package daemon

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func standaloneCodexUpgradeAffectsInstance(inst *state.InstanceRecord) bool {
	if inst == nil {
		return false
	}
	return !strings.EqualFold(firstNonEmpty(inst.Source, "vscode"), "vscode")
}

func (a *App) standaloneCodexUpgradeAffectsInstanceIDLocked(instanceID string) bool {
	if strings.TrimSpace(instanceID) == "" {
		return false
	}
	return standaloneCodexUpgradeAffectsInstance(a.service.Instance(instanceID))
}

func (a *App) standaloneCodexUpgradeAffectsSurfaceLocked(surface *state.SurfaceConsoleRecord) bool {
	if surface == nil {
		return false
	}
	if instanceID := strings.TrimSpace(surface.AttachedInstanceID); instanceID != "" {
		if inst := a.service.Instance(instanceID); inst != nil {
			return standaloneCodexUpgradeAffectsInstance(inst)
		}
	}
	return state.NormalizeProductMode(surface.ProductMode) != state.ProductModeVSCode
}

func (a *App) standaloneCodexUpgradeAffectsSurfaceIDLocked(surfaceID string) bool {
	if strings.TrimSpace(surfaceID) == "" {
		return false
	}
	return a.standaloneCodexUpgradeAffectsSurfaceLocked(a.service.Surface(surfaceID))
}
