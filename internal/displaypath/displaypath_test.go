package displaypath

import "testing"

func TestShortestUniqueSuffixes(t *testing.T) {
	labels := ShortestUniqueSuffixes([]string{
		"/data/dl/app/repo",
		"/data/dl/feature/repo",
		"/data/dl/app/service",
	})
	if got := labels["data/dl/app/repo"]; got != "app/repo" {
		t.Fatalf("repo label = %q, want %q", got, "app/repo")
	}
	if got := labels["data/dl/feature/repo"]; got != "feature/repo" {
		t.Fatalf("feature repo label = %q, want %q", got, "feature/repo")
	}
	if got := labels["data/dl/app/service"]; got != "service" {
		t.Fatalf("service label = %q, want %q", got, "service")
	}
}

func TestDisplayLabelFallsBackToClampedPath(t *testing.T) {
	path := "/very/long/path/that/keeps/going/and/going/project"
	got := DisplayLabel(path, nil)
	if got == "" || len(got) > maxLabelLen {
		t.Fatalf("DisplayLabel() = %q, want non-empty clamped label", got)
	}
}

func TestNormalize(t *testing.T) {
	if got := Normalize(`\data\dl\repo\`); got != "data/dl/repo" {
		t.Fatalf("Normalize() = %q, want %q", got, "data/dl/repo")
	}
}
