package control

import "strings"

type FeishuConfigFlowValueKey string

const (
	FeishuConfigFlowValueNone                     FeishuConfigFlowValueKey = ""
	FeishuConfigFlowValueSurfaceProductMode       FeishuConfigFlowValueKey = "surface_product_mode"
	FeishuConfigFlowValueSurfaceCodexProvider     FeishuConfigFlowValueKey = "surface_codex_provider"
	FeishuConfigFlowValueSurfaceClaudeProfile     FeishuConfigFlowValueKey = "surface_claude_profile"
	FeishuConfigFlowValueSurfaceAutoWhip          FeishuConfigFlowValueKey = "surface_auto_whip"
	FeishuConfigFlowValueSurfaceAutoContinue      FeishuConfigFlowValueKey = "surface_auto_continue"
	FeishuConfigFlowValueSurfacePlanMode          FeishuConfigFlowValueKey = "surface_plan_mode"
	FeishuConfigFlowValueSurfaceVerbosity         FeishuConfigFlowValueKey = "surface_verbosity"
	FeishuConfigFlowValuePromptEffectiveReasoning FeishuConfigFlowValueKey = "prompt_effective_reasoning"
	FeishuConfigFlowValuePromptOverrideReasoning  FeishuConfigFlowValueKey = "prompt_override_reasoning"
	FeishuConfigFlowValuePromptEffectiveAccess    FeishuConfigFlowValueKey = "prompt_effective_access"
	FeishuConfigFlowValuePromptOverrideAccess     FeishuConfigFlowValueKey = "prompt_override_access"
	FeishuConfigFlowValuePromptObservedThreadPlan FeishuConfigFlowValueKey = "prompt_observed_thread_plan"
	FeishuConfigFlowValuePromptEffectiveModel     FeishuConfigFlowValueKey = "prompt_effective_model"
	FeishuConfigFlowValuePromptOverrideModel      FeishuConfigFlowValueKey = "prompt_override_model"
)

type FeishuConfigFlowDefinition struct {
	CommandID             string
	ActionKind            ActionKind
	BareCommand           string
	IntentKind            FeishuUIIntentKind
	PageBuilder           func(FeishuCatalogConfigView) FeishuPageView
	CurrentValueKey       FeishuConfigFlowValueKey
	EffectiveValueKey     FeishuConfigFlowValueKey
	OverrideValueKey      FeishuConfigFlowValueKey
	OverrideExtraValueKey FeishuConfigFlowValueKey
	RequiresAttachment    bool
}

func (d FeishuConfigFlowDefinition) CatalogFamilyID() string {
	return strings.TrimSpace(d.CommandID)
}

func (d FeishuConfigFlowDefinition) DefaultVariantID() string {
	familyID := d.CatalogFamilyID()
	if familyID == "" {
		return ""
	}
	return defaultFeishuCommandDisplayVariantID(familyID)
}

func (d FeishuConfigFlowDefinition) BaseCatalogView() FeishuCatalogConfigView {
	return FeishuCatalogConfigView{
		CommandID:        strings.TrimSpace(d.CommandID),
		CatalogFamilyID:  d.CatalogFamilyID(),
		CatalogVariantID: d.DefaultVariantID(),
	}
}

func (d FeishuConfigFlowDefinition) MatchesCatalog(familyID, variantID string) bool {
	familyID = strings.TrimSpace(familyID)
	variantID = strings.TrimSpace(variantID)
	currentFamilyID := d.CatalogFamilyID()
	if familyID != "" && familyID != currentFamilyID {
		return false
	}
	if variantID == "" {
		return familyID != ""
	}
	if variantID == d.DefaultVariantID() {
		return true
	}
	return currentFamilyID != "" && strings.HasPrefix(variantID, currentFamilyID+".")
}

func (d FeishuConfigFlowDefinition) UsesPromptSummary() bool {
	return d.CurrentValueKey.UsesPromptSummary() ||
		d.EffectiveValueKey.UsesPromptSummary() ||
		d.OverrideValueKey.UsesPromptSummary() ||
		d.OverrideExtraValueKey.UsesPromptSummary()
}

