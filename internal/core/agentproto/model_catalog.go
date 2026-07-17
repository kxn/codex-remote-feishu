package agentproto

func CloneModelCatalogSnapshot(snapshot *ModelCatalogSnapshot) *ModelCatalogSnapshot {
	if snapshot == nil {
		return nil
	}
	cloned := *snapshot
	cloned.Entries = make([]ModelCatalogEntry, 0, len(snapshot.Entries))
	for _, entry := range snapshot.Entries {
		cloned.Entries = append(cloned.Entries, CloneModelCatalogEntry(entry))
	}
	return &cloned
}

func CloneModelCatalogEntry(entry ModelCatalogEntry) ModelCatalogEntry {
	cloned := entry
	if len(entry.SupportedReasoningEfforts) != 0 {
		cloned.SupportedReasoningEfforts = append([]ReasoningEffortOption(nil), entry.SupportedReasoningEfforts...)
	}
	if len(entry.ServiceTiers) != 0 {
		cloned.ServiceTiers = append([]ModelServiceTier(nil), entry.ServiceTiers...)
	}
	if entry.UpgradeInfo != nil {
		info := *entry.UpgradeInfo
		cloned.UpgradeInfo = &info
	}
	return cloned
}
