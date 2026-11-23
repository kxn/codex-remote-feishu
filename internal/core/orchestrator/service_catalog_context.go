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
	workspaceKey := ""
	if surface != nil {
		workspaceKey = state.ResolveWorkspaceKey(s.surfaceCurrentWorkspaceKey(surface))
	}
	if inst != nil {
		workspaceKey = state.ResolveWorkspaceKey(workspaceKey, instanceWorkspaceClaimKey(inst))
	}
	return s.buildCatalogContextWithWorkspaceKey(surface, inst, workspaceKey)
}

func (s *Service) buildCatalogContextForAction(surface *state.SurfaceConsoleRecord, action control.Action) control.CatalogContext {
	if s.IsTargetPickerDraftNavigation(action) {
		inst := s.root.Instances[strings.TrimSpace(surface.AttachedInstanceID)]
		return s.buildCatalogContextWithWorkspaceKey(surface, inst, s.targetPickerNavigationWorkspaceProjection(surface))
	}
	return s.buildCatalogContext(surface)
}

func (s *Service) buildCatalogContextWithWorkspaceKey(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, workspaceKey string) control.CatalogContext {
	if s == nil {
		return control.NormalizeCatalogContext(control.CatalogContext{})
	}
	productMode := state.ProductModeNormal
	backend := agentproto.BackendCodex
	if surface != nil {
		productMode = s.normalizeSurfaceProductMode(surface)
		backend = s.surfaceBackend(surface)
	}
	capabilities := agentproto.DefaultCapabilitiesForBackend(backend)
	instanceID := ""
	if inst != nil {
		backend = state.EffectiveInstanceBackend(inst)
		capabilities = state.EffectiveInstanceCapabilities(inst)
		instanceID = strings.TrimSpace(inst.InstanceID)
	}
	attachedKind := string(control.CatalogAttachedKindDetached)
	if instanceID != "" {
		if productMode == state.ProductModeVSCode {
			attachedKind = string(control.CatalogAttachedKindInstance)
		} else {
			attachedKind = string(control.CatalogAttachedKindWorkspace)
		}
	}
	surfaceScopeKind := string(control.CatalogSurfaceScopeKindUser)
	primaryBotState := string(control.CatalogPrimaryBotStateUnknown)
	if surfaceFeishuRoomID(surface) != "" {
		surfaceScopeKind = string(control.CatalogSurfaceScopeKindChat)
		primaryBotState = string(primaryBotStateForSurface(surface, s.ensureFeishuRoomContextForSurface(surface)))
	}
	return control.NormalizeCatalogContext(control.CatalogContext{
		PrivateApplication:            s.config.PrivateApplication,
		Backend:                       backend,
		ProductMode:                   string(productMode),
		MenuStage:                     string(s.commandMenuStage(surface)),
		AttachedKind:                  attachedKind,
		WorkspaceKey:                  workspaceKey,
		InstanceID:                    instanceID,
		SurfaceScopeKind:              surfaceScopeKind,
		PrimaryBotState:               primaryBotState,
		Capabilities:                  capabilities,
		CapabilitiesDeclared:          inst != nil && inst.CapabilitiesDeclared,
		BotCapabilitySettingsReadOnly: surfaceFeishuRoomID(surface) != "",
	})
}
