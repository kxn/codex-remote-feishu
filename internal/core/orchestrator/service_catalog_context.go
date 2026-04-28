package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) buildCatalogContext(surface *state.SurfaceConsoleRecord) control.CatalogContext {
	if s == nil {
		return control.NormalizeCatalogContext(control.CatalogContext{})
	}
	var inst *state.InstanceRecord
	if surface != nil {
		inst = s.root.Instances[strings.TrimSpace(surface.AttachedInstanceID)]
	}
	return s.buildCatalogContextWithInstance(surface, inst)
}

func (s *Service) buildCatalogContextWithInstance(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord) control.CatalogContext {
	if s == nil {
		return control.NormalizeCatalogContext(control.CatalogContext{})
	}
	productMode := state.ProductModeNormal
	workspaceKey := ""
	backend := agentproto.BackendCodex
	if surface != nil {
		productMode = s.normalizeSurfaceProductMode(surface)
		workspaceKey = state.ResolveWorkspaceKey(s.surfaceCurrentWorkspaceKey(surface))
		backend = s.surfaceBackend(surface)
	}
	capabilities := agentproto.DefaultCapabilitiesForBackend(backend)
	instanceID := ""
	if inst != nil {
		backend = state.EffectiveInstanceBackend(inst)
		capabilities = state.EffectiveInstanceCapabilities(inst)
		instanceID = strings.TrimSpace(inst.InstanceID)
		workspaceKey = state.ResolveWorkspaceKey(workspaceKey, instanceWorkspaceClaimKey(inst))
	}
	attachedKind := string(control.CatalogAttachedKindDetached)
	if instanceID != "" {
		if productMode == state.ProductModeVSCode {
			attachedKind = string(control.CatalogAttachedKindInstance)
		} else {
			attachedKind = string(control.CatalogAttachedKindWorkspace)
		}
	}
	return control.NormalizeCatalogContext(control.CatalogContext{
		Backend:      backend,
		ProductMode:  string(productMode),
		MenuStage:    string(s.commandMenuStage(surface)),
		AttachedKind: attachedKind,
		WorkspaceKey: workspaceKey,
		InstanceID:   instanceID,
		Capabilities: capabilities,
	})
}
