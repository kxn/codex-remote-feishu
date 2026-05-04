package displaypath

import "testing"

func TestFileLabelsUseShortestUniqueSuffixesWithoutTruncatingBasename(t *testing.T) {
	labels := FileLabels([]string{
		"repo/internal/core/orchestrator/service_exec_command_progress_test.go",
		"repo/internal/adapter/feishu/projector_exec_command_progress_test.go",
		"repo/internal/core/orchestrator/service.go",
		"repo/internal/adapter/feishu/service.go",
	})
	if got := labels["repo/internal/core/orchestrator/service_exec_command_progress_test.go"]; got != "service_exec_command_progress_test.go" {
		t.Fatalf("long unique basename label = %q, want %q", got, "service_exec_command_progress_test.go")
	}
	if got := labels["repo/internal/adapter/feishu/projector_exec_command_progress_test.go"]; got != "projector_exec_command_progress_test.go" {
		t.Fatalf("second long unique basename label = %q, want %q", got, "projector_exec_command_progress_test.go")
	}
	if got := labels["repo/internal/core/orchestrator/service.go"]; got != "orchestrator/service.go" {
		t.Fatalf("conflicting basename label = %q, want %q", got, "orchestrator/service.go")
	}
	if got := labels["repo/internal/adapter/feishu/service.go"]; got != "feishu/service.go" {
		t.Fatalf("second conflicting basename label = %q, want %q", got, "feishu/service.go")
	}
}

func TestPathLabelsKeepFrontAndBackSegments(t *testing.T) {
	labels := PathLabels([]string{
		"dl/data/test/aaaa/bbbb/cccc/abcd",
	})
	if got := labels["dl/data/test/aaaa/bbbb/cccc/abcd"]; got != "dl/.../abcd" {
		t.Fatalf("long path compact label = %q, want %q", got, "dl/.../abcd")
	}
}

func TestPathLabelsAlternateAcrossFrontAndBackUntilUnique(t *testing.T) {
	labels := PathLabels([]string{
		"data/dl/alice/repo",
		"data/dl/bob/repo",
	})
	if got := labels["data/dl/alice/repo"]; got != "data/.../alice/repo" {
		t.Fatalf("alice path label = %q, want %q", got, "data/.../alice/repo")
	}
	if got := labels["data/dl/bob/repo"]; got != "data/.../bob/repo" {
		t.Fatalf("bob path label = %q, want %q", got, "data/.../bob/repo")
	}
}

func TestNormalize(t *testing.T) {
	if got := Normalize(`\data\dl\repo\`); got != "data/dl/repo" {
		t.Fatalf("Normalize() = %q, want %q", got, "data/dl/repo")
	}
}
