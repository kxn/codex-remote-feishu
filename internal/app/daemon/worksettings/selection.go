package worksettings

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func selectedProfile(record state.BotCapabilitySettingsRecord) string {
	switch record.Backend {
	case agentproto.BackendClaude:
		return record.ClaudeProfileID
	case agentproto.BackendOpenCode:
		return record.OpenCodeProfileID
	default:
		return record.CodexProfileID
	}
}
func profileFor(snapshot Snapshot, backend agentproto.Backend, id string) (Profile, bool) {
	for _, profile := range snapshot.Profiles {
		if profile.Backend == backend && strings.EqualFold(profile.ID, strings.TrimSpace(id)) {
			return profile, true
		}
	}
	return Profile{}, false
}
func nextSettings(snapshot Snapshot, request UpdateRequest) (state.BotCapabilitySettingsRecord, error) {
	next := snapshot.Settings
	if request.Backend == nil && request.ProfileID == nil && request.Model == nil && request.Reasoning == nil {
		return next, fmt.Errorf("choose an explicit setting change")
	}
	backend := next.Backend
	if request.Backend != nil {
		backend = *request.Backend
		if backend != agentproto.BackendCodex && backend != agentproto.BackendClaude && backend != agentproto.BackendOpenCode {
			return next, fmt.Errorf("unknown configured backend")
		}
	}
	profileID := selectedProfile(next)
	if backend != next.Backend {
		switch backend {
		case agentproto.BackendCodex:
			profileID = next.CodexProfileID
		case agentproto.BackendClaude:
			profileID = next.ClaudeProfileID
		case agentproto.BackendOpenCode:
			profileID = next.OpenCodeProfileID
		}
	}
	if request.ProfileID != nil {
		profileID = strings.TrimSpace(*request.ProfileID)
		if profileID == "" {
			return next, fmt.Errorf("profile must be explicitly selected from configured choices")
		}
	}
	profile, ok := profileFor(snapshot, backend, profileID)
	if !ok || !profile.Available {
		return next, fmt.Errorf("selected profile is not configured and available")
	}
	changedProfile := backend != next.Backend || profile.ID != selectedProfile(next)
	next.Backend = backend
	next.ProductMode = state.ProductModeNormal
	switch backend {
	case agentproto.BackendClaude:
		next.ClaudeProfileID = profile.ID
	case agentproto.BackendOpenCode:
		next.OpenCodeProfileID = profile.ID
		next.OpenCodeAdmissionRef = nil
	default:
		next.CodexProfileID = profile.ID
	}
	if changedProfile {
		next.PromptOverride.Model = ""
		next.PromptOverride.ReasoningEffort = ""
	}
	if request.Model != nil {
		next.PromptOverride.Model = strings.TrimSpace(*request.Model)
		if next.PromptOverride.Model == "" {
			next.PromptOverride.ReasoningEffort = ""
		}
	}
	if request.Reasoning != nil {
		next.PromptOverride.ReasoningEffort = strings.TrimSpace(*request.Reasoning)
	}
	model, effort := next.PromptOverride.Model, next.PromptOverride.ReasoningEffort
	if profile.FixedModel != "" {
		if model != "" && !strings.EqualFold(model, profile.FixedModel) {
			return next, fmt.Errorf("configured profile fixes model %s", profile.FixedModel)
		}
		if effort != "" && !strings.EqualFold(effort, profile.FixedReasoning) {
			return next, fmt.Errorf("configured fixed-model profile requires its declared reasoning effort or automatic")
		}
		next.PromptOverride.Model = ""
		next.PromptOverride.ReasoningEffort = ""
	} else if request.Model != nil && model != "" || request.Reasoning != nil && effort != "" {
		if changedProfile || snapshot.CatalogBackend != backend || snapshot.CatalogProfileID != profile.ID {
			return next, fmt.Errorf("the selected profile has no current matching model catalog; choose the profile first, then refresh its model list")
		}
		if model == "" && snapshot.Catalog != nil {
			for _, entry := range snapshot.Catalog.Entries {
				if entry.IsDefault && !entry.Hidden {
					model = entry.Model
					break
				}
			}
		}
		canonicalModel, canonicalEffort, err := orchestrator.ValidateCatalogSelection(backend, snapshot.Catalog, model, effort)
		if err != nil {
			return next, err
		}
		if next.PromptOverride.Model != "" {
			next.PromptOverride.Model = canonicalModel
		}
		next.PromptOverride.ReasoningEffort = canonicalEffort
	}
	if backend == agentproto.BackendOpenCode && request.Model != nil && *request.Model != "" {
		return next, fmt.Errorf("OpenCode model selection belongs to its explicit profile/session configuration")
	}
	// Access/Plan/Verbose belong to a context. Existing legacy fields are removed
	// from this global desired record instead of leaking session preferences.
	next.PromptOverride.AccessMode = ""
	next.PlanMode = state.PlanModeSettingOff
	next.PlanModeOverrideSet = false
	normalized, ok := state.NormalizeBotCapabilitySettingsRecord(next)
	if !ok {
		return next, fmt.Errorf("invalid Bot settings selection")
	}
	return normalized, nil
}
