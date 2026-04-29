package control

import "strings"

type FeishuCommandDisplayProfile struct {
	ProductMode string
	Families    map[string]FeishuCommandDisplayFamilyProfile
}

type FeishuCommandDisplayFamilyProfile struct {
	FamilyID   string
	MenuStages map[FeishuCommandMenuStage]struct{}
}

var feishuCommandDisplayProfiles = map[string]FeishuCommandDisplayProfile{
	"normal": newFeishuCommandDisplayProfile("normal",
		displayProfileFamily(FeishuCommandStop),
		displayProfileFamily(FeishuCommandCompact),
		displayProfileFamily(FeishuCommandSteerAll),
		displayProfileFamilyWithStages(FeishuCommandNew, FeishuCommandMenuStageNormalWorking),
		displayProfileFamily(FeishuCommandStatus),
		displayProfileFamily(FeishuCommandReasoning),
		displayProfileFamily(FeishuCommandModel),
		displayProfileFamily(FeishuCommandAccess),
		displayProfileFamily(FeishuCommandPlan),
		displayProfileFamily(FeishuCommandVerbose),
		displayProfileFamily(FeishuCommandAutoContinue),
		displayProfileFamily(FeishuCommandWorkspace),
		displayProfileFamily(FeishuCommandWorkspaceList),
		displayProfileFamily(FeishuCommandWorkspaceNew),
		displayProfileFamily(FeishuCommandWorkspaceNewDir),
		displayProfileFamily(FeishuCommandWorkspaceNewGit),
		displayProfileFamily(FeishuCommandWorkspaceNewWorktree),
		displayProfileFamily(FeishuCommandWorkspaceDetach),
		displayProfileFamily(FeishuCommandAutoWhip),
		displayProfileFamily(FeishuCommandHistory),
		displayProfileFamily(FeishuCommandCron),
		displayProfileFamily(FeishuCommandSendFile),
		displayProfileFamily(FeishuCommandMode),
		displayProfileFamily(FeishuCommandUpgrade),
		displayProfileFamily(FeishuCommandRestart),
		displayProfileFamilyWithStages(FeishuCommandPatch, FeishuCommandMenuStageNormalWorking),
		displayProfileFamily(FeishuCommandDebug),
		displayProfileFamily(FeishuCommandHelp),
		displayProfileFamily(FeishuCommandMenu),
	),
	"vscode": newFeishuCommandDisplayProfile("vscode",
		displayProfileFamily(FeishuCommandStop),
		displayProfileFamily(FeishuCommandCompact),
		displayProfileFamily(FeishuCommandSteerAll),
		displayProfileFamily(FeishuCommandStatus),
		displayProfileFamily(FeishuCommandReasoning),
		displayProfileFamily(FeishuCommandModel),
		displayProfileFamily(FeishuCommandAccess),
		displayProfileFamily(FeishuCommandPlan),
		displayProfileFamily(FeishuCommandVerbose),
		displayProfileFamily(FeishuCommandAutoContinue),
		displayProfileFamily(FeishuCommandList),
		displayProfileFamily(FeishuCommandUse),
		displayProfileFamily(FeishuCommandUseAll),
		displayProfileFamily(FeishuCommandDetach),
		displayProfileFamilyWithStages(FeishuCommandFollow, FeishuCommandMenuStageVSCodeWorking),
		displayProfileFamily(FeishuCommandAutoWhip),
		displayProfileFamily(FeishuCommandHistory),
		displayProfileFamily(FeishuCommandCron),
		displayProfileFamily(FeishuCommandSendFile),
		displayProfileFamily(FeishuCommandMode),
		displayProfileFamily(FeishuCommandUpgrade),
		displayProfileFamily(FeishuCommandRestart),
		displayProfileFamily(FeishuCommandDebug),
		displayProfileFamily(FeishuCommandHelp),
		displayProfileFamily(FeishuCommandMenu),
		displayProfileFamily(FeishuCommandVSCodeMigrate),
	),
}

func ResolveFeishuCommandDisplayProfileForContext(ctx CatalogContext) FeishuCommandDisplayProfile {
	normalized := NormalizeCatalogContext(ctx)
	if profile, ok := feishuCommandDisplayProfiles[normalized.ProductMode]; ok {
		return profile
	}
	return feishuCommandDisplayProfiles["normal"]
}

func ResolveFeishuCommandDisplayProfile(productMode string) FeishuCommandDisplayProfile {
	normalized := normalizeFeishuCommandProductMode(productMode)
	if profile, ok := feishuCommandDisplayProfiles[normalized]; ok {
		return profile
	}
	return feishuCommandDisplayProfiles["normal"]
}

func (p FeishuCommandDisplayProfile) FamilyProfile(familyID string) (FeishuCommandDisplayFamilyProfile, bool) {
	familyID = strings.TrimSpace(familyID)
	if familyID == "" {
		return FeishuCommandDisplayFamilyProfile{}, false
	}
	profile, ok := p.Families[familyID]
	return profile, ok
}

func (p FeishuCommandDisplayProfile) IncludesFamily(familyID string) bool {
	_, ok := p.FamilyProfile(familyID)
	return ok
}

func (p FeishuCommandDisplayProfile) MenuVisibleInStage(familyID, stage string) bool {
	profile, ok := p.FamilyProfile(familyID)
	if !ok {
		return false
	}
	return profile.MenuVisibleInStage(stage)
}

func (p FeishuCommandDisplayProfile) VisibleFamiliesForGroup(groupID string) []string {
	defs := FeishuCommandDefinitionsForGroup(groupID)
	visible := make([]string, 0, len(defs))
	for _, def := range defs {
		if p.IncludesFamily(def.ID) {
			visible = append(visible, def.ID)
		}
	}
	return visible
}

func (p FeishuCommandDisplayProfile) IncludesGroup(groupID string) bool {
	return len(p.VisibleFamiliesForGroup(groupID)) > 0
}

func (f FeishuCommandDisplayFamilyProfile) MenuVisibleInStage(stage string) bool {
	if len(f.MenuStages) == 0 {
		return true
	}
	_, ok := f.MenuStages[NormalizeFeishuCommandMenuStage(stage)]
	return ok
}

func displayProfileFamily(familyID string) FeishuCommandDisplayFamilyProfile {
	return displayProfileFamilyWithStages(familyID)
}

func displayProfileFamilyWithStages(familyID string, stages ...FeishuCommandMenuStage) FeishuCommandDisplayFamilyProfile {
	familyID = strings.TrimSpace(familyID)
	profile := FeishuCommandDisplayFamilyProfile{FamilyID: familyID}
	if len(stages) == 0 {
		return profile
	}
	profile.MenuStages = make(map[FeishuCommandMenuStage]struct{}, len(stages))
	for _, stage := range stages {
		normalized := NormalizeFeishuCommandMenuStage(string(stage))
		profile.MenuStages[normalized] = struct{}{}
	}
	return profile
}

func newFeishuCommandDisplayProfile(productMode string, families ...FeishuCommandDisplayFamilyProfile) FeishuCommandDisplayProfile {
	profile := FeishuCommandDisplayProfile{
		ProductMode: normalizeFeishuCommandProductMode(productMode),
		Families:    make(map[string]FeishuCommandDisplayFamilyProfile, len(families)),
	}
	for _, family := range families {
		familyID := strings.TrimSpace(family.FamilyID)
		if familyID == "" {
			continue
		}
		profile.Families[familyID] = family
	}
	return profile
}
