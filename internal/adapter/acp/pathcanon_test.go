package acp

import (
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/pathcanon"
)

func TestPathWithinWorkspaceRootWindowsExtendedForms(t *testing.T) {
	cases := []struct {
		name, root, target string
		want               bool
	}{
		{"drive descendant", `C:\repo`, `C:\repo\pkg`, true},
		{"drive self", `C:\repo`, `C:\repo`, true},
		{"drive sibling", `C:\repo`, `C:\repo2`, false},
		{"extended drive", `\\?\C:\repo`, `//?/C:/repo/pkg`, true},
		{"extended UNC", `\\?\UNC\server\share`, `//?/UNC/server/share/repo`, true},
		{"UNC outside", `\\server\share`, `\\other\share`, false},
		{"case-insensitive", `C:\Repo`, `c:/repo/pkg`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := pathWithinWorkspaceRoot(tc.root, tc.target); got != tc.want {
				t.Fatalf("pathWithinWorkspaceRoot(%q, %q) = %v, want %v", tc.root, tc.target, got, tc.want)
			}
		})
	}
}

func TestResolveWorkspaceWritePathNativeCWD(t *testing.T) {
	// On the host, a native workspace root must resolve write paths without
	// leaking extended-length or slash-mixed forms into the relative result.
	root := t.TempDir()
	targetAbs, rel, err := resolveWorkspaceWritePath(root, filepath.Join(root, "notes.txt"))
	if err != nil {
		t.Fatalf("resolveWorkspaceWritePath() error = %v", err)
	}
	if rel != "notes.txt" {
		t.Fatalf("resolveWorkspaceWritePath() rel = %q, want notes.txt", rel)
	}
	if pathcanon.Native(targetAbs) != filepath.Clean(filepath.Join(root, "notes.txt")) {
		t.Fatalf("resolveWorkspaceWritePath() targetAbs = %q", targetAbs)
	}
}
