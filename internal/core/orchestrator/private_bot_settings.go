package orchestrator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) capturePrivateSettingsProposal(surface *state.SurfaceConsoleRecord, mutate func(*state.BotCapabilitySettingsRecord)) {
	record, status := state.LookupSurfaceBotCapabilitySettings(s.root, surface)
	if status == state.BotCapabilitySettingsLookupAbsent || status == state.BotCapabilitySettingsLookupNotApplicable {
		record = botCapabilitySettingsFromSurface(surface)
	}
	mutate(&record)
	if normalized, ok := state.NormalizeBotCapabilitySettingsRecord(record); ok {
		s.privateSettingsProposal = &normalized
	}
}

// PreviewPrivateSettingsTransition executes only the legacy validation branch;
// route mutations are postponed until the authoritative config receipt.
func (s *Service) PreviewPrivateSettingsTransition(action control.Action) (*state.BotCapabilitySettingsRecord, []eventcontract.Event, error) {
	if !s.config.PrivateApplication || s.privateSettingsPreview {
		return nil, nil, fmt.Errorf("private settings preview unavailable")
	}
	switch action.Kind {
	case control.ActionModeCommand:
		parts := strings.Fields(strings.ToLower(action.Text))
		if len(parts) > 1 && parts[1] == "vscode" || strings.EqualFold(action.OptionID, "vscode") {
			return nil, nil, fmt.Errorf("vscode mode is unavailable")
		}
	case control.ActionCodexProfileCommand, control.ActionClaudeProfileCommand, control.ActionOpenCodeProfileCommand, control.ActionModelCommand, control.ActionReasoningCommand:
	default:
		return nil, nil, fmt.Errorf("unsupported private settings preview")
	}
	s.privateSettingsPreview = true
	s.privateSettingsProposal = nil
	events := s.ApplySurfaceAction(action)
	proposal := s.privateSettingsProposal
	s.privateSettingsPreview = false
	s.privateSettingsProposal = nil
	return proposal, events, nil
}

// ReconcilePrivateBotSettings runs the legacy single-surface contract logic.
// The caller must already have projected the authoritative Bot settings.
func (s *Service) ReconcilePrivateBotSettings(surfaceID string) ([]eventcontract.Event, error) {
	if !s.config.PrivateApplication {
		return nil, fmt.Errorf("private service required")
	}
	surface := s.root.Surfaces[surfaceID]
	if surface == nil {
		return nil, fmt.Errorf("private surface missing")
	}
	return s.reconcileHeadlessSurfaceContract(surface), nil
}

// ApplyPrivateBotSettingsToSurface updates only the named owner's projection.
// The shared record is cached globally, but other surfaces transition through
// their own durable reconciliation transaction.
func (s *Service) ApplyPrivateBotSettingsToSurface(record state.BotCapabilitySettingsRecord, surfaceID string) error {
	if !s.config.PrivateApplication {
		return fmt.Errorf("private service required")
	}
	normalized, ok := state.NormalizeBotCapabilitySettingsRecord(record)
	if !ok {
		return fmt.Errorf("invalid bot settings")
	}
	surface := s.root.Surfaces[surfaceID]
	if surface == nil || surface.GatewayID != normalized.GatewayID {
		return fmt.Errorf("private bot settings owner mismatch")
	}
	if s.root.BotCapabilitySettings == nil {
		s.root.BotCapabilitySettings = map[string]state.BotCapabilitySettingsRecord{}
	}
	before, _ := json.Marshal(state.SurfaceDesiredBackendContract(surface))
	s.root.BotCapabilitySettings[state.BotCapabilitySettingsKey(normalized.GatewayID)] = normalized
	s.projectBotCapabilitySettingsToSurface(surface, normalized)
	after, _ := json.Marshal(state.SurfaceDesiredBackendContract(surface))
	if s.PrivateHasOnlineAttachedCarrier(surfaceID) && !bytes.Equal(before, after) {
		surface.ContractRefreshPending = true
	}
	return nil
}
