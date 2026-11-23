package preview

import "testing"

func TestFinalBlockCandidatesMatchVisibleLocalReferences(t *testing.T) {
	for _, tc := range []struct {
		text string
		want bool
	}{
		{"hi", false},
		{"[报告](out/report.md)", true},
		{"See ./out/report.md", true},
		{"Here is `out/report.md`", true},
		{"```text\n./example.md\n```", false},
		{"[remote](https://example.invalid/report.md)", false},
		{"See https://example.invalid/report.md", false},
	} {
		if got := HasFinalBlockCandidates(tc.text); got != tc.want {
			t.Errorf("%q: got %v, want %v", tc.text, got, tc.want)
		}
	}
}
