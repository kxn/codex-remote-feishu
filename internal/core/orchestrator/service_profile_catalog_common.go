package orchestrator

import "sort"

type profileCatalogSortKey struct {
	BuiltIn bool
	Name    string
	ID      string
}

func materializeProfileCatalogRecords[T any](records []T, defaultRecord T, normalize func(T) T, idOf func(T) string) map[string]T {
	catalog := map[string]T{}
	defaultRecord = normalize(defaultRecord)
	catalog[idOf(defaultRecord)] = defaultRecord
	for _, record := range records {
		current := normalize(record)
		if id := idOf(current); id != "" {
			catalog[id] = current
		}
	}
	return catalog
}

func sortedProfileCatalogRecords[T any](catalog map[string]T, normalize func(T) T, keyOf func(T) profileCatalogSortKey) []T {
	records := make([]T, 0, len(catalog))
	for _, record := range catalog {
		records = append(records, normalize(record))
	}
	sort.SliceStable(records, func(i, j int) bool {
		return profileCatalogSortKeyLess(keyOf(records[i]), keyOf(records[j]))
	})
	return records
}

func profileCatalogSortKeyLess(left, right profileCatalogSortKey) bool {
	if left.BuiltIn != right.BuiltIn {
		return left.BuiltIn
	}
	if left.Name != right.Name {
		return left.Name < right.Name
	}
	return left.ID < right.ID
}
