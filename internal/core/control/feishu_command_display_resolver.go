package control

import "strings"

type FeishuCommandDisplayFamily struct {
	FamilyID string
	GroupID  string
	Rank     int
	Variants []FeishuCommandDisplayVariant
}

type FeishuCommandDisplayVariant struct {
	VariantID string
	FamilyID  string
	CommandID string
}

type FeishuCommandDisplayResolution struct {
	FamilyID   string
	VariantID  string
	Definition FeishuCommandDefinition
}

func FeishuCommandDisplayFamilyByID(familyID string) (FeishuCommandDisplayFamily, bool) {
	def, ok := FeishuCommandDefinitionByID(strings.TrimSpace(familyID))
	if !ok {
		return FeishuCommandDisplayFamily{}, false
	}
	return newFeishuCommandDisplayFamily(def), true
}

func FeishuCommandDisplayFamiliesForGroup(groupID string) []FeishuCommandDisplayFamily {
	defs := FeishuCommandDefinitionsForGroup(groupID)
	families := make([]FeishuCommandDisplayFamily, 0, len(defs))
	for _, def := range defs {
		families = append(families, newFeishuCommandDisplayFamily(def))
	}
	return families
}

func ResolveFeishuCommandDisplayFamily(familyID string, interactive bool, ctx CatalogContext) (FeishuCommandDisplayResolution, bool) {
	family, ok := FeishuCommandDisplayFamilyByID(familyID)
	if !ok {
		return FeishuCommandDisplayResolution{}, false
	}
	return resolveFeishuCommandDisplayFamily(family, interactive, ctx)
}

func ResolveFeishuCommandDisplayGroup(groupID string, interactive bool, ctx CatalogContext) []FeishuCommandDisplayResolution {
	families := FeishuCommandDisplayFamiliesForGroup(groupID)
	resolved := make([]FeishuCommandDisplayResolution, 0, len(families))
	for _, family := range families {
		current, ok := resolveFeishuCommandDisplayFamily(family, interactive, ctx)
		if !ok {
			continue
		}
		resolved = append(resolved, current)
	}
	return resolved
}

func resolveFeishuCommandDisplayFamily(family FeishuCommandDisplayFamily, interactive bool, ctx CatalogContext) (FeishuCommandDisplayResolution, bool) {
	ctx = NormalizeCatalogContext(ctx)
	for _, variant := range family.Variants {
		current, ok := resolveFeishuCommandDisplayVariant(variant, interactive, ctx)
		if ok {
			return current, true
		}
	}
	return FeishuCommandDisplayResolution{}, false
}

func resolveFeishuCommandDisplayVariant(variant FeishuCommandDisplayVariant, interactive bool, ctx CatalogContext) (FeishuCommandDisplayResolution, bool) {
	ctx = NormalizeCatalogContext(ctx)
	def, ok := FeishuCommandDefinitionByID(variant.CommandID)
	if !ok {
		return FeishuCommandDisplayResolution{}, false
	}
	projected, ok := projectFeishuCommandDefinitionForDisplay(def, interactive, ctx)
	if !ok {
		return FeishuCommandDisplayResolution{}, false
	}
	variantID := strings.TrimSpace(variant.VariantID)
	if variantID == "" || variantID == defaultFeishuCommandDisplayVariantID(variant.FamilyID) {
		variantID = feishuCommandVariantIDForContext(variant.FamilyID, ctx)
	}
	return FeishuCommandDisplayResolution{
		FamilyID:   strings.TrimSpace(variant.FamilyID),
		VariantID:  variantID,
		Definition: projected,
	}, true
}

func newFeishuCommandDisplayFamily(def FeishuCommandDefinition) FeishuCommandDisplayFamily {
	commandID := strings.TrimSpace(def.ID)
	return FeishuCommandDisplayFamily{
		FamilyID: commandID,
		GroupID:  strings.TrimSpace(def.GroupID),
		Rank:     feishuCommandDisplayRank(def.GroupID, commandID),
		Variants: []FeishuCommandDisplayVariant{{
			VariantID: defaultFeishuCommandDisplayVariantID(commandID),
			FamilyID:  commandID,
			CommandID: commandID,
		}},
	}
}

func defaultFeishuCommandDisplayVariantID(commandID string) string {
	commandID = strings.TrimSpace(commandID)
	if commandID == "" {
		return ""
	}
	return commandID + ".default"
}
