package orchestrator

import (
	"strings"
	"testing"
)

func TestProfileCatalogCommonMaterializesDefaultAndSortsBuiltInFirst(t *testing.T) {
	defaultRecord := testProfileCatalogRecord{ID: "default", Name: "Default", BuiltIn: true}
	records := materializeProfileCatalogRecords(
		[]testProfileCatalogRecord{
			{ID: "beta", Name: "Beta"},
			{ID: " ", Name: "Ignored"},
			{ID: "alpha", Name: "Alpha"},
		},
		defaultRecord,
		normalizeTestProfileCatalogRecord,
		func(record testProfileCatalogRecord) string { return record.ID },
	)
	got := sortedProfileCatalogRecords(records, normalizeTestProfileCatalogRecord, func(record testProfileCatalogRecord) profileCatalogSortKey {
		return profileCatalogSortKey{BuiltIn: record.BuiltIn, Name: record.Name, ID: record.ID}
	})

	if len(got) != 3 {
		t.Fatalf("len(sorted records) = %d, want 3: %#v", len(got), got)
	}
	for index, wantID := range []string{"default", "alpha", "beta"} {
		if got[index].ID != wantID {
			t.Fatalf("sorted record[%d].ID = %q, want %q: %#v", index, got[index].ID, wantID, got)
		}
	}
	if !got[0].BuiltIn {
		t.Fatalf("default record must stay built-in: %#v", got[0])
	}
}

type testProfileCatalogRecord struct {
	ID      string
	Name    string
	BuiltIn bool
}

func normalizeTestProfileCatalogRecord(record testProfileCatalogRecord) testProfileCatalogRecord {
	record.ID = strings.TrimSpace(record.ID)
	record.Name = strings.TrimSpace(record.Name)
	return record
}
