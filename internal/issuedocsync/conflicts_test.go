package issuedocsync

import (
	"strings"
	"testing"
)

func TestFindRecordConflictsWarnsWhenOlderIssueTargetsDocAfterNewerIssue(t *testing.T) {
	state := StateFile{
		Version: 1,
		Repo:    "kxn/codex-remote-feishu",
		Issues: map[string]IssueRecord{
			"47": {
				Number:     47,
				UpdatedAt:  "2026-04-09T12:00:00Z",
				ClosedAt:   "2026-04-09T11:00:00Z",
				Decision:   "merge",
				TargetDocs: []string{"docs/general/feishu-product-design.md"},
			},
		},
	}

	incoming := IssueRecord{
		Number:     41,
		UpdatedAt:  "2026-04-08T12:00:00Z",
		ClosedAt:   "2026-04-08T11:00:00Z",
		Decision:   "merge",
		TargetDocs: []string{"docs/general/feishu-product-design.md"},
	}

	warnings := FindRecordConflicts(state, incoming)
	if len(warnings) != 1 {
		t.Fatalf("warning count = %d, want 1 (%#v)", len(warnings), warnings)
	}
	if !strings.Contains(warnings[0], "#47") || !strings.Contains(warnings[0], "#41") {
		t.Fatalf("unexpected warning: %q", warnings[0])
	}
}

func TestFindRecordConflictsIgnoresOlderOrDifferentDocEntries(t *testing.T) {
	state := StateFile{
		Version: 1,
		Repo:    "kxn/codex-remote-feishu",
		Issues: map[string]IssueRecord{
			"30": {
				Number:     30,
				UpdatedAt:  "2026-04-07T12:00:00Z",
				ClosedAt:   "2026-04-07T11:00:00Z",
				Decision:   "merge",
				TargetDocs: []string{"docs/general/user-guide.md"},
			},
		},
	}

	incoming := IssueRecord{
		Number:     41,
		UpdatedAt:  "2026-04-08T12:00:00Z",
		ClosedAt:   "2026-04-08T11:00:00Z",
		Decision:   "merge",
		TargetDocs: []string{"docs/general/feishu-product-design.md"},
	}

	warnings := FindRecordConflicts(state, incoming)
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %#v", warnings)
	}
}