func (k FeishuConfigFlowValueKey) UsesPromptSummary() bool {
	switch k {
	case FeishuConfigFlowValuePromptEffectiveReasoning,
		FeishuConfigFlowValuePromptOverrideReasoning,
		FeishuConfigFlowValuePromptEffectiveAccess,
		FeishuConfigFlowValuePromptOverrideAccess,
		FeishuConfigFlowValuePromptObservedThreadPlan,
		FeishuConfigFlowValuePromptEffectiveModel,
		FeishuConfigFlowValuePromptOverrideModel:
		return true
	default:
		return false
	}
}

var feishuConfigFlowDefinitions = []FeishuConfigFlowDefinition{
	{
		CommandID:       FeishuCommandMode,
		ActionKind:      ActionModeCommand,
		BareCommand:     "/mode",
		IntentKind:      FeishuUIIntentShowModeCatalog,
		PageBuilder:     modePageViewFromCommandConfigView,
		CurrentValueKey: FeishuConfigFlowValueSurfaceProductMode,
	},
	{
		CommandID:       FeishuCommandCodexProvider,
		ActionKind:      ActionCodexProviderCommand,
		BareCommand:     "/codexprovider",
		IntentKind:      FeishuUIIntentShowCodexProviderCatalog,
		PageBuilder:     codexProviderPageViewFromCommandConfigView,
		CurrentValueKey: FeishuConfigFlowValueSurfaceCodexProvider,
	},
	{
		CommandID:       FeishuCommandClaudeProfile,
		ActionKind:      ActionClaudeProfileCommand,
		BareCommand:     "/claudeprofile",
		IntentKind:      FeishuUIIntentShowClaudeProfileCatalog,
		PageBuilder:     claudeProfilePageViewFromCommandConfigView,
		CurrentValueKey: FeishuConfigFlowValueSurfaceClaudeProfile,
	},
	{
		CommandID:       FeishuCommandAutoWhip,
		ActionKind:      ActionAutoWhipCommand,
		BareCommand:     "/autowhip",
		IntentKind:      FeishuUIIntentShowAutoWhipCatalog,
		PageBuilder:     autoWhipPageViewFromCommandConfigView,
		CurrentValueKey: FeishuConfigFlowValueSurfaceAutoWhip,
	},
	{
		CommandID:       FeishuCommandAutoContinue,
		ActionKind:      ActionAutoContinueCommand,
		BareCommand:     "/autocontinue",
		IntentKind:      FeishuUIIntentShowAutoContinueCatalog,
		PageBuilder:     autoContinuePageViewFromCommandConfigView,
		CurrentValueKey: FeishuConfigFlowValueSurfaceAutoContinue,
	},
	{
		CommandID:          FeishuCommandReasoning,
		ActionKind:         ActionReasoningCommand,
		BareCommand:        "/reasoning",
		IntentKind:         FeishuUIIntentShowReasoningCatalog,
		PageBuilder:        reasoningPageViewFromCommandConfigView,
		EffectiveValueKey:  FeishuConfigFlowValuePromptEffectiveReasoning,
		OverrideValueKey:   FeishuConfigFlowValuePromptOverrideReasoning,
		RequiresAttachment: true,
	},
	{
		CommandID:          FeishuCommandAccess,
		ActionKind:         ActionAccessCommand,
		BareCommand:        "/access",
		IntentKind:         FeishuUIIntentShowAccessCatalog,
		PageBuilder:        accessPageViewFromCommandConfigView,
		EffectiveValueKey:  FeishuConfigFlowValuePromptEffectiveAccess,
		OverrideValueKey:   FeishuConfigFlowValuePromptOverrideAccess,
		RequiresAttachment: true,
	},
	{
		CommandID:         FeishuCommandPlan,
		ActionKind:        ActionPlanCommand,
		BareCommand:       "/plan",
		IntentKind:        FeishuUIIntentShowPlanCatalog,
		PageBuilder:       planPageViewFromCommandConfigView,
		CurrentValueKey:   FeishuConfigFlowValueSurfacePlanMode,
		EffectiveValueKey: FeishuConfigFlowValuePromptObservedThreadPlan,
	},
	{
		CommandID:             FeishuCommandModel,
		ActionKind:            ActionModelCommand,
		BareCommand:           "/model",
		IntentKind:            FeishuUIIntentShowModelCatalog,
		PageBuilder:           modelPageViewFromCommandConfigView,
		EffectiveValueKey:     FeishuConfigFlowValuePromptEffectiveModel,
		OverrideValueKey:      FeishuConfigFlowValuePromptOverrideModel,
		OverrideExtraValueKey: FeishuConfigFlowValuePromptOverrideReasoning,
		RequiresAttachment:    true,
	},
	{
		CommandID:       FeishuCommandVerbose,
		ActionKind:      ActionVerboseCommand,
		BareCommand:     "/verbose",
		IntentKind:      FeishuUIIntentShowVerboseCatalog,
		PageBuilder:     verbosePageViewFromCommandConfigView,
		CurrentValueKey: FeishuConfigFlowValueSurfaceVerbosity,
	},
}

