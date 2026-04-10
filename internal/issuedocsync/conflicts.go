package issuedocsync

import (
	"fmt"
	"sort"
)

func FindRecordConflicts(state StateFile, incoming IssueRecord) []string {
	if len(incoming.TargetDocs) == 0 {
		return nil
	}

	warnings := make([]string, 0)
	for _, existing := range state.Issues {
		if existing.Number == incoming.Number {
			continue
		}
		sharedDocs := intersectTargetDocs(existing.TargetDocs, incoming.TargetDocs)
		if len(sharedDocs) == 0 {
			continue
		}
		if compareIssueRecordsForSync(existing, incoming) <= 0 {
			continue
		}
		for _, targetDoc := range sharedDocs {
			warnings = append(warnings, fmt.Sprintf(
				"target doc %s already has newer synced issue #%d; backfilling older issue #%d now can overwrite newer design conclusions",
				targetDoc,
				existing.Number,
				incoming.Number,
			))
		}
	}
	sort.Strings(warnings)
	return warnings
}

func intersectTargetDocs(left []string, right []string) []string {
	if len(left) == 0 || len(right) == 0 {
		return nil
	}
	index := make(map[string]struct{}, len(left))
	for _, value := range left {
		if value == "" {
			continue
		}
		index[value] = struct{}{}
	}
	shared := make([]string, 0, len(right))
	for _, value := range right {
		if value == "" {
			continue
		}
		if _, ok := index[value]; ok {
			shared = append(shared, value)
		}
	}
	sort.Strings(shared)
	return shared
}
