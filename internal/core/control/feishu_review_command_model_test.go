package control

import "testing"

func TestParseFeishuReviewCommandText(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		wantMode  ReviewCommandMode
		wantSHA   string
		wantError bool
	}{
		{name: "bare review opens root", text: "/review", wantMode: ReviewCommandModeRoot},
		{name: "uncommitted", text: "/review uncommitted", wantMode: ReviewCommandModeUncommitted},
		{name: "commit picker", text: "/review commit", wantMode: ReviewCommandModeCommitPicker},
		{name: "commit sha", text: "/review commit AbC1234", wantMode: ReviewCommandModeCommitSHA, wantSHA: "abc1234"},
		{name: "cancel", text: "/review cancel", wantMode: ReviewCommandModeCancel},
		{name: "invalid target", text: "/review branch main", wantError: true},
		{name: "invalid commit sha", text: "/review commit xyz", wantError: true},
		{name: "too many args", text: "/review commit abc1234 extra", wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseFeishuReviewCommandText(tt.text)
			if tt.wantError {
				if err == nil {
					t.Fatalf("ParseFeishuReviewCommandText(%q) unexpectedly succeeded: %#v", tt.text, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseFeishuReviewCommandText(%q) error = %v", tt.text, err)
			}
			if got.Mode != tt.wantMode || got.CommitSHA != tt.wantSHA {
				t.Fatalf("ParseFeishuReviewCommandText(%q) = %#v, want mode=%q sha=%q", tt.text, got, tt.wantMode, tt.wantSHA)
			}
		})
	}
}