func FeishuConfigFlowDefinitions() []FeishuConfigFlowDefinition {
	return append([]FeishuConfigFlowDefinition(nil), feishuConfigFlowDefinitions...)
}

func FeishuConfigFlowDefinitionByCommandID(commandID string) (FeishuConfigFlowDefinition, bool) {
	commandID = strings.TrimSpace(commandID)
	for _, def := range feishuConfigFlowDefinitions {
		if def.CommandID == commandID {
			return def, true
		}
	}
	return FeishuConfigFlowDefinition{}, false
}

func FeishuConfigFlowDefinitionByActionKind(kind ActionKind) (FeishuConfigFlowDefinition, bool) {
	for _, def := range feishuConfigFlowDefinitions {
		if def.ActionKind == kind {
			return def, true
		}
	}
	return FeishuConfigFlowDefinition{}, false
}

func FeishuConfigFlowDefinitionForCatalog(familyID, variantID string) (FeishuConfigFlowDefinition, bool) {
	for _, def := range feishuConfigFlowDefinitions {
		if def.MatchesCatalog(familyID, variantID) {
			return def, true
		}
	}
	return FeishuConfigFlowDefinition{}, false
}

func FeishuConfigFlowDefinitionByIntentKind(kind FeishuUIIntentKind) (FeishuConfigFlowDefinition, bool) {
	for _, def := range feishuConfigFlowDefinitions {
		if def.IntentKind == kind {
			return def, true
		}
	}
	return FeishuConfigFlowDefinition{}, false
}

func ResolveFeishuConfigFlowDefinitionFromView(view FeishuCatalogConfigView) (FeishuConfigFlowDefinition, bool) {
	if def, ok := FeishuConfigFlowDefinitionForCatalog(view.CatalogFamilyID, view.CatalogVariantID); ok {
		return def, true
	}
	if def, ok := FeishuConfigFlowDefinitionByCommandID(strings.TrimSpace(view.CommandID)); ok {
		return def, true
	}
	return FeishuConfigFlowDefinition{}, false
}

func ResolveFeishuConfigFlowDefinitionFromAction(action Action) (FeishuConfigFlowDefinition, bool) {
	if def, ok := FeishuConfigFlowDefinitionForCatalog(action.CatalogFamilyID, action.CatalogVariantID); ok {
		return def, true
	}
	if def, ok := FeishuConfigFlowDefinitionByCommandID(strings.TrimSpace(action.CommandID)); ok {
		return def, true
	}
	if def, ok := FeishuConfigFlowDefinitionByActionKind(action.Kind); ok {
		return def, true
	}
	return FeishuConfigFlowDefinition{}, false
}

func FeishuConfigFlowIntentFromAction(action Action) (*FeishuUIIntent, bool) {
	def, ok := ResolveFeishuConfigFlowDefinitionFromAction(action)
	if !ok || !isBareInlineCommand(action.Text, def.BareCommand) {
		return nil, false
	}
	return &FeishuUIIntent{
		Kind:    def.IntentKind,
		RawText: action.Text,
	}, true
}
